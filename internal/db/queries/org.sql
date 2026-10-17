-- name: GetOrgBySlug :one
SELECT id, slug, name, created_at
FROM orgs
WHERE slug = $1;

-- name: GetOrgSettings :one
SELECT *
FROM org_settings
WHERE org_id = $1;
