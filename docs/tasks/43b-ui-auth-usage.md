# 43b-ui-auth-usage – Two-factor sign-in, SSO button, usage by project

**Writer:** GLM 5.3 Flash
**Depends on:** 41, 42
**License of touched packages:** AGPL-3.0-or-later

## Goal

Users turn on two-factor sign-in with a QR code and save their recovery codes; the login page asks for a code when needed and shows an SSO button when the hub has one; admins can turn off two-factor for a user; the Usage page groups by project.

## Files to touch

- `web/src/routes/login.tsx` (second step and SSO button)
- `web/src/components/settings/profile-tab.tsx` and a new `web/src/components/settings/two-factor-card.tsx`
- `web/src/components/settings/users-tab.tsx` (2FA and SSO badges, admin "Turn off two-factor" with confirm)
- `web/src/routes/usage.tsx`, `web/src/components/usage/*` (group by Project)
- `web/package.json`, `web/pnpm-lock.yaml`: add `qrcode` and `@types/qrcode` (approved, Decision 31); nothing else

Touch nothing else.

## Read first

- `CLAUDE.md` Frontend conventions; `web/src/api/client.ts`, `web/src/api/schema.d.ts`
- `api/openapi.yaml`: `login` (200 Me or 202 `LoginTotpRequired`), `loginTotp`, `getOidcConfig`, `getMyTotp`, `setupMyTotp`, `enableMyTotp`, `disableMyTotp`, `resetUserTotp`, `User.totp_enabled`/`sso_linked`, `getUsage` `group_by=project`, `UsageRow.project`
- `web/src/routes/login.tsx`, `web/src/components/settings/profile-tab.tsx`, `users-tab.tsx`, `web/src/routes/usage.tsx`

## Contract

**Login**
- After a 202, replace the password form with a code step: one input (`autocomplete="one-time-code"`, numeric keyboard, also accepts a recovery code), "Use a recovery code" hint, Back. Submit to `/api/auth/login/totp`; errors inline (`invalid_code` "That code didn't work", `challenge_expired` "Start again: the sign-in timed out", 429 "Too many attempts").
- When `GET /api/auth/oidc` says enabled: a secondary button "Sign in with <name>" linking to `start_url` (a plain link, full page navigation). Show `?error=sso_unknown_user` as "No aicm user has that email. Ask an admin to add you." and `sso_unavailable` as "Single sign-on is not reachable right now."

**Profile → Two-factor card**
- Off: one sentence and "Turn on". Setup: QR code rendered locally from `otpauth_url` with `qrcode` (canvas or SVG, never an external service), the secret in a copyable monospace block for manual entry, a 6-digit input, Confirm. After `enable`: the ten recovery codes in a grid with Copy and Download (.txt), and "I saved them" to close; a warning that they are shown once.
- On: "On since <date> · N recovery codes left", "Turn off" dialog asking for password and a code.

**Users tab (admins)**
- Badges "2FA" and "SSO" next to users that have them; menu item "Turn off two-factor" (confirm dialog explaining when to use it).

**Usage**
- Group by gains "Project"; rows show the project name or "Unknown" for empty; CSV export unchanged (the server adds the column).

### Rules for every UI task

- React + TypeScript, shadcn/ui components (remove any `cn` dependency the CLI adds and import `cn` from `@/lib/utils`), Tailwind v4 tokens from `index.css`. Motion only for small transitions (≤150 ms).
- All data through `query.useQuery` / `query.useMutation` with typed paths; invalidate with keys shaped `["get", "/api/..."]`. No `fetch`, no hand-written types.
- Viewers see their own Profile only; admin-only controls need `isAdmin`.
- Static lookup tables are `Record`s.

## Done when

- [ ] `pnpm -C web lint` and `pnpm -C web build` clean (zero warnings); `go build ./...` passes.
- [ ] Browser smoke on a local hub: turn on two-factor (compute codes from the secret with a small script), log out, log in with password then code; log in with a recovery code; admin turns it off for another user. With `AICM_OIDC_*` pointing at an unreachable issuer the button shows and the error message renders. Usage grouped by project shows rows (seed events with projects via SQL). Desktop and mobile, light and dark, described in the summary.

## Outcome (filled in by Opus)
