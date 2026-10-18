# 15a-openrouter – OpenRouter adapter

**Writer:** DeepSeek Flash
**Depends on:** 04
**License of touched packages:** AGPL-3.0-or-later

## Goal

`internal/provider/openrouter` implements the provider adapter below and is registered in the catalog, with one golden test from a fixture.

## Files to touch

- `internal/provider/openrouter/openrouter.go` (new)
- `internal/provider/openrouter/openrouter_test.go` (new): one golden test, `TestOpenRouterGolden`
- `internal/provider/openrouter/testdata/openrouter.fixture.json`, `internal/provider/openrouter/testdata/openrouter.golden.json` (new)
- `internal/provider/registry/registry.go` (edit: one line `openrouter.New(c),` in the API adapter list)

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

**Info:** Name `OpenRouter`, Currency USD, AuthKinds `api_key`, `admin_key` (management key), ModelFamilies `["*"]`, MinPollInterval 15 min. Links: TopUp `https://openrouter.ai/settings/credits`, Keys `https://openrouter.ai/settings/keys`, Usage `https://openrouter.ai/activity`, Docs `https://openrouter.ai/docs/api/api-reference/credits/get-remaining-credits`.

**Meters:**
- `credits` · Credits remaining · currency USD · never: from `GET /api/v1/credits` → `total_credits − total_usage` (both USD numbers), as remaining. If the endpoint answers 401/403 for a normal key, skip this meter (no error).
- `key_limit` · Key spend limit · currency USD: from `GET /api/v1/key` → `limit`, `limit_remaining`, `limit_reset` (`daily`/`weekly`/`monthly`/null). Reset kind `fixed` when `limit_reset` is set (compute `resets_at` as the next UTC midnight / next Monday 00:00 UTC / 1st of next month), `never` otherwise; the Def in `Info.Meters` uses `fixed`. Skip the reading when the key has no limit.

**Fetchers:** `QuotaFetcher`; `UsageFetcher` from `GET /api/v1/activity` (needs a management key per research; verify) grouped per day and model with `usage` cost in USD → `CostMicro`. If it rejects a normal key, return the wrapped `ErrInvalidCredentials` only from `FetchUsage`.

### Rules for every adapter

- Package `internal/provider/openrouter`, constructor `func New(c *provider.HTTPClient) *Provider` (type name `Provider`), `var _ provider.Provider = (*Provider)(nil)` plus one `var _` line per fetcher you implement.
- Base URLs are package constants; a `ConfigFields` entry may switch hosts (e.g. region) but never take a free-form URL.
- Money: parse decimal strings with `money.ParseDecimal`; JSON numbers with `money.FromFloat` / `money.MicroFromFloat`; never multiply floats for money. Meters whose unit is a currency hold whole minor units.
- Readings: `Source: meter.SourceAPI`, `CapturedAt: p.c.Now().UTC()`, every `Key` must be in `Info().Meters`; validate them in the test with `Reading.Validate(def)`.
- `Validate` makes the cheapest authenticated call and wraps the client's error (401/403 already unwrap to `provider.ErrInvalidCredentials`).
- Unofficial endpoints: set `Info.Unofficial = true` and return `provider.ErrUnofficialNotAllowed` when `!c.AllowUnofficial`.
- Follow every page of paged endpoints. Return usage per UTC day and model.
- Never log or return the key. No new dependencies.

### Fixtures without live keys

You have no real API key. Build `testdata/openrouter.fixture.json` by hand from the **response examples in the provider's current official docs** (fetch the docs page; quote the URL in a top-level `"_source"` field of the fixture JSON, which the replayer ignores), with `"recorded_at"` set and the request URLs exactly as your code sends them. Include at least one paged response where the API pages. Then run the test with `-update` to write the golden file and check every number in it by hand against the fixture. Atakan will re-record with a real key later (`AICM_RECORD=1 AICM_RECORD_SECRET=… go test ./internal/provider/openrouter/`).

### Live docs vs research

If the live docs differ from `docs/research/` (endpoint path, auth header, field names, units, whether a management key is needed), follow the live docs and list every difference under a heading **Docs differences** in your final summary. Do not edit `docs/`.

## Done when

- [ ] `go test ./internal/provider/...` passes, including `TestOpenRouterGolden` (replays the fixture: `Validate`, then every fetcher you implement, readings validated against their defs) and the catalog test.
- [ ] Every number in the golden file traced by hand to the fixture (say so in the summary).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
