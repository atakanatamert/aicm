-- fx_rates is global reference data, not tenant data, so these queries
-- deliberately have no org_id filter.

-- name: UpsertRate :exec
INSERT INTO fx_rates (rate_date, base, quote, rate, source)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (source, base, quote, rate_date)
DO UPDATE SET rate = EXCLUDED.rate, fetched_at = now();

-- name: CountRates :one
SELECT count(*)
FROM fx_rates;

-- name: NewestRateDate :one
SELECT max(rate_date)::date
FROM fx_rates;

-- name: RateOnOrBefore :one
SELECT rate_date, base, quote, rate, source
FROM fx_rates
WHERE source = sqlc.arg(source)
  AND quote = sqlc.arg(quote)
  AND base = 'EUR'
  AND rate_date <= sqlc.arg(as_of)::date
  AND rate_date >= sqlc.arg(earliest)::date
ORDER BY rate_date DESC
LIMIT 1;
