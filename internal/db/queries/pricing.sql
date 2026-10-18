-- name: GetModelPrice :one
SELECT model, provider, currency,
       input_micro_per_mtok, output_micro_per_mtok,
       cache_read_micro_per_mtok, cache_write_micro_per_mtok
FROM model_prices
WHERE model = sqlc.arg(model)::text
ORDER BY (provider = '') DESC, provider
LIMIT 1;

-- name: UpsertModelPrice :exec
INSERT INTO model_prices (
    model, provider, currency,
    input_micro_per_mtok, output_micro_per_mtok,
    cache_read_micro_per_mtok, cache_write_micro_per_mtok,
    source, updated_at
) VALUES (
    sqlc.arg(model), sqlc.arg(provider), sqlc.arg(currency),
    sqlc.arg(input_micro_per_mtok), sqlc.arg(output_micro_per_mtok),
    sqlc.arg(cache_read_micro_per_mtok), sqlc.arg(cache_write_micro_per_mtok),
    'litellm', now()
)
ON CONFLICT (model, provider) DO UPDATE SET
    currency = excluded.currency,
    input_micro_per_mtok = excluded.input_micro_per_mtok,
    output_micro_per_mtok = excluded.output_micro_per_mtok,
    cache_read_micro_per_mtok = excluded.cache_read_micro_per_mtok,
    cache_write_micro_per_mtok = excluded.cache_write_micro_per_mtok,
    source = excluded.source,
    updated_at = now();
