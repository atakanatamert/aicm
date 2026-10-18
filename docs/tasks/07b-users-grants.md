# 07b – Users, groups, API tokens and per-account grants

**Writer:** DeepSeek Flash
**Depends on:** 07
**License of touched packages:** AGPL-3.0-or-later

## Goal

Admins manage users, groups and account grants; every user manages their own API tokens. A viewer cannot see an account that isn't granted to them or one of their groups (test). Every mutation writes one audit row.

## Files to touch

- `internal/auth/users.go` (new): users, groups, grants, tokens service methods
- `internal/auth/grants_test.go` (new)
- `internal/db/queries/users.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/users.go` (new): handlers below
- `internal/httpapi/unimplemented.go` (edit: delete the methods you implement)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `internal/auth/service.go`, `internal/auth/password.go`, `internal/auth/principal.go`: reuse `HashPassword`, `ValidatePassword`, token hashing helpers, `Principal`
- `internal/audit/audit.go`: call `audit.Write` in the same transaction as each change
- `internal/httpapi/auth.go`, `internal/httpapi/summary.go` (`requirePrincipal`), `internal/httpapi/httpapi.go` (`APIError`, `BadRequest`, `NotFound`, `Conflict`)
- `api/openapi.yaml`: operations `listApiTokens`, `createApiToken`, `revokeApiToken`, `listUsers`, `createUser`, `updateUser`, `listGroups`, `createGroup`, `updateGroup`, `deleteGroup`, `getAccountGrants`, `setAccountGrants`
- `migrations/00001_init.sql` (`users`, `groups`, `group_members`, `account_grants`, `api_tokens`), `migrations/00002_account_visible.sql`
- `internal/dbtest/dbtest.go`

## Contract

Methods on `*auth.Service` (all take the acting `Principal`; all queries filter by `p.OrgID`):

```go
func (s *Service) ListAPITokens(ctx context.Context, p Principal) ([]db.ApiToken, error)
// CreateAPIToken returns the row and the raw token ("aicm_pat_" + base64url of 32 random bytes), shown once.
func (s *Service) CreateAPIToken(ctx context.Context, p Principal, name string, expiresAt *time.Time) (db.ApiToken, string, error)
func (s *Service) RevokeAPIToken(ctx context.Context, p Principal, tokenID string) error // only the principal's own tokens; ErrNotFound otherwise

func (s *Service) ListUsers(ctx context.Context, p Principal) ([]db.User, error)
func (s *Service) CreateUser(ctx context.Context, p Principal, email, name, password string, role Role) (db.User, error) // duplicate email → ErrEmailTaken
type UserUpdate struct { Name *string; Role *Role; Disabled *bool; Password *string }
// UpdateUser refuses (ErrLastAdmin) to demote or disable the last enabled admin.
// Disabling a user or resetting their password deletes their sessions.
func (s *Service) UpdateUser(ctx context.Context, p Principal, userID string, u UserUpdate) (db.User, error)

type Group struct { ID, Name string; MemberIDs []string }
func (s *Service) ListGroups(ctx context.Context, p Principal) ([]Group, error)
func (s *Service) CreateGroup(ctx context.Context, p Principal, name string, memberIDs []string) (Group, error) // duplicate name → ErrNameTaken
func (s *Service) UpdateGroup(ctx context.Context, p Principal, groupID, name string, memberIDs []string) (Group, error) // replaces members
func (s *Service) DeleteGroup(ctx context.Context, p Principal, groupID string) error

type Grants struct { UserIDs, GroupIDs []string }
func (s *Service) AccountGrants(ctx context.Context, p Principal, accountID string) (Grants, error)
// SetAccountGrants replaces the account's grants. Unknown user, group or account ids → ErrNotFound.
func (s *Service) SetAccountGrants(ctx context.Context, p Principal, accountID string, g Grants) (Grants, error)

var (
	ErrNotFound   = errors.New("not found")
	ErrEmailTaken = errors.New("email already in use")
	ErrNameTaken  = errors.New("name already in use")
	ErrLastAdmin  = errors.New("the last admin cannot be demoted or disabled")
)
```

If any of these names already exists in `internal/auth`, reuse it instead of redeclaring.

Audit actions (one `audit.Write` per call, in the transaction): `api_token.create` (after: id, name, last4, expires_at), `api_token.revoke`, `user.create`, `user.update` (before/after of changed fields; password changes record `{"password": "changed"}` which redaction stores as `[redacted]`), `group.create`, `group.update`, `group.delete`, `account_grants.set` (before/after `Grants`).

Role checks are already enforced by the middleware from `x-aicm-role`. Members and grant targets must belong to the principal's org.

### Handlers (`internal/httpapi/users.go`)

Map service results to the generated types (`ApiToken`, `ApiTokenCreated`, `User`, `Group`, `AccountGrants`). Error mapping: `ErrNotFound` → 404 `not_found`; `ErrEmailTaken`, `ErrNameTaken`, `ErrLastAdmin` → 409 `conflict` with the error text; weak password or invalid role → 400 `validation_failed`. `CreateApiToken` returns 201 with `token` set.

## Done when

- [ ] `grants_test.go` `TestGrantVisibility` (with `dbtest.New`; create users, a group and two accounts with SQL or the service): for each case assert `SELECT account_visible($org, $user, $is_admin, $account)`:
  - admin sees both accounts with no grants;
  - viewer with no grant sees neither;
  - a direct user grant makes exactly that account visible;
  - a grant to a group the viewer belongs to makes it visible; removing the viewer from the group (`UpdateGroup`) hides it again;
  - `SetAccountGrants` with empty lists hides it again;
  - a grant in another org never makes an account visible.
- [ ] `TestLastAdmin`: demoting or disabling the only admin returns `ErrLastAdmin`; with a second admin it succeeds.
- [ ] Smoke on a running hub: admin creates a viewer; viewer logs in; `GET /api/users` as viewer → 403; admin creates a token, `GET /api/me` with `Authorization: Bearer <token>` → 200; revoke → 401; `SELECT action FROM audit_log` lists one row per mutation you made.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
