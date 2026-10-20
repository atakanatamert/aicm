// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// toAccount maps one stored account to the API shape. The secret value is
// never part of it.
func toAccount(a db.Account, info provider.Info, secret secrets.Info, hasSecret bool) Account {
	config := map[string]string{}
	if len(a.Config) > 0 {
		_ = json.Unmarshal(a.Config, &config)
	}
	secretInfo := SecretInfo{Set: false}
	if hasSecret {
		secretInfo.Set = true
		secretInfo.Last4 = new(secret.Last4)
		secretInfo.UpdatedAt = new(secret.UpdatedAt)
	}
	return Account{
		Id:              a.ID,
		ProviderId:      a.ProviderID,
		Name:            a.Name,
		Status:          AccountStatus(a.Status),
		ExternalId:      a.ExternalID,
		AuthKind:        AuthKind(a.AuthKind),
		Currency:        a.Currency,
		Secret:          secretInfo,
		Config:          config,
		AllowUnofficial: a.AllowUnofficial,
		Plan:            toPlan(a),
		Notes:           a.Notes,
		Sync: SyncState{
			LastSyncedAt: a.LastSyncedAt,
			LastError:    optString(a.LastSyncError),
			FailingSince: a.SyncFailingSince,
		},
		Links:      toLinks(info.Links),
		ArchivedAt: a.ArchivedAt,
		CreatedAt:  a.CreatedAt,
		UpdatedAt:  a.UpdatedAt,
	}
}

// errUnofficialNotAllowed: the key can only be checked through an endpoint the
// provider does not document, and the account has not opted in.
var errUnofficialNotAllowed = &APIError{Status: http.StatusConflict, Code: "unofficial_not_allowed",
	Message: `this provider's usage endpoint is unofficial; turn on "Allow unofficial quota endpoints" in the account's plan settings first`}

func accountError(err error) error {
	switch {
	case errors.Is(err, account.ErrNotFound):
		return NotFound("account not found")
	case errors.Is(err, account.ErrUnknownProvider):
		return BadRequest(err.Error())
	case errors.Is(err, account.ErrInvalid):
		return BadRequest(err.Error())
	case errors.Is(err, provider.ErrInvalidCredentials):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "invalid_credentials", Message: "the provider rejected this key"}
	case errors.Is(err, provider.ErrUnofficialNotAllowed):
		return errUnofficialNotAllowed
	}
	return err
}

// providerInfo returns the catalog entry, or an empty one for a provider
// that is no longer compiled in.
func (s *Server) providerInfo(id string) provider.Info {
	for _, p := range s.Providers {
		if p.Info().ID == id {
			return p.Info()
		}
	}
	return provider.Info{ID: id, Name: id}
}

// accountResponse builds one API account, including its secret metadata.
func (s *Server) accountResponse(ctx context.Context, p auth.Principal, a db.Account) (Account, error) {
	secretsInfo, hasSecret, err := s.Accounts.SecretInfo(ctx, p.OrgID, a)
	if err != nil {
		return Account{}, err
	}
	return toAccount(a, s.providerInfo(a.ProviderID), secretsInfo, hasSecret), nil
}

// ListAccounts returns visible accounts, active by default.
func (s *Server) ListAccounts(ctx context.Context, req ListAccountsRequestObject) (ListAccountsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	status := "active"
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	includeArchived := req.Params.IncludeArchived != nil && *req.Params.IncludeArchived
	providerID := ""
	if req.Params.ProviderId != nil {
		providerID = *req.Params.ProviderId
	}
	accounts, err := s.Accounts.List(ctx, p, status, includeArchived, providerID)
	if err != nil {
		return nil, accountError(err)
	}
	out := make(ListAccounts200JSONResponse, 0, len(accounts))
	for _, a := range accounts {
		item, err := s.accountResponse(ctx, p, a)
		if err != nil {
			return nil, accountError(err)
		}
		out = append(out, item)
	}
	return out, nil
}

// CreateAccount adds an account by hand.
func (s *Server) CreateAccount(ctx context.Context, req CreateAccountRequestObject) (CreateAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	created, err := s.Accounts.Create(ctx, p, createInput(*req.Body))
	if err != nil {
		return nil, accountError(err)
	}
	resp, err := s.accountResponse(ctx, p, created)
	if err != nil {
		return nil, accountError(err)
	}
	return CreateAccount201JSONResponse(resp), nil
}

