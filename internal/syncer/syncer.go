// SPDX-License-Identifier: AGPL-3.0-or-later

// Package syncer runs provider adapters for accounts the hub polls: one
// River job per account sync, plus a dispatcher that queues accounts whose
// minimum poll interval has passed. It calls only the fetchers a provider
// implements, stores what comes back through the owning services, and keeps
// accounts.last_synced_at / last_sync_error current for the UI and alerts.
package syncer

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/riverqueue/river"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/quota"
	"github.com/atakanatamert/aicm/internal/secrets"
	"github.com/atakanatamert/aicm/internal/usage"
)

// DefaultPollInterval applies when a provider declares none.
const DefaultPollInterval = time.Hour

// ChargeSink stores charges an adapter fetched (invoices, top-ups),
// idempotently by external id.
type ChargeSink interface {
	RecordAPICharges(ctx context.Context, orgID, accountID string, cs []provider.Charge) error
}

// Errors returned by Check.
var (
	ErrNotFound      = errors.New("account not found")
	ErrNothingToSync = errors.New("this account has nothing the hub can fetch")
)

// TooSoonError means the account synced less than the provider's minimum
// poll interval ago.
type TooSoonError struct{ RetryAfter time.Duration }

func (e *TooSoonError) Error() string {
	return fmt.Sprintf("synced recently; retry in %s", e.RetryAfter)
}

// Service syncs accounts.
type Service struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	log       *slog.Logger
	providers map[string]provider.Provider
	secrets   secrets.Store
	quota     *quota.Service
	usage     *usage.Service
	charges   ChargeSink
	now       func() time.Time
}

// NewService builds the sync engine. charges may be nil; charge fetchers
// are then not called.
func NewService(pool *pgxpool.Pool, log *slog.Logger, ps []provider.Provider, store secrets.Store, q *quota.Service, u *usage.Service, charges ChargeSink) *Service {
	byID := make(map[string]provider.Provider, len(ps))
	for _, p := range ps {
		byID[p.Info().ID] = p
	}
	return &Service{pool: pool, q: db.New(pool), log: log, providers: byID, secrets: store, quota: q, usage: u, charges: charges, now: time.Now}
}

func pollInterval(info provider.Info) time.Duration {
	if info.MinPollInterval > 0 {
		return info.MinPollInterval
	}
	return DefaultPollInterval
}

// syncable reports whether the hub can fetch anything for the provider.
func (s *Service) syncable(p provider.Provider) bool {
	u, q, c := provider.Capabilities(p)
	return u || q || (c && s.charges != nil)
}

// Request queues a manual sync for an account the principal may see, after
// checking that its provider fetches something and the poll interval has
// passed. It writes one audit row.
func (s *Service) Request(ctx context.Context, p auth.Principal, accountID string, client *river.Client[pgx.Tx]) error {
	userID := p.UserID
	if userID == "" {
		userID = "00000000-0000-0000-0000-000000000000"
	}
	a, err := s.q.GetVisibleAccount(ctx, db.GetVisibleAccountParams{OrgID: p.OrgID, AccountID: accountID, UserID: userID, IsAdmin: p.IsAdmin()})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("loading account: %w", err)
	}
	prov, ok := s.providers[a.ProviderID]
	if !ok || !s.syncable(prov) || a.SecretID == nil || (a.AuthKind != string(provider.AuthAPIKey) && a.AuthKind != string(provider.AuthAdminKey)) {
		return ErrNothingToSync
	}
	if a.LastSyncedAt != nil {
		if wait := pollInterval(prov.Info()) - s.now().Sub(*a.LastSyncedAt); wait > 0 {
			return &TooSoonError{RetryAfter: wait.Round(time.Second)}
		}
	}
	if _, err := client.Insert(ctx, SyncArgs{OrgID: p.OrgID, AccountID: a.ID}, nil); err != nil {
		return fmt.Errorf("queueing sync: %w", err)
	}
	if err := audit.Write(ctx, s.q, p.Actor(), audit.Entry{Action: "account.sync", TargetKind: "account", TargetID: a.ID}); err != nil {
		return err //nolint:wrapcheck // audit.Write wraps
	}
	return nil
}

