# 04 – Provider, meter and money contracts; HTTP client; fixtures

**Writer:** Opus
**Depends on:** 01
**License of touched packages:** AGPL-3.0-or-later (provider, money), Apache-2.0 (meter)

## Goal

Phase 0 contract. See `docs/tasks/README.md` row 4.

## Files

`internal/meter/meter.go`, `internal/money/money.go` + test, `internal/provider/provider.go`, `internal/provider/httpclient.go`, `internal/provider/fixture.go`, `internal/provider/providertest/` (helpers, example adapter golden test, fixture), `internal/provider/registry/` + catalog test.

## Done when

- [x] The example adapter in `providertest/example_test.go` passes its golden test, including a 429 + Retry-After retry and two usage pages.
- [x] `go build`, `go vet`, `golangci-lint run`, `go test -race ./...`, `reuse lint`, `scripts/check-licenses.sh` clean

## Outcome (filled in by Opus)

Done by Opus (contract task), squash-merged to `main`. Decisions taken:

- `meter` is Apache because the agent sends readings too.
- Money from providers is parsed digit by digit (`money.ParseDecimal`, `FromFloat` via shortest decimal), never float math.
- Min poll interval lives in `Info.MinPollInterval` and is enforced by the sync engine from `accounts.last_synced_at`, not inside the HTTP client, so it survives restarts.
- Fixture replay pins the clock to `recorded_at` and uses `provider.Redacted` as the secret.
- The harness interface moved to task 05 because it uses the agentlink wire types.
