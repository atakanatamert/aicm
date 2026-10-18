# 27b-ui-account-detail – Account detail page

**Writer:** GLM 5.3 Flash
**Depends on:** 9, 13, 27a
**License of touched packages:** AGPL-3.0-or-later

## Goal

One page per account to edit its plan, set its key, see its meters, machines, charges and usage, manage access and archive it.

## Files to touch

- `web/src/routes/account-detail.tsx` (new)
- `web/src/routes/router.tsx` (add `/accounts/:id`)
- `web/src/components/account-detail/*` (new)

Touch nothing else.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` sections "Stack and UI", "Quota model and scoring rule", "Money: currency, conversion and balance history"
- `web/src/api/client.ts` (`query.useQuery`, `query.useMutation`, `errorOf`), `web/src/api/schema.d.ts` (types; never hand-write response types)
- `web/src/lib/format.ts` (`formatMoney`, `formatBilledAndConverted`, `formatMicro`, `formatMeterValue`, `formatRelative`, `formatDate`), `web/src/hooks/use-me.ts` (`canEdit`, `isAdmin`, `displayCurrency`)
- `web/src/components/page-header.tsx`, `web/src/components/empty-state.tsx`, `web/src/components/app-shell.tsx`, `web/src/components/ui/*`, `web/src/routes/router.tsx`, `web/src/index.css` (theme tokens, `--urgent`)
- `api/openapi.yaml` for the operations listed below

## Contract

**Operations:** `GET/PATCH /api/accounts/{id}`, `PUT /api/accounts/{id}/secret`, `POST /api/accounts/{id}/archive`, `POST /api/accounts/{id}/sync`, `GET /api/quotas?account_id=`, `GET /api/accounts/{id}/meters/{key}/readings`, `POST /api/accounts/{id}/meters`, `POST /api/accounts/{id}/meters/{key}/readings`, `GET /api/accounts/{id}/machines`, `GET /api/charges?account_id=`, `GET /api/usage?account_id=&group_by=day`, `GET/PUT /api/accounts/{id}/grants` (admin), `GET /api/users`, `GET /api/groups` (admin).

**Behaviour:**
- Header: name, provider, status, links (Top up / Billing / Manage / Cancel / Usage), Sync now (editors).
- Plan & renewal card with an Edit form (editors): plan name, price, cycle, renews on, auto-renew, promo kind/end, notes, allow unofficial.
- Key card: "Key ending in abcd, set 3 days ago" or "No key"; Replace key form (password input, validate checkbox default on); never shows the key.
- Meters: each meter with its latest reading as on the Quotas page plus a small history chart (readings over the last 7 days, used or remaining in the meter's unit). Manual meters: "Add meter" (key, label, unit, currency, reset kind, limit) and "Record reading" forms (editors).
- Machines that report it (machine, harness, first/last seen).
- Charges for this account (last 20) and usage by day (last 30 days, small chart).
- Access (admins): who can see this account: users and groups multi-select, saved with `PUT …/grants`.
- Archive (admins) behind a confirm dialog that requires typing the account name.

### Rules for every UI task

- React + TypeScript, shadcn/ui components (add missing ones with `pnpm dlx shadcn@latest add <name>` from `web/`; they land in `src/components/ui/`), Tailwind v4 tokens from `index.css`. Charts: shadcn chart (`pnpm dlx shadcn@latest add chart`, Recharts v3). Motion only for meters filling and small transitions (≤150 ms).
- All data through `query.useQuery` / `query.useMutation` with typed paths; invalidate the affected queries after a mutation (`queryClient.invalidateQueries`). No `fetch`, no hand-written types.
- Money: billed currency first, converted second (`formatBilledAndConverted`). Meters: the provider's label and unit (`formatMeterValue`); a progress bar only when a limit is known. Derived scores (headroom, expiring share) are secondary text, never instead of the provider's numbers.
- Quiet and neutral; the only accent is `--urgent`, used for: exhausted or infeasible accounts, headroom < 0.3, a renewal tomorrow or a renewal within 7 days with auto-renew off, unread critical alerts. An urgent element always states its reason in text.
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