// Due returns the accounts (all orgs) whose poll interval has passed.
func (s *Service) Due(ctx context.Context) ([]SyncArgs, error) {
	rows, err := s.q.ListSyncCandidates(ctx)
	if err != nil {
		return nil, fmt.Errorf("listing sync candidates: %w", err)
	}
	now := s.now()
	var out []SyncArgs
	for _, r := range rows {
		p, ok := s.providers[r.ProviderID]
		if !ok || !s.syncable(p) {
			continue
		}
		if r.LastSyncedAt != nil && now.Sub(*r.LastSyncedAt) < pollInterval(p.Info()) {
			continue
		}
		out = append(out, SyncArgs{OrgID: r.OrgID, AccountID: r.ID})
	}
	return out, nil
}

// errPermanent marks failures retrying won't fix (bad key, no opt-in).
type errPermanent struct{ err error }

func (e errPermanent) Error() string { return e.err.Error() }
func (e errPermanent) Unwrap() error { return e.err }

// Sync fetches everything the account's provider offers and stores it.
func (s *Service) Sync(ctx context.Context, orgID, accountID string) error {
	a, err := s.q.GetAccountForSync(ctx, db.GetAccountForSyncParams{OrgID: orgID, ID: accountID})
	if errors.Is(err, pgx.ErrNoRows) {
		return errPermanent{ErrNotFound}
	}
	if err != nil {
		return fmt.Errorf("loading account: %w", err)
	}
	p, ok := s.providers[a.ProviderID]
	if !ok || a.SecretID == nil || a.Status != "active" || a.ArchivedAt != nil {
		return errPermanent{ErrNothingToSync}
	}
	info := p.Info()
	if info.Unofficial && !a.AllowUnofficial {
		return s.fail(ctx, a, errPermanent{provider.ErrUnofficialNotAllowed})
	}
	secret, err := s.secrets.Reveal(ctx, orgID, *a.SecretID)
	if err != nil {
		return fmt.Errorf("revealing account key: %w", err)
	}
	cfg := map[string]string{}
	if len(a.Config) > 0 {
		if err := json.Unmarshal(a.Config, &cfg); err != nil {
			return s.fail(ctx, a, errPermanent{fmt.Errorf("reading account config: %w", err)})
		}
	}
	creds := provider.Credentials{Secret: secret, Config: cfg, AllowUnofficial: a.AllowUnofficial}
	now := s.now().UTC()

	if f, ok := p.(provider.QuotaFetcher); ok {
		readings, err := f.FetchQuota(ctx, creds)
		if err != nil {
			return s.fail(ctx, a, classify(err))
		}
		byKey := map[string][]meter.Reading{}
		for _, r := range readings {
			byKey[r.Key] = append(byKey[r.Key], r)
		}
		for _, key := range slices.Sorted(maps.Keys(byKey)) {
			def, ok := info.Meter(key)
			if !ok {
				s.log.WarnContext(ctx, "adapter returned an undeclared meter", "provider", info.ID, "meter", key)
				continue
			}
			if _, err := s.quota.RecordReadings(ctx, orgID, a.ID, def, byKey[key], nil); err != nil {
				return s.fail(ctx, a, err)
			}
		}
	}

	if f, ok := p.(provider.UsageFetcher); ok {
		to := day(now).AddDate(0, 0, 1)
		from := to.AddDate(0, 0, -31)
		if a.LastSyncedAt != nil {
			if since := day(*a.LastSyncedAt).AddDate(0, 0, -2); since.After(from) {
				from = since
			}
		}
		recs, err := f.FetchUsage(ctx, creds, from, to)
		if err != nil {
			return s.fail(ctx, a, classify(err))
		}
		if err := s.usage.ReplaceAPIUsage(ctx, orgID, a.ID, from, to, recs); err != nil {
			return s.fail(ctx, a, err)
		}
	}

	if f, ok := p.(provider.ChargeFetcher); ok && s.charges != nil {
		since := day(now).AddDate(0, 0, -400)
		if a.LastSyncedAt != nil {
			since = day(*a.LastSyncedAt).AddDate(0, 0, -7)
		}
		cs, err := f.FetchCharges(ctx, creds, since)
		if err != nil {
			return s.fail(ctx, a, classify(err))
		}
		if err := s.charges.RecordAPICharges(ctx, orgID, a.ID, cs); err != nil {
			return s.fail(ctx, a, err)
		}
	}

	if err := s.q.MarkSyncOK(ctx, db.MarkSyncOKParams{OrgID: orgID, ID: a.ID}); err != nil {
		return fmt.Errorf("marking sync ok: %w", err)
	}
	return nil
}

