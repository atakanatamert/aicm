# 07 – Auth core: passwords, sessions, middleware, roles, setup and login

**Writer:** DeepSeek Flash
**Depends on:** 02, 03
**License of touched packages:** AGPL-3.0-or-later

## Goal

Users can complete first-run setup, log in with email and password, stay logged in with a session cookie, and every API request is authenticated and checked against the role the spec declares (`x-aicm-role`). API tokens, users, groups and grants come in task 07b; this task only authenticates API tokens that already exist.

## Files to touch

- `internal/auth/password.go` (new)
- `internal/auth/password_test.go` (new)
- `internal/auth/service.go` (new)
- `internal/auth/middleware.go` (new)
- `internal/auth/roles.go` (new)
- `internal/auth/roles_test.go` (new)
- `internal/db/queries/auth.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/auth.go` (new)
- `internal/httpapi/unimplemented.go` (edit: delete the methods you implement)
- `internal/httpapi/httpapi.go` (edit: `Deps.Auth`, wire the two middlewares)
- `internal/config/config.go` (edit: nothing unless needed for `PublicURL`; it already exists)
- `cmd/aicm/main.go` (edit: build the auth service; `create-admin` command)
- `go.mod`, `go.sum` (add `golang.org/x/crypto` if missing)

Touch nothing else. `internal/auth/principal.go` already exists: use it, don't change it.

## Read first

- `CLAUDE.md`
- `internal/auth/principal.go`: `Principal`, `Role`, `WithPrincipal`, `FromContext`
- `internal/httpapi/httpapi.go`: `Server`, `APIError`, `NewRouter`
- `internal/httpapi/api.gen.go`: search for `StrictMiddlewareFunc`, `MiddlewareFunc`, `LoginRequestObject`, `LoginResponseObject`, `CompleteSetupResponseObject`
- `api/openapi.yaml`: the `auth` tag operations and `x-aicm-role` on every operation
- `migrations/00001_init.sql`: `users`, `sessions`, `api_tokens`, `orgs`, `org_settings`
- `internal/dbtest/dbtest.go`

## Contract

### Passwords (`password.go`)

Argon2id via `golang.org/x/crypto/argon2`, encoded in PHC format:
`$argon2id$v=19$m=65536,t=3,p=2$<salt b64 raw std>$<hash b64 raw std>`, salt 16 random bytes, key length 32.

```go
func HashPassword(password string) (string, error)
// VerifyPassword compares in constant time (crypto/subtle). It returns
// false for malformed hashes instead of an error.
func VerifyPassword(encoded, password string) bool
// ValidatePassword enforces a minimum of 12 characters and a maximum of 256.
func ValidatePassword(password string) error
```

### Tokens

- Session token: `"aicm_s_" + base64.RawURLEncoding(32 random bytes)`. Stored as `sha256(token)` in `sessions.token_hash`. Lifetime 30 days from creation (`expires_at`). Update `last_seen_at` at most once per minute per session.
- API token: format `aicm_pat_…` (created in 07b). Authenticate by `sha256(token)` lookup in `api_tokens`, rejecting `revoked_at IS NOT NULL` or `expires_at < now()`. Update `last_used_at` at most once per minute.
- Use `crypto/rand` and `crypto/sha256`. Never log tokens or hashes.

### Service (`service.go`)

