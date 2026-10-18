-- name: UpsertMeter :one
-- Provider- and agent-defined meters are upserted on every report. A manual
-- meter with the same key is never overwritten by a report.
INSERT INTO quota_meters (org_id, account_id, key, label, unit, unit_currency, reset_kind, window_minutes, limit_value, manual, supplemental)
VALUES (sqlc.arg(org_id), sqlc.arg(account_id), sqlc.arg(key), sqlc.arg(label), sqlc.arg(unit), sqlc.narg(unit_currency),
        sqlc.arg(reset_kind), sqlc.narg(window_minutes), sqlc.narg(limit_value), sqlc.arg(manual), sqlc.arg(supplemental))
ON CONFLICT (account_id, key) DO UPDATE
SET label = EXCLUDED.label,
    unit = EXCLUDED.unit,
    unit_currency = EXCLUDED.unit_currency,
    reset_kind = EXCLUDED.reset_kind,
    window_minutes = EXCLUDED.window_minutes,
    limit_value = EXCLUDED.limit_value,
    supplemental = EXCLUDED.supplemental,
    updated_at = now()
WHERE quota_meters.org_id = sqlc.arg(org_id) AND quota_meters.manual = EXCLUDED.manual
RETURNING *;

-- name: GetMeter :one
SELECT * FROM quota_meters
WHERE org_id = sqlc.arg(org_id) AND account_id = sqlc.arg(account_id) AND key = sqlc.arg(key);

-- name: InsertReading :execrows
INSERT INTO quota_readings (org_id, meter_id, used, remaining, limit_value, resets_at, captured_at, source, machine_id)
VALUES (sqlc.arg(org_id), sqlc.arg(meter_id), sqlc.narg(used), sqlc.narg(remaining), sqlc.narg(limit_value),
        sqlc.narg(resets_at), sqlc.arg(captured_at), sqlc.arg(source), sqlc.narg(machine_id))
ON CONFLICT (meter_id, captured_at, source) DO NOTHING;

-- name: ListVisibleQuotaAccounts :many
-- Active, unarchived accounts the user may see that have a meter or a renewal date.
SELECT a.*
FROM accounts a
WHERE a.org_id = sqlc.arg(org_id)
  AND a.status = 'active'
  AND a.archived_at IS NULL
  AND (sqlc.narg(account_id)::uuid IS NULL OR a.id = sqlc.narg(account_id)::uuid)
  AND account_visible(sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(is_admin), a.id)
  AND (a.renews_on IS NOT NULL OR EXISTS (SELECT 1 FROM quota_meters m WHERE m.org_id = a.org_id AND m.account_id = a.id))
ORDER BY a.name;

-- name: ListMetersForAccounts :many
SELECT * FROM quota_meters
WHERE org_id = sqlc.arg(org_id) AND account_id = ANY(sqlc.arg(account_ids)::uuid[])
ORDER BY account_id, key;

-- name: ListRecentReadings :many
-- Readings since a cutoff plus, per meter, the newest one before it, so a
-- meter that hasn't reported recently still has its latest reading.
SELECT r.* FROM quota_readings r
WHERE r.org_id = sqlc.arg(org_id)
  AND r.meter_id = ANY(sqlc.arg(meter_ids)::uuid[])
  AND (r.captured_at >= sqlc.arg(since)
       OR r.id = (SELECT r2.id FROM quota_readings r2
                  WHERE r2.meter_id = r.meter_id AND r2.org_id = r.org_id
                  ORDER BY r2.captured_at DESC, r2.id DESC LIMIT 1))
ORDER BY r.meter_id, r.captured_at, r.id;

-- name: PreviousReading :one
-- The newest reading of the same meter captured before this one. Used to
-- notice a balance that went up.
SELECT * FROM quota_readings
WHERE org_id = sqlc.arg(org_id)
  AND meter_id = sqlc.arg(meter_id)
  AND captured_at < sqlc.arg(before_time)
ORDER BY captured_at DESC, id DESC
LIMIT 1;

-- name: ListMeterReadings :many
SELECT * FROM quota_readings
WHERE org_id = sqlc.arg(org_id)
  AND meter_id = sqlc.arg(meter_id)
  AND captured_at >= sqlc.arg(from_time)
  AND captured_at < sqlc.arg(to_time)
ORDER BY captured_at DESC
LIMIT sqlc.arg(row_limit);

-- name: GetVisibleAccount :one
SELECT a.* FROM accounts a
WHERE a.org_id = sqlc.arg(org_id) AND a.id = sqlc.arg(account_id)
  AND account_visible(sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(is_admin), a.id);
