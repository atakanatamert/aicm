# 09 – Accounts: CRUD, plans, secrets, provider catalog, renewals and roll-forward

**Writer:** DeepSeek Flash
**Depends on:** 06, 07, 11
**License of touched packages:** AGPL-3.0-or-later

## Goal

Users add, edit and archive accounts with their plan, price and renewal date; store an account's key (validated, encrypted, write-only); see the provider catalog; and see upcoming renewals with the price converted to the display currency. A daily job rolls passed renewal dates forward by the billing cycle without drifting off the original day of month.

Not in this task: confirming or dismissing agent-discovered accounts (task 18), sync now (sync engine task), subscription fee charges (task 10, which plugs into the roll-forward hook below).

## Files to touch

- `migrations/00003_renewal_anchor.sql` (new)
- `internal/account/account.go` (new): service
- `internal/account/renewal.go` (new): `NextRenewal`, roll-forward
- `internal/account/renewal_test.go` (new)
- `internal/account/job.go` (new): River worker + periodic job
- `internal/db/queries/accounts.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/accounts.go` (new)
- `internal/httpapi/unimplemented.go` (edit: delete the methods you implement)
- `internal/httpapi/httpapi.go` (edit: `Deps` fields `Accounts *account.Service`, `FX *fx.Service`, `Providers []provider.Provider`)
- `cmd/aicm/main.go` (edit: build the service, register the job, pass `Deps`)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `migrations/00001_init.sql`: `accounts` (note the CHECK constraints), `account_machines`; `migrations/00002_account_visible.sql`
- `api/openapi.yaml`: `listAccounts`, `createAccount`, `getAccount`, `updateAccount`, `archiveAccount`, `setAccountSecret`, `listAccountMachines`, `listProviders`, `listRenewals`; schemas `Account`, `AccountCreate`, `AccountUpdate`, `Plan`, `PlanWrite`, `ProviderInfo`, `Renewal`
- `internal/provider/provider.go` (`Info`, `Provider.Validate`, `Credentials`, `Capabilities`, `ErrInvalidCredentials`), `internal/provider/registry/registry.go`
- `internal/secrets/secrets.go` (`Store.Put`, `Replace`, `Info`)
- `internal/fx/fx.go` (`Convert`)
- `internal/audit/audit.go`
- `internal/httpapi/summary.go` (`requirePrincipal`, `toAccountSummary`, `toPlan`, `toLinks`) and `internal/httpapi/quota.go` for handler style
- `internal/jobs/jobs.go`, `internal/fx/job.go` for a River job example
- `internal/dbtest/dbtest.go`

## Contract

### Migration 00003

```sql
-- +goose Up
-- Day of month the subscription renews on, kept so monthly roll-forward
-- returns to the 31st after a short month.
ALTER TABLE accounts ADD COLUMN renewal_anchor_day smallint CHECK (renewal_anchor_day BETWEEN 1 AND 31);
UPDATE accounts SET renewal_anchor_day = extract(day FROM renews_on) WHERE renews_on IS NOT NULL;
-- +goose Down
ALTER TABLE accounts DROP COLUMN renewal_anchor_day;
```

Whenever the API sets `renews_on`, also set `renewal_anchor_day` to its day of month. The roll-forward job never changes the anchor.

### Renewal math (`renewal.go`)

```go
// NextRenewal returns the renewal after prev for the cycle, on anchorDay
// clamped to the month's length. Weekly adds 7 days. Yearly keeps month and
// anchor day (Feb 29 anchor → Feb 28 in non-leap years, Feb 29 again in leap years).
func NextRenewal(prev time.Time, cycle string, anchorDay int) time.Time

// RollForward returns every renewal date d with prev <= d < today in order,
// and the first date >= today. For cycle "none" it returns (nil, prev).
func RollForward(prev, today time.Time, cycle string, anchorDay int) (passed []time.Time, next time.Time)
```

All dates are UTC midnights. Never use `time.AddDate(0, 1, 0)` on a day > 28 without clamping.

### Service (`account.go`)

```go
type Service struct { /* pool, queries, secrets, providers, log */ }
func NewService(pool *pgxpool.Pool, store secrets.Store, providers []provider.Provider, log *slog.Logger) *Service

func (s *Service) List(ctx context.Context, p auth.Principal, status string, includeArchived bool, providerID string) ([]db.Account, error)
func (s *Service) Get(ctx context.Context, p auth.Principal, id string) (db.Account, error)
func (s *Service) Create(ctx context.Context, p auth.Principal, in CreateInput) (db.Account, error)
func (s *Service) Update(ctx context.Context, p auth.Principal, id string, in UpdateInput) (db.Account, error)
func (s *Service) Archive(ctx context.Context, p auth.Principal, id string) (db.Account, error)
// SetSecret validates the key with the provider (when validate is true and
// the provider is in the catalog), stores it through secrets.Store (Put the
// first time, Replace after) and links it to the account.
func (s *Service) SetSecret(ctx context.Context, p auth.Principal, id, value string, validate bool) (secrets.Info, error)
func (s *Service) SecretInfo(ctx context.Context, orgID string, a db.Account) (secrets.Info, bool, error)
func (s *Service) Machines(ctx context.Context, p auth.Principal, id string) ([]db.ListAccountMachinesRow, error)
func (s *Service) Renewals(ctx context.Context, p auth.Principal, withinDays int) ([]db.Account, error)

// OnRenewal is called for each passed renewal date inside the roll-forward
// transaction. Task 10 uses it to record the subscription fee.
type OnRenewal func(ctx context.Context, q *db.Queries, a db.Account, renewedOn time.Time) error
// RollForwardDue advances every auto-renewing account (all orgs) whose
// renews_on is before today, one transaction per account.
func (s *Service) RollForwardDue(ctx context.Context, today time.Time, hook OnRenewal) (int, error)

var (
	ErrNotFound       = errors.New("account not found")
	ErrInvalid        = errors.New("invalid account")        // wrap with details: fmt.Errorf("%w: price is required for a monthly plan", ErrInvalid)
	ErrUnknownProvider = errors.New("unknown provider")
)
```

