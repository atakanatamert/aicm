// SPDX-License-Identifier: AGPL-3.0-or-later

// Package account owns accounts: their plan and renewal date, the key stored
// for them, their machines and the daily renewal roll-forward.
package account

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// Errors returned by the service.
var (
	ErrNotFound        = errors.New("account not found")
	ErrInvalid         = errors.New("invalid account")
	ErrUnknownProvider = errors.New("unknown provider")
)

// hiddenUserID is the zero uuid used for account_visible when the principal
// has no user id.
const hiddenUserID = "00000000-0000-0000-0000-000000000000"

// Plan is the subscription part of CreateInput and UpdateInput. Fields that
// are pointers may be omitted; Cycle is required when Plan is supplied.
type Plan struct {
	Name        *string
	Price       *money.Amount
	Cycle       string
	RenewsOn    *time.Time
	AutoRenew   *bool
	PromoKind   *string
	PromoEndsOn *time.Time
}

// CreateInput mirrors AccountCreate.
type CreateInput struct {
	ProviderID      string
	Name            string
	AuthKind        *string
	Currency        *string
	Secret          *string
	Config          *map[string]string
	AllowUnofficial *bool
	Plan            *Plan
	Notes           *string
}

// UpdateInput mirrors AccountUpdate. Only fields that are set change.
type UpdateInput struct {
	Name            *string
	Currency        *string
	Config          *map[string]string
	AllowUnofficial *bool
	Plan            *Plan
	Notes           *string
}

// OnRenewal is called for each passed renewal date inside the roll-forward
// transaction. Task 10 uses it to record the subscription fee.
type OnRenewal func(ctx context.Context, q *db.Queries, a db.Account, renewedOn time.Time) error

// Service reads and writes accounts.
type Service struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	secrets   secrets.Store
	providers []provider.Provider
	log       *slog.Logger
}

// NewService returns an account service.
func NewService(pool *pgxpool.Pool, store secrets.Store, providers []provider.Provider, log *slog.Logger) *Service {
	return &Service{pool: pool, q: db.New(pool), secrets: store, providers: providers, log: log}
}

