# 02 – Migrations, sqlc config, River setup

**Writer:** Opus
**Depends on:** 01
**License of touched packages:** AGPL-3.0-or-later

## Goal

Phase 0 contract. See `docs/tasks/README.md` row 2.

## Files

`migrations/00001_init.sql`, `migrations/embed.go`, `sqlc.yaml`, `internal/db/queries/org.sql`, `internal/db/*.go` (generated), `internal/db/migrate.go`, `internal/jobs/jobs.go`, `cmd/aicm/main.go` (`migrate up|down|status`; `serve` migrates first).

## Done when

- [x] `aicm migrate up` runs clean, `down` then `up` round-trips, a second `up` is a no-op; River migrations applied after goose.
- [x] `go build`, `go vet`, `golangci-lint run`, `go test -race ./...`, `reuse lint`, `scripts/check-licenses.sh` clean

## Outcome (filled in by Opus)

Done by Opus (contract task), squash-merged to `main`. Decisions taken:

- Child tables reference parents by `(org_id, id)`, so rows can't point into another org.
- Agent-discovered accounts are rows in `accounts` with `status = 'pending'` (no separate table); `account_machines` records which machine and harness saw each account.
- Token-cost estimates use micro units (`cost_micro`); charges use minor units. Meter values are `double precision` in the provider's unit (currency meters hold whole minor units).
- `fx_rates` and `model_prices` are global, not tenant data.
- `audit_log` has a trigger rejecting UPDATE and DELETE, except cascades from deleting an org.
- sqlc maps uuid to `string`, timestamptz/date to `time.Time`, jsonb to `json.RawMessage`, inet to `netip.Addr` (verified against Postgres 17).
- `jobs.Registry` keeps River insert-only until the first worker registers (River refuses to start with zero workers).