// GetAccount returns one account.
func (s *Server) GetAccount(ctx context.Context, req GetAccountRequestObject) (GetAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.Accounts.Get(ctx, p, req.AccountId)
	if err != nil {
		return nil, accountError(err)
	}
	resp, err := s.accountResponse(ctx, p, a)
	if err != nil {
		return nil, accountError(err)
	}
	return GetAccount200JSONResponse(resp), nil
}

// UpdateAccount changes the fields present in the request.
func (s *Server) UpdateAccount(ctx context.Context, req UpdateAccountRequestObject) (UpdateAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	updated, err := s.Accounts.Update(ctx, p, req.AccountId, updateInput(*req.Body))
	if err != nil {
		return nil, accountError(err)
	}
	resp, err := s.accountResponse(ctx, p, updated)
	if err != nil {
		return nil, accountError(err)
	}
	return UpdateAccount200JSONResponse(resp), nil
}

// ArchiveAccount hides an account after an explicit confirmation.
func (s *Server) ArchiveAccount(ctx context.Context, req ArchiveAccountRequestObject) (ArchiveAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil || !bool(req.Body.Confirm) {
		return nil, BadRequest("confirm must be true")
	}
	archived, err := s.Accounts.Archive(ctx, p, req.AccountId)
	if err != nil {
		return nil, accountError(err)
	}
	resp, err := s.accountResponse(ctx, p, archived)
	if err != nil {
		return nil, accountError(err)
	}
	return ArchiveAccount200JSONResponse(resp), nil
}

// SetAccountSecret stores or replaces an account key. Write-only.
func (s *Server) SetAccountSecret(ctx context.Context, req SetAccountSecretRequestObject) (SetAccountSecretResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	if req.Body.Value == "" {
		return nil, BadRequest("value is required")
	}
	validate := true
	if req.Body.Validate != nil {
		validate = *req.Body.Validate
	}
	stored, err := s.Accounts.SetSecret(ctx, p, req.AccountId, req.Body.Value, validate)
	if err != nil {
		return nil, accountError(err)
	}
	return SetAccountSecret200JSONResponse(secretInfo(stored)), nil
}

// ListAccountMachines returns where an account was seen.
func (s *Server) ListAccountMachines(ctx context.Context, req ListAccountMachinesRequestObject) (ListAccountMachinesResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Accounts.Machines(ctx, p, req.AccountId)
	if err != nil {
		return nil, accountError(err)
	}
	out := make(ListAccountMachines200JSONResponse, len(rows))
	for i, r := range rows {
		out[i] = AccountMachine{
			MachineId:   r.MachineID,
			MachineName: r.MachineName,
			Harness:     r.Harness,
			FirstSeenAt: r.FirstSeenAt,
			LastSeenAt:  r.LastSeenAt,
		}
	}
	return out, nil
}

// ListProviders returns the compiled-in provider catalog.
func (s *Server) ListProviders(ctx context.Context, _ ListProvidersRequestObject) (ListProvidersResponseObject, error) {
	if _, err := requirePrincipal(ctx); err != nil {
		return nil, err
	}
	out := make(ListProviders200JSONResponse, 0, len(s.Providers))
	for _, p := range s.Providers {
		out = append(out, toProviderInfo(p))
	}
	return out, nil
}

// ListRenewals returns subscriptions renewing within N days.
func (s *Server) ListRenewals(ctx context.Context, req ListRenewalsRequestObject) (ListRenewalsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	withinDays := 30
	if req.Params.WithinDays != nil {
		withinDays = *req.Params.WithinDays
	}
	if withinDays < 1 || withinDays > 400 {
		return nil, BadRequest("within_days must be between 1 and 400")
	}
	accounts, err := s.Accounts.Renewals(ctx, p, withinDays)
	if err != nil {
		return nil, accountError(err)
	}
	_, _, settings, err := s.Auth.Me(ctx, p)
	if err != nil {
		return nil, mapAuthError(err)
	}
	today := utcDay(time.Now())
	out := make(ListRenewals200JSONResponse, 0, len(accounts))
	for _, a := range accounts {
		r := Renewal{
			Account:   toAccountSummary(a, s.providerInfo(a.ProviderID)),
			RenewsOn:  openapi_types.Date{Time: *a.RenewsOn},
			DaysLeft:  int(a.RenewsOn.Sub(today) / (24 * time.Hour)),
			AutoRenew: a.AutoRenew,
			PromoKind: promoKind(a.PromoKind),
			PromoEndsOn: func() *openapi_types.Date {
				if a.PromoEndsOn == nil {
					return nil
				}
				return &openapi_types.Date{Time: *a.PromoEndsOn}
			}(),
		}
		if a.PriceMinor != nil {
			price := money.Amount{Minor: *a.PriceMinor, Currency: a.Currency}
			r.Price = &Money{AmountMinor: price.Minor, Currency: price.Currency}
			if s.FX != nil {
				converted, rateDate, err := s.FX.Convert(ctx, price, settings.DisplayCurrency, today, settings.FxSource)
				switch {
				case err == nil:
					r.Converted = &ConvertedMoney{
						AmountMinor: converted.Minor,
						Currency:    converted.Currency,
						RateDate:    openapi_types.Date{Time: rateDate},
					}
				case !errors.Is(err, fx.ErrNoRate):
					return nil, err
				}
			}
		}
		out = append(out, r)
	}
	return out, nil
}

