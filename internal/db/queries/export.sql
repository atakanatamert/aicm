-- Export queries used by internal/httpapi/export.go.
-- Both filter by org_id and account_visible, and join accounts for name and
-- provider.

-- name: ExportUsageRows :many
SELECT
    d.day,
    d.account_id,
    a.name AS account_name,
    a.provider_id,
    d.model,
    d.project,
    d.source,
    d.requests,
    d.input_tokens,
    d.output_tokens,
    d.cache_read_tokens,
    d.cache_write_tokens,
    d.reasoning_tokens,
    d.cost_micro,
    d.cost_currency,
    d.cost_reported
FROM usage_daily d
JOIN accounts a ON a.org_id = d.org_id AND a.id = d.account_id
WHERE d.org_id = sqlc.arg(org_id)
  AND account_visible(sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(is_admin), d.account_id)
  AND d.day >= sqlc.arg(from_day)
  AND d.day <= sqlc.arg(to_day)
  AND (sqlc.narg(account_id)::uuid IS NULL OR d.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(provider_id)::text IS NULL OR a.provider_id = sqlc.narg(provider_id)::text)
ORDER BY d.day, a.name, d.account_id, d.model, d.project, d.source;

-- name: ExportReadingRows :many
SELECT
    r.captured_at,
    m.account_id,
    a.name AS account_name,
    a.provider_id,
    m.key AS meter_key,
    m.label AS meter_label,
    m.unit,
    m.unit_currency,
    r.used,
    r.remaining,
    r.limit_value,
    r.resets_at,
    r.source
FROM quota_readings r
JOIN quota_meters m ON m.org_id = r.org_id AND m.id = r.meter_id
JOIN accounts a ON a.org_id = m.org_id AND a.id = m.account_id
WHERE r.org_id = sqlc.arg(org_id)
  AND account_visible(sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(is_admin), m.account_id)
  AND r.captured_at >= sqlc.arg(from_time)
  AND r.captured_at < sqlc.arg(to_time)
  AND (sqlc.narg(account_id)::uuid IS NULL OR m.account_id = sqlc.narg(account_id)::uuid)
ORDER BY r.captured_at, r.id;
