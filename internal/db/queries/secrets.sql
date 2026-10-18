-- name: GetActiveOrgKey :one
SELECT id, org_id, version, wrapped_key, master_key_version, active, created_at
FROM org_keys
WHERE org_id = sqlc.arg(org_id) AND active;

-- name: GetOrgKey :one
SELECT id, org_id, version, wrapped_key, master_key_version, active, created_at
FROM org_keys
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: InsertOrgKey :one
INSERT INTO org_keys (org_id, version, wrapped_key, master_key_version)
VALUES (sqlc.arg(org_id), sqlc.arg(version), sqlc.arg(wrapped_key), sqlc.arg(master_key_version))
ON CONFLICT (org_id, version) DO NOTHING
RETURNING id, org_id, version, wrapped_key, master_key_version, active, created_at;

-- name: ListOrgKeysForRotation :many
-- rotation reads every org by design
SELECT id, org_id, version, wrapped_key, master_key_version, active, created_at
FROM org_keys
ORDER BY org_id, version
FOR UPDATE;

-- name: UpdateOrgKeyWrap :exec
UPDATE org_keys
SET wrapped_key = sqlc.arg(wrapped_key),
    master_key_version = sqlc.arg(master_key_version)
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: InsertSecret :one
INSERT INTO secrets (id, org_id, org_key_id, nonce, ciphertext, last4)
VALUES (sqlc.arg(id), sqlc.arg(org_id), sqlc.arg(org_key_id), sqlc.arg(nonce), sqlc.arg(ciphertext), sqlc.arg(last4))
RETURNING id, last4, updated_at;

-- name: UpdateSecret :one
UPDATE secrets
SET org_key_id = sqlc.arg(org_key_id),
    nonce = sqlc.arg(nonce),
    ciphertext = sqlc.arg(ciphertext),
    last4 = sqlc.arg(last4),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING id, last4, updated_at;

-- name: GetSecret :one
SELECT id, org_id, org_key_id, nonce, ciphertext, last4, created_at, updated_at
FROM secrets
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: DeleteSecret :execrows
DELETE FROM secrets
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);
