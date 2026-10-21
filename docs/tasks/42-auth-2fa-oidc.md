# 42 – Two-factor (TOTP) and OIDC sign-in

**Writer:** DeepSeek Flash
**Depends on:** 36 (contracts)
**License of touched packages:** AGPL-3.0-or-later

## Goal

Users can turn on two-factor sign-in with any authenticator app (RFC 6238 TOTP) and get ten single-use recovery codes; admins can turn it off for a user who lost their phone. When configured, users can sign in with an OIDC provider (Authentik, Pocket ID, Keycloak, Google, …) instead of a password.

## Files to touch

- `internal/auth/totp.go` (new): RFC 6238 (SHA-1, 6 digits, 30 s step, ±1 step window), base32 secrets (20 random bytes), `otpauth://totp/aicm:<email>?secret=…&issuer=aicm` URLs; recovery codes (10 × 10 characters from an unambiguous alphabet, shown once, stored as SHA-256 hashes, used once); code reuse protection (reject a TOTP code already accepted in its time step: remember the last accepted step per user in memory or in `login_challenges`, your choice, explained in a comment)
- `internal/auth/totp_test.go`: the RFC 6238 SHA-1 test vectors, window edges, recovery code single use
- `internal/auth/oidc.go` (new): OIDC with `github.com/coreos/go-oidc/v3/oidc` and `golang.org/x/oauth2` (both approved, Decision 31): authorization code flow with PKCE and `state`/`nonce` in a short-lived signed cookie (HMAC with a key derived from the master key, or a random key held in memory — explain), ID token verification, `email` and `email_verified` claims
- `internal/auth/service.go` (edit): `Login` returns a challenge instead of a session when the user has TOTP on; `LoginTOTP(challenge, code)`; challenges in `login_challenges` (token is 32 random bytes, only its SHA-256 stored, 5 minutes, 5 attempts, single use); the existing per-email rate limit also counts failed codes
- `internal/db/queries/auth.sql` or a new `internal/db/queries/totp.sql`; `internal/config/config.go` (edit: OIDC settings)
- `internal/httpapi/auth.go` (edit: `Login` 202 path), `internal/httpapi/totp.go` (new: `LoginTotp`, `GetMyTotp`, `SetupMyTotp`, `EnableMyTotp`, `DisableMyTotp`, `ResetUserTotp`), `internal/httpapi/oidc.go` (new: `GetOidcConfig` plus plain handlers `GET /auth/oidc/start` and `GET /auth/oidc/callback` registered in `httpapi.go` like `/install.sh`), `internal/httpapi/users.go` (edit: fill `totp_enabled`, `sso_linked`), `internal/httpapi/unimplemented.go` (edit), `cmd/aicm/main.go` (wire)
- `go.mod`/`go.sum` for the two libraries; `THIRD_PARTY_NOTICES.md` not needed (dependencies, not copied code)

## Read first

- `api/openapi.yaml`: `login` (200 or 202 `LoginTotpRequired`), `loginTotp`, `getOidcConfig`, `getMyTotp`, `setupMyTotp`, `enableMyTotp`, `disableMyTotp`, `resetUserTotp`, `User` (`totp_enabled`, `sso_linked`)
- `migrations/00007_release_features.sql`: `users` new columns, `user_recovery_codes`, `login_challenges`
- `internal/auth/service.go` (login, sessions, limiter), `internal/auth/middleware.go`, `internal/httpapi/auth.go` (cookie-setting responses), `internal/secrets/secrets.go` (TOTP secrets are stored through it), `internal/config/config.go`

## Contract

