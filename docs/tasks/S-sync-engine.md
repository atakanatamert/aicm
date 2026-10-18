# S – Sync engine

**Writer:** Opus
**Depends on:** 09, 12, 13 (10 for charges)
**License of touched packages:** AGPL-3.0-or-later

## Files

`internal/syncer/syncer.go`, `internal/db/queries/syncer.sql`, `internal/httpapi/sync.go`, wiring in `cmd/aicm/main.go`.

## Outcome (filled in by Opus)

Not in the original task list: adapters had nothing calling them. A dispatcher job every 5 minutes queues one `account_sync` River job per account whose provider's `MinPollInterval` has passed (unique by account). The worker reveals the key, calls only the fetchers the adapter implements, stores readings (quota), usage (replace by day) and charges (idempotent by external id), and records `last_synced_at` / `last_sync_error` / `sync_failing_since` for the UI and the sync-failing alert. Rate limits snooze the job; bad keys and missing opt-ins cancel it. `POST /api/accounts/{id}/sync` checks visibility, capabilities and the poll interval (429 with Retry-After) and writes an audit row.
