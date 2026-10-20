-- +goose Up
-- Subscription fees inferred from a detected plan's list price, for the months
-- before aicm saw the account. One per account and day.
ALTER TABLE charges DROP CONSTRAINT charges_source_check;
ALTER TABLE charges ADD CONSTRAINT charges_source_check
    CHECK (source IN ('manual', 'renewal', 'detected', 'api', 'estimated'));
CREATE UNIQUE INDEX charges_estimated_once ON charges (account_id, occurred_on) WHERE source = 'estimated';

-- +goose Down
DROP INDEX charges_estimated_once;
DELETE FROM charges WHERE source = 'estimated';
ALTER TABLE charges DROP CONSTRAINT charges_source_check;
ALTER TABLE charges ADD CONSTRAINT charges_source_check
    CHECK (source IN ('manual', 'renewal', 'detected', 'api'));
