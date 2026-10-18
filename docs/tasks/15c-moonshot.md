# 15c-moonshot – Moonshot / Kimi adapter

**Writer:** DeepSeek Flash
**Depends on:** 04
**License of touched packages:** AGPL-3.0-or-later

## Goal

`internal/provider/moonshot` implements the provider adapter below and is registered in the catalog, with one golden test from a fixture.

## Files to touch

- `internal/provider/moonshot/moonshot.go` (new)
- `internal/provider/moonshot/moonshot_test.go` (new): one golden test, `TestMoonshotGolden`
- `internal/provider/moonshot/testdata/moonshot.fixture.json`, `internal/provider/moonshot/testdata/moonshot.golden.json` (new)
- `internal/provider/registry/registry.go` (edit: one line `moonshot.New(c),` in the API adapter list)

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

**Info:** Name `Moonshot (Kimi)`, Currency USD, AuthKinds `api_key`, ModelFamilies `["kimi"]`, MinPollInterval 15 min, ConfigFields: `region` (Label "Region", Options `["global", "china"]`, not required; default global). Hosts: global `https://api.moonshot.ai`, china `https://api.moonshot.cn`; the china region bills in CNY. Links: TopUp `https://platform.moonshot.ai/console/pay`, Docs `https://platform.kimi.ai/docs/api/balance`.

**Meters:** `GET {host}/v1/users/me/balance` → `data.available_balance`, `data.voucher_balance`, `data.cash_balance` (JSON numbers in the region's currency). Declare per currency: `cash_usd`, `voucher_usd`, `cash_cny`, `voucher_cny` (labels "Cash balance (USD)" etc.). Return cash and voucher readings for the region's currency. `available_balance` is their sum and is not stored.

**Fetchers:** `QuotaFetcher` only. The unofficial coding-plan endpoint is out of scope.

### Rules for every adapter

- Package `internal/provider/moonshot`, constructor `func New(c *provider.HTTPClient) *Provider` (type name `Provider`), `var _ provider.Provider = (*Provider)(nil)` plus one `var _` line per fetcher you implement.
- Base URLs are package constants; a `ConfigFields` entry may switch hosts (e.g. region) but never take a free-form URL.
- Money: parse decimal strings with `money.ParseDecimal`; JSON numbers with `money.FromFloat` / `money.MicroFromFloat`; never multiply floats for money. Meters whose unit is a currency hold whole minor units.
- Readings: `Source: meter.SourceAPI`, `CapturedAt: p.c.Now().UTC()`, every `Key` must be in `Info().Meters`; validate them in the test with `Reading.Validate(def)`.
- `Validate` makes the cheapest authenticated call and wraps the client's error (401/403 already unwrap to `provider.ErrInvalidCredentials`).
- Unofficial endpoints: set `Info.Unofficial = true` and return `provider.ErrUnofficialNotAllowed` when `!c.AllowUnofficial`.
- Follow every page of paged endpoints. Return usage per UTC day and model.
- Never log or return the key. No new dependencies.

### Fixtures without live keys

You have no real API key. Build `testdata/moonshot.fixture.json` by hand from the **response examples in the provider's current official docs** (fetch the docs page; quote the URL in a top-level `"_source"` field of the fixture JSON, which the replayer ignores), with `"recorded_at"` set and the request URLs exactly as your code sends them. Include at least one paged response where the API pages. Then run the test with `-update` to write the golden file and check every number in it by hand against the fixture. Atakan will re-record with a real key later (`AICM_RECORD=1 AICM_RECORD_SECRET=… go test ./internal/provider/moonshot/`).

### Live docs vs research

If the live docs differ from `docs/research/` (endpoint path, auth header, field names, units, whether a management key is needed), follow the live docs and list every difference under a heading **Docs differences** in your final summary. Do not edit `docs/`.

## Done when

- [ ] `go test ./internal/provider/...` passes, including `TestMoonshotGolden` (replays the fixture: `Validate`, then every fetcher you implement, readings validated against their defs) and the catalog test.
- [ ] Every number in the golden file traced by hand to the fixture (say so in the summary).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

Merged in `4582cf7` after the first attempt. No docs differences.
