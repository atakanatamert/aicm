# 14a – Alerts: rule evaluation, inbox, rules API

**Writer:** DeepSeek Flash
**Depends on:** 09, 10, 13, 18a
**License of touched packages:** AGPL-3.0-or-later

## Goal

An hourly River job evaluates every alert rule and writes one row in `alerts` per event (renewal 7 and 1 days before, promo ending, low balance, meter nearly exhausted, quota going to waste, sync failing, agent offline). Users see the inbox, mark alerts read, snooze them, and edit rules. Sending email is task 14b, which consumes the `OnNewAlert` hook defined here.

## Files to touch

- `internal/alert/alert.go` (new): service (inbox, rules)
- `internal/alert/evaluate.go` (new): rule evaluation
- `internal/alert/evaluate_test.go` (new)
- `internal/alert/job.go` (new): River worker + periodic job
- `internal/db/queries/alerts.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/alerts.go` (new): `ListAlerts`, `MarkAllAlertsRead`, `UpdateAlert`, `ListAlertRules`, `UpsertAlertRule`
- `internal/httpapi/unimplemented.go` (edit: delete those five)
- `internal/httpapi/httpapi.go` (edit: `Deps.Alerts *alert.Service`)
- `cmd/aicm/main.go` (edit: build the service, register the job)

Touch nothing else.

## Read first

- `CLAUDE.md`, `docs/PLAN.md` section "Alerts, audit log and CSV export"
- `migrations/00001_init.sql`: `alert_rules` (org defaults seeded; `UNIQUE NULLS NOT DISTINCT (org_id, account_id, kind)`), `alerts` (`UNIQUE (org_id, event_key)`)
- `api/openapi.yaml`: alert operations, `Alert`, `AlertPage`, `AlertRule`, `AlertRuleWrite`, `AlertParams`
- `internal/quota/quota.go` (`Overview` returns scored meters per account) and `score.go` (`MeterScore`: `Expiring`, `HoursToReset`, `HoursToEmpty`, `Exhausted`, `Stale`)
- `internal/account/account.go`, `internal/machine/machine.go`, `internal/provider/registry/registry.go` (links for alert bodies)
- `internal/audit/audit.go`, `internal/auth/principal.go`, `internal/jobs/jobs.go`, `internal/fx/job.go`
- `internal/dbtest/dbtest.go`

## Contract

```go
package alert

// NewAlert is passed to the hook after an alert row is inserted for the
// first time (never for a duplicate event_key).
type NewAlert struct { Alert db.Alert }
type Hook func(ctx context.Context, a NewAlert) error

type Service struct { /* pool, queries, quota, providers, log, hook */ }
func NewService(pool *pgxpool.Pool, log *slog.Logger, quota *quota.Service, providers []provider.Provider) *Service
func (s *Service) SetHook(h Hook) // task 14b sets the email hook

// Evaluate checks every org's rules at now and inserts new alerts
// (INSERT … ON CONFLICT (org_id, event_key) DO NOTHING RETURNING). It
// returns how many were new. Calls the hook for each new row; a hook error
// is logged, not returned.
func (s *Service) Evaluate(ctx context.Context, now time.Time) (int, error)

func (s *Service) List(ctx context.Context, p auth.Principal, unread *bool, cursor string, limit int) ([]db.Alert, int /*unread count*/, string /*next cursor*/, error)
func (s *Service) MarkAllRead(ctx context.Context, p auth.Principal) error
func (s *Service) Update(ctx context.Context, p auth.Principal, id string, read *bool, snoozedUntil *time.Time) (db.Alert, error)
func (s *Service) Rules(ctx context.Context, p auth.Principal, accountID *string) ([]db.AlertRule, error)
func (s *Service) UpsertRule(ctx context.Context, p auth.Principal, w RuleWrite) (db.AlertRule, error) // org default (account_id nil) needs admin → ErrForbidden
```

Visibility: alerts with an `account_id` are visible only when `account_visible(...)` allows it; alerts without one (agent offline) are visible to everyone in the org. Rules follow the same rule.

### Effective rule