```go
type Service struct { /* pool, queries, log, publicURL, login limiter */ }

func NewService(pool *pgxpool.Pool, log *slog.Logger, publicURL string) *Service

// SetupNeeded reports whether no user exists yet.
func (s *Service) SetupNeeded(ctx context.Context) (bool, error)
// CompleteSetup creates the first admin in the default org and a session.
// It returns ErrSetupDone if any user exists. Take
// pg_advisory_xact_lock(hashtext('aicm-setup')) inside the transaction so
// two concurrent calls can't both succeed.
func (s *Service) CompleteSetup(ctx context.Context, email, name, password, userAgent string, ip netip.Addr) (Session, error)
// Login checks the password and creates a session. Unknown email, wrong
// password and disabled user all return ErrInvalidLogin (same error, same
// timing: verify against a fixed dummy hash when the user is unknown).
// More than 5 failed attempts per email in 10 minutes returns
// ErrTooManyAttempts until the window passes (in-memory, per Service).
func (s *Service) Login(ctx context.Context, email, password, userAgent string, ip netip.Addr) (Session, error)
func (s *Service) Logout(ctx context.Context, sessionToken string) error
// Authenticate resolves a session cookie value or a Bearer API token to a Principal.
func (s *Service) AuthenticateSession(ctx context.Context, token string) (Principal, error)
func (s *Service) AuthenticateAPIToken(ctx context.Context, token string) (Principal, error)
// ChangePassword verifies the current password, sets the new one and deletes
// every other session of the user.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next, keepSessionToken string) error
// Me returns the user and org for the principal.
func (s *Service) Me(ctx context.Context, p Principal) (db.User, db.Org, db.OrgSetting, error)
// CreateAdmin is used by the create-admin CLI command (works even when users exist).
func (s *Service) CreateAdmin(ctx context.Context, email, name, password string) error

type Session struct {
	Token     string // raw cookie value, only returned at creation
	ExpiresAt time.Time
	Principal Principal
}

var (
	ErrInvalidLogin    = errors.New("invalid email or password")
	ErrTooManyAttempts = errors.New("too many login attempts")
	ErrSetupDone       = errors.New("setup already completed")
	ErrUnauthenticated = errors.New("not authenticated")
)
```

Emails are trimmed and compared case-insensitively (the `users_email_key` index is on `lower(email)`). Principal for a session: `Kind ActorUser`, `ActorID` = user id, `Label` = email. For an API token: `Kind ActorAPIToken`, `ActorID` = token id, `UserID` = token's user, `Role` = the user's current role, `Label` = `email + " (token " + name + ")"`. A disabled user's sessions and tokens fail authentication.

### Roles (`roles.go`)

```go
// OperationRoles maps every operationId in api/openapi.yaml to the minimum
// role from its x-aicm-role. "" means public (no login).
var OperationRoles = map[string]Role{ "getHealth": "", "getReady": "", "listAccounts": RoleViewer, ... }
```

The map is a package-level constant table (allowed: it is read-only data, not state). Fill it by reading `api/openapi.yaml`: every `operationId` and its `x-aicm-role` (`public` → `""`).

`roles_test.go`:
- `TestOperationRolesMatchSpec`: read `../../api/openapi.yaml` line by line; for every line `operationId: X`, find the next `x-aicm-role: Y` line; assert `OperationRoles[X]` equals Y (public → ""), and that the map has no extra keys. No YAML library: plain string scanning.
- `TestRoleAtLeast`: viewer < editor < admin; unknown role is never AtLeast anything.

### Middleware (`middleware.go`)

```go
// HTTPMiddleware authenticates the request and stores the Principal in the
// context when credentials are present. It never rejects by itself.
// Order: "Authorization: Bearer aicm_pat_…" header, then the "aicm_session" cookie.
// For cookie-authenticated unsafe methods (POST, PUT, PATCH, DELETE), reject
// with 403 {"error":{"code":"bad_origin",...}} when an Origin header is
// present and differs from the public URL's origin (CSRF guard).
func (s *Service) HTTPMiddleware(next http.Handler) http.Handler

// Authorize returns a strict middleware that enforces OperationRoles:
// public operations pass; otherwise a missing Principal yields
// httpapi-style 401 and a weaker role yields 403. Unknown operationIds are
// denied (403) so a new endpoint can't ship unprotected.
func Authorize(writeErr func(w http.ResponseWriter, status int, code, msg string)) func(f StrictHandlerFunc, operationID string) StrictHandlerFunc
```

`internal/auth` must not import `internal/httpapi` (httpapi imports auth). So `Authorize` takes plain function types. Its signature must match the generated `StrictMiddlewareFunc` in `api.gen.go`: copy the underlying types there (`func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (response interface{}, err error)`), defined in `auth` as:

```go
type StrictHandlerFunc = func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error)
```

