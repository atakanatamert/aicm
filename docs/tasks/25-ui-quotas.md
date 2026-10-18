# 25-ui-quotas – Quotas and renewals page

**Writer:** GLM 5.3 Flash
**Depends on:** 13, 24
**License of touched packages:** AGPL-3.0-or-later

## Goal

The Quotas page shows every account's provider-native meters and every upcoming renewal, sorted by urgency, with one-click provider links.

## Files to touch

- `web/src/routes/quotas.tsx` (replace the placeholder)
- `web/src/components/quotas/*` (new)

Touch nothing else.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` sections "Stack and UI", "Quota model and scoring rule", "Money: currency, conversion and balance history"
- `web/src/api/client.ts` (`query.useQuery`, `query.useMutation`, `errorOf`), `web/src/api/schema.d.ts` (types; never hand-write response types)
- `web/src/lib/format.ts` (`formatMoney`, `formatBilledAndConverted`, `formatMicro`, `formatMeterValue`, `formatRelative`, `formatDate`), `web/src/hooks/use-me.ts` (`canEdit`, `isAdmin`, `displayCurrency`)
- `web/src/components/page-header.tsx`, `web/src/components/empty-state.tsx`, `web/src/components/app-shell.tsx`, `web/src/components/ui/*`, `web/src/routes/router.tsx`, `web/src/index.css` (theme tokens, `--urgent`)
- `api/openapi.yaml` for the operations listed below

## Contract

**Operations:** `GET /api/quotas`, `GET /api/renewals?within_days=30`, `POST /api/accounts/{id}/sync` (editor), `GET /api/accounts/{id}/meters/{key}/readings` (sparkline, optional).

**Behaviour:**
- Two sections: **Renewals** (next 30 days, soonest first: account, provider, plan, price billed + converted, date + "in N days", auto-renew off badge, Manage and Cancel buttons opening the catalog links in a new tab) and **Quotas** (one card per account in the API's order, which is already sorted by urgency).
- Quota card: provider name, account name, plan, last synced ("synced 5 min ago"; "never" otherwise), and per meter: the provider's label, used / remaining / limit in the meter's unit, reset ("resets in 3 h", absolute time on hover), source badge (`statusline`, `rollout`, `app_server`, `api`, `manual`) and a "stale" note when the window has reset since the reading. Percent meters and meters with a limit get a bar (Motion fill); others show the number only.
- Card footer: one-click link buttons from `account.links` (Top up, Billing, Manage subscription, Usage) and, for editors, "Sync now" (show 409 `nothing_to_sync` and 429 `too_soon` messages inline with the retry time).
- Score as secondary text per card: "Weakest: Weekly limit · 30% of the 5-hour window expires unused".
- Urgency accent on the card border for infeasible accounts or headroom < 0.3, and on renewals within 7 days.
- Empty states: no accounts → link to Accounts; no renewals in 30 days → one line, not a big illustration.

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
