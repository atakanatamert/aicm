-- Organization settings and the alert-email queries the notifier runs. The
-- email worker is system-wide by design (it runs from the River queue), so
-- its lookups take the org from the job args.

-- name: UpdateOrgSettings :one
UPDATE org_settings
SET display_currency = sqlc.arg(display_currency),
    fx_source = sqlc.arg(fx_source),
    smtp_host = sqlc.arg(smtp_host),
    smtp_port = sqlc.arg(smtp_port),
    smtp_username = sqlc.arg(smtp_username),
    smtp_password_secret_id = sqlc.narg(smtp_password_secret_id),
    smtp_tls = sqlc.arg(smtp_tls),
    smtp_from = sqlc.arg(smtp_from),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
RETURNING *;

-- name: GetAlertForEmail :one
-- One alert for its org, loaded by the email worker before sending.
SELECT *
FROM alerts
WHERE org_id = sqlc.arg(org_id)
  AND id = sqlc.arg(id);

-- name: ListAlertEmailRecipients :many
-- Enabled users who can see the alert's account; every enabled user for an
-- alert with no account.
SELECT email
FROM users
WHERE org_id = sqlc.arg(org_id)
  AND disabled_at IS NULL
  AND (sqlc.narg(account_id)::uuid IS NULL
       OR account_visible(org_id, users.id, users.role = 'admin', sqlc.narg(account_id)::uuid))
ORDER BY lower(email);

-- name: MarkAlertEmailed :execrows
-- Conditional so a retry after a partial failure never marks twice.
UPDATE alerts
SET emailed_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND id = sqlc.arg(id)
  AND emailed_at IS NULL;