and in `httpapi.NewRouter` adapt it: the generated `StrictMiddlewareFunc` uses its own named type `StrictHandlerFunc`; wrap with a small closure converting between them. Rejections return an `*httpapi.APIError`-equivalent: since auth can't import httpapi, return `auth.ErrUnauthenticated` / a new `auth.ErrForbidden`, and map those two in `Server.writeHandlerError` (edit `httpapi.go`) to 401 `unauthorized` and 403 `forbidden`.

### Handlers (`internal/httpapi/auth.go`)

Implement on `*Server` (delete them from `unimplemented.go`): `GetSetupStatus`, `CompleteSetup`, `Login`, `Logout`, `GetMe`, `ChangePassword`.

- Login and CompleteSetup must set the cookie. Strict responses can't set cookies, so return your own type implementing the generated response interface, for example:

```go
type loginOK struct{ me Me; cookie *http.Cookie }
func (r loginOK) VisitLoginResponse(w http.ResponseWriter) error {
	http.SetCookie(w, r.cookie)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	return json.NewEncoder(w).Encode(r.me)
}
```

- Cookie: name `aicm_session`, `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` when `PublicURL` starts with `https://`, `Expires` = session expiry. Logout clears it (`MaxAge: -1`) and deletes the session row.
- The handler gets the request's raw cookie and IP: the strict handler doesn't pass `*http.Request`, so add an `http` middleware in `HTTPMiddleware` that stores the session token, `User-Agent` and client IP (`r.RemoteAddr` host part) in the context, with accessor functions `auth.RequestMeta(ctx) (sessionToken, userAgent string, ip netip.Addr)`.
- Error mapping: `ErrInvalidLogin` → 401 `invalid_login`; `ErrTooManyAttempts` → 429 `too_many_attempts`; `ErrSetupDone` → 409 `setup_done`; weak password → 400 `validation_failed`.
- `Me` response: user fields per the `User` schema (`disabled` = `disabled_at != nil`), org `{id, name, display_currency}`.

### Wiring

- `httpapi.Deps` gets `Auth *auth.Service`.
- `NewRouter`: pass `[]StrictMiddlewareFunc{authorize}` to `NewStrictHandlerWithOptions`, and wrap the final handler with `d.Auth.HTTPMiddleware`.
- `cmd/aicm/main.go`: `auth.NewService(pool, log, cfg.PublicURL)`; new command `aicm create-admin --email E --name N` that reads the password from the `AICM_ADMIN_PASSWORD` env var (never a flag, so it stays out of shell history) and calls `CreateAdmin`. Add it to the usage text.

### SQL (`internal/db/queries/auth.sql`)

Queries you need, all filtered by `org_id` except the two lookups by unique hash and the email lookup (comment why): `CountUsers`, `GetUserByEmail`, `GetUserByID`, `InsertUser`, `UpdateUserPassword`, `TouchUserLogin`, `InsertSession`, `GetSessionByHash` (join users), `TouchSession`, `DeleteSession`, `DeleteOtherSessions`, `GetAPITokenByHash` (join users), `TouchAPIToken`, `GetOrg`, `GetOrgBySlug` exists already in `org.sql`.

## Done when

- [ ] Tests pass (`AICM_TEST_DATABASE_URL=postgres://aicm:aicm@localhost:55432/aicm?sslmode=disable go test ./internal/auth/`):
  - `password_test.go`: `TestHashAndVerify` (round trip, wrong password false, tampered hash false, two hashes of the same password differ), `TestValidatePassword` (11 chars fails, 12 passes).
  - `roles_test.go`: the two tests above.
- [ ] Smoke against a running hub (`go run ./cmd/aicm serve` with `AICM_DATABASE_URL` and a master key if task 06 has landed):
  - `GET /api/setup` → `{"needed":true}`; `POST /api/setup` creates the admin and sets the cookie; second `POST /api/setup` → 409.
  - `GET /api/me` without cookie → 401; with cookie → 200.
  - `POST /api/auth/login` wrong password ×6 → the 6th is 429.
  - `GET /api/users` as admin → 501 (not implemented yet, but authorized); `/healthz` works without login.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` leaves no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes. No audit rows yet (task 08 adds them).

## Outcome (filled in by Opus)
