# 10 – Charges: money out, renewal fees, top-up detection, spend report

**Writer:** DeepSeek Flash
**Depends on:** 09, 11, 13
**License of touched packages:** AGPL-3.0-or-later

## Goal

Every payment is a row in `charges` in its billed currency: subscription fees appear automatically on each renewal date, top-ups are entered by hand or suggested when a balance jumps up, and invoices can be added. `GET /api/spend` reports money out per month or year and provider, billed currency first and converted at each charge's own date second.

## Files to touch

- `internal/charge/charge.go` (new): service, CRUD, list with cursor
- `internal/charge/renewal.go` (new): the `account.OnRenewal` hook
- `internal/charge/topup.go` (new): top-up detection
- `internal/charge/spend.go` (new): spend report
- `internal/charge/charge_test.go` (new): the money-math tests below
- `internal/db/queries/charges.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/charges.go` (new): `ListCharges`, `CreateCharge`, `UpdateCharge`, `GetSpend`, `ExportCharges`
- `internal/httpapi/unimplemented.go` (edit: delete those five)
- `internal/httpapi/httpapi.go` (edit: `Deps.Charges *charge.Service`)
- `internal/quota/quota.go` (edit: an optional hook called when a currency meter reading arrives, see below)
- `internal/db/queries/quota.sql` (edit: add `PreviousReading`)
- `cmd/aicm/main.go` (edit: build the service; pass the renewal hook to the account roll-forward worker and the top-up hook to the quota service)

Touch nothing else.

## Read first

- `CLAUDE.md` (money rules)
- `migrations/00001_init.sql`: `charges` (CHECKs, unique indexes `charges_external`, `charges_renewal`)
- `api/openapi.yaml`: charges, spend and export operations; schemas `Charge`, `ChargeCreate`, `ChargeUpdate`, `ChargePage`, `SpendReport`, `SpendRow`, `SpendTotal`, `ConvertedMoney`
- `internal/money/money.go`, `internal/fx/fx.go` (`Convert` returns the rate date it used; `ErrNoRate`)
- `internal/account/account.go` and `renewal.go` (`OnRenewal`, `RollForwardWorker.Hook`)
- `internal/quota/quota.go` (`RecordReadings`), `internal/meter/meter.go`
- `internal/audit/audit.go`, `internal/auth/principal.go`, `internal/httpapi/accounts.go` for handler style
- `internal/dbtest/dbtest.go`

## Contract

```go
package charge

type Service struct { /* pool, queries, fx, log */ }
func NewService(pool *pgxpool.Pool, fx *fx.Service, log *slog.Logger) *Service

type CreateInput struct {
	AccountID   string
	Kind        string // subscription | topup | invoice | refund
	Amount      money.Amount
	OccurredOn  time.Time // UTC midnight
	PeriodStart, PeriodEnd *time.Time
	Description string
}
func (s *Service) Create(ctx context.Context, p auth.Principal, in CreateInput) (db.Charge, error) // source manual, status confirmed

type UpdateInput struct {
	Status      *string // confirmed | dismissed (only from suggested)
	Amount      *money.Amount
	OccurredOn  *time.Time
	Description *string
}
func (s *Service) Update(ctx context.Context, p auth.Principal, id string, in UpdateInput) (db.Charge, error)

type ListFilter struct { From, To *time.Time; AccountID, Kind, Status string; Cursor string; Limit int }
// List returns charges newest first (occurred_on DESC, id DESC); status defaults to confirmed.
func (s *Service) List(ctx context.Context, p auth.Principal, f ListFilter) ([]db.ListChargesRow, string, error)

// RenewalHook records the subscription fee for a passed renewal date:
// account price and currency, kind subscription, source renewal, status
// confirmed, period [renewedOn, next renewal). Inserts with ON CONFLICT DO
// NOTHING on charges_renewal, so running twice records once. Accounts
// without a price are skipped.
func (s *Service) RenewalHook() account.OnRenewal

// TopUpHook is called by quota.RecordReadings for each new reading of a
// currency meter with reset kind never. When remaining went up compared with
// the previous reading of the same meter, it records a suggested top-up of
// the increase (minor units, the meter's currency), occurred_on = the new
// reading's UTC date, source detected, status suggested, description
// "Balance rose from X to Y". Meters whose key contains "granted" or
// "voucher" are free credit and never produce a top-up.
func (s *Service) TopUpHook() quota.BalanceHook

type SpendRequest struct {
	OrgID, UserID string; IsAdmin bool
	Period  string // month | year
	From, To time.Time // inclusive days
	GroupBy string // provider | account
	Currency string // conversion target
	FXSource string // org preference
}
func (s *Service) Spend(ctx context.Context, r SpendRequest) (Report, error)
```

