-- name: InsertAuditLog :exec
INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_label, action, target_kind, target_id, before, after, ip, request_id)
VALUES (sqlc.arg(org_id), sqlc.arg(actor_kind), sqlc.narg(actor_id), sqlc.arg(actor_label), sqlc.arg(action),
        sqlc.arg(target_kind), sqlc.arg(target_id), sqlc.narg(before), sqlc.narg(after), sqlc.narg(ip), sqlc.arg(request_id));

-- name: ListAuditLog :many
SELECT * FROM audit_log
WHERE org_id = sqlc.arg(org_id)
  AND (sqlc.narg(actor_id)::uuid IS NULL OR actor_id = sqlc.narg(actor_id)::uuid)
  AND (sqlc.narg(target_kind)::text IS NULL OR target_kind = sqlc.narg(target_kind)::text)
  AND (sqlc.narg(target_id)::text IS NULL OR target_id = sqlc.narg(target_id)::text)
  AND (sqlc.narg(cursor)::bigint IS NULL OR id < sqlc.narg(cursor)::bigint)
ORDER BY id DESC
LIMIT sqlc.arg(row_limit);
