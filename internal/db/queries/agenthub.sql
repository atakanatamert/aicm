-- Agent ingest: accounts discovered by agents, their machine links and the
-- review actions. Every query filters by org_id.

-- name: UpsertDiscoveredAccount :one
-- One account per (org, provider, external id), even when several machines
-- report it at the same time. An existing account keeps its status and name;
-- the plan name the harness reports fills an empty plan name.
INSERT INTO accounts (org_id, provider_id, name, status, external_id, auth_kind, currency, plan_name, allow_unofficial)
VALUES (
    sqlc.arg(org_id)::uuid,
    sqlc.arg(provider_id),
    sqlc.arg(name),
    'pending',
    sqlc.arg(external_id),
    sqlc.arg(auth_kind),
    sqlc.arg(currency)::char(3),
    sqlc.arg(plan_name),
    sqlc.arg(allow_unofficial)
)
ON CONFLICT (org_id, provider_id, external_id) WHERE external_id IS NOT NULL
DO UPDATE SET plan_name = CASE WHEN accounts.plan_name = '' THEN EXCLUDED.plan_name ELSE accounts.plan_name END
RETURNING *, (xmax = '0'::xid) AS inserted;

-- name: UpsertAccountMachine :exec
-- details are merged so a later report cannot drop an earlier has_api_key.
INSERT INTO account_machines (org_id, account_id, machine_id, harness, details, first_seen_at, last_seen_at)
VALUES (
    sqlc.arg(org_id)::uuid,
    sqlc.arg(account_id)::uuid,
    sqlc.arg(machine_id)::uuid,
    sqlc.arg(harness),
    sqlc.arg(details),
    now(),
    now()
)
ON CONFLICT (account_id, machine_id, harness) DO UPDATE
SET details = account_machines.details || EXCLUDED.details,
    last_seen_at = now();

-- name: ListAgentAccounts :many
-- The config the hub sends to agents: every known external account.
SELECT * FROM accounts
WHERE org_id = sqlc.arg(org_id)::uuid
  AND external_id IS NOT NULL
ORDER BY provider_id, external_id;

-- name: ConfirmAccount :one
-- Moves a pending account to active; a missing row means it is not pending.
UPDATE accounts
SET status = 'active',
    name = COALESCE(sqlc.narg(name)::text, name),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid
  AND id = sqlc.arg(id)::uuid
  AND status = 'pending'
RETURNING *;

-- name: DismissAccount :one
UPDATE accounts
SET status = 'dismissed',
    updated_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid
  AND id = sqlc.arg(id)::uuid
  AND status = 'pending'
RETURNING *;

-- name: ListAccountKeyMachines :many
-- Machines that reported a plain API key for the account.
SELECT machine_id FROM account_machines
WHERE org_id = sqlc.arg(org_id)::uuid
  AND account_id = sqlc.arg(account_id)::uuid
  AND details->>'has_api_key' = 'true';

-- name: ListAccountMachineDetails :many
-- What each machine reported about the account, most recently seen first.
SELECT details FROM account_machines
WHERE org_id = sqlc.arg(org_id)::uuid
  AND account_id = sqlc.arg(account_id)::uuid
ORDER BY last_seen_at DESC, machine_id, harness;

-- name: TouchMachineSeen :exec
-- Incoming messages refresh last_seen_at, at most once a minute.
UPDATE machines
SET last_seen_at = now()
WHERE org_id = sqlc.arg(org_id)::uuid
  AND id = sqlc.arg(id)::uuid
  AND (last_seen_at IS NULL OR last_seen_at < now() - interval '1 minute');