`quota` change: add to `quota.Service`

```go
// BalanceHook sees each newly stored reading of a currency meter that never
// resets, with the previous reading of that meter (nil for the first).
type BalanceHook func(ctx context.Context, orgID, accountID string, def meter.Def, prev *meter.Reading, cur meter.Reading) error
func (s *Service) SetBalanceHook(h BalanceHook)
```

and call it from `RecordReadings` after each reading that was actually inserted (`InsertReading` returned 1), only for `def.Unit == meter.UnitCurrency && def.ResetKind == meter.ResetNever`, passing the previous reading of that meter. Load it with a new query `PreviousReading` (newest reading of the meter with `captured_at < $cur`, filtered by `org_id`) that you add to `internal/db/queries/quota.sql` (allowed edit for this task). Hook errors are logged, not returned: a failed suggestion must not drop the reading.

### Spend math (exact)

1. Select confirmed charges in [From, To] visible to the user (`account_visible`), with provider and account.
2. Bucket by period key (`2026-10` or `2026`) and group key (provider id, or account id).
3. `billed`: per bucket, sum `amount_minor` per currency (refunds subtract). Never add two currencies.
4. `converted`: convert **each charge** with `fx.Convert(amount, Currency, charge.occurred_on, FXSource)`, then sum the converted minor units. If any charge in the bucket has no rate, `converted` is absent and `unconverted_count` counts them.
5. `totals`: the same per period across groups.
6. Use `money.Amount.Add` for sums (overflow-checked).

### Handlers

- `CreateCharge`: kind subscription/topup/invoice/refund; amount must be > 0 (refunds are positive amounts with kind refund); currency valid.
- `UpdateCharge`: changing amount/date/description of a `renewal` or `api` charge is allowed (corrections); status changes only `suggested → confirmed|dismissed`, else 409.
- `GetSpend`: defaults: period month, from = first day of the current UTC year, to = today, group_by provider, currency = org display currency.
- `ExportCharges` (CSV, `text/csv`, `Content-Disposition: attachment; filename="charges.csv"`): columns exactly as in the spec description; amounts as decimals via `money.Amount.Decimal()`; converted columns empty when no rate. Use `encoding/csv`. Return a custom response type that writes the CSV.
- Audit: `charge.create`, `charge.update`, `charge.confirm`, `charge.dismiss` (one per call). The renewal hook and top-up hook write `charge.create` with a system principal.

## Done when

- [ ] `charge_test.go` (with `dbtest.New`; insert accounts and `fx_rates` with SQL):
  - `TestSpendConvertsEachChargeAtItsDate`: two CNY charges in October on dates with different EUR→CNY rates and one USD charge; `billed` has a CNY and a USD entry; `converted` (USD) equals the sum of each charge converted separately (compute the expected integers in the test by hand and write them as literals, with the arithmetic in a comment).
  - `TestSpendMissingRate`: a charge on a date with no rate within 7 days → `converted` absent, `unconverted_count` 1, `billed` still correct.
  - `TestSpendRefund`: a refund subtracts from `billed` and `converted`.
  - `TestSpendYearBuckets`: charges in two years with `period=year` → two buckets.
  - `TestRenewalHookIdempotent`: calling the hook twice for the same date creates one charge with the plan price.
  - `TestTopUpDetection`: readings 5000 → 4800 → 9800 → 9700 on a `balance` meter create exactly one suggested top-up of 5000; the same pattern on a `granted_cny` meter creates none.
- [ ] Smoke on a running hub: create a manual top-up, list it, `GET /api/spend` shows it billed and converted, export CSV downloads with the header row.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
