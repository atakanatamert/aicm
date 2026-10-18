-- name: ListSyncCandidates :many
-- The dispatcher reads every org by design: it is a system job. Only
-- active, unarchived accounts with a stored key are candidates; the Go side
-- filters by provider capabilities and minimum poll interval.
SELECT id, org_id, provider_id, last_synced_at
FROM accounts
WHERE status = 'active'
  AND archived_at IS NULL
  AND secret_id IS NOT NULL
  AND auth_kind IN ('api_key', 'admin_key');

-- name: GetAccountForSync :one
SELECT * FROM accounts
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: MarkSyncOK :exec
UPDATE accounts
SET last_synced_at = now(), last_sync_error = '', sync_failing_since = NULL
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);

-- name: MarkSyncFailed :exec
UPDATE accounts
SET last_sync_error = sqlc.arg(error_text),
    sync_failing_since = COALESCE(sync_failing_since, now())
WHERE org_id = sqlc.arg(org_id) AND id = sqlc.arg(id);
