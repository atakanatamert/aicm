-- Auth queries. Setup is org-wide by design: the hub has exactly one org in
-- this version, so CountUsers and the email lookup have no org filter.

-- name: CountUsers :one
SELECT count(*) FROM users;

-- name: GetUserByEmail :one
-- Login is by email alone (users_email_key is on lower(email)), so there is
-- no org to filter by yet.
SELECT *
FROM users
WHERE lower(email) = lower($1);

-- name: GetUserByID :one
SELECT *
FROM users
WHERE org_id = $1 AND id = $2;

-- name: InsertUser :one
INSERT INTO users (org_id, email, name, password_hash, role)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: UpdateUserPassword :exec
UPDATE users
SET password_hash = $3, updated_at = now()
WHERE org_id = $1 AND id = $2;

-- name: TouchUserLogin :exec
UPDATE users
SET last_login_at = now(), updated_at = now()
WHERE org_id = $1 AND id = $2;

-- name: InsertSession :one
INSERT INTO sessions (org_id, user_id, token_hash, user_agent, ip, expires_at)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING *;

-- name: GetSessionByHash :one
-- Sessions are looked up by their unique token hash only; no org is known
-- before the session is resolved.
SELECT
    s.id AS session_id,
    s.org_id AS session_org_id,
    s.user_id AS session_user_id,
    s.user_agent,
    s.ip AS session_ip,
    s.created_at AS session_created_at,
    s.last_seen_at,
    s.expires_at,
    u.id AS user_id,
    u.org_id AS user_org_id,
    u.email,
    u.name AS user_name,
    u.role,
    u.disabled_at
FROM sessions s
JOIN users u ON u.org_id = s.org_id AND u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- name: TouchSession :exec
UPDATE sessions
SET last_seen_at = now()
WHERE token_hash = $1
  AND last_seen_at < now() - interval '1 minute';

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteOtherSessions :exec
DELETE FROM sessions
WHERE org_id = $1 AND user_id = $2 AND token_hash <> $3;

-- name: GetAPITokenByHash :one
-- API tokens are looked up by their unique token hash only; no org is known
-- before the token is resolved.
SELECT
    t.id AS token_id,
    t.org_id AS token_org_id,
    t.user_id AS token_user_id,
    t.name AS token_name,
    t.last4,
    t.created_at AS token_created_at,
    t.last_used_at,
    t.expires_at,
    t.revoked_at,
    u.id AS user_id,
    u.org_id AS user_org_id,
    u.email,
    u.name AS user_name,
    u.role,
    u.disabled_at
FROM api_tokens t
JOIN users u ON u.org_id = t.org_id AND u.id = t.user_id
WHERE t.token_hash = $1
  AND t.revoked_at IS NULL
  AND (t.expires_at IS NULL OR t.expires_at > now());

-- name: TouchAPIToken :exec
UPDATE api_tokens
SET last_used_at = now()
WHERE token_hash = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: GetOrg :one
SELECT *
FROM orgs
WHERE id = $1;
