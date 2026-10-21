-- Monthly budgets. account_id NULL is the org-wide budget. The unique
-- (org_id, account_id) with NULLS NOT DISTINCT makes the org budget a single
-- row per org.

-- name: ListBudgetsForOrg :many
-- Every budget in the org with the names the API and alerts render.
SELECT b.*, COALESCE(a.name, '')::text AS account_name, o.name AS org_name
FROM budgets b
JOIN orgs o ON o.id = b.org_id
LEFT JOIN accounts a ON a.org_id = b.org_id AND a.id = b.account_id
WHERE b.org_id = sqlc.arg(org_id)::uuid
ORDER BY b.account_id NULLS FIRST, a.name, b.id;

-- name: ListVisibleBudgetIDs :many
-- The budgets a user may see: the org budget and budgets of visible
-- accounts.
SELECT b.id
FROM budgets b
WHERE b.org_id = sqlc.arg(org_id)::uuid
  AND (b.account_id IS NULL
       OR account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid,
                          sqlc.arg(is_admin)::boolean, b.account_id));

-- name: GetBudgetForAccount :one
-- The budget a put replaces, for the audit log's before image.
SELECT *
FROM budgets
WHERE org_id = sqlc.arg(org_id)::uuid
  AND account_id IS NOT DISTINCT FROM sqlc.narg(account_id)::uuid;

-- name: GetVisibleBudget :one
SELECT b.*
FROM budgets b
WHERE b.org_id = sqlc.arg(org_id)::uuid
  AND b.id = sqlc.arg(id)::uuid
  AND (b.account_id IS NULL
       OR account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid,
                          sqlc.arg(is_admin)::boolean, b.account_id));

-- name: UpsertBudget :one
INSERT INTO budgets (org_id, account_id, amount_minor, currency, thresholds)
VALUES (
    sqlc.arg(org_id)::uuid, sqlc.narg(account_id)::uuid,
    sqlc.arg(amount_minor), sqlc.arg(currency)::char(3), sqlc.arg(thresholds)::integer[]
)
ON CONFLICT (org_id, account_id) DO UPDATE
SET amount_minor = excluded.amount_minor,
    currency = excluded.currency,
    thresholds = excluded.thresholds,
    updated_at = now()
RETURNING *;

-- name: DeleteVisibleBudget :execrows
DELETE FROM budgets b
WHERE b.org_id = sqlc.arg(org_id)::uuid
  AND b.id = sqlc.arg(id)::uuid
  AND (b.account_id IS NULL
       OR account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid,
                          sqlc.arg(is_admin)::boolean, b.account_id));

-- name: ListBudgetCharges :many
-- Confirmed money out in [from_day, to_day). Refunds are rows too and
-- subtract. FX conversion happens per charge in Go, at occurred_on.
SELECT *
FROM charges
WHERE org_id = sqlc.arg(org_id)::uuid
  AND status = 'confirmed'
  AND kind IN ('subscription', 'topup', 'invoice', 'refund')
  AND occurred_on >= sqlc.arg(from_day)::date
  AND occurred_on < sqlc.arg(to_day)::date
ORDER BY occurred_on, id;

-- name: ListBudgetRenewals :many
-- Plan prices still due this month: auto-renewing accounts whose renewal
-- falls after today and before the next month.
SELECT *
FROM accounts
WHERE org_id = sqlc.arg(org_id)::uuid
  AND auto_renew
  AND archived_at IS NULL
  AND renews_on IS NOT NULL
  AND price_minor IS NOT NULL
  AND renews_on > sqlc.arg(today)::date
  AND renews_on < sqlc.arg(month_end)::date
ORDER BY renews_on, id;