// List returns visible accounts. status "" does not filter; providerID ""
// does not filter.
func (s *Service) List(ctx context.Context, p auth.Principal, status string, includeArchived bool, providerID string) ([]db.Account, error) {
	var statusArg, providerArg *string
	if status != "" {
		statusArg = new(status)
	}
	if providerID != "" {
		providerArg = new(providerID)
	}
	rows, err := s.q.ListAccounts(ctx, db.ListAccountsParams{
		OrgID: p.OrgID, Status: statusArg, IncludeArchived: includeArchived, ProviderID: providerArg,
		UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing accounts: %w", err)
	}
	return rows, nil
}

// Get returns one visible account; hidden accounts are ErrNotFound.
func (s *Service) Get(ctx context.Context, p auth.Principal, id string) (db.Account, error) {
	a, err := s.q.GetAccount(ctx, db.GetAccountParams{
		OrgID: p.OrgID, AccountID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("loading account: %w", err)
	}
	return a, nil
}

// Create adds an account and, when secret is set, its key. A key the provider
// rejects stops the create before anything is stored.
func (s *Service) Create(ctx context.Context, p auth.Principal, in CreateInput) (db.Account, error) {
	prov, ok := s.lookupProvider(in.ProviderID)
	if !ok {
		return db.Account{}, fmt.Errorf("%w: %s", ErrUnknownProvider, in.ProviderID)
	}
	info := prov.Info()

	name := strings.TrimSpace(in.Name)
	if name == "" {
		return db.Account{}, fmt.Errorf("%w: name is required", ErrInvalid)
	}

	authKind := string(provider.AuthNone)
	if len(info.AuthKinds) > 0 {
		authKind = string(info.AuthKinds[0])
	}
	if in.AuthKind != nil {
		authKind = *in.AuthKind
		if !supportedAuthKind(info, authKind) {
			return db.Account{}, fmt.Errorf("%w: %s does not support auth kind %q", ErrInvalid, info.ID, authKind)
		}
	}
	currency := info.Currency
	if in.Currency != nil {
		currency = *in.Currency
	}
	if !money.ValidCurrency(currency) {
		return db.Account{}, fmt.Errorf("%w: invalid currency %q", ErrInvalid, currency)
	}

	config := map[string]string{}
	if in.Config != nil {
		config = copyStrings(*in.Config)
	}
	if err := validateConfig(info, config, true); err != nil {
		return db.Account{}, err
	}
	// Default on for providers whose only data source is undocumented, so a
	// new account syncs without an extra setting (Decision 9, revised).
	allowUnofficial := info.Unofficial
	if in.AllowUnofficial != nil {
		allowUnofficial = *in.AllowUnofficial
	}
	notes := ""
	if in.Notes != nil {
		notes = *in.Notes
	}

	plan, err := normalizePlan(in.Plan, currency, planFields{Cycle: "none", AutoRenew: true})
	if err != nil {
		return db.Account{}, err
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return db.Account{}, fmt.Errorf("encoding account config: %w", err)
	}

	var secretID *string
	var secretInfo secrets.Info
	if in.Secret != nil && *in.Secret != "" {
		secretInfo, err = s.putSecret(ctx, p.OrgID, info, config, allowUnofficial, nil, *in.Secret, true)
		if err != nil {
			return db.Account{}, err
		}
		secretID = new(secretInfo.ID)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Account{}, fmt.Errorf("starting account create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	a, err := q.InsertAccount(ctx, db.InsertAccountParams{
		OrgID:            p.OrgID,
		ProviderID:       in.ProviderID,
		Name:             name,
		AuthKind:         authKind,
		SecretID:         secretID,
		Config:           configJSON,
		AllowUnofficial:  allowUnofficial,
		PlanName:         plan.Name,
		PriceMinor:       priceMinor(plan.Price),
		Currency:         currency,
		BillingCycle:     plan.Cycle,
		RenewsOn:         plan.RenewsOn,
		RenewalAnchorDay: anchorDay(plan.RenewsOn),
		AutoRenew:        plan.AutoRenew,
		PromoKind:        plan.PromoKind,
		PromoEndsOn:      plan.PromoEndsOn,
		Notes:            notes,
	})
	if err != nil {
		return db.Account{}, fmt.Errorf("creating account: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action: "account.create", TargetKind: "account", TargetID: a.ID, After: accountAudit(a),
	}); err != nil {
		return db.Account{}, err
	}
	if secretID != nil {
		if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
			Action: "secret.set", TargetKind: "account", TargetID: a.ID,
			After: map[string]string{"last4": secretInfo.Last4},
		}); err != nil {
			return db.Account{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Account{}, fmt.Errorf("committing account create: %w", err)
	}
	return a, nil
}

// Update changes the fields present in in and returns the updated account.
func (s *Service) Update(ctx context.Context, p auth.Principal, id string, in UpdateInput) (db.Account, error) {
	a, err := s.Get(ctx, p, id)
	if err != nil {
		return a, err
	}

	name := a.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" {
			return a, fmt.Errorf("%w: name is required", ErrInvalid)
		}
	}
	currency := a.Currency
	if in.Currency != nil {
		currency = *in.Currency
		if !money.ValidCurrency(currency) {
			return a, fmt.Errorf("%w: invalid currency %q", ErrInvalid, currency)
		}
	}
	config := configOf(a)
	if in.Config != nil {
		config = copyStrings(*in.Config)
		if prov, ok := s.lookupProvider(a.ProviderID); ok {
			if err := validateConfig(prov.Info(), config, false); err != nil {
				return a, err
			}
		}
	}
	allowUnofficial := a.AllowUnofficial
	if in.AllowUnofficial != nil {
		allowUnofficial = *in.AllowUnofficial
	}
	notes := a.Notes
	if in.Notes != nil {
		notes = *in.Notes
	}

	plan := planOf(a)
	anchor := a.RenewalAnchorDay
	if in.Plan != nil {
		plan, err = normalizePlan(in.Plan, currency, plan)
		if err != nil {
			return a, err
		}
		switch {
		case plan.Cycle == "none":
			anchor = nil
		case in.Plan.RenewsOn != nil:
			anchor = anchorDay(plan.RenewsOn)
		}
	} else if plan.Price != nil && plan.Price.Currency != currency {
		return a, fmt.Errorf("%w: price currency does not match account currency", ErrInvalid)
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return a, fmt.Errorf("encoding account config: %w", err)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return a, fmt.Errorf("starting account update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	updated, err := q.UpdateAccount(ctx, db.UpdateAccountParams{
		Name:             name,
		Currency:         currency,
		Config:           configJSON,
		AllowUnofficial:  allowUnofficial,
		PlanName:         plan.Name,
		PriceMinor:       priceMinor(plan.Price),
		BillingCycle:     plan.Cycle,
		RenewsOn:         plan.RenewsOn,
		RenewalAnchorDay: anchor,
		AutoRenew:        plan.AutoRenew,
		PromoKind:        plan.PromoKind,
		PromoEndsOn:      plan.PromoEndsOn,
		Notes:            notes,
		OrgID:            p.OrgID,
		ID:               id,
		UserID:           userIDOf(p),
		IsAdmin:          p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("updating account: %w", err)
	}

	before, after := changedFields(a, updated)
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action: "account.update", TargetKind: "account", TargetID: id, Before: before, After: after,
	}); err != nil {
		return a, err
	}
	if err := tx.Commit(ctx); err != nil {
		return a, fmt.Errorf("committing account update: %w", err)
	}
	return updated, nil
}

