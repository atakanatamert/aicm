-- Enrollment tokens, machines and the org's hub signing key. Every query
-- filters by org_id.

-- name: ListEnrollTokens :many
SELECT * FROM enroll_tokens
WHERE org_id = sqlc.arg(org_id)
ORDER BY created_at DESC, id;

-- name: InsertEnrollToken :one
INSERT INTO enroll_tokens (org_id, token_hash, created_by, expires_at)
VALUES (sqlc.arg(org_id), sqlc.arg(token_hash), sqlc.arg(created_by), sqlc.narg(expires_at))
RETURNING *;

-- name: RevokeEnrollToken :one
UPDATE enroll_tokens
SET revoked_at = COALESCE(revoked_at, now())
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: GetUsableEnrollToken :one
SELECT * FROM enroll_tokens
WHERE org_id = sqlc.arg(org_id)
  AND token_hash = sqlc.arg(token_hash)
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > now())
FOR UPDATE;

-- name: TouchEnrollToken :one
UPDATE enroll_tokens
SET use_count = use_count + 1,
    last_used_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: ListMachines :many
SELECT * FROM machines
WHERE org_id = sqlc.arg(org_id)
ORDER BY enrolled_at DESC, id;

-- name: GetMachine :one
SELECT * FROM machines
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: GetMachineByFingerprint :one
SELECT * FROM machines
WHERE org_id = sqlc.arg(org_id) AND fingerprint = sqlc.arg(fingerprint);

-- name: GetMachineByTokenHash :one
SELECT * FROM machines
WHERE org_id = sqlc.arg(org_id) AND token_hash = sqlc.arg(token_hash);

-- name: InsertMachine :one
INSERT INTO machines (
    org_id, name, hostname, fingerprint, os, arch, agent_version,
    token_hash, enroll_token_id, last_seen_at
)
VALUES (
    sqlc.arg(org_id), sqlc.arg(name), sqlc.arg(hostname), sqlc.arg(fingerprint),
    sqlc.arg(os), sqlc.arg(arch), sqlc.arg(agent_version), sqlc.arg(token_hash),
    sqlc.narg(enroll_token_id), now()
)
RETURNING *;

-- name: UpdateMachineToken :one
UPDATE machines
SET token_hash = sqlc.arg(token_hash),
    enroll_token_id = sqlc.narg(enroll_token_id)
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: TouchMachine :one
UPDATE machines
SET last_seen_at = now(),
    agent_version = sqlc.arg(agent_version),
    os = sqlc.arg(os),
    arch = sqlc.arg(arch),
    hostname = sqlc.arg(hostname)
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: RenameMachine :one
UPDATE machines
SET name = sqlc.arg(name)
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: RevokeMachine :one
UPDATE machines
SET revoked_at = COALESCE(revoked_at, now())
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: GetHubKey :one
SELECT * FROM hub_keys
WHERE org_id = sqlc.arg(org_id);

-- name: InsertHubKey :one
INSERT INTO hub_keys (org_id, public_key, secret_id)
VALUES (sqlc.arg(org_id), sqlc.arg(public_key), sqlc.arg(secret_id))
ON CONFLICT (org_id) DO NOTHING
RETURNING *;

-- name: ListMachineHarnesses :many
-- Harnesses each machine has reported accounts from, for machines that are
-- offline (the live list comes from the agent connection).
SELECT machine_id, array_agg(DISTINCT harness ORDER BY harness)::text[] AS harnesses
FROM account_machines
WHERE org_id = sqlc.arg(org_id)
GROUP BY machine_id;