// createInput maps the request body to the service input.
func createInput(b AccountCreate) account.CreateInput {
	in := account.CreateInput{
		ProviderID:      b.ProviderId,
		Name:            b.Name,
		Secret:          b.Secret,
		Config:          b.Config,
		AllowUnofficial: b.AllowUnofficial,
		Plan:            planFromWrite(b.Plan),
		Notes:           b.Notes,
	}
	if b.AuthKind != nil {
		in.AuthKind = new(string(*b.AuthKind))
	}
	if b.Currency != nil {
		in.Currency = b.Currency
	}
	return in
}

// updateInput maps the request body to the service input.
func updateInput(b AccountUpdate) account.UpdateInput {
	return account.UpdateInput{
		Name:            b.Name,
		Currency:        b.Currency,
		Config:          b.Config,
		AllowUnofficial: b.AllowUnofficial,
		Plan:            planFromWrite(b.Plan),
		Notes:           b.Notes,
	}
}

// planFromWrite maps the API plan body to the service plan.
func planFromWrite(pw *PlanWrite) *account.Plan {
	if pw == nil {
		return nil
	}
	out := &account.Plan{
		Name:        pw.Name,
		Cycle:       string(pw.Cycle),
		AutoRenew:   pw.AutoRenew,
		PromoKind:   stringPtr(pw.PromoKind),
		PromoEndsOn: ptrDate(pw.PromoEndsOn),
	}
	if pw.RenewsOn != nil {
		out.RenewsOn = &pw.RenewsOn.Time
	}
	if pw.Price != nil {
		out.Price = &money.Amount{Minor: pw.Price.AmountMinor, Currency: pw.Price.Currency}
	}
	return out
}

// toProviderInfo maps one catalog entry to the API shape.
func toProviderInfo(p provider.Provider) ProviderInfo {
	info := p.Info()
	out := ProviderInfo{
		Id:           info.ID,
		Name:         info.Name,
		Currency:     info.Currency,
		AuthKinds:    make([]AuthKind, len(info.AuthKinds)),
		Links:        toLinks(info.Links),
		Meters:       make([]MeterDef, len(info.Meters)),
		Unofficial:   info.Unofficial,
		ConfigFields: make([]ConfigField, len(info.ConfigFields)),
	}
	for i, k := range info.AuthKinds {
		out.AuthKinds[i] = AuthKind(k)
	}
	for i, m := range info.Meters {
		out.Meters[i] = toMeterDef(m)
	}
	for i, f := range info.ConfigFields {
		field := ConfigField{Key: f.Key, Label: f.Label, Required: f.Required}
		if f.Help != "" {
			field.Help = new(f.Help)
		}
		if len(f.Options) > 0 {
			options := append([]string(nil), f.Options...)
			field.Options = &options
		}
		out.ConfigFields[i] = field
	}
	out.Capabilities.Usage, out.Capabilities.Quota, out.Capabilities.Charges = provider.Capabilities(p)
	if info.MinPollInterval > 0 {
		seconds := int(info.MinPollInterval / time.Second)
		out.MinPollSeconds = &seconds
	}
	return out
}

// secretInfo maps safe secret metadata to the API shape.
func secretInfo(i secrets.Info) SecretInfo {
	return SecretInfo{Set: true, Last4: new(i.Last4), UpdatedAt: new(i.UpdatedAt)}
}

func stringPtr[T ~string](p *T) *string {
	if p == nil {
		return nil
	}
	return new(string(*p))
}

func ptrDate(d *openapi_types.Date) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time
	return &t
}

func promoKind(k *string) *PromoKind {
	if k == nil {
		return nil
	}
	return new(PromoKind(*k))
}
