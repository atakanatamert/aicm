-- account_visible is the one place grant visibility is decided. Every query
-- that returns account-scoped rows for a user adds
--   AND account_visible(@org_id, @user_id, @is_admin, <table>.account_id)
-- Admins see every account in their org; others see accounts granted to
-- them directly or to one of their groups.

-- +goose Up
-- +goose StatementBegin
CREATE FUNCTION account_visible(p_org uuid, p_user uuid, p_is_admin boolean, p_account uuid)
RETURNS boolean
LANGUAGE sql STABLE AS $$
    SELECT p_is_admin OR EXISTS (
        SELECT 1
        FROM account_grants g
        WHERE g.org_id = p_org
          AND g.account_id = p_account
          AND (
              g.user_id = p_user
              OR g.group_id IN (
                  SELECT m.group_id FROM group_members m
                  WHERE m.org_id = p_org AND m.user_id = p_user
              )
          )
    )
$$;
-- +goose StatementEnd

CREATE INDEX account_grants_account ON account_grants (org_id, account_id);

-- +goose Down
DROP INDEX account_grants_account;
DROP FUNCTION account_visible(uuid, uuid, boolean, uuid);
