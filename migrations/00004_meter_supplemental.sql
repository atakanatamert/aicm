-- +goose Up
-- Supplemental meters (Codex credits) are drawn on only after the plan's own
-- limits run out; scoring skips them.
ALTER TABLE quota_meters ADD COLUMN supplemental boolean NOT NULL DEFAULT false;
UPDATE quota_meters m SET supplemental = true
FROM accounts a
WHERE a.id = m.account_id AND a.provider_id = 'chatgpt' AND m.key = 'credits';

-- +goose Down
ALTER TABLE quota_meters DROP COLUMN supplemental;
