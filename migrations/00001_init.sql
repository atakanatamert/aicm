-- Initial schema. Conventions:
--   * Every tenant table has org_id. Child tables reference parents through
--     (org_id, id) so a row can never point into another org.
--   * Ids are uuid unless the table is append-heavy (bigint identity).
--   * Times are timestamptz in UTC. Calendar days (renewals, charge dates,
--     FX rate dates) are date.
--   * Money is bigint minor units plus a char(3) ISO 4217 code. Token-cost
--     estimates use micro units (1e-6 of the currency unit) because
--     per-request costs are fractions of a cent.
--   * Quota meter values are double precision in the provider's own unit.
--     For meters whose unit is a currency the values are minor units.

-- +goose Up

-- Organizations ------------------------------------------------------------

CREATE TABLE orgs (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug       text NOT NULL UNIQUE,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now()
);

-- Self-hosted installs run with exactly this org.
INSERT INTO orgs (slug, name) VALUES ('default', 'Default');

CREATE TABLE org_settings (
    org_id                  uuid PRIMARY KEY REFERENCES orgs (id) ON DELETE CASCADE,
    display_currency        char(3) NOT NULL DEFAULT 'USD',
    fx_source               text NOT NULL DEFAULT 'frankfurter' CHECK (fx_source IN ('frankfurter', 'ecb')),
    smtp_host               text NOT NULL DEFAULT '',
    smtp_port               integer NOT NULL DEFAULT 587,
    smtp_username           text NOT NULL DEFAULT '',
    smtp_password_secret_id uuid,
    smtp_tls                text NOT NULL DEFAULT 'starttls' CHECK (smtp_tls IN ('none', 'starttls', 'tls')),
    smtp_from               text NOT NULL DEFAULT '',
    updated_at              timestamptz NOT NULL DEFAULT now()
);

INSERT INTO org_settings (org_id) SELECT id FROM orgs WHERE slug = 'default';

-- Secrets ------------------------------------------------------------------
-- Envelope encryption: one data key per org and version, wrapped by the
-- master key (AICM_MASTER_KEY). Each secret is sealed with its org's data key
-- using XChaCha20-Poly1305, with the secret id as associated data.

CREATE TABLE org_keys (
    id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id             uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    version            integer NOT NULL,
    wrapped_key        bytea NOT NULL,
    master_key_version integer NOT NULL,
    active             boolean NOT NULL DEFAULT true,
    created_at         timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, version),
    UNIQUE (org_id, id)
);

CREATE UNIQUE INDEX org_keys_one_active ON org_keys (org_id) WHERE active;

CREATE TABLE secrets (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    org_key_id uuid NOT NULL,
    nonce      bytea NOT NULL,
    ciphertext bytea NOT NULL,
    last4      text NOT NULL DEFAULT '',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, org_key_id) REFERENCES org_keys (org_id, id)
);

ALTER TABLE org_settings
    ADD FOREIGN KEY (org_id, smtp_password_secret_id) REFERENCES secrets (org_id, id) ON DELETE SET NULL (smtp_password_secret_id);

-- Users, groups, tokens ----------------------------------------------------

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    email         text NOT NULL,
    name          text NOT NULL DEFAULT '',
    password_hash text NOT NULL,
    role          text NOT NULL CHECK (role IN ('admin', 'editor', 'viewer')),
    disabled_at   timestamptz,
    last_login_at timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id)
);

-- Login is by email alone, so emails are unique across orgs.
CREATE UNIQUE INDEX users_email_key ON users (lower(email));

CREATE TABLE sessions (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL,
    user_id      uuid NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    user_agent   text NOT NULL DEFAULT '',
    ip           inet,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, id) ON DELETE CASCADE
);

CREATE INDEX sessions_expires_at ON sessions (expires_at);

-- API tokens act as their user, with that user's role and grants.
CREATE TABLE api_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL,
    user_id      uuid NOT NULL,
    name         text NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    last4        text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz,
    expires_at   timestamptz,
    revoked_at   timestamptz,
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, id) ON DELETE CASCADE
);

CREATE TABLE groups (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    name       text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, name),
    UNIQUE (org_id, id)
);

CREATE TABLE group_members (
    org_id   uuid NOT NULL,
    group_id uuid NOT NULL,
    user_id  uuid NOT NULL,
    PRIMARY KEY (group_id, user_id),
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, id) ON DELETE CASCADE
);

