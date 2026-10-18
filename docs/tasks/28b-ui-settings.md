# 28b-ui-settings – Settings: users, groups, SMTP, currency, API tokens, audit log

**Writer:** GLM 5.3 Flash
**Depends on:** 7b, 8, 14b, 24
**License of touched packages:** AGPL-3.0-or-later

## Goal

Admins manage users, groups, organization settings, SMTP and read the audit log; everyone manages their password and API tokens.

## Files to touch

- `web/src/routes/settings.tsx` (replace the placeholder)
- `web/src/components/settings/*` (new)
- `web/src/routes/router.tsx` (only if you split settings into sub-routes)

Touch nothing else.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` sections "Stack and UI", "Quota model and scoring rule", "Money: currency, conversion and balance history"
- `web/src/api/client.ts` (`query.useQuery`, `query.useMutation`, `errorOf`), `web/src/api/schema.d.ts` (types; never hand-write response types)
- `web/src/lib/format.ts` (`formatMoney`, `formatBilledAndConverted`, `formatMicro`, `formatMeterValue`, `formatRelative`, `formatDate`), `web/src/hooks/use-me.ts` (`canEdit`, `isAdmin`, `displayCurrency`)
- `web/src/components/page-header.tsx`, `web/src/components/empty-state.tsx`, `web/src/components/app-shell.tsx`, `web/src/components/ui/*`, `web/src/routes/router.tsx`, `web/src/index.css` (theme tokens, `--urgent`)
- `api/openapi.yaml` for the operations listed below

## Contract

**Operations:** `GET/POST /api/users`, `PATCH /api/users/{id}`, `GET/POST /api/groups`, `PUT/DELETE /api/groups/{id}`, `GET/PATCH /api/settings`, `POST /api/settings/smtp/test`, `GET/POST /api/me/tokens`, `DELETE /api/me/tokens/{id}`, `POST /api/me/password`, `GET /api/audit`.

**Behaviour:** tabs (or sub-routes) in this order:
- **Profile** (everyone): change password; personal API tokens (create with name and optional expiry → show the token once with Copy and the `claude mcp add aicm --env AICM_TOKEN=… -- aicm-agent mcp` hint; list with last 4, last used, revoke).
- **Users** (admin): list (email, name, role, disabled, last login), invite = create with a temporary password, change role, disable/enable, reset password; show 409 `conflict` (last admin) clearly.
- **Groups** (admin): create, rename, edit members, delete.
- **Organization** (admin): display currency (ISO code select of common currencies plus free entry), FX source (Frankfurter / ECB).
- **Email** (admin): SMTP host, port, TLS, username, password (write-only: "set, ends in 3456" with Replace / Clear), from address; "Send test email" to an address (show 502 errors with the server's message).
- **Audit log** (admin): table newest first with cursor paging: time, actor, action, target, and an expandable before/after JSON diff.

### Rules for every UI task

- React + TypeScript, shadcn/ui components (add missing ones with `pnpm dlx shadcn@latest add <name>` from `web/`; they land in `src/components/ui/`), Tailwind v4 tokens from `index.css`. Charts: shadcn chart (`pnpm dlx shadcn@latest add chart`, Recharts v3). Motion only for meters filling and small transitions (≤150 ms).
- All data through `query.useQuery` / `query.useMutation` with typed paths; invalidate the affected queries after a mutation (`queryClient.invalidateQueries`). No `fetch`, no hand-written types.
- Money: billed currency first, converted second (`formatBilledAndConverted`). Meters: the provider's label and unit (`formatMeterValue`); a progress bar only when a limit is known. Derived scores (headroom, expiring share) are secondary text, never instead of the provider's numbers.
- Quiet and neutral; the only accent is `--urgent`, used for: exhausted or infeasible accounts, headroom < 0.3, renewals within 7 days, unread critical alerts.
- Loading: skeletons. Errors: inline message from `errorOf(err)?.error.message`. Empty: `EmptyState` with a Hairline figure and one sentence that says what to do next.
- Viewers (`!canEdit`) see no edit controls at all (not disabled buttons: absent). Admin-only controls need `isAdmin`.
- Accessible: labels on inputs, buttons with text or `aria-label`, focus visible, keyboard reachable dialogs.
- Every new hand-written file starts with no license header requirement (REUSE.toml covers `web/`), but keep components small: one screen per route file plus local components in `web/src/components/<screen>/`.

## Done when

- [ ] `pnpm -C web lint` and `pnpm -C web build` pass with zero warnings; `go build ./...` passes.
- [ ] Smoke in a real browser against a local hub with seeded data (use the API or SQL to seed; describe what you seeded): every element listed under Behaviour is visible and works; take desktop (1280×800) and mobile (390×844) screenshots in light and dark and describe them in your summary.
- [ ] As a viewer (create one through `POST /api/users` as admin, grant one account), edit controls are absent and only granted accounts appear.
- [ ] The verification checklist in `docs/DELEGATION.md` passes (Opus runs the Impeccable critique).

## Outcome (filled in by Opus)
