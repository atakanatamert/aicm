-- Alert rules and the in-app inbox. User-facing queries filter by org_id and
-- apply account_visible to alerts and rules that belong to an account.
--
-- The "System job" queries at the top drive the hourly alert evaluation. That
-- job reads every org by design (docs/tasks/14a-alerts.md), so they are the
-- only queries in the schema without an org_id argument.

-- name: ListAlertAccounts :many
-- System job: every org's active, unarchived accounts.
SELECT * FROM accounts
WHERE status = 'active'
  AND archived_at IS NULL
ORDER BY org_id, id;

-- name: ListAlertRules :many
-- System job: every org's rules.
SELECT * FROM alert_rules
ORDER BY org_id, account_id NULLS FIRST, kind;

-- name: ListAlertMeters :many
-- System job: every org's meters.
SELECT * FROM quota_meters
ORDER BY org_id, account_id, key;

-- name: ListAlertReadings :many
-- System job: every org's readings since a cutoff plus, per meter, the newest
-- one before it (same shape as ListRecentReadings).
SELECT r.* FROM quota_readings r
WHERE r.captured_at >= sqlc.arg(since)
   OR r.id = (SELECT r2.id FROM quota_readings r2
              WHERE r2.meter_id = r.meter_id
              ORDER BY r2.captured_at DESC, r2.id DESC LIMIT 1)
ORDER BY r.meter_id, r.captured_at, r.id;

-- name: ListAlertMachines :many
-- System job: every org's machines that are not revoked.
SELECT * FROM machines
WHERE revoked_at IS NULL
ORDER BY org_id, id;

-- name: InsertAlert :one
-- The unique (org_id, event_key) makes evaluation idempotent: a duplicate
-- returns no row, so the caller never emails or hooks the same event twice.
INSERT INTO alerts (
    org_id, account_id, machine_id, kind, event_key, severity, title, body, data
)
VALUES (
    sqlc.arg(org_id), sqlc.narg(account_id), sqlc.narg(machine_id),
    sqlc.arg(kind), sqlc.arg(event_key), sqlc.arg(severity), sqlc.arg(title),
    sqlc.arg(body), sqlc.arg(data)
)
ON CONFLICT (org_id, event_key) DO NOTHING
RETURNING *;

-- name: ListAlerts :many
SELECT * FROM alerts
WHERE org_id = sqlc.arg(org_id)
  AND (sqlc.narg(unread)::boolean IS NULL OR (read_at IS NULL) = sqlc.narg(unread)::boolean)
  AND (sqlc.narg(cursor_created)::timestamptz IS NULL
       OR (created_at, id) < (sqlc.narg(cursor_created)::timestamptz, sqlc.narg(cursor_id)::uuid))
  AND (account_id IS NULL
       OR account_visible(org_id, sqlc.arg(user_id), sqlc.arg(is_admin), account_id))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit);

-- name: CountUnreadAlerts :one
SELECT count(*) FROM alerts
WHERE org_id = sqlc.arg(org_id)
  AND read_at IS NULL
  AND (account_id IS NULL
       OR account_visible(org_id, sqlc.arg(user_id), sqlc.arg(is_admin), account_id));

-- name: MarkAllAlertsRead :execrows
UPDATE alerts
SET read_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND read_at IS NULL
  AND (account_id IS NULL
       OR account_visible(org_id, sqlc.arg(user_id), sqlc.arg(is_admin), account_id));

-- name: GetAlert :one
SELECT * FROM alerts
WHERE org_id = sqlc.arg(org_id)
  AND id = sqlc.arg(id)
  AND (account_id IS NULL
       OR account_visible(org_id, sqlc.arg(user_id), sqlc.arg(is_admin), account_id));

-- name: UpdateAlert :one
UPDATE alerts
SET read_at = CASE
        WHEN sqlc.narg(read)::boolean IS NULL THEN read_at
        WHEN sqlc.narg(read)::boolean THEN COALESCE(read_at, now())
        ELSE NULL
    END,
    snoozed_until = CASE
        WHEN sqlc.narg(snoozed_until)::timestamptz IS NULL THEN snoozed_until
        ELSE sqlc.narg(snoozed_until)::timestamptz
    END
WHERE org_id = sqlc.arg(org_id)
  AND id = sqlc.arg(id)
  AND (account_id IS NULL
       OR account_visible(org_id, sqlc.arg(user_id), sqlc.arg(is_admin), account_id))
RETURNING *;

-- name: ListAlertRulesForOrg :many
-- Org defaults plus, when account_id is given, that account's overrides.
SELECT * FROM alert_rules
WHERE org_id = sqlc.arg(org_id)
  AND (sqlc.narg(account_id)::uuid IS NULL
       OR account_id IS NULL
       OR account_id = sqlc.narg(account_id)::uuid)
  AND (account_id IS NULL
       OR account_visible(org_id, sqlc.arg(user_id), sqlc.arg(is_admin), account_id))
ORDER BY account_id NULLS FIRST, kind;

-- name: GetAlertRule :one
SELECT * FROM alert_rules
WHERE org_id = sqlc.arg(org_id)
  AND account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::uuid
  AND kind = sqlc.arg(kind);

-- name: UpsertAlertRule :one
INSERT INTO alert_rules (org_id, account_id, kind, enabled, params, snoozed_until)
VALUES (
    sqlc.arg(org_id), sqlc.narg(account_id), sqlc.arg(kind), sqlc.arg(enabled),
    sqlc.arg(params), sqlc.narg(snoozed_until)
)
ON CONFLICT (org_id, account_id, kind) DO UPDATE
SET enabled = excluded.enabled,
    params = excluded.params,
    snoozed_until = excluded.snoozed_until,
    updated_at = now()
RETURNING *;
