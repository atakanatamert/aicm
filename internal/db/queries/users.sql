-- Users, groups, API tokens and account grants. Every query filters by
-- org_id; API tokens are additionally scoped to their user.

-- name: ListAPITokens :many
SELECT * FROM api_tokens
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id)
ORDER BY created_at DESC, id;

-- name: InsertAPIToken :one
INSERT INTO api_tokens (org_id, user_id, name, token_hash, last4, expires_at)
VALUES (sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(name), sqlc.arg(token_hash),
        sqlc.arg(last4), sqlc.narg(expires_at))
RETURNING *;

-- name: RevokeAPIToken :one
UPDATE api_tokens
SET revoked_at = now()
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id);

-- name: ListUsers :many
SELECT * FROM users
WHERE org_id = sqlc.arg(org_id)
ORDER BY lower(email);

-- name: CountEnabledAdmins :one
SELECT count(*) FROM users
WHERE org_id = sqlc.arg(org_id) AND role = 'admin' AND disabled_at IS NULL;

-- name: UpdateUser :one
UPDATE users
SET name = sqlc.arg(name),
    role = sqlc.arg(role),
    disabled_at = sqlc.narg(disabled_at),
    password_hash = sqlc.arg(password_hash),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: ListGroups :many
SELECT * FROM groups
WHERE org_id = sqlc.arg(org_id)
ORDER BY lower(name);

-- name: GetGroup :one
SELECT * FROM groups
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: InsertGroup :one
INSERT INTO groups (org_id, name)
VALUES (sqlc.arg(org_id), sqlc.arg(name))
RETURNING *;

-- name: UpdateGroupName :one
UPDATE groups
SET name = sqlc.arg(name)
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: DeleteGroup :one
DELETE FROM groups
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING *;

-- name: ListGroupMembers :many
SELECT user_id FROM group_members
WHERE org_id = sqlc.arg(org_id) AND group_id = sqlc.arg(group_id)
ORDER BY user_id;

-- name: ListGroupMemberships :many
SELECT group_id, user_id FROM group_members
WHERE org_id = sqlc.arg(org_id)
ORDER BY group_id, user_id;

-- name: DeleteGroupMembers :exec
DELETE FROM group_members
WHERE org_id = sqlc.arg(org_id) AND group_id = sqlc.arg(group_id);

-- name: InsertGroupMember :exec
INSERT INTO group_members (org_id, group_id, user_id)
VALUES (sqlc.arg(org_id), sqlc.arg(group_id), sqlc.arg(user_id));

-- name: GetAccountByID :one
SELECT id FROM accounts
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: ListAccountGrants :many
SELECT * FROM account_grants
WHERE org_id = sqlc.arg(org_id) AND account_id = sqlc.arg(account_id)
ORDER BY user_id NULLS LAST, group_id NULLS LAST;

-- name: DeleteAccountGrants :exec
DELETE FROM account_grants
WHERE org_id = sqlc.arg(org_id) AND account_id = sqlc.arg(account_id);

-- name: InsertAccountGrant :exec
INSERT INTO account_grants (org_id, account_id, user_id, group_id)
VALUES (sqlc.arg(org_id), sqlc.arg(account_id), sqlc.narg(user_id), sqlc.narg(group_id));
