-- +goose Up
-- Features from docs/research/feature-gaps.md: alert channels, budgets, new
-- alert kinds, usage by project, two-factor and OIDC sign-in.

-- New alert kinds and their org-wide defaults.
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_kind_check;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_kind_check CHECK (kind IN (
    'renewal', 'promo_ending', 'low_balance', 'meter_exhausted', 'quota_waste', 'sync_failing', 'agent_offline',
    'meter_reset', 'budget', 'spend_anomaly', 'wrong_wallet'));

INSERT INTO alert_rules (org_id, kind, params)
SELECT o.id, d.kind, d.params::jsonb
FROM orgs o
CROSS JOIN (VALUES
    ('meter_reset',   '{"used_percent": 90}'),
    ('budget',        '{"thresholds": [80, 100]}'),
    ('spend_anomaly', '{"usage_multiple": 3, "min_daily_cost": 5, "topups_per_day": 2}'),
    ('wrong_wallet',  '{}')
) AS d (kind, params)
ON CONFLICT DO NOTHING;

-- Alert channels besides email. The URL or token that grants access to the
-- channel is a secret (webhook URLs carry their token); config holds the rest.
CREATE TABLE notification_channels (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    kind         text NOT NULL CHECK (kind IN ('webhook', 'ntfy', 'telegram', 'discord', 'slack')),
    name         text NOT NULL,
    config       jsonb NOT NULL DEFAULT '{}',
    secret_id    uuid,
    -- Lowest severity sent; kinds empty means every kind.
    min_severity text NOT NULL DEFAULT 'warning' CHECK (min_severity IN ('info', 'warning', 'critical')),
    kinds        text[] NOT NULL DEFAULT '{}',
    enabled      boolean NOT NULL DEFAULT true,
    last_sent_at timestamptz,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, secret_id) REFERENCES secrets (org_id, id) ON DELETE SET NULL (secret_id)
);

-- One row per alert and channel once delivered, so restarts never resend.
CREATE TABLE alert_deliveries (
    org_id       uuid NOT NULL,
    alert_id     uuid NOT NULL REFERENCES alerts (id) ON DELETE CASCADE,
    channel_id   uuid NOT NULL,
    delivered_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (alert_id, channel_id),
    FOREIGN KEY (org_id, channel_id) REFERENCES notification_channels (org_id, id) ON DELETE CASCADE
);

-- Monthly budgets: account_id NULL is the org-wide budget.
CREATE TABLE budgets (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    account_id   uuid,
    amount_minor bigint NOT NULL CHECK (amount_minor > 0),
    currency     char(3) NOT NULL,
    -- Percent of the budget at which an alert fires.
    thresholds   integer[] NOT NULL DEFAULT '{80,100}',
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    UNIQUE NULLS NOT DISTINCT (org_id, account_id),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE
);

-- Usage by project: the repository or working directory's base name the
-- agent saw; '' when unknown or for provider-reported usage.
ALTER TABLE usage_events ADD COLUMN project text NOT NULL DEFAULT '';
ALTER TABLE usage_daily ADD COLUMN project text NOT NULL DEFAULT '';
ALTER TABLE usage_daily DROP CONSTRAINT usage_daily_pkey;
ALTER TABLE usage_daily ADD PRIMARY KEY (account_id, day, model, source, project);

-- Two-factor sign-in (TOTP, RFC 6238). The pending secret is kept until the
-- user confirms a code; recovery codes are stored hashed and used once.
ALTER TABLE users
    ADD COLUMN totp_secret_id uuid,
    ADD COLUMN totp_pending_secret_id uuid,
    ADD COLUMN totp_enabled_at timestamptz,
    ADD COLUMN oidc_issuer text,
    ADD COLUMN oidc_subject text,
    ADD FOREIGN KEY (org_id, totp_secret_id) REFERENCES secrets (org_id, id) ON DELETE SET NULL (totp_secret_id),
    ADD FOREIGN KEY (org_id, totp_pending_secret_id) REFERENCES secrets (org_id, id) ON DELETE SET NULL (totp_pending_secret_id);
CREATE UNIQUE INDEX users_oidc_identity ON users (oidc_issuer, oidc_subject) WHERE oidc_subject IS NOT NULL;

CREATE TABLE user_recovery_codes (
    id        uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id    uuid NOT NULL,
    user_id   uuid NOT NULL,
    code_hash bytea NOT NULL,
    used_at   timestamptz,
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, id) ON DELETE CASCADE
);
CREATE INDEX user_recovery_codes_user ON user_recovery_codes (user_id);

-- A password that passed but still needs a second factor: a short-lived,
-- single-use challenge (hash of the token sent to the browser).
CREATE TABLE login_challenges (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL,
    user_id    uuid NOT NULL,
    token_hash bytea NOT NULL UNIQUE,
    attempts   integer NOT NULL DEFAULT 0,
    expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, id) ON DELETE CASCADE
);

-- +goose Down
DROP TABLE login_challenges;
DROP TABLE user_recovery_codes;
DROP INDEX users_oidc_identity;
ALTER TABLE users
    DROP COLUMN oidc_subject,
    DROP COLUMN oidc_issuer,
    DROP COLUMN totp_enabled_at,
    DROP COLUMN totp_pending_secret_id,
    DROP COLUMN totp_secret_id;
DELETE FROM usage_daily WHERE project <> '';
ALTER TABLE usage_daily DROP CONSTRAINT usage_daily_pkey;
ALTER TABLE usage_daily ADD PRIMARY KEY (account_id, day, model, source);
ALTER TABLE usage_daily DROP COLUMN project;
ALTER TABLE usage_events DROP COLUMN project;
DROP TABLE budgets;
DROP TABLE alert_deliveries;
DROP TABLE notification_channels;
DELETE FROM alert_rules WHERE kind IN ('meter_reset', 'budget', 'spend_anomaly', 'wrong_wallet');
ALTER TABLE alert_rules DROP CONSTRAINT alert_rules_kind_check;
ALTER TABLE alert_rules ADD CONSTRAINT alert_rules_kind_check CHECK (kind IN (
    'renewal', 'promo_ending', 'low_balance', 'meter_exhausted', 'quota_waste', 'sync_failing', 'agent_offline'));