For an account and kind, the rule is the row with that `account_id` if present, else the org default row (`account_id IS NULL`). Skip when `enabled = false` or `snoozed_until > now`.

### Events and keys (exact)

All dates UTC. `{a}` = account id, `{m}` = machine id, `{k}` = meter key.

| Kind | Fires when (params) | event_key | Severity |
| --- | --- | --- | --- |
| renewal | `renews_on − today` equals one of `days_before` (default `[7, 1]`), account active, not archived, `auto_renew` true | `renewal:{a}:{renews_on}:{n}d` | warning (critical when n ≤ 1) |
| promo_ending | `promo_ends_on − today` equals one of `days_before` (default `[3]`) | `promo:{a}:{promo_ends_on}:{n}d` | warning |
| low_balance | a currency meter with reset `never` has remaining < `below_percent`% of its highest reading in the last 30 days, **or** `HoursToEmpty` < `days_left`×24 | `low_balance:{a}:{k}:{YYYY-MM-DD}` (one per day) | warning |
| meter_exhausted | a meter with a limit has used ≥ `used_percent`% (percent meters: used ≥ value) and is not stale | `meter:{a}:{k}:{resets_at or "none"}` (one per window) | critical when ≥ 100% else warning |
| quota_waste | `Expiring` ≥ `unused_percent`/100 and `HoursToReset` ≤ `hours_before_reset` | `waste:{a}:{k}:{resets_at}` | info |
| sync_failing | `sync_failing_since` older than `after_hours` | `sync:{a}:{sync_failing_since date}` | warning |
| agent_offline | a non-revoked machine's `last_seen_at` older than `after_hours` | `offline:{m}:{last_seen_at date}` | warning |

Titles and bodies use the provider's own words and the account name, e.g. title `Claude Max renews in 7 days`, body `$200.00 on 2026-10-27. Manage: https://claude.ai/settings/billing`. Put the price (billed currency) and the catalog links (`manage_subscription`, `cancel`, `top_up`, `billing`) into `data` as JSON so the UI and email can render buttons.

Evaluation reads all orgs (system job; comment the SQL).

### Job

`EvaluateArgs{}` kind `"alert_evaluate"`, hourly at minute 5 (`RunOnStart: true`), worker calls `Evaluate(ctx, time.Now())`; `func PeriodicJob() *river.PeriodicJob`.

### Handlers

`ListAlerts` (cursor = base64url of `created_at|id`, newest first), `MarkAllAlertsRead`, `UpdateAlert` (`read`, `snoozed_until`), `ListAlertRules`, `UpsertAlertRule` (validate params per kind: only the fields listed above, numbers in range; 400 otherwise). Audit: `alert_rule.upsert`, `alert.update`, `alert.read_all`.

## Done when

- [ ] `evaluate_test.go` (with `dbtest.New`; build accounts, meters, readings, machines with SQL; call `Evaluate` with fixed `now` values):
  - `TestRenewalAlerts`: renews in 7 days → one alert; evaluating again at the same `now` → still one; at 6 days → none new; at 1 day → a second alert (critical); `auto_renew = false` → none.
  - `TestPerAccountOverride`: an account rule with `enabled = false` suppresses the org default for that account only.
  - `TestMeterExhaustedOncePerWindow`: 92% used → one warning; 95% later in the same window → no new alert; a new window (`resets_at` changed) at 91% → a new alert.
  - `TestQuotaWaste`: 60% unused 10 h before reset with no burn → one info alert; 30% unused → none.
  - `TestAgentOffline`: machine last seen 49 h ago → one alert; revoked machine → none.
  - `TestHookOnlyForNewAlerts`: the hook runs once per new alert, not for duplicates.
- [ ] Smoke on a running hub: `GET /api/alerts` lists alerts after the job runs (seed a renewal 7 days out with SQL); `PATCH` marks one read; `PUT /api/alert-rules` as editor without `account_id` → 403.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

Merged in `b96f8ab` after the first attempt. Smoke: renewal alert after the on-start job, read/snooze, editor 403 on org defaults.
