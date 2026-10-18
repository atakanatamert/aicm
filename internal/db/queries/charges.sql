-- Money out: every payment is a row in its billed currency.

-- name: InsertCharge :one
INSERT INTO charges (
    org_id, account_id, kind, status, source, amount_minor, currency,
    occurred_on, period_start, period_end, description, created_by
) VALUES (
    sqlc.arg(org_id), sqlc.arg(account_id), sqlc.arg(kind), sqlc.arg(status),
    sqlc.arg(source), sqlc.arg(amount_minor), sqlc.arg(currency),
    sqlc.arg(occurred_on)::date, sqlc.narg(period_start)::date, sqlc.narg(period_end)::date,
    sqlc.arg(description), sqlc.narg(created_by)::uuid
)
RETURNING *;

-- name: InsertRenewalCharge :one
-- One automatic subscription fee per renewal date. Running the roll-forward
-- again for the same date inserts nothing.
INSERT INTO charges (
    org_id, account_id, kind, status, source, amount_minor, currency,
    occurred_on, period_start, period_end, description
) VALUES (
    sqlc.arg(org_id), sqlc.arg(account_id), 'subscription', 'confirmed', 'renewal',
    sqlc.arg(amount_minor), sqlc.arg(currency), sqlc.arg(occurred_on)::date,
    sqlc.arg(period_start)::date, sqlc.arg(period_end)::date, sqlc.arg(description)
)
ON CONFLICT (account_id, occurred_on) WHERE source = 'renewal' DO NOTHING
RETURNING *;

-- name: GetVisibleCharge :one
SELECT c.*
FROM charges c
WHERE c.org_id = sqlc.arg(org_id)
  AND c.id = sqlc.arg(charge_id)
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, c.account_id);

-- name: UpdateCharge :one
UPDATE charges
SET status = sqlc.arg(status),
    amount_minor = sqlc.arg(amount_minor),
    currency = sqlc.arg(currency),
    occurred_on = sqlc.arg(occurred_on)::date,
    description = sqlc.arg(description),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)
  AND id = sqlc.arg(charge_id)
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, account_id)
RETURNING *;

-- name: ListCharges :many
-- Newest first. The cursor is the (occurred_on, id) pair of the last row of
-- the previous page; a nil cursor starts at the newest charge.
SELECT c.*, a.name AS account_name, a.provider_id AS provider_id
FROM charges c
JOIN accounts a ON a.org_id = c.org_id AND a.id = c.account_id
WHERE c.org_id = sqlc.arg(org_id)
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, c.account_id)
  AND (sqlc.narg(from_day)::date IS NULL OR c.occurred_on >= sqlc.narg(from_day)::date)
  AND (sqlc.narg(to_day)::date IS NULL OR c.occurred_on <= sqlc.narg(to_day)::date)
  AND (sqlc.narg(account_id)::uuid IS NULL OR c.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(kind)::text IS NULL OR c.kind = sqlc.narg(kind)::text)
  AND c.status = sqlc.arg(status)::text
  AND (sqlc.narg(cursor_date)::date IS NULL
       OR c.occurred_on < sqlc.narg(cursor_date)::date
       OR (c.occurred_on = sqlc.narg(cursor_date)::date AND c.id < sqlc.narg(cursor_id)::uuid))
ORDER BY c.occurred_on DESC, c.id DESC
LIMIT sqlc.narg(row_limit)::int;
