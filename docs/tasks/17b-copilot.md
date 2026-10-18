# 17b-copilot – GitHub Copilot billing adapter

**Writer:** DeepSeek Flash
**Depends on:** 04
**License of touched packages:** AGPL-3.0-or-later

## Goal

`internal/provider/copilot` implements the provider adapter below and is registered in the catalog, with one golden test from a fixture.

## Files to touch

- `internal/provider/copilot/copilot.go` (new)
- `internal/provider/copilot/copilot_test.go` (new): one golden test, `TestCopilotGolden`
- `internal/provider/copilot/testdata/copilot.fixture.json`, `internal/provider/copilot/testdata/copilot.golden.json` (new)
- `internal/provider/registry/registry.go` (edit: one line `copilot.New(c),` in the API adapter list)

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

**Info:** Name `GitHub Copilot`, Currency USD, AuthKinds `api_key` (fine-grained PAT with "Plan: read", Bearer, header `X-GitHub-Api-Version: 2026-03-10`), ModelFamilies `["claude", "gpt", "gemini"]`, MinPollInterval 1 h, ConfigFields: `username` (Required), `plan` (Options `["free", "pro", "pro_plus", "max"]`, Required). Links: Billing `https://github.com/settings/billing`, ManageSubscription `https://github.com/settings/copilot`, Usage `https://github.com/settings/billing/usage`, Docs `https://docs.github.com/en/rest/billing/usage`.

**Background (verified by Opus on 2026-10-18):** since June 2026 Copilot bills in GitHub AI Credits (1 credit = $0.01). Individual plans include a monthly allowance that resets at 00:00 UTC on the 1st: Pro 1,500, Pro+ 7,000, Max 20,000 (base + flex; https://docs.github.com/en/copilot/concepts/billing-and-usage/individuals/billing). Free has an allowance the docs don't quantify. Premium requests are the legacy unit: do not use them. Business and Enterprise pools are org-level and out of scope (the endpoint below is per user).

**Meters:** `ai_credits` · AI credits · credits · calendar_month, from `GET https://api.github.com/users/{username}/settings/billing/ai_credit/usage` (current month; follow `Link: rel="next"` pages; the fixture replayer and recorder keep the `Link` header): `Used` = sum of the usage items' net quantity, `Limit` = the plan allowance (pro 1500, pro_plus 7000, max 20000; nil for free), `Remaining` = Limit − Used, not below 0, only when Limit is known. `ResetsAt` = 1st of next month 00:00 UTC. Check the response schema on https://docs.github.com/en/rest/billing/usage (section "Get billing AI credit usage report for a user") and cite it in the fixture's `_source`. The `copilot_internal/user` endpoint is out of scope.

**Fetchers:** `QuotaFetcher` only.

### Rules for every adapter

- Package `internal/provider/copilot`, constructor `func New(c *provider.HTTPClient) *Provider` (type name `Provider`), `var _ provider.Provider = (*Provider)(nil)` plus one `var _` line per fetcher you implement.
- Base URLs are package constants; a `ConfigFields` entry may switch hosts (e.g. region) but never take a free-form URL.
- Money: parse decimal strings with `money.ParseDecimal`; JSON numbers with `money.FromFloat` / `money.MicroFromFloat`; never multiply floats for money. Meters whose unit is a currency hold whole minor units.
- Readings: `Source: meter.SourceAPI`, `CapturedAt: p.c.Now().UTC()`, every `Key` must be in `Info().Meters`; validate them in the test with `Reading.Validate(def)`.
- `Validate` makes the cheapest authenticated call and wraps the client's error (401/403 already unwrap to `provider.ErrInvalidCredentials`).
- Unofficial endpoints: set `Info.Unofficial = true` and return `provider.ErrUnofficialNotAllowed` when `!c.AllowUnofficial`.
- Follow every page of paged endpoints. Return usage per UTC day and model.
- Never log or return the key. No new dependencies.

### Fixtures without live keys

You have no real API key. Build `testdata/copilot.fixture.json` by hand from the **response examples in the provider's current official docs** (fetch the docs page; quote the URL in a top-level `"_source"` field of the fixture JSON, which the replayer ignores), with `"recorded_at"` set and the request URLs exactly as your code sends them. Include at least one paged response where the API pages. Then run the test with `-update` to write the golden file and check every number in it by hand against the fixture. Atakan will re-record with a real key later (`AICM_RECORD=1 AICM_RECORD_SECRET=… go test ./internal/provider/copilot/`).

### Live docs vs research

If the live docs differ from `docs/research/` (endpoint path, auth header, field names, units, whether a management key is needed), follow the live docs and list every difference under a heading **Docs differences** in your final summary. Do not edit `docs/`.

## Done when

- [ ] `go test ./internal/provider/...` passes, including `TestCopilotGolden` (replays the fixture: `Validate`, then every fetcher you implement, readings validated against their defs) and the catalog test.
- [ ] Every number in the golden file traced by hand to the fixture (say so in the summary).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

## Attempt history

- Attempt 1 (DeepSeek Flash) implemented the card as first written (premium requests). The writer reported that GitHub moved individual plans to AI credits in June 2026; Opus verified the live docs, discarded the attempt and rewrote the meter section. Not a research contradiction: `docs/research/quota-capture.md` already lists the `ai_credit/usage` endpoint first.
