-- +goose Up
-- Day of month the subscription renews on, kept so monthly roll-forward
-- returns to the 31st after a short month.
ALTER TABLE accounts ADD COLUMN renewal_anchor_day smallint CHECK (renewal_anchor_day BETWEEN 1 AND 31);
UPDATE accounts SET renewal_anchor_day = extract(day FROM renews_on) WHERE renews_on IS NOT NULL;
-- +goose Down
ALTER TABLE accounts DROP COLUMN renewal_anchor_day;