// classify turns adapter errors that retrying can't fix into permanent ones.
func classify(err error) error {
	if errors.Is(err, provider.ErrInvalidCredentials) || errors.Is(err, provider.ErrUnofficialNotAllowed) {
		return errPermanent{err}
	}
	return err
}

// fail records the error on the account (rate limits excepted: they are
// expected and only delay the job) and returns it.
func (s *Service) fail(ctx context.Context, a db.Account, err error) error {
	var rl *provider.RateLimitError
	if errors.As(err, &rl) {
		return err
	}
	if mErr := s.q.MarkSyncFailed(ctx, db.MarkSyncFailedParams{OrgID: a.OrgID, ID: a.ID, ErrorText: truncate(err.Error(), 500)}); mErr != nil {
		s.log.ErrorContext(ctx, "recording sync failure", "account_id", a.ID, "err", mErr)
	}
	s.log.WarnContext(ctx, "account sync failed", "account_id", a.ID, "provider", a.ProviderID, "err", err)
	return err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func day(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// SyncArgs is one account sync job.
type SyncArgs struct {
	OrgID     string `json:"org_id"`
	AccountID string `json:"account_id"`
}

// Kind implements river.JobArgs.
func (SyncArgs) Kind() string { return "account_sync" }

// InsertOpts keeps at most one queued sync per account.
func (SyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{MaxAttempts: 5, UniqueOpts: river.UniqueOpts{ByArgs: true}}
}

// SyncWorker runs SyncArgs.
type SyncWorker struct {
	river.WorkerDefaults[SyncArgs]
	Service *Service
}

// Work syncs one account. Rate limits snooze the job; permanent failures
// cancel it.
func (w *SyncWorker) Work(ctx context.Context, job *river.Job[SyncArgs]) error {
	err := w.Service.Sync(ctx, job.Args.OrgID, job.Args.AccountID)
	var rl *provider.RateLimitError
	var perm errPermanent
	switch {
	case err == nil:
		return nil
	case errors.As(err, &rl):
		return river.JobSnooze(max(rl.RetryAfter, time.Minute))
	case errors.As(err, &perm):
		return river.JobCancel(err)
	default:
		return err
	}
}

// DispatchArgs queues due account syncs.
type DispatchArgs struct{}

// Kind implements river.JobArgs.
func (DispatchArgs) Kind() string { return "account_sync_dispatch" }

// DispatchWorker queues a SyncArgs job for every due account.
type DispatchWorker struct {
	river.WorkerDefaults[DispatchArgs]
	Service *Service
}

// Work inserts one sync job per due account.
func (w *DispatchWorker) Work(ctx context.Context, _ *river.Job[DispatchArgs]) error {
	due, err := w.Service.Due(ctx)
	if err != nil {
		return err
	}
	client, err := river.ClientFromContextSafely[pgx.Tx](ctx)
	if err != nil {
		return fmt.Errorf("getting river client: %w", err)
	}
	for _, a := range due {
		if _, err := client.Insert(ctx, a, nil); err != nil {
			return fmt.Errorf("queueing sync for %s: %w", a.AccountID, err)
		}
	}
	return nil
}

// PeriodicJob runs the dispatcher every five minutes.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(river.PeriodicInterval(5*time.Minute),
		func() (river.JobArgs, *river.InsertOpts) { return DispatchArgs{}, nil },
		&river.PeriodicJobOpts{RunOnStart: true})
}
