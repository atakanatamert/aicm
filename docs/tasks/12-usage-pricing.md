# 12 – Usage rollups and model price table

**Writer:** DeepSeek Flash
**Depends on:** 02
**License of touched packages:** AGPL-3.0-or-later

## Goal

Agent usage events are stored once and rolled up into `usage_daily` with an estimated cost; provider usage reports replace their days in `usage_daily`; model prices are imported daily from the LiteLLM price file; `GET /api/usage` returns grouped usage for the accounts the caller may see.

## Files to touch

- `internal/pricing/pricing.go` (new): price lookup, cost estimate
- `internal/pricing/litellm.go` (new): fetch and parse the LiteLLM file, River job
- `internal/pricing/litellm_test.go` (new): golden parser test
- `internal/pricing/testdata/litellm.fixture.json`, `internal/pricing/testdata/litellm.golden.json` (new)
- `internal/usage/usage.go` (new): ingest, rollup, API replace, query
- `internal/usage/usage_test.go` (new): aggregation test
- `internal/db/queries/usage.sql`, `internal/db/queries/pricing.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/usage.go` (new): `GetUsage`
- `internal/httpapi/unimplemented.go` (edit: delete `GetUsage`)
- `internal/httpapi/httpapi.go` (edit: add `Usage *usage.Service` to `Deps`)
- `cmd/aicm/main.go` (edit: build services, register the pricing job)
- `THIRD_PARTY_NOTICES.md` (edit: add the LiteLLM price data row)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `migrations/00001_init.sql`: `usage_events`, `usage_daily`, `model_prices`; `migrations/00002_account_visible.sql`
- `internal/agentlink/protocol.go`: `UsageEvent` (what agents send)
- `internal/provider/provider.go`: `UsageRecord` (what adapters return)
- `internal/money/money.go`: `MicroFromFloat`, micro units
- `internal/provider/providertest/example_test.go`: fixture + golden pattern
- `internal/auth/principal.go`: `FromContext`, `IsAdmin`
- `internal/jobs/jobs.go`
- `internal/dbtest/dbtest.go`

## Contract

### pricing

```go
package pricing

// Price is per million tokens in micro units of Currency ($3.00/Mtok = 3_000_000).
type Price struct {
	Model, Provider, Currency                 string
	Input, Output, CacheRead, CacheWrite      int64
}

type Service struct { /* pool, queries, http client, log */ }
func NewService(pool *pgxpool.Pool, c *provider.HTTPClient, log *slog.Logger) *Service

// Lookup finds a price by exact model name; if none, by the model name with
// a "provider/" prefix stripped (e.g. "anthropic/claude-sonnet-4-5"), then by
// the name with a trailing date suffix "-YYYYMMDD" stripped. ok is false when
// nothing matches.
func (s *Service) Lookup(ctx context.Context, model string) (Price, bool, error)

// Estimate returns cost_micro for token counts: Σ tokens × price / 1_000_000,
// rounded half away from zero, computed in int64 with math/big only if the
// product could overflow (use big.Int when tokens × price > 9e18).
func Estimate(p Price, input, output, cacheRead, cacheWrite int64) int64

// Import fetches and upserts the LiteLLM file. Returns the row count.
func (s *Service) Import(ctx context.Context) (int, error)

// ParseLiteLLM is the pure parser used by Import and the golden test.
func ParseLiteLLM(r io.Reader) ([]Price, error)
```

LiteLLM file: `https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json`, an object keyed by model name. Use fields `input_cost_per_token`, `output_cost_per_token`, `cache_read_input_token_cost`, `cache_creation_input_token_cost` (USD per token, JSON numbers) and `litellm_provider`. Skip the `sample_spec` key and entries with neither input nor output cost. Convert per-token USD to micro per Mtok with `money.MicroFromFloat(cost * 1e6)`. Currency is always USD. Keep every other field out.

River job in `litellm.go`: `ImportArgs{}` kind `"pricing_import"`, worker calls `Import`, periodic daily at 03:00 UTC with `RunOnStart: true`; `func PeriodicJob() *river.PeriodicJob`.

Golden test `TestParseLiteLLMGolden`: `testdata/litellm.fixture.json` is a hand-trimmed copy of the real file with `sample_spec`, two Anthropic models, two OpenAI models, one DeepSeek model, one entry without costs and one with only input cost (take values from the live file). Parse it from disk with `ParseLiteLLM` (no HTTP) and compare with `providertest.Golden`.

### usage