CREATE INDEX group_members_user ON group_members (org_id, user_id);

-- Machines (enrolled agents) -----------------------------------------------

-- Ed25519 key the hub signs agent handshakes with. The private key is a
-- secret; agents pin the public key from their enrollment command.
CREATE TABLE hub_keys (
    org_id      uuid PRIMARY KEY REFERENCES orgs (id) ON DELETE CASCADE,
    public_key  bytea NOT NULL,
    secret_id   uuid NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, secret_id) REFERENCES secrets (org_id, id)
);

-- Universal enrollment tokens. expires_at NULL means permanent.
CREATE TABLE enroll_tokens (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL,
    token_hash   bytea NOT NULL UNIQUE,
    created_by   uuid NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz,
    revoked_at   timestamptz,
    last_used_at timestamptz,
    use_count    integer NOT NULL DEFAULT 0,
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, id)
);

-- token_hash is the hash of the per-machine credential issued at enrollment.
-- The hub rejects a connection whose fingerprint differs from the stored one.
CREATE TABLE machines (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id          uuid NOT NULL,
    name            text NOT NULL,
    hostname        text NOT NULL,
    fingerprint     text NOT NULL,
    os              text NOT NULL,
    arch            text NOT NULL,
    agent_version   text NOT NULL,
    token_hash      bytea NOT NULL UNIQUE,
    enroll_token_id uuid,
    enrolled_at     timestamptz NOT NULL DEFAULT now(),
    last_seen_at    timestamptz,
    last_ip         inet,
    revoked_at      timestamptz,
    UNIQUE (org_id, fingerprint),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, enroll_token_id) REFERENCES enroll_tokens (org_id, id) ON DELETE SET NULL (enroll_token_id)
);

-- Accounts -----------------------------------------------------------------
-- One login or key at a provider. Accounts reported by an agent start as
-- 'pending' and appear in the discovered-accounts review; data the agent
-- sends for them is kept, so nothing is lost before confirmation.
-- Subscription fields are null for pure API or wallet accounts.

CREATE TABLE accounts (
    id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id            uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    provider_id       text NOT NULL,
    name              text NOT NULL,
    status            text NOT NULL DEFAULT 'active' CHECK (status IN ('pending', 'active', 'dismissed')),
    -- The provider's own account id (Claude account UUID, ChatGPT account
    -- id). Used to merge one account seen on several machines.
    external_id       text,
    auth_kind         text NOT NULL CHECK (auth_kind IN ('api_key', 'admin_key', 'agent_capture', 'none')),
    secret_id         uuid,
    -- Non-secret adapter options declared by the provider (team id, region).
    config            jsonb NOT NULL DEFAULT '{}',
    -- Opt-in for unofficial endpoints (Decision 9), including the agent's
    -- Claude usage poll.
    allow_unofficial  boolean NOT NULL DEFAULT false,
    plan_name         text NOT NULL DEFAULT '',
    price_minor       bigint CHECK (price_minor >= 0),
    currency          char(3) NOT NULL,
    billing_cycle     text NOT NULL DEFAULT 'none' CHECK (billing_cycle IN ('none', 'weekly', 'monthly', 'yearly')),
    renews_on         date,
    auto_renew        boolean NOT NULL DEFAULT true,
    promo_kind        text CHECK (promo_kind IN ('trial', 'discount')),
    promo_ends_on     date,
    notes             text NOT NULL DEFAULT '',
    last_synced_at    timestamptz,
    last_sync_error   text NOT NULL DEFAULT '',
    sync_failing_since timestamptz,
    archived_at       timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, secret_id) REFERENCES secrets (org_id, id) ON DELETE SET NULL (secret_id),
    CHECK ((price_minor IS NULL) = (billing_cycle = 'none')),
    CHECK ((promo_kind IS NULL) = (promo_ends_on IS NULL))
);

CREATE UNIQUE INDEX accounts_external_key ON accounts (org_id, provider_id, external_id) WHERE external_id IS NOT NULL;
CREATE INDEX accounts_renews_on ON accounts (org_id, renews_on) WHERE renews_on IS NOT NULL AND archived_at IS NULL;

