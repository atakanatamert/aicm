-- name: InsertAuditLog :exec
INSERT INTO audit_log (org_id, actor_kind, actor_id, actor_label, action, target_kind, target_id, before, after, ip, request_id)
VALUES (sqlc.arg(org_id), sqlc.arg(actor_kind), sqlc.narg(actor_id), sqlc.arg(actor_label), sqlc.arg(action),
        sqlc.arg(target_kind), sqlc.arg(target_id), sqlc.narg(before), sqlc.narg(after), sqlc.narg(ip), sqlc.arg(request_id));
