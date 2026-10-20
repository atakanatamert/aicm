-- +goose Up
-- Decision 9, revised: accounts of providers whose only data source is an
-- undocumented endpoint (Z.ai) allow it by default.
UPDATE accounts SET allow_unofficial = true WHERE provider_id = 'zai' AND NOT allow_unofficial;

-- +goose Down
SELECT 1;