-- Which machines and harnesses report an account.
CREATE TABLE account_machines (
    org_id        uuid NOT NULL,
    account_id    uuid NOT NULL,
    machine_id    uuid NOT NULL,
    harness       text NOT NULL,
    details       jsonb NOT NULL DEFAULT '{}',
    first_seen_at timestamptz NOT NULL DEFAULT now(),
    last_seen_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, machine_id, harness),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, machine_id) REFERENCES machines (org_id, id) ON DELETE CASCADE
);

-- A grant makes an account visible to a user or a group. The user's role
-- decides what they may do with it. Admins see every account.
CREATE TABLE account_grants (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id     uuid NOT NULL,
    account_id uuid NOT NULL,
    user_id    uuid,
    group_id   uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, user_id) REFERENCES users (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, group_id) REFERENCES groups (org_id, id) ON DELETE CASCADE,
    CHECK ((user_id IS NULL) <> (group_id IS NULL))
);

CREATE UNIQUE INDEX account_grants_user ON account_grants (account_id, user_id) WHERE user_id IS NOT NULL;
CREATE UNIQUE INDEX account_grants_group ON account_grants (account_id, group_id) WHERE group_id IS NOT NULL;

-- Quota meters -------------------------------------------------------------
-- Meters keep the provider's own key, label, unit and reset rule. The
-- definition is upserted by the adapter or agent that reports readings.

CREATE TABLE quota_meters (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id         uuid NOT NULL,
    account_id     uuid NOT NULL,
    key            text NOT NULL,
    label          text NOT NULL,
    unit           text NOT NULL CHECK (unit IN ('percent', 'count', 'tokens', 'requests', 'credits', 'currency')),
    -- Set when unit = 'currency'; values are then minor units.
    unit_currency  char(3),
    -- How the window resets: never (wallet), rolling (window_minutes after
    -- first use), fixed (provider-announced resets_at), calendar_month.
    reset_kind     text NOT NULL CHECK (reset_kind IN ('never', 'rolling', 'fixed', 'calendar_month')),
    window_minutes integer,
    limit_value    double precision,
    manual         boolean NOT NULL DEFAULT false,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, key),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE,
    CHECK ((unit = 'currency') = (unit_currency IS NOT NULL))
);

-- Every reading, unchanged from the provider. The newest captured_at wins.
CREATE TABLE quota_readings (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id      uuid NOT NULL,
    meter_id    uuid NOT NULL,
    used        double precision,
    remaining   double precision,
    limit_value double precision,
    resets_at   timestamptz,
    captured_at timestamptz NOT NULL,
    source      text NOT NULL CHECK (source IN ('statusline', 'stream_json', 'rollout', 'app_server', 'api', 'manual')),
    machine_id  uuid,
    received_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (meter_id, captured_at, source),
    FOREIGN KEY (org_id, meter_id) REFERENCES quota_meters (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, machine_id) REFERENCES machines (org_id, id) ON DELETE SET NULL (machine_id),
    CHECK (used IS NOT NULL OR remaining IS NOT NULL)
);

CREATE INDEX quota_readings_latest ON quota_readings (meter_id, captured_at DESC);

-- Money out ----------------------------------------------------------------

CREATE TABLE charges (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id       uuid NOT NULL,
    account_id   uuid NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('subscription', 'topup', 'invoice', 'refund')),
    -- suggested: detected from a balance jump, waiting for the user.
    status       text NOT NULL DEFAULT 'confirmed' CHECK (status IN ('confirmed', 'suggested', 'dismissed')),
    source       text NOT NULL CHECK (source IN ('manual', 'renewal', 'detected', 'api')),
    amount_minor bigint NOT NULL CHECK (amount_minor >= 0),
    currency     char(3) NOT NULL,
    -- The day the money left; FX conversion uses this date's rate.
    occurred_on  date NOT NULL,
    period_start date,
    period_end   date,
    -- Invoice id or similar from the provider, for idempotent imports.
    external_id  text,
    description  text NOT NULL DEFAULT '',
    created_by   uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (org_id, id),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, created_by) REFERENCES users (org_id, id) ON DELETE SET NULL (created_by)
);

CREATE INDEX charges_occurred ON charges (org_id, occurred_on);
CREATE UNIQUE INDEX charges_external ON charges (account_id, kind, external_id) WHERE external_id IS NOT NULL;
-- One automatic fee per renewal date.
CREATE UNIQUE INDEX charges_renewal ON charges (account_id, occurred_on) WHERE source = 'renewal';