- **TOTP lifecycle:** `setup` stores a new secret as `totp_pending_secret_id` (replacing any pending one; 409 when already enabled). `enable` checks a code against the pending secret, moves it to `totp_secret_id`, sets `totp_enabled_at`, replaces all recovery codes, returns the ten codes once. `disable` needs the password and a current code (or a recovery code); clears secret ids, enabled_at and recovery codes. `ResetUserTotp` (admin) does the same for another user in the org without the code. Each writes one audit row (`user.totp_enable`, `user.totp_disable`, `user.totp_reset`) without secrets.
- **Login:** password OK + TOTP on → 202 `{totp_required: true, challenge}` and no session cookie. `POST /api/auth/login/totp` with a valid code or unused recovery code → 200 Me with the cookie (like a normal login, sets `last_login_at`). Wrong code → 401 `invalid_code`; expired/used challenge → 401 `challenge_expired`; too many attempts → 429.
- **OIDC config** (env, all optional; enabled only when issuer, client id and secret are set): `AICM_OIDC_ISSUER`, `AICM_OIDC_CLIENT_ID`, `AICM_OIDC_CLIENT_SECRET`, `AICM_OIDC_NAME` (button label, default "Single sign-on"), `AICM_OIDC_SCOPES` (default `openid email profile`). Redirect URI: `<AICM_PUBLIC_URL>/auth/oidc/callback`. Discovery runs lazily on first use and is retried later if the provider is down (the hub must start without it).
- **OIDC sign-in:** `/auth/oidc/start` redirects to the provider; `/auth/oidc/callback` verifies state, nonce and the ID token, requires `email_verified` true, finds the active user by email (case-insensitive) or by stored `(oidc_issuer, oidc_subject)`; on first use stores issuer and subject; then creates a session exactly like a password login (cookie), skips TOTP (the provider handles its own MFA; say so in `docs/DECISIONS.md` is not needed — add a comment) and redirects to `/`. Unknown email or disabled user → redirect to `/login?error=sso_unknown_user`. No user is ever created by OIDC. Audit `user.login_sso` is not required (logins are not audited today); keep it consistent.
- **README:** add a short "Sign-in" section: two-factor in Settings → Profile; OIDC env variables and the redirect URI.


### Rules for every Go task

- Read `CLAUDE.md` first. Go 1.26 (`new(expr)` for pointers). No new dependencies unless this card names them.
- Every query filters by `org_id`; account-scoped reads use `account_visible(...)` (`migrations/00002_account_visible.sql`). Every mutation writes exactly one `audit_log` row with `audit.Write(ctx, q, principal.Actor(), audit.Entry{...})`, in the same transaction.
- Secrets only through `secrets.Store` (`internal/secrets/secrets.go`); API responses show at most `last4`; never log a secret, webhook URL or token.
- Queries live in `internal/db/queries/<feature>.sql`; run `~/go/bin/sqlc generate`. Never edit `*.gen.go` or `internal/db/*.sql.go` by hand.
- HTTP handlers are methods on `httpapi.Server` in one file per feature. The operations this card implements currently answer 501 from `internal/httpapi/unimplemented.go`: delete each method you implement from that file (delete the file if it becomes empty).
- Errors wrapped `fmt.Errorf("doing x: %w", err)`; handler errors as `*httpapi.APIError` (see `internal/httpapi/errors.go`).
- Tests only where `CLAUDE.md` allows (crypto, permissions, money, scoring, parsers, adapters with golden files). The migration `migrations/00007_release_features.sql` and the spec `api/openapi.yaml` are the contract: do not change them; if you think they are wrong, say so in your summary.
- Before you finish: `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean, `~/go/bin/sqlc generate` leaves no diff, `go test ./...` passes with `AICM_TEST_DATABASE_URL` set, `./scripts/check-licenses.sh` passes. Then smoke-test on a running hub (fresh database on the dev Postgres at `localhost:55432`, user/password `aicm`) and describe exactly what you ran and saw.

## Done when

- [ ] RFC 6238 vectors pass; recovery code single use; challenge expiry and attempt limit covered by tests where they are pure functions.
- [ ] Smoke with curl on a fresh hub: setup → enable (compute the code with a small Go or Python snippet from the returned secret) → logout → login returns 202 → login/totp with a code returns 200 and a cookie → a recovery code works once and fails the second time → admin reset turns it off.
- [ ] OIDC smoke against a real provider is not required; instead run the hub with OIDC env pointing at an unreachable issuer and show it still starts, `GET /api/auth/oidc` reports enabled, and `/auth/oidc/start` answers a clear error page or redirect to `/login?error=sso_unavailable`. Unit-test the state/nonce cookie round trip.

## Outcome (filled in by Opus)

Merged in `1590d8d` after the first attempt. Opus verified the full OIDC round trip against a local mock provider (PKCE S256, state, nonce, email_verified, login by verified email) and added sso_failed for replayed or forged state. merge.sh no longer regenerates the provider registry (it dropped zai.NewAPI).
