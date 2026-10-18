# 28a-ui-alerts-machines – Alerts inbox and Machines page

**Writer:** GLM 5.3 Flash
**Depends on:** 14a, 18a, 18b, 24
**License of touched packages:** AGPL-3.0-or-later

## Goal

Users read and snooze alerts and tune alert rules; admins add machines with one pasted command and manage them.

## Files to touch

- `web/src/routes/alerts.tsx`
- `web/src/routes/machines.tsx` (replace the placeholders)
- `web/src/components/alerts/*`
- `web/src/components/machines/*` (new)
- `web/src/components/app-shell.tsx` (unread badge on the Alerts nav item only)

Touch nothing else.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` sections "Stack and UI", "Quota model and scoring rule", "Money: currency, conversion and balance history"
- `web/src/api/client.ts` (`query.useQuery`, `query.useMutation`, `errorOf`), `web/src/api/schema.d.ts` (types; never hand-write response types)
- `web/src/lib/format.ts` (`formatMoney`, `formatBilledAndConverted`, `formatMicro`, `formatMeterValue`, `formatRelative`, `formatDate`), `web/src/hooks/use-me.ts` (`canEdit`, `isAdmin`, `displayCurrency`)
- `web/src/components/page-header.tsx`, `web/src/components/empty-state.tsx`, `web/src/components/app-shell.tsx`, `web/src/components/ui/*`, `web/src/routes/router.tsx`, `web/src/index.css` (theme tokens, `--urgent`)
- `api/openapi.yaml` for the operations listed below

## Contract

**Operations:** `GET /api/alerts`, `PATCH /api/alerts/{id}`, `POST /api/alerts/read-all`, `GET/PUT /api/alert-rules`, `GET /api/machines`, `PATCH /api/machines/{id}` (admin), `GET/POST/DELETE /api/enroll-tokens` (admin).

**Behaviour (Alerts):**
- Inbox newest first with "Load more" (cursor): severity icon (critical uses `--urgent`), title, body, account, time, link buttons from `data` (Manage, Cancel, Top up, Billing), Mark read, Snooze (1 day, 1 week, until date). "Mark all read". Unread filter toggle. Nav badge with the unread count (poll every 60 s).
- "Alert rules" panel: org defaults (admins edit) and per-account overrides (editors): enable/disable and the kind's parameters (days before, thresholds, hours) with the defaults from the spec as placeholders.

**Behaviour (Machines):**
- Table: name (rename inline, admins), hostname, OS/arch, agent version, online dot, last seen ("2 min ago"), harnesses, revoke (admins, confirm dialog).
- "Add a machine" (admins): choose 1 hour or permanent token, then show the four commands from the response (macOS/Linux sh, Homebrew, Windows PowerShell, Docker) in tabs with a Copy button each, and the note "The token is shown once". List existing tokens (created, expires, uses, revoke).
- Empty state (no machines): Hairline figure, one sentence, the Add a machine button.

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