// Archive hides an account, keeping its history.
func (s *Service) Archive(ctx context.Context, p auth.Principal, id string) (db.Account, error) {
	a, err := s.Get(ctx, p, id)
	if err != nil {
		return a, err
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return a, fmt.Errorf("starting account archive: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	archived, err := q.ArchiveAccount(ctx, db.ArchiveAccountParams{
		OrgID: p.OrgID, ID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("archiving account: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action: "account.archive", TargetKind: "account", TargetID: id,
		Before: map[string]any{"archived_at": dateOrNil(a.ArchivedAt)},
		After:  map[string]any{"archived_at": dateOrNil(archived.ArchivedAt)},
	}); err != nil {
		return a, err
	}
	if err := tx.Commit(ctx); err != nil {
		return a, fmt.Errorf("committing account archive: %w", err)
	}
	return archived, nil
}

// SetSecret validates the key with the provider (when validate is true and
// the provider is in the catalog), stores it through secrets.Store (Put the
// first time, Replace after) and links it to the account.
func (s *Service) SetSecret(ctx context.Context, p auth.Principal, id, value string, validate bool) (secrets.Info, error) {
	a, err := s.Get(ctx, p, id)
	if err != nil {
		return secrets.Info{}, err
	}
	var info provider.Info
	if prov, ok := s.lookupProvider(a.ProviderID); ok {
		info = prov.Info()
	}
	stored, err := s.putSecret(ctx, p.OrgID, info, configOf(a), a.AllowUnofficial, a.SecretID, value, validate)
	if err != nil {
		return secrets.Info{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return secrets.Info{}, fmt.Errorf("starting secret link: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if _, err := q.SetAccountSecret(ctx, db.SetAccountSecretParams{
		SecretID: stored.ID, OrgID: p.OrgID, ID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return secrets.Info{}, ErrNotFound
		}
		return secrets.Info{}, fmt.Errorf("linking secret to account: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action: "secret.set", TargetKind: "account", TargetID: id,
		After: map[string]string{"last4": stored.Last4},
	}); err != nil {
		return secrets.Info{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return secrets.Info{}, fmt.Errorf("committing secret link: %w", err)
	}
	return stored, nil
}

// SecretInfo returns the account's secret metadata and whether one is set.
func (s *Service) SecretInfo(ctx context.Context, orgID string, a db.Account) (secrets.Info, bool, error) {
	if a.SecretID == nil {
		return secrets.Info{}, false, nil
	}
	if s.secrets == nil {
		return secrets.Info{}, false, errors.New("secret store is not configured")
	}
	info, err := s.secrets.Info(ctx, orgID, *a.SecretID)
	if errors.Is(err, secrets.ErrNotFound) {
		return secrets.Info{}, false, nil
	}
	if err != nil {
		return secrets.Info{}, false, fmt.Errorf("loading secret info: %w", err)
	}
	return info, true, nil
}

// Machines returns the machines and harnesses that report an account.
func (s *Service) Machines(ctx context.Context, p auth.Principal, id string) ([]db.ListAccountMachinesRow, error) {
	if _, err := s.Get(ctx, p, id); err != nil {
		return nil, err
	}
	rows, err := s.q.ListAccountMachines(ctx, db.ListAccountMachinesParams{
		OrgID: p.OrgID, AccountID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing account machines: %w", err)
	}
	return rows, nil
}

// Renewals returns active accounts renewing in the next withinDays days.
func (s *Service) Renewals(ctx context.Context, p auth.Principal, withinDays int) ([]db.Account, error) {
	today := dayUTC(time.Now())
	rows, err := s.q.ListRenewals(ctx, db.ListRenewalsParams{
		OrgID: p.OrgID, Today: today, Until: today.AddDate(0, 0, withinDays),
		UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing renewals: %w", err)
	}
	return rows, nil
}

// RollForwardDue advances every auto-renewing account (all orgs) whose
// renews_on is before today, one transaction per account.
func (s *Service) RollForwardDue(ctx context.Context, today time.Time, hook OnRenewal) (int, error) {
	today = dayUTC(today)
	due, err := s.q.ListAccountsDueForRenewal(ctx, today)
	if err != nil {
		return 0, fmt.Errorf("listing accounts due for renewal: %w", err)
	}
	advanced := 0
	for _, a := range due {
		anchor := 0
		if a.RenewalAnchorDay != nil {
			anchor = int(*a.RenewalAnchorDay)
		}
		passed, next := RollForward(*a.RenewsOn, today, a.BillingCycle, anchor)
		if len(passed) == 0 {
			continue
		}
		if err := s.advance(ctx, a, passed, next, hook); err != nil {
			return advanced, err
		}
		advanced++
	}
	return advanced, nil
}

// advance moves one account to next and calls hook for every passed date in
// one transaction.
func (s *Service) advance(ctx context.Context, a db.Account, passed []time.Time, next time.Time, hook OnRenewal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting renewal advance for account %s: %w", a.ID, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	for _, renewedOn := range passed {
		if hook == nil {
			continue
		}
		if err := hook(ctx, q, a, renewedOn); err != nil {
			return fmt.Errorf("recording renewal of account %s on %s: %w", a.ID, renewedOn.Format(time.DateOnly), err)
		}
	}
	if err := q.SetAccountRenewsOn(ctx, db.SetAccountRenewsOnParams{
		RenewsOn: next, OrgID: a.OrgID, ID: a.ID,
	}); err != nil {
		return fmt.Errorf("advancing renewal of account %s: %w", a.ID, err)
	}
	p := auth.Principal{OrgID: a.OrgID, Kind: auth.ActorSystem, Label: "renewal job"}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action: "account.renew", TargetKind: "account", TargetID: a.ID,
		Before: map[string]string{"renews_on": a.RenewsOn.Format(time.DateOnly)},
		After:  map[string]string{"renews_on": next.Format(time.DateOnly)},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing renewal advance for account %s: %w", a.ID, err)
	}
	return nil
}

// putSecret validates and stores value, returning safe metadata.
func (s *Service) putSecret(ctx context.Context, orgID string, info provider.Info, config map[string]string, allowUnofficial bool, existingID *string, value string, validate bool) (secrets.Info, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return secrets.Info{}, fmt.Errorf("%w: secret value is required", ErrInvalid)
	}
	if validate && info.ID != "" {
		prov, ok := s.lookupProvider(info.ID)
		if ok {
			c := provider.Credentials{Secret: value, Config: config, AllowUnofficial: allowUnofficial}
			if err := prov.Validate(ctx, c); err != nil {
				return secrets.Info{}, err
			}
		}
	}
	if s.secrets == nil {
		return secrets.Info{}, errors.New("secret store is not configured")
	}
	if existingID != nil {
		stored, err := s.secrets.Replace(ctx, orgID, *existingID, value)
		if err != nil {
			return secrets.Info{}, fmt.Errorf("replacing secret: %w", err)
		}
		return stored, nil
	}
	stored, err := s.secrets.Put(ctx, orgID, value)
	if err != nil {
		return secrets.Info{}, fmt.Errorf("storing secret: %w", err)
	}
	return stored, nil
}

// lookupProvider finds a provider in the catalog.
func (s *Service) lookupProvider(id string) (provider.Provider, bool) {
	for _, p := range s.providers {
		if p.Info().ID == id {
			return p, true
		}
	}
	return nil, false
}

// planFields is the normalized subscription stored on an account.
type planFields struct {
	Name        string
	Price       *money.Amount
	Cycle       string
	RenewsOn    *time.Time
	AutoRenew   bool
	PromoKind   *string
	PromoEndsOn *time.Time
}

// normalizePlan validates in and merges it onto current. A nil in keeps the
// current plan; none of the pointers are retained.
func normalizePlan(in *Plan, currency string, current planFields) (planFields, error) {
	if in == nil {
		return current, nil
	}
	plan := current
	plan.Cycle = in.Cycle
	switch plan.Cycle {
	case "none", "weekly", "monthly", "yearly":
	default:
		return planFields{}, fmt.Errorf("%w: unknown billing cycle %q", ErrInvalid, plan.Cycle)
	}
	if in.Name != nil {
		plan.Name = *in.Name
	}
	if in.AutoRenew != nil {
		plan.AutoRenew = *in.AutoRenew
	}

	if plan.Cycle == "none" {
		if in.Price != nil {
			return planFields{}, fmt.Errorf("%w: price must be absent for a plan with no cycle", ErrInvalid)
		}
		plan.Price = nil
		plan.RenewsOn = nil
	} else {
		if in.Price == nil {
			return planFields{}, fmt.Errorf("%w: price is required for a %s plan", ErrInvalid, plan.Cycle)
		}
		if in.Price.Minor < 0 {
			return planFields{}, fmt.Errorf("%w: price must not be negative", ErrInvalid)
		}
		if in.Price.Currency != currency {
			return planFields{}, fmt.Errorf("%w: price currency %s does not match account currency %s", ErrInvalid, in.Price.Currency, currency)
		}
		price := *in.Price
		plan.Price = &price
		if in.RenewsOn != nil {
			renews := dayUTC(*in.RenewsOn)
			plan.RenewsOn = &renews
		}
	}

	switch {
	case in.PromoKind == nil && in.PromoEndsOn == nil:
		// Keep the current promo.
	case in.PromoKind != nil && in.PromoEndsOn != nil:
		switch *in.PromoKind {
		case "trial", "discount":
		default:
			return planFields{}, fmt.Errorf("%w: unknown promo kind %q", ErrInvalid, *in.PromoKind)
		}
		kind := *in.PromoKind
		ends := dayUTC(*in.PromoEndsOn)
		plan.PromoKind = &kind
		plan.PromoEndsOn = &ends
	default:
		return planFields{}, fmt.Errorf("%w: promo kind and promo end date must be set together", ErrInvalid)
	}
	return plan, nil
}

// planOf reads an account's stored subscription.
func planOf(a db.Account) planFields {
	p := planFields{
		Name:      a.PlanName,
		Cycle:     a.BillingCycle,
		AutoRenew: a.AutoRenew,
		PromoKind: a.PromoKind,
		PromoEndsOn: func() *time.Time {
			if a.PromoEndsOn == nil {
				return nil
			}
			return ptrDate(*a.PromoEndsOn)
		}(),
	}
	if a.PriceMinor != nil {
		p.Price = &money.Amount{Minor: *a.PriceMinor, Currency: a.Currency}
	}
	if a.RenewsOn != nil {
		p.RenewsOn = ptrDate(*a.RenewsOn)
	}
	return p
}

// validateConfig checks config keys and, on create, required fields.
func validateConfig(info provider.Info, config map[string]string, requireAll bool) error {
	fields := make(map[string]provider.ConfigField, len(info.ConfigFields))
	for _, f := range info.ConfigFields {
		fields[f.Key] = f
	}
	for key := range config {
		if _, ok := fields[key]; !ok {
			return fmt.Errorf("%w: %s has no config field %q", ErrInvalid, info.ID, key)
		}
	}
	if requireAll {
		for _, f := range info.ConfigFields {
			if f.Required && config[f.Key] == "" {
				return fmt.Errorf("%w: config field %q is required", ErrInvalid, f.Key)
			}
		}
	}
	return nil
}

// supportedAuthKind reports whether the provider lists kind.
func supportedAuthKind(info provider.Info, kind string) bool {
	for _, k := range info.AuthKinds {
		if string(k) == kind {
			return true
		}
	}
	return false
}

// changedFields returns the before and after maps of only the changed fields.
func changedFields(before, after db.Account) (map[string]any, map[string]any) {
	b, a := map[string]any{}, map[string]any{}
	if before.Name != after.Name {
		b["name"], a["name"] = before.Name, after.Name
	}
	if before.Currency != after.Currency {
		b["currency"], a["currency"] = before.Currency, after.Currency
	}
	if !reflect.DeepEqual(configOf(before), configOf(after)) {
		b["config"], a["config"] = configOf(before), configOf(after)
	}
	if before.AllowUnofficial != after.AllowUnofficial {
		b["allow_unofficial"], a["allow_unofficial"] = before.AllowUnofficial, after.AllowUnofficial
	}
	if before.Notes != after.Notes {
		b["notes"], a["notes"] = before.Notes, after.Notes
	}
	if !reflect.DeepEqual(planFieldsAudit(before), planFieldsAudit(after)) {
		b["plan"], a["plan"] = planFieldsAudit(before), planFieldsAudit(after)
	}
	return b, a
}

// accountAudit is the account shape written for a create.
func accountAudit(a db.Account) map[string]any {
	return map[string]any{
		"provider_id":      a.ProviderID,
		"name":             a.Name,
		"status":           a.Status,
		"auth_kind":        a.AuthKind,
		"currency":         a.Currency,
		"config":           configOf(a),
		"allow_unofficial": a.AllowUnofficial,
		"plan":             planFieldsAudit(a),
		"notes":            a.Notes,
	}
}

// planFieldsAudit turns the stored plan into an audit-safe map.
func planFieldsAudit(a db.Account) map[string]any {
	p := planOf(a)
	out := map[string]any{
		"name":       p.Name,
		"cycle":      p.Cycle,
		"auto_renew": p.AutoRenew,
	}
	if p.Price != nil {
		out["price"] = map[string]any{"amount_minor": p.Price.Minor, "currency": p.Price.Currency}
	}
	if p.RenewsOn != nil {
		out["renews_on"] = p.RenewsOn.Format(time.DateOnly)
	}
	if p.PromoKind != nil {
		out["promo_kind"] = *p.PromoKind
	}
	if p.PromoEndsOn != nil {
		out["promo_ends_on"] = p.PromoEndsOn.Format(time.DateOnly)
	}
	return out
}

// configOf decodes an account's config jsonb. A malformed value reads as an
// empty map; the API only writes objects here.
func configOf(a db.Account) map[string]string {
	config := map[string]string{}
	if len(a.Config) == 0 {
		return config
	}
	_ = json.Unmarshal(a.Config, &config)
	return config
}

func copyStrings(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func priceMinor(p *money.Amount) *int64 {
	if p == nil {
		return nil
	}
	return new(p.Minor)
}

func anchorDay(renewsOn *time.Time) *int16 {
	if renewsOn == nil {
		return nil
	}
	return new(int16(renewsOn.Day())) //nolint:gosec // a day of month fits in int16
}

func ptrDate(t time.Time) *time.Time {
	d := dayUTC(t)
	return &d
}

func userIDOf(p auth.Principal) string {
	if p.UserID == "" {
		return hiddenUserID
	}
	return p.UserID
}

func dateOrNil(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}