```go
package usage

type Service struct { /* pool, queries, pricing, log */ }
func NewService(pool *pgxpool.Pool, prices *pricing.Service, log *slog.Logger) *Service

// RecordAgentEvents stores events (ON CONFLICT (machine_id, source, message_id)
// DO NOTHING) and recomputes usage_daily rows (source 'agent') for every
// (account, UTC day) the batch touched, in one transaction. accountIDs maps
// each event's AccountRef to the hub account id (the caller resolves them).
// Returns how many events were new.
func (s *Service) RecordAgentEvents(ctx context.Context, orgID, machineID string, accountIDs map[agentlink.AccountRef]string, events []agentlink.UsageEvent) (int, error)

// ReplaceAPIUsage replaces usage_daily rows with source 'api' for the account
// and days in [from, to) with recs, in one transaction (delete then insert).
// Records with CostMicro set are cost_reported=true; others are estimated.
func (s *Service) ReplaceAPIUsage(ctx context.Context, orgID, accountID string, from, to time.Time, recs []provider.UsageRecord) error

type Query struct {
	OrgID, UserID string
	IsAdmin       bool
	From, To      time.Time // inclusive UTC days
	GroupBy       string    // day | model | account | provider
	AccountID     string    // optional
	ProviderID    string    // optional
}

type Row struct {
	Day                                                                         *time.Time
	Model, AccountID, AccountName, ProviderID                                   string
	Requests, Input, Output, CacheRead, CacheWrite, Reasoning                   int64
	Cost                                                                        []CostTotal // one per currency
	CostReported                                                                bool        // true only if every contributing row was reported
}
type CostTotal struct { Micro int64; Currency string }

func (s *Service) Query(ctx context.Context, q Query) ([]Row, error)
```

Rollup rule for one (account, day): `SELECT model, count(*), sums… FROM usage_events WHERE org_id=$ AND account_id=$ AND occurred_at >= day AND occurred_at < day+1 GROUP BY model`; per model, cost = Σ reported `cost_micro` of events that have one (all agent events in a group share the harness currency in practice; if currencies differ, use the price-table estimate for the whole group) + `Estimate(price, tokens of events without cost)`; `cost_reported` = every event had a cost; `cost_currency` = the reported currency, else the price currency, else "USD". If no price is found the estimate is 0. Upsert with `ON CONFLICT (account_id, day, model, source) DO UPDATE`.

`Query` must add `AND account_visible(@org_id, @user_id, @is_admin, d.account_id)` (see migration 00002) and join `accounts` for name and provider. Write one SQL query per group_by value (four queries) rather than building SQL strings.

### Handler (`internal/httpapi/usage.go`)

`GetUsage`: principal from `auth.FromContext` (missing → `ErrUnauthorized`), defaults `from` = first day of the current UTC month, `to` = today, reject `from > to` or a range over 400 days with `BadRequest`. Map `Row` to the generated `UsageRow` type (`cost` as `[]MicroMoney`). The auth middleware arrives in task 07; until then the handler answers 401, which is expected.

### Wiring (`cmd/aicm/main.go`)

```go
prices := pricing.NewService(pool, httpc, log)   // httpc := provider.NewHTTPClient(log, nil) if not already defined
usageSvc := usage.NewService(pool, prices, log)
jobs.Register(reg, &pricing.ImportWorker{Service: prices})
reg.Periodic = append(reg.Periodic, pricing.PeriodicJob())
```
and `httpapi.Deps{..., Usage: usageSvc}`.

### THIRD_PARTY_NOTICES.md

Replace the `_none yet_` row (or add a row) with: `| BerriAI/litellm model_prices_and_context_window.json | MIT, Copyright (c) 2023 Berri AI | Model price data, imported at runtime | internal/pricing |`.

## Done when

- [ ] `TestParseLiteLLMGolden` passes.
- [ ] `usage_test.go` `TestRollup` (with `dbtest.New`; insert an account and a machine with SQL; insert a price row):
  - 3 events for model A on day 1 (one sent twice → stored once), 1 event for model B on day 1, 2 events for model A on day 2 (one crossing midnight UTC by occurred_at stays on its own day) → `usage_daily` has exactly 3 rows with the right sums.
  - Estimated cost for model A day 1 equals `Estimate` of its token sums; an event with a reported cost makes `cost_reported=false` for a mixed group and true for an all-reported group.
  - Sending the same batch again changes nothing (`RecordAgentEvents` returns 0).
  - `ReplaceAPIUsage` for day 1 with two records, then again with one record → one `api` row left for day 1; `agent` rows untouched.
  - `Query` as a non-admin user without a grant returns nothing; with a grant (insert into `account_grants`) returns the rows.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

Merged in `2186c50` after the first attempt (DeepSeek Flash, ~6 min). Smoke: 3,427 model prices imported. Writer's choices accepted: `usage` imported as `usagepkg` in `main.go` (name clash with the usage text constant).
