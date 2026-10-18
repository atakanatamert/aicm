-- name: InsertUsageEvent :execrows
INSERT INTO usage_events (
    org_id, account_id, machine_id, source, message_id, occurred_at, model,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    reasoning_tokens, cost_micro, cost_currency
) VALUES (
    sqlc.arg(org_id)::uuid,
    sqlc.arg(account_id)::uuid,
    sqlc.arg(machine_id)::uuid,
    sqlc.arg(source),
    sqlc.arg(message_id),
    sqlc.arg(occurred_at),
    sqlc.arg(model),
    sqlc.arg(input_tokens),
    sqlc.arg(output_tokens),
    sqlc.arg(cache_read_tokens),
    sqlc.arg(cache_write_tokens),
    sqlc.arg(reasoning_tokens),
    sqlc.narg(cost_micro),
    sqlc.narg(cost_currency)::char(3)
)
ON CONFLICT (machine_id, source, message_id) DO NOTHING;

-- name: RollupUsageEvents :many
SELECT
    model,
    count(*)::bigint AS requests,
    COALESCE(sum(input_tokens), 0)::bigint AS input_tokens,
    COALESCE(sum(output_tokens), 0)::bigint AS output_tokens,
    COALESCE(sum(cache_read_tokens), 0)::bigint AS cache_read_tokens,
    COALESCE(sum(cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(sum(reasoning_tokens), 0)::bigint AS reasoning_tokens,
    count(*) FILTER (WHERE cost_micro IS NULL)::bigint AS uncosted_events,
    COALESCE(sum(input_tokens) FILTER (WHERE cost_micro IS NULL), 0)::bigint AS uncosted_input_tokens,
    COALESCE(sum(output_tokens) FILTER (WHERE cost_micro IS NULL), 0)::bigint AS uncosted_output_tokens,
    COALESCE(sum(cache_read_tokens) FILTER (WHERE cost_micro IS NULL), 0)::bigint AS uncosted_cache_read_tokens,
    COALESCE(sum(cache_write_tokens) FILTER (WHERE cost_micro IS NULL), 0)::bigint AS uncosted_cache_write_tokens,
    count(*) FILTER (WHERE cost_micro IS NOT NULL)::bigint AS reported_events,
    COALESCE(sum(cost_micro) FILTER (WHERE cost_micro IS NOT NULL), 0)::bigint AS reported_cost_micro,
    count(DISTINCT cost_currency) FILTER (WHERE cost_currency IS NOT NULL)::bigint AS cost_currencies,
    COALESCE(min(cost_currency) FILTER (WHERE cost_currency IS NOT NULL), '')::text AS cost_currency
FROM usage_events
WHERE org_id = sqlc.arg(org_id)::uuid
  AND account_id = sqlc.arg(account_id)::uuid
  AND occurred_at >= sqlc.arg(from_time)
  AND occurred_at < sqlc.arg(to_time)
GROUP BY model
ORDER BY model;

-- name: UpsertAgentUsageDaily :exec
INSERT INTO usage_daily (
    org_id, account_id, day, model, source, requests,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    reasoning_tokens, cost_micro, cost_currency, cost_reported, updated_at
) VALUES (
    sqlc.arg(org_id)::uuid,
    sqlc.arg(account_id)::uuid,
    sqlc.arg(day),
    sqlc.arg(model),
    'agent',
    sqlc.arg(requests),
    sqlc.arg(input_tokens),
    sqlc.arg(output_tokens),
    sqlc.arg(cache_read_tokens),
    sqlc.arg(cache_write_tokens),
    sqlc.arg(reasoning_tokens),
    sqlc.arg(cost_micro),
    sqlc.arg(cost_currency)::char(3),
    sqlc.arg(cost_reported),
    now()
)
ON CONFLICT (account_id, day, model, source) DO UPDATE SET
    requests = excluded.requests,
    input_tokens = excluded.input_tokens,
    output_tokens = excluded.output_tokens,
    cache_read_tokens = excluded.cache_read_tokens,
    cache_write_tokens = excluded.cache_write_tokens,
    reasoning_tokens = excluded.reasoning_tokens,
    cost_micro = excluded.cost_micro,
    cost_currency = excluded.cost_currency,
    cost_reported = excluded.cost_reported,
    updated_at = now();

-- name: DeleteAPIUsageDaily :exec
DELETE FROM usage_daily
WHERE org_id = sqlc.arg(org_id)::uuid
  AND account_id = sqlc.arg(account_id)::uuid
  AND source = 'api'
  AND day >= sqlc.arg(from_day)
  AND day < sqlc.arg(to_day);

-- name: InsertAPIUsageDaily :exec
INSERT INTO usage_daily (
    org_id, account_id, day, model, source, requests,
    input_tokens, output_tokens, cache_read_tokens, cache_write_tokens,
    reasoning_tokens, cost_micro, cost_currency, cost_reported, updated_at
) VALUES (
    sqlc.arg(org_id)::uuid,
    sqlc.arg(account_id)::uuid,
    sqlc.arg(day),
    sqlc.arg(model),
    'api',
    sqlc.arg(requests),
    sqlc.arg(input_tokens),
    sqlc.arg(output_tokens),
    sqlc.arg(cache_read_tokens),
    sqlc.arg(cache_write_tokens),
    sqlc.arg(reasoning_tokens),
    sqlc.arg(cost_micro),
    sqlc.arg(cost_currency)::char(3),
    sqlc.arg(cost_reported),
    now()
);

-- name: UsageByDay :many
SELECT
    d.day AS day,
    ''::text AS model,
    ''::text AS account_id,
    ''::text AS account_name,
    ''::text AS provider_id,
    COALESCE(sum(d.requests), 0)::bigint AS requests,
    COALESCE(sum(d.input_tokens), 0)::bigint AS input_tokens,
    COALESCE(sum(d.output_tokens), 0)::bigint AS output_tokens,
    COALESCE(sum(d.cache_read_tokens), 0)::bigint AS cache_read_tokens,
    COALESCE(sum(d.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(sum(d.reasoning_tokens), 0)::bigint AS reasoning_tokens,
    bool_and(d.cost_reported) AS cost_reported,
    COALESCE((
        SELECT jsonb_agg(jsonb_build_object('micro', c.micro, 'currency', c.currency) ORDER BY c.currency)
        FROM (
            SELECT sum(d2.cost_micro)::bigint AS micro, d2.cost_currency::text AS currency
            FROM usage_daily d2
            JOIN accounts a2 ON a2.org_id = d2.org_id AND a2.id = d2.account_id
            WHERE d2.org_id = sqlc.arg(org_id)::uuid
              AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d2.account_id)
              AND d2.day = d.day
              AND (sqlc.narg(account_id)::uuid IS NULL OR d2.account_id = sqlc.narg(account_id)::uuid)
              AND (sqlc.narg(provider_id)::text IS NULL OR a2.provider_id = sqlc.narg(provider_id)::text)
            GROUP BY d2.cost_currency
        ) c
    ), '[]'::jsonb)::text AS cost
FROM usage_daily d
JOIN accounts a ON a.org_id = d.org_id AND a.id = d.account_id
WHERE d.org_id = sqlc.arg(org_id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d.account_id)
  AND d.day >= sqlc.arg(from_day)
  AND d.day <= sqlc.arg(to_day)
  AND (sqlc.narg(account_id)::uuid IS NULL OR d.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(provider_id)::text IS NULL OR a.provider_id = sqlc.narg(provider_id)::text)
GROUP BY d.day
ORDER BY d.day;

-- name: UsageByModel :many
SELECT
    NULL::date AS day,
    d.model AS model,
    ''::text AS account_id,
    ''::text AS account_name,
    ''::text AS provider_id,
    COALESCE(sum(d.requests), 0)::bigint AS requests,
    COALESCE(sum(d.input_tokens), 0)::bigint AS input_tokens,
    COALESCE(sum(d.output_tokens), 0)::bigint AS output_tokens,
    COALESCE(sum(d.cache_read_tokens), 0)::bigint AS cache_read_tokens,
    COALESCE(sum(d.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(sum(d.reasoning_tokens), 0)::bigint AS reasoning_tokens,
    bool_and(d.cost_reported) AS cost_reported,
    COALESCE((
        SELECT jsonb_agg(jsonb_build_object('micro', c.micro, 'currency', c.currency) ORDER BY c.currency)
        FROM (
            SELECT sum(d2.cost_micro)::bigint AS micro, d2.cost_currency::text AS currency
            FROM usage_daily d2
            JOIN accounts a2 ON a2.org_id = d2.org_id AND a2.id = d2.account_id
            WHERE d2.org_id = sqlc.arg(org_id)::uuid
              AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d2.account_id)
              AND d2.model = d.model
              AND (sqlc.narg(account_id)::uuid IS NULL OR d2.account_id = sqlc.narg(account_id)::uuid)
              AND (sqlc.narg(provider_id)::text IS NULL OR a2.provider_id = sqlc.narg(provider_id)::text)
            GROUP BY d2.cost_currency
        ) c
    ), '[]'::jsonb)::text AS cost
FROM usage_daily d
JOIN accounts a ON a.org_id = d.org_id AND a.id = d.account_id
WHERE d.org_id = sqlc.arg(org_id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d.account_id)
  AND d.day >= sqlc.arg(from_day)
  AND d.day <= sqlc.arg(to_day)
  AND (sqlc.narg(account_id)::uuid IS NULL OR d.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(provider_id)::text IS NULL OR a.provider_id = sqlc.narg(provider_id)::text)
GROUP BY d.model
ORDER BY d.model;

-- name: UsageByAccount :many
SELECT
    NULL::date AS day,
    ''::text AS model,
    d.account_id AS account_id,
    a.name AS account_name,
    a.provider_id AS provider_id,
    COALESCE(sum(d.requests), 0)::bigint AS requests,
    COALESCE(sum(d.input_tokens), 0)::bigint AS input_tokens,
    COALESCE(sum(d.output_tokens), 0)::bigint AS output_tokens,
    COALESCE(sum(d.cache_read_tokens), 0)::bigint AS cache_read_tokens,
    COALESCE(sum(d.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(sum(d.reasoning_tokens), 0)::bigint AS reasoning_tokens,
    bool_and(d.cost_reported) AS cost_reported,
    COALESCE((
        SELECT jsonb_agg(jsonb_build_object('micro', c.micro, 'currency', c.currency) ORDER BY c.currency)
        FROM (
            SELECT sum(d2.cost_micro)::bigint AS micro, d2.cost_currency::text AS currency
            FROM usage_daily d2
            JOIN accounts a2 ON a2.org_id = d2.org_id AND a2.id = d2.account_id
            WHERE d2.org_id = sqlc.arg(org_id)::uuid
              AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d2.account_id)
              AND d2.account_id = d.account_id
              AND (sqlc.narg(account_id)::uuid IS NULL OR d2.account_id = sqlc.narg(account_id)::uuid)
              AND (sqlc.narg(provider_id)::text IS NULL OR a2.provider_id = sqlc.narg(provider_id)::text)
            GROUP BY d2.cost_currency
        ) c
    ), '[]'::jsonb)::text AS cost
FROM usage_daily d
JOIN accounts a ON a.org_id = d.org_id AND a.id = d.account_id
WHERE d.org_id = sqlc.arg(org_id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d.account_id)
  AND d.day >= sqlc.arg(from_day)
  AND d.day <= sqlc.arg(to_day)
  AND (sqlc.narg(account_id)::uuid IS NULL OR d.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(provider_id)::text IS NULL OR a.provider_id = sqlc.narg(provider_id)::text)
GROUP BY d.account_id, a.name, a.provider_id
ORDER BY a.name;

-- name: UsageByProvider :many
SELECT
    NULL::date AS day,
    ''::text AS model,
    ''::text AS account_id,
    ''::text AS account_name,
    a.provider_id AS provider_id,
    COALESCE(sum(d.requests), 0)::bigint AS requests,
    COALESCE(sum(d.input_tokens), 0)::bigint AS input_tokens,
    COALESCE(sum(d.output_tokens), 0)::bigint AS output_tokens,
    COALESCE(sum(d.cache_read_tokens), 0)::bigint AS cache_read_tokens,
    COALESCE(sum(d.cache_write_tokens), 0)::bigint AS cache_write_tokens,
    COALESCE(sum(d.reasoning_tokens), 0)::bigint AS reasoning_tokens,
    bool_and(d.cost_reported) AS cost_reported,
    COALESCE((
        SELECT jsonb_agg(jsonb_build_object('micro', c.micro, 'currency', c.currency) ORDER BY c.currency)
        FROM (
            SELECT sum(d2.cost_micro)::bigint AS micro, d2.cost_currency::text AS currency
            FROM usage_daily d2
            JOIN accounts a2 ON a2.org_id = d2.org_id AND a2.id = d2.account_id
            WHERE d2.org_id = sqlc.arg(org_id)::uuid
              AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d2.account_id)
              AND a2.provider_id = a.provider_id
              AND (sqlc.narg(account_id)::uuid IS NULL OR d2.account_id = sqlc.narg(account_id)::uuid)
              AND (sqlc.narg(provider_id)::text IS NULL OR a2.provider_id = sqlc.narg(provider_id)::text)
            GROUP BY d2.cost_currency
        ) c
    ), '[]'::jsonb)::text AS cost
FROM usage_daily d
JOIN accounts a ON a.org_id = d.org_id AND a.id = d.account_id
WHERE d.org_id = sqlc.arg(org_id)::uuid
  AND account_visible(sqlc.arg(org_id)::uuid, sqlc.arg(user_id)::uuid, sqlc.arg(is_admin)::boolean, d.account_id)
  AND d.day >= sqlc.arg(from_day)
  AND d.day <= sqlc.arg(to_day)
  AND (sqlc.narg(account_id)::uuid IS NULL OR d.account_id = sqlc.narg(account_id)::uuid)
  AND (sqlc.narg(provider_id)::text IS NULL OR a.provider_id = sqlc.narg(provider_id)::text)
GROUP BY a.provider_id
ORDER BY a.provider_id;
