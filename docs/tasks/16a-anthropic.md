# 16a-anthropic – Anthropic Admin API adapter

**Writer:** DeepSeek Flash
**Depends on:** 04
**License of touched packages:** AGPL-3.0-or-later

## Goal

`internal/provider/anthropic` implements the provider adapter below and is registered in the catalog, with one golden test from a fixture.

## Files to touch

- `internal/provider/anthropic/anthropic.go` (new)
- `internal/provider/anthropic/anthropic_test.go` (new): one golden test, `TestAnthropicGolden`
- `internal/provider/anthropic/testdata/anthropic.fixture.json`, `internal/provider/anthropic/testdata/anthropic.golden.json` (new)
- `internal/provider/registry/registry.go` (edit: one line `anthropic.New(c),` in the API adapter list)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `internal/provider/provider.go`: `Info`, `Provider`, `UsageFetcher`, `QuotaFetcher`, `ChargeFetcher`, `Credentials`, `UsageRecord`, `Charge`, errors
- `internal/provider/httpclient.go`: all HTTP goes through `*provider.HTTPClient` (`GetJSON`, `SendJSON`, `Do`); use `c.Now()` for `CapturedAt`
- `internal/provider/providertest/example_test.go` and `providertest.go`: **copy this structure** (adapter shape, fixture, golden)
- `internal/meter/meter.go`, `internal/money/money.go` (`ParseDecimal`, `FromFloat`, `ParseMicro`, `MicroFromFloat`)
- `internal/provider/registry/registry.go`
- `docs/research/providers.md`, `docs/research/quota-capture.md`

## Contract

**Info:** Name `Anthropic API`, Currency USD, AuthKinds `admin_key` (`sk-ant-admin…` in `x-api-key`, plus `anthropic-version: 2023-06-01`), ModelFamilies `["claude"]`, MinPollInterval 1 h. Links: Billing `https://platform.claude.com/settings/billing`, Usage `https://platform.claude.com/usage`, Keys `https://platform.claude.com/settings/admin-keys`, Docs `https://platform.claude.com/docs/en/build-with-claude/usage-cost-api`. No meters.

**Fetchers:** `UsageFetcher` only:
- tokens from `GET https://api.anthropic.com/v1/organizations/usage_report/messages` with `starting_at`, `ending_at`, `bucket_width=1d`, `group_by[]=model`, following `has_more` / `next_page`. Map uncached input, cache creation, cache read and output tokens to the `UsageRecord` fields.
- cost from `GET /v1/organizations/cost_report` (daily, grouped by description/model if supported) → `CostMicro` per day and model when the report can be attributed to a model; otherwise leave `CostMicro` nil (the hub estimates). Amounts in the cost report are decimal strings in cents or dollars: check the docs and convert exactly.
- `Validate`: one `usage_report/messages` call for the last day.

### Rules for every adapter

- Package `internal/provider/anthropic`, constructor `func New(c *provider.HTTPClient) *Provider` (type name `Provider`), `var _ provider.Provider = (*Provider)(nil)` plus one `var _` line per fetcher you implement.
- Base URLs are package constants; a `ConfigFields` entry may switch hosts (e.g. region) but never take a free-form URL.
- Money: parse decimal strings with `money.ParseDecimal`; JSON numbers with `money.FromFloat` / `money.MicroFromFloat`; never multiply floats for money. Meters whose unit is a currency hold whole minor units.
- Readings: `Source: meter.SourceAPI`, `CapturedAt: p.c.Now().UTC()`, every `Key` must be in `Info().Meters`; validate them in the test with `Reading.Validate(def)`.
- `Validate` makes the cheapest authenticated call and wraps the client's error (401/403 already unwrap to `provider.ErrInvalidCredentials`).
- Unofficial endpoints: set `Info.Unofficial = true` and return `provider.ErrUnofficialNotAllowed` when `!c.AllowUnofficial`.
- Follow every page of paged endpoints. Return usage per UTC day and model.
- Never log or return the key. No new dependencies.

### Fixtures without live keys

You have no real API key. Build `testdata/anthropic.fixture.json` by hand from the **response examples in the provider's current official docs** (fetch the docs page; quote the URL in a top-level `"_source"` field of the fixture JSON, which the replayer ignores), with `"recorded_at"` set and the request URLs exactly as your code sends them. Include at least one paged response where the API pages. Then run the test with `-update` to write the golden file and check every number in it by hand against the fixture. Atakan will re-record with a real key later (`AICM_RECORD=1 AICM_RECORD_SECRET=… go test ./internal/provider/anthropic/`).

### Live docs vs research

If the live docs differ from `docs/research/` (endpoint path, auth header, field names, units, whether a management key is needed), follow the live docs and list every difference under a heading **Docs differences** in your final summary. Do not edit `docs/`.

## Done when

- [ ] `go test ./internal/provider/...` passes, including `TestAnthropicGolden` (replays the fixture: `Validate`, then every fetcher you implement, readings validated against their defs) and the catalog test.
- [ ] Every number in the golden file traced by hand to the fixture (say so in the summary).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

Merged in `e43afb6` after the first attempt. Docs differences: cost report amounts are decimal strings in cents; no request counts in the usage report; `claude_code` report not documented on that page.