-- Daily reference rates, global (not tenant data). rate = units of quote
-- per one unit of base.
CREATE TABLE fx_rates (
    rate_date  date NOT NULL,
    base       char(3) NOT NULL,
    quote      char(3) NOT NULL,
    rate       double precision NOT NULL CHECK (rate > 0),
    source     text NOT NULL CHECK (source IN ('frankfurter', 'ecb')),
    fetched_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (source, base, quote, rate_date)
);

-- Usage --------------------------------------------------------------------

-- Per-request usage pushed by agents. Deduped on machine + source + message
-- id so agent restarts never double-count. Rolled up into usage_daily.
CREATE TABLE usage_events (
    id                 bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id             uuid NOT NULL,
    account_id         uuid NOT NULL,
    machine_id         uuid NOT NULL,
    source             text NOT NULL,
    message_id         text NOT NULL,
    occurred_at        timestamptz NOT NULL,
    model              text NOT NULL,
    input_tokens       bigint NOT NULL DEFAULT 0,
    output_tokens      bigint NOT NULL DEFAULT 0,
    cache_read_tokens  bigint NOT NULL DEFAULT 0,
    cache_write_tokens bigint NOT NULL DEFAULT 0,
    reasoning_tokens   bigint NOT NULL DEFAULT 0,
    -- Cost reported by the harness, if any, in micro units.
    cost_micro         bigint,
    cost_currency      char(3),
    received_at        timestamptz NOT NULL DEFAULT now(),
    UNIQUE (machine_id, source, message_id),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, machine_id) REFERENCES machines (org_id, id) ON DELETE CASCADE,
    CHECK ((cost_micro IS NULL) = (cost_currency IS NULL))
);

CREATE INDEX usage_events_account_time ON usage_events (account_id, occurred_at);

-- Tokens and estimated cost per account, UTC day and model. Rows with
-- source 'api' come from provider usage reports; 'agent' rows are rolled up
-- from usage_events.
CREATE TABLE usage_daily (
    org_id             uuid NOT NULL,
    account_id         uuid NOT NULL,
    day                date NOT NULL,
    model              text NOT NULL,
    source             text NOT NULL CHECK (source IN ('api', 'agent')),
    requests           bigint NOT NULL DEFAULT 0,
    input_tokens       bigint NOT NULL DEFAULT 0,
    output_tokens      bigint NOT NULL DEFAULT 0,
    cache_read_tokens  bigint NOT NULL DEFAULT 0,
    cache_write_tokens bigint NOT NULL DEFAULT 0,
    reasoning_tokens   bigint NOT NULL DEFAULT 0,
    cost_micro         bigint NOT NULL DEFAULT 0,
    cost_currency      char(3) NOT NULL,
    -- true when cost_micro was reported by the provider, false when estimated
    -- from model_prices.
    cost_reported      boolean NOT NULL DEFAULT false,
    updated_at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (account_id, day, model, source),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE
);

CREATE INDEX usage_daily_org_day ON usage_daily (org_id, day);

-- Model prices, global (seeded from the LiteLLM price file). Micro units per
-- million tokens, so $3.00 / Mtok is 3000000.
CREATE TABLE model_prices (
    model                   text NOT NULL,
    provider                text NOT NULL DEFAULT '',
    currency                char(3) NOT NULL DEFAULT 'USD',
    input_micro_per_mtok       bigint NOT NULL DEFAULT 0,
    output_micro_per_mtok      bigint NOT NULL DEFAULT 0,
    cache_read_micro_per_mtok  bigint NOT NULL DEFAULT 0,
    cache_write_micro_per_mtok bigint NOT NULL DEFAULT 0,
    source                  text NOT NULL DEFAULT 'litellm',
    updated_at              timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (model, provider)
);

-- Alerts -------------------------------------------------------------------

-- account_id NULL is the org-wide default for a kind; a row with an
-- account_id overrides it for that account.
CREATE TABLE alert_rules (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    account_id    uuid,
    kind          text NOT NULL CHECK (kind IN ('renewal', 'promo_ending', 'low_balance', 'meter_exhausted', 'quota_waste', 'sync_failing', 'agent_offline')),
    enabled       boolean NOT NULL DEFAULT true,
    params        jsonb NOT NULL DEFAULT '{}',
    snoozed_until timestamptz,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (org_id, account_id, kind),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE
);

