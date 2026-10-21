# 38 – Budgets and four new alert kinds

**Writer:** DeepSeek Flash
**Depends on:** 36 (contracts)
**License of touched packages:** AGPL-3.0-or-later

## Goal

Monthly budgets for the org and for single accounts, with month-to-date spend and a projection. The alert evaluation gains four kinds: `budget`, `meter_reset` ("limits are back"), `spend_anomaly` (runaway spend) and `wrong_wallet` (a harness bills an API key, or paid overage is on, while a subscription is active).

## Files to touch

- `internal/budget/budget.go` (new): service — list with status, put (upsert), delete; `Status(ctx, orgID, now)` used by alerts
- `internal/db/queries/budgets.sql` (new); `internal/db/queries/alerts.sql` (edit: new read queries the evaluation needs)
- `internal/httpapi/budgets.go` (new): `ListBudgets`, `PutBudget`, `DeleteBudget`; `internal/httpapi/unimplemented.go` (edit); `internal/httpapi/httpapi.go` (add a `Budgets` dependency); `cmd/aicm/main.go` (wire)
- `internal/alert/evaluate.go` (edit) and new files in `internal/alert/` for each kind (`budget.go`, `reset.go`, `anomaly.go`, `wallet.go`); `internal/alert/job.go` (edit: run every 15 minutes instead of hourly, keep the uniqueness by period)
- `internal/httpapi/alerts.go` (edit only if the new `AlertParams` fields need mapping)
- tests: `internal/budget/budget_test.go` (money: conversion and projection), `internal/alert/*_test.go` (one test per new kind, using the existing helpers)

## Read first

- `api/openapi.yaml`: `Budget`, `BudgetInput`, the three budget operations, `AlertKind`, `AlertParams` (new fields), `Severity`
- `migrations/00007_release_features.sql`: `budgets`, the new `alert_rules` defaults
- `internal/alert/evaluate.go` (the `emit` closure, event keys, `rulesByKey`), `internal/alert/evaluate_test.go` (helpers)
- `internal/charge/spend.go` (FX conversion per charge date: reuse its converter, do not reimplement), `internal/fx`
- `internal/quota/score.go` (`ScoreMeter`, `MeterScore.Stale`, `ResetsAt`)
- `internal/agentlink/protocol.go`: `DetailBillingMode`, `DetailExtraUsage` (stored per machine in `account_machines.details`)

## Contract

**Budgets**
- `PUT /api/budgets` upserts by (org, account_id); `account_id` must be an account the caller may see (404 otherwise); `amount.amount_minor > 0`, valid currency; `thresholds` default `[80, 100]`, sorted, unique, 1–1000. Audit `budget.put`; delete audits `budget.delete`.
- Status for the current UTC month: `month_to_date` = confirmed charges (kind subscription, topup, invoice; refunds subtract) with `occurred_on` in the month, for the account or (org budget) all accounts, each converted to the budget currency at its own date; charges without a rate counted in `unconverted_count` and left out. `projected` = month_to_date + the plan price of each auto-renewing account whose `renews_on` falls after today and within this month (converted at today's rate). `percent_used` = month_to_date / amount × 100. Viewers see the org budget and budgets of accounts they can see; the org budget's sums then still cover all accounts (it is the org's money) — say so in a comment.

**Alert kinds** (rules already seeded; `ruleActive` and per-account overrides work as for existing kinds; event keys make each alert fire once)
- `budget`: for each budget and each threshold t in the rule's `thresholds` (or the budget's own when set): when percent_used ≥ t → severity critical if t ≥ 100 else warning, title "Budget: <org or account name> at N% of <amount>", event key `budget:<budget id>:<YYYY-MM>:<t>`. Projection: when projected ≥ amount and percent_used < 100 → info "on track to exceed", key `budget:<id>:<YYYY-MM>:projected`.
- `meter_reset`: for each non-supplemental meter with a reset window: the previous window's last reading had used ≥ `used_percent` (default 90) and the newest reading is in a new window (its `resets_at` is later than the previous window's, or `ScoreMeter` says the previous reading is stale) → info "<account> <meter label> is available again". When the new window began more than 30 minutes before the previous window's announced `resets_at`, say "reset early". Key `reset:<account>:<meter key>:<new window resets_at, RFC 3339>`.
- `spend_anomaly`: (a) today's estimated usage cost for an account (sum of `usage_daily.cost_micro` for today, USD only) ≥ `usage_multiple` × the median of its last 14 days with usage, and ≥ `min_daily_cost` USD → warning "Usage of <account> is N× its usual day"; key `anomaly:usage:<account>:<date>`. (b) at least `topups_per_day` top-up charges (confirmed or suggested) in the last 24 hours on one account → warning "N top-ups on <account> in 24 hours"; key `anomaly:topups:<account>:<date>`.
- `wrong_wallet`: for an active subscription account (`auth_kind = agent_capture`) whose `account_machines.details` on any machine has `billing_mode = api_key` → warning "<harness> on <machine> bills an API key, not <account>", key `wallet:<account>:<machine>`; `extra_usage = on` → info "Paid extra usage is on for <account>: past its limits it charges your card", key `extra_usage:<account>` (once).
- All new alerts go through the same `emit` so channels and email pick them up.


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

- [ ] Budget tests: conversion per charge date with two currencies, a refund, an unconverted charge, the projection with one renewal due later this month; percent_used.
- [ ] One evaluate test per new kind proving it fires once and does not fire below its threshold.
- [ ] Smoke on a hub with seeded data: create an org budget and an account budget through the API, see `month_to_date`/`projected` match a hand calculation, run the evaluation and see the expected alerts in `GET /api/alerts`.

## Outcome (filled in by Opus)

Merged in `cabcc65` after the first attempt. The writer also edited internal/alert/alert.go (kinds and params live there); accepted.