`CreateInput` and `UpdateInput` mirror `AccountCreate` / `AccountUpdate` with Go types (`*string`, `*Plan` …). Rules:
- Every read and write filters by `org_id` and, for non-admins, `account_visible(...)` (pass the zero uuid `00000000-0000-0000-0000-000000000000` when `UserID` is empty). Hidden accounts are `ErrNotFound`.
- `provider_id` must exist in the catalog, else `ErrUnknownProvider`. Defaults: `auth_kind` = the provider's first `AuthKinds`, `currency` = the provider's `Currency`.
- Plan: `cycle` none ⇒ price must be absent; otherwise price required, `price.currency` must equal the account currency, `amount_minor >= 0`. `promo_kind` and `promo_ends_on` both or neither. Violations → `ErrInvalid` (400).
- `config` keys must be among the provider's `ConfigFields`; required fields must be present on create.
- `Create` with `secret` set calls `SetSecret` in the same transaction flow (validate first; on `provider.ErrInvalidCredentials` nothing is created → 422).
- Audit (one row per call, same transaction): `account.create`, `account.update` (before/after of changed fields), `account.archive`, `secret.set` (after `{"last4": …}` only).
- `RollForwardDue` reads across orgs (system job; comment it in SQL) and writes one audit row per account with principal `Principal{OrgID: a.OrgID, Kind: ActorSystem, Label: "renewal job"}`, action `account.renew`, before/after `{renews_on}`.

### Job (`job.go`)

`RollForwardArgs{}` kind `"account_roll_forward"`, worker calls `RollForwardDue(ctx, todayUTC, w.Hook)` where `Hook OnRenewal` is a field (nil for now). Periodic daily at 00:10 UTC with `RunOnStart: true`; `func PeriodicJob() *river.PeriodicJob`.

### Handlers (`internal/httpapi/accounts.go`)

Implement `ListAccounts`, `CreateAccount`, `GetAccount`, `UpdateAccount`, `ArchiveAccount`, `SetAccountSecret`, `ListAccountMachines`, `ListProviders`, `ListRenewals`.

- Write one `toAccount(a db.Account, info provider.Info, secret secrets.Info, hasSecret bool) Account` mapper; reuse `toPlan`, `toLinks`, `toAccountSummary` from `summary.go`. `config` jsonb → `map[string]string`. `secret` → `SecretInfo{Set, Last4, UpdatedAt}`; the value is never returned.
- `ListProviders`: one `ProviderInfo` per catalog entry, `capabilities` from `provider.Capabilities`, `min_poll_seconds` from `MinPollInterval`.
- `ListRenewals`: `within_days` default 30; `days_left` = renews_on − today (UTC); `converted` via `FX.Convert(ctx, price, displayCurrency, today, fxSource)` using the org's `org_settings`; omit `converted` when `fx.ErrNoRate`.
- Errors: `ErrNotFound` → 404, `ErrInvalid`/`ErrUnknownProvider` → 400 `validation_failed`, `provider.ErrInvalidCredentials` → 422 `invalid_credentials`.

### Wiring (`cmd/aicm/main.go`)

`accounts := account.NewService(pool, store, providers, log)`; `jobs.Register(reg, &account.RollForwardWorker{Service: accounts})`; `reg.Periodic = append(reg.Periodic, account.PeriodicJob())`; `Deps{…, Accounts: accounts, FX: fxSvc, Providers: providers}`. `providers` is built later in `serve` today; move `providers := registry.All(httpc)` above the job registration.

## Done when

- [ ] `renewal_test.go`:
  - `TestNextRenewal`: monthly anchor 31: Jan 31 → Feb 28 (2027) → Mar 31 → Apr 30; leap year Jan 31 2028 → Feb 29; anchor 15 stays on the 15th; weekly +7 days across a month end; yearly anchor Feb 29 2028 → Feb 28 2029 → … → Feb 29 2032.
  - `TestRollForward`: prev three months ago monthly → three passed dates and next ≥ today; prev == today → no passed dates, next == today; cycle none → nothing.
  - `TestRollForwardDue` (with `dbtest.New`): two auto-renewing accounts (one overdue by two cycles, one in the future) and one with `auto_renew = false` overdue; after the call only the first moved, the hook was called twice with the right dates, `renewal_anchor_day` unchanged, and exactly one `account.renew` audit row exists.
- [ ] Smoke on a running hub. The provider catalog is empty until task 31 lands, so: `POST /api/accounts` with an unknown `provider_id` → 400; insert an account with SQL; then `GET`, `PATCH` the plan, `PUT /secret` with `validate:false` (returns `last4`; `GET` never shows the value), `GET /api/renewals?within_days=60` (lists it with `days_left`), archive with `{"confirm":true}`.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
