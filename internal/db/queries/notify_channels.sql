-- Alert channels besides email and the delivery ledger. The notifier's
-- delivery worker is system-wide by design (it runs from the River queue and
-- reads the org from its job args), so every lookup still takes org_id.

-- name: ListNotificationChannels :many
SELECT * FROM notification_channels
WHERE org_id = sqlc.arg(org_id)
ORDER BY created_at, id;

-- name: GetNotificationChannel :one
SELECT * FROM notification_channels
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: ListEnabledNotificationChannels :many
-- Channels a new alert may be queued for.
SELECT * FROM notification_channels
WHERE org_id = sqlc.arg(org_id) AND enabled
ORDER BY created_at, id;

-- name: GetEnabledNotificationChannel :one
-- One channel for delivery; a disabled or deleted channel returns no row.
SELECT * FROM notification_channels
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id) AND enabled;

-- name: InsertNotificationChannel :one
INSERT INTO notification_channels (
    org_id, kind, name, config, secret_id, min_severity, kinds, enabled
)
VALUES (
    sqlc.arg(org_id), sqlc.arg(kind), sqlc.arg(name), sqlc.arg(config),
    sqlc.narg(secret_id), sqlc.arg(min_severity), sqlc.arg(kinds),
    sqlc.arg(enabled)
)
RETURNING *;

-- name: UpdateNotificationChannel :one
UPDATE notification_channels
SET name = sqlc.arg(name),
    config = sqlc.arg(config),
    secret_id = sqlc.narg(secret_id),
    min_severity = sqlc.arg(min_severity),
    kinds = sqlc.arg(kinds),
    enabled = sqlc.arg(enabled),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: DeleteNotificationChannel :execrows
DELETE FROM notification_channels
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: AlertDeliveryExists :one
SELECT EXISTS (
    SELECT 1 FROM alert_deliveries
    WHERE org_id = sqlc.arg(org_id)
      AND alert_id = sqlc.arg(alert_id)
      AND channel_id = sqlc.arg(channel_id)
);

-- name: InsertAlertDelivery :exec
-- The primary key makes a duplicate delivery a no-op.
INSERT INTO alert_deliveries (org_id, alert_id, channel_id)
VALUES (sqlc.arg(org_id), sqlc.arg(alert_id), sqlc.arg(channel_id))
ON CONFLICT (alert_id, channel_id) DO NOTHING;

-- name: MarkNotificationChannelSent :exec
UPDATE notification_channels
SET last_sent_at = now(), last_error = NULL, updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: MarkNotificationChannelError :exec
UPDATE notification_channels
SET last_error = sqlc.arg(last_error), updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);
