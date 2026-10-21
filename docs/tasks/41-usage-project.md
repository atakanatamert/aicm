# 41 – Hub: store and report usage by project

**Writer:** DeepSeek Flash
**Depends on:** 36 (contracts)
**License of touched packages:** AGPL-3.0-or-later

## Goal

The hub keeps the project the agent reports on every usage event, rolls usage up per project, and `GET /api/usage?group_by=project` (and the CSV export) shows tokens and estimated cost per project.

## Files to touch

- `internal/db/queries/usage.sql` (edit): `InsertUsageEvent` stores `project`; the agent roll-up groups by `model, project` and upserts with `project` (the conflict target already includes it); add `UsageByProject` like the other `UsageBy*` queries; `internal/db/queries/export.sql` (edit: a `project` column in usage.csv)
- `internal/usage/*.go` (edit): record the field, roll up per project, the new grouping
- `internal/httpapi/usage.go`, `internal/httpapi/export.go` (edit): accept `group_by=project`, fill `UsageRow.project`
- `internal/agenthub/ingest.go` only if the event mapping lives there
- `api/openapi.yaml` (edit only the `exportUsage` description to list the new `project` column)
- tests: extend the existing usage aggregation test (roll-up per project)

## Read first

- `migrations/00007_release_features.sql` (`usage_events.project`, `usage_daily.project` and its new primary key)
- `internal/agentlink/protocol.go` (`UsageEvent.Project`)
- `internal/usage` (record, roll-up, report), `internal/db/queries/usage.sql`, `internal/httpapi/usage.go`, `internal/httpapi/export.go`

## Contract

- Provider-reported usage (`source = 'api'`) keeps `project = ''`.
- The roll-up for a day replaces that day's agent rows for the account; rows per (day, model, project). Existing rows (project `''`) stay valid: a day that is rolled up again is rewritten with projects, nothing double counts (delete the account's agent rows for the day before upserting, or prove the upsert covers it).
- `group_by=project`: one row per project across visible accounts in the range; project `''` shown as an empty string (the UI labels it "Unknown"). Same visibility rules as the other groupings.
- Re-roll existing data once: on startup nothing changes automatically; instead state in your summary how to rebuild (the agent's next scan sends new events only, so old events keep `''`). Do not add a backfill job.


### Rules for every Go task

- Read `CLAUDE.md` first. Go 1.26 (`new(expr)` for pointers). No new dependencies unless this card names them.
- Every query filters by `org_id`; account-scoped reads use `account_visible(...)` (`migrations/00002_account_visible.sql`). Every mutation writes exactly one `audit_log` row with `audit.Write(ctx, q, principal.Actor(), audit.Entry{...})`, in the same transaction.
- Secrets only through `secrets.Store` (`internal/secrets/secrets.go`); API responses show at most `last4`; never log a secret, webhook URL or token.
- Queries live in `internal/db/queries/<feature>.sql`; run `~/go/bin/sqlc generate`. Never edit `*.gen.go` or `internal/db/*.sql.go` by hand.
- HTTP handlers are methods on `httpapi.Server` in one file per feature. The operations this card implements currently answer 501 from `internal/httpapi/unimplemented.go`: delete each method you implement from that file (delete the file if it becomes empty).
- Errors wrapped `fmt.Errorf("doing x: %w", err)`; handler errors as `*httpapi.APIError` (see `internal/httpapi/errors.go`).
- Tests only where `CLAUDE.md` allows (crypto, permissions, money, scoring, parsers, adapters with golden files). The migration `migrations/00007_release_features.sql` and the spec `api/openapi.yaml` are the contract: do not change them; if you think they are wrong, say so in your summary.
- Before you finish: `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean, `~/go/bin/sqlc generate` leaves no diff, `go test ./...` passes with `AICM_TEST_DATABASE_URL` set, `./scripts/check-licenses.sh` passes. Then smoke-test on a running hub (fresh database on the dev Postgres at `localhost:55432`, user/password `aicm`) and describe exactly what you ran and saw.

## Done when

- [ ] Aggregation test: events in two projects and one without a project roll up into three rows; totals unchanged.
- [ ] Smoke: insert usage events with projects through the agent ingest path (or SQL plus the roll-up job), then `GET /api/usage?group_by=project&from=…&to=…` returns per-project rows; usage.csv has the column.

## Outcome (filled in by Opus)

Merged in `f01187c` after the first attempt. Usage already on a hub keeps project "" because events carry no directory and Claude Code prunes old logs, so a rescan could lose history; projects apply from now on.
