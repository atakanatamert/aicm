# 33 – CSV export: usage and meter readings

**Writer:** DeepSeek Flash
**Depends on:** 12, 13
**License of touched packages:** AGPL-3.0-or-later

## Goal

`GET /api/export/usage.csv` and `GET /api/export/readings.csv` download the same data the UI shows, with the current filters, for the accounts the caller may see. (`charges.csv` exists already: copy its style.)

## Files to touch

- `internal/httpapi/export.go` (new): `ExportUsage`, `ExportReadings`
- `internal/db/queries/export.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/unimplemented.go` (edit: delete the two methods; if it becomes empty, delete the file)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `internal/httpapi/charges.go` (`ExportCharges`: custom response type writing CSV), `internal/httpapi/summary.go` (`requirePrincipal`)
- `api/openapi.yaml`: `exportUsage`, `exportReadings` (the description lists the exact columns)
- `migrations/00001_init.sql`: `usage_daily`, `quota_meters`, `quota_readings`; `migrations/00002_account_visible.sql`
- `internal/money/money.go` (`Amount.Decimal` for minor units; micro units → decimal with 6 places for usage cost)

## Contract

- Both queries filter by `org_id` and `account_visible(...)`, join `accounts` for name and provider, and stream rows (`pgx` rows → `csv.Writer` as they come; no loading everything into memory first).
- `usage.csv`: one row per account, day and model, ordered by day then account then model; columns exactly as the spec description; `cost` = `cost_micro` as a decimal with 6 fraction digits (write a small helper using integer math), `cost_reported` `true`/`false`. Defaults: from = first day of the current UTC month, to = today; range over 400 days → 400.
- `readings.csv`: one row per reading in [from, to) (defaults: last 30 days), ordered by captured_at; currency meters' values written as decimals in that currency (`money.Amount{Minor: int64(v), Currency}.Decimal()`), others as plain numbers (`strconv.FormatFloat(v, 'f', -1, 64)`); empty cells for nulls; times RFC 3339 UTC.
- Headers: `Content-Type: text/csv; charset=utf-8`, `Content-Disposition: attachment; filename="usage.csv"` / `"readings.csv"`.

## Done when

- [ ] Smoke on a running hub with seeded usage and readings (SQL): both downloads have the header row and the seeded rows; a viewer without grants gets only the header row.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.

## Outcome (filled in by Opus)