-- Org-wide defaults from docs/PLAN.md "Alerts".
INSERT INTO alert_rules (org_id, kind, params)
SELECT o.id, d.kind, d.params::jsonb
FROM orgs o
CROSS JOIN (VALUES
    ('renewal',         '{"days_before": [7, 1]}'),
    ('promo_ending',    '{"days_before": [3]}'),
    ('low_balance',     '{"below_percent": 20, "days_left": 7}'),
    ('meter_exhausted', '{"used_percent": 90}'),
    ('quota_waste',     '{"unused_percent": 50, "hours_before_reset": 12}'),
    ('sync_failing',    '{"after_hours": 24}'),
    ('agent_offline',   '{"after_hours": 48}')
) AS d (kind, params)
WHERE o.slug = 'default';

-- The inbox. event_key identifies one event (for example
-- "renewal:<account>:2026-11-01:7d"); the unique index plus River job
-- uniqueness guarantee one email per event.
CREATE TABLE alerts (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    org_id        uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    account_id    uuid,
    machine_id    uuid,
    kind          text NOT NULL,
    event_key     text NOT NULL,
    severity      text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    title         text NOT NULL,
    body          text NOT NULL DEFAULT '',
    data          jsonb NOT NULL DEFAULT '{}',
    created_at    timestamptz NOT NULL DEFAULT now(),
    emailed_at    timestamptz,
    read_at       timestamptz,
    snoozed_until timestamptz,
    resolved_at   timestamptz,
    UNIQUE (org_id, event_key),
    FOREIGN KEY (org_id, account_id) REFERENCES accounts (org_id, id) ON DELETE CASCADE,
    FOREIGN KEY (org_id, machine_id) REFERENCES machines (org_id, id) ON DELETE CASCADE
);

CREATE INDEX alerts_inbox ON alerts (org_id, created_at DESC);

-- Audit log ----------------------------------------------------------------
-- Append-only. before/after hold the changed fields with secrets redacted.

CREATE TABLE audit_log (
    id          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    org_id      uuid NOT NULL REFERENCES orgs (id) ON DELETE CASCADE,
    occurred_at timestamptz NOT NULL DEFAULT now(),
    actor_kind  text NOT NULL CHECK (actor_kind IN ('user', 'api_token', 'agent', 'system')),
    actor_id    uuid,
    actor_label text NOT NULL DEFAULT '',
    action      text NOT NULL,
    target_kind text NOT NULL,
    target_id   text NOT NULL DEFAULT '',
    before      jsonb,
    after       jsonb,
    ip          inet,
    request_id  text NOT NULL DEFAULT ''
);

CREATE INDEX audit_log_org_time ON audit_log (org_id, occurred_at DESC);

-- Rows are never edited. Deletes are allowed only as a cascade from
-- deleting the whole org (pg_trigger_depth() > 1 inside the RI trigger).
-- +goose StatementBegin
CREATE FUNCTION audit_log_append_only() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND pg_trigger_depth() > 1 THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION 'audit_log is append-only';
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER audit_log_no_update BEFORE UPDATE OR DELETE ON audit_log
    FOR EACH ROW EXECUTE FUNCTION audit_log_append_only();

-- +goose Down

DROP TABLE audit_log;
DROP FUNCTION audit_log_append_only();
DROP TABLE alerts;
DROP TABLE alert_rules;
DROP TABLE model_prices;
DROP TABLE usage_daily;
DROP TABLE usage_events;
DROP TABLE fx_rates;
DROP TABLE charges;
DROP TABLE quota_readings;
DROP TABLE quota_meters;
DROP TABLE account_grants;
DROP TABLE account_machines;
DROP TABLE accounts;
DROP TABLE machines;
DROP TABLE enroll_tokens;
DROP TABLE hub_keys;
DROP TABLE group_members;
DROP TABLE groups;
DROP TABLE api_tokens;
DROP TABLE sessions;
DROP TABLE users;
DROP TABLE org_settings;
DROP TABLE secrets;
DROP TABLE org_keys;
DROP TABLE orgs;
