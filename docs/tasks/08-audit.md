# 08 – Audit: admin view endpoint and audit rows for existing mutations

**Writer:** DeepSeek Flash
**Depends on:** 07
**License of touched packages:** AGPL-3.0-or-later

## Goal

Admins can page through the audit log, and every mutation that already exists writes exactly one audit row: first-run setup, password change, `create-admin`, manual meter and manual reading. `internal/audit/audit.go` (`audit.Write`) already exists; use it.

## Files to touch

- `internal/audit/query.go` (new): listing with a cursor
- `internal/db/queries/audit.sql` (edit: add the list query), then `~/go/bin/sqlc generate`
- `internal/httpapi/audit.go` (new): `ListAuditLog`
- `internal/httpapi/unimplemented.go` (edit: delete `ListAuditLog`)
- `internal/httpapi/httpapi.go` (edit: `Deps.Audit *audit.Service`)
- `internal/auth/service.go` (edit: audit rows in `CompleteSetup`, `ChangePassword`, `CreateAdmin`)
- `internal/quota/quota.go` (edit: audit rows in `UpsertManualMeter`, `RecordManualReading`)
- `cmd/aicm/main.go` (edit: build `audit.NewService(pool)`, pass it in `Deps`)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `internal/audit/audit.go`, `internal/audit/audit_test.go`
- `migrations/00001_init.sql`: `audit_log`
- `api/openapi.yaml`: `listAuditLog`, `AuditEntry`, `AuditPage`
- `internal/auth/service.go`, `internal/quota/quota.go`

## Contract

```go
package audit

type Service struct { /* pool, queries */ }
func NewService(pool *pgxpool.Pool) *Service

type Filter struct {
	ActorID, TargetKind, TargetID string // optional, exact match
	Cursor string // opaque; empty for the first page
	Limit  int    // 1..500
}
// List returns entries newest first and the cursor for the next page ("" at the end).
// The cursor is the last row's id, base64url-encoded; rows with id < cursor follow.
func (s *Service) List(ctx context.Context, orgID string, f Filter) ([]db.AuditLog, string, error)
```

SQL: `ListAuditLog` filtered by `org_id`, optional `actor_id`, `target_kind`, `target_id` (use `sqlc.narg`), `id < cursor` when given, `ORDER BY id DESC LIMIT n`.

Handler: `ListAuditLog` maps rows to `AuditEntry` (`before`/`after` decoded into `map[string]interface{}`; null stays null). Bad cursor → 400.

### Audit rows for existing mutations

Write inside the same transaction as the change where one exists; otherwise right after the successful write with the same `*db.Queries`.

| Where | Action | Target | Before / After |
| --- | --- | --- | --- |
| `CompleteSetup` | `user.create` | `user`, new id | nil / `{email, name, role}`; principal = the new admin |
| `CreateAdmin` | `user.create` | `user` | nil / `{email, name, role}`; principal = `Principal{OrgID, Kind: ActorSystem, Label: "cli"}` |
| `ChangePassword` | `user.password_change` | `user`, own id | nil / `{"password": "changed"}` |
| `UpsertManualMeter` | `meter.upsert` | `meter`, `account_id + "/" + key` | nil / the definition |
| `RecordManualReading` | `reading.create` | `meter`, `account_id + "/" + key` | nil / the reading |

Login and logout are not mutations of data and get no rows.

## Done when

- [ ] Smoke on a running hub (fresh DB): setup, change password, add a manual meter and a reading (insert an account with SQL first), run `AICM_ADMIN_PASSWORD=… go run ./cmd/aicm create-admin --email b@example.com --name B`; then `GET /api/audit` as admin returns exactly five entries, newest first, with no password anywhere in the body; `GET /api/audit?limit=2` then `?cursor=<next_cursor>&limit=2` pages correctly; as a viewer → 403.
- [ ] `go test ./...` passes (no new tests required).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
