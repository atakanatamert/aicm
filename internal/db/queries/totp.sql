-- Two-factor and OIDC sign-in queries. Users are looked up by org and id
-- except the OIDC identity lookup: like login by email, no org is known
-- before the user is resolved.

-- name: SetTotpPendingSecret :execrows
UPDATE users
SET totp_pending_secret_id = sqlc.arg(totp_secret_id), updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id) AND totp_enabled_at IS NULL;

-- name: EnableTotp :execrows
UPDATE users
SET totp_secret_id = totp_pending_secret_id,
    totp_pending_secret_id = NULL,
    totp_enabled_at = now(),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
  AND totp_enabled_at IS NULL AND totp_pending_secret_id = sqlc.arg(pending_secret_id);

-- name: DisableTotp :execrows
UPDATE users
SET totp_secret_id = NULL,
    totp_pending_secret_id = NULL,
    totp_enabled_at = NULL,
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: DeleteRecoveryCodes :exec
DELETE FROM user_recovery_codes
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id);

-- name: InsertRecoveryCode :exec
INSERT INTO user_recovery_codes (org_id, user_id, code_hash)
VALUES (sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(code_hash));

-- name: CountUnusedRecoveryCodes :one
SELECT count(*) FROM user_recovery_codes
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id) AND used_at IS NULL;

-- name: ConsumeRecoveryCode :execrows
UPDATE user_recovery_codes
SET used_at = now()
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id)
  AND code_hash = sqlc.arg(code_hash) AND used_at IS NULL;

-- name: DeleteExpiredLoginChallenges :exec
DELETE FROM login_challenges
WHERE org_id = sqlc.arg(org_id) AND user_id = sqlc.arg(user_id) AND expires_at <= now();

-- name: InsertLoginChallenge :one
INSERT INTO login_challenges (org_id, user_id, token_hash, expires_at)
VALUES (sqlc.arg(org_id), sqlc.arg(user_id), sqlc.arg(token_hash), sqlc.arg(expires_at))
RETURNING *;

-- name: GetLoginChallenge :one
-- Challenge lookup by unique token hash; the org is not known beforehand.
SELECT c.id, c.org_id, c.user_id, c.token_hash, c.attempts, c.expires_at, c.created_at,
       u.email, u.role, u.disabled_at
FROM login_challenges c
JOIN users u ON u.org_id = c.org_id AND u.id = c.user_id
WHERE c.token_hash = sqlc.arg(token_hash);

-- name: IncrementLoginChallengeAttempts :one
UPDATE login_challenges
SET attempts = attempts + 1
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
RETURNING attempts;

-- name: DeleteLoginChallenge :execrows
DELETE FROM login_challenges
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: GetUserByOIDCIdentity :one
-- OIDC sign-in is like email login: no org is known before the user is found.
SELECT * FROM users
WHERE oidc_issuer = sqlc.arg(oidc_issuer) AND oidc_subject = sqlc.arg(oidc_subject);

-- name: LinkUserOIDC :one
UPDATE users
SET oidc_issuer = sqlc.arg(oidc_issuer),
    oidc_subject = sqlc.arg(oidc_subject),
    updated_at = now()
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id)
  AND oidc_subject IS NULL
RETURNING *;
