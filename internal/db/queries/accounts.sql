-- name: ListAccounts :many
SELECT a.*
FROM accounts a
WHERE a.org_id = sqlc.arg(org_id)::uuid
  AND (sqlc.narg(status)::text IS NULL OR a.status = sqlc.narg(status)::text)
  AND (sqlc.arg(include_archived)::boolean OR a.archived_at IS NULL)
  AND (sqlc.narg(provider_id)::text IS NULL OR a.provider_id = sqlc.narg(provider_id)::text)
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, a.id)
ORDER BY a.name, a.id;

-- name: GetAccount :one
SELECT a.*
FROM accounts a
WHERE a.org_id = sqlc.arg(org_id)::uuid
  AND a.id = sqlc.arg(account_id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, a.id);

-- name: InsertAccount :one
INSERT INTO accounts (
    org_id, provider_id, name, status, auth_kind, secret_id, config,
    allow_unofficial, plan_name, price_minor, currency, billing_cycle, renews_on,
    renewal_anchor_day, auto_renew, promo_kind, promo_ends_on, notes
) VALUES (
    sqlc.arg(org_id)::uuid,
    sqlc.arg(provider_id),
    sqlc.arg(name),
    'active',
    sqlc.arg(auth_kind),
    sqlc.narg(secret_id)::uuid,
    sqlc.arg(config),
    sqlc.arg(allow_unofficial),
    sqlc.arg(plan_name),
    sqlc.narg(price_minor),
    sqlc.arg(currency)::char(3),
    sqlc.arg(billing_cycle),
    sqlc.narg(renews_on)::date,
    sqlc.narg(renewal_anchor_day),
    sqlc.arg(auto_renew),
    sqlc.narg(promo_kind),
    sqlc.narg(promo_ends_on)::date,
    sqlc.arg(notes)
)
RETURNING *;

-- name: UpdateAccount :one
UPDATE accounts
SET name = sqlc.arg(name),
    currency = sqlc.arg(currency)::char(3),
    config = sqlc.arg(config),
    allow_unofficial = sqlc.arg(allow_unofficial),
    plan_name = sqlc.arg(plan_name),
    price_minor = sqlc.narg(price_minor),
    billing_cycle = sqlc.arg(billing_cycle),
    renews_on = sqlc.narg(renews_on)::date,
    renewal_anchor_day = sqlc.narg(renewal_anchor_day),
    auto_renew = sqlc.arg(auto_renew),
    promo_kind = sqlc.narg(promo_kind),
    promo_ends_on = sqlc.narg(promo_ends_on)::date,
    notes = sqlc.arg(notes),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid
  AND id = sqlc.arg(id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, accounts.id)
RETURNING *;

-- name: ArchiveAccount :one
UPDATE accounts
SET archived_at = now(), updated_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid
  AND id = sqlc.arg(id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, accounts.id)
RETURNING *;

-- name: SetAccountSecret :one
UPDATE accounts
SET secret_id = sqlc.arg(secret_id)::uuid, updated_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid
  AND id = sqlc.arg(id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, accounts.id)
RETURNING *;

-- name: ListAccountMachines :many
SELECT am.machine_id, m.name AS machine_name, am.harness, am.first_seen_at, am.last_seen_at
FROM account_machines am
JOIN machines m ON m.org_id = am.org_id AND m.id = am.machine_id
WHERE am.org_id = sqlc.arg(org_id)::uuid
  AND am.account_id = sqlc.arg(account_id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, am.account_id)
ORDER BY m.name, am.harness;

-- name: ListRenewals :many
SELECT a.*
FROM accounts a
WHERE a.org_id = sqlc.arg(org_id)::uuid
  AND a.archived_at IS NULL
  AND a.status = 'active'
  AND a.renews_on IS NOT NULL
  AND a.renews_on >= sqlc.arg(today)::date
  AND a.renews_on <= sqlc.arg(until)::date
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, a.id)
ORDER BY a.renews_on, a.name;

-- name: ListAccountsDueForRenewal :many
-- The renewal job is system-wide: it reads accounts across every org by
-- design. Only auto-renewing accounts with a date before today advance.
SELECT *
FROM accounts
WHERE auto_renew
  AND billing_cycle <> 'none'
  AND renews_on IS NOT NULL
  AND renews_on < sqlc.arg(today)::date
ORDER BY renews_on, id;

-- name: SetAccountRenewsOn :exec
UPDATE accounts
SET renews_on = sqlc.arg(renews_on)::date, updated_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid AND id = sqlc.arg(id)::uuid;

-- name: FillDetectedPlan :one
-- Sets a plan an agent detected, only on an active account whose price and
-- renewal date are both still empty, so a user's own entry always wins.
UPDATE accounts
SET plan_name = sqlc.arg(plan_name),
    price_minor = sqlc.arg(price_minor),
    currency = sqlc.arg(currency)::char(3),
    billing_cycle = 'monthly',
    renews_on = sqlc.arg(renews_on)::date,
    auto_renew = true,
    renewal_anchor_day = sqlc.arg(renewal_anchor_day),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
  AND status = 'active' AND price_minor IS NULL AND renews_on IS NULL
RETURNING *;
