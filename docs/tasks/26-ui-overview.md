# 26-ui-overview – Overview: money out

**Writer:** GLM 5.3 Flash
**Depends on:** 10, 11, 24
**License of touched packages:** AGPL-3.0-or-later

## Goal

The Overview shows money out this month and this year per provider, billed currency first and converted second, plus suggested top-ups to confirm.

## Files to touch

- `web/src/routes/overview.tsx` (replace the placeholder)
- `web/src/components/overview/*` (new)

Touch nothing else.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` sections "Stack and UI", "Quota model and scoring rule", "Money: currency, conversion and balance history"
- `web/src/api/client.ts` (`query.useQuery`, `query.useMutation`, `errorOf`), `web/src/api/schema.d.ts` (types; never hand-write response types)
- `web/src/lib/format.ts` (`formatMoney`, `formatBilledAndConverted`, `formatMicro`, `formatMeterValue`, `formatRelative`, `formatDate`), `web/src/hooks/use-me.ts` (`canEdit`, `isAdmin`, `displayCurrency`)
- `web/src/components/page-header.tsx`, `web/src/components/empty-state.tsx`, `web/src/components/app-shell.tsx`, `web/src/components/ui/*`, `web/src/routes/router.tsx`, `web/src/index.css` (theme tokens, `--urgent`)
- `api/openapi.yaml` for the operations listed below

## Contract

**Operations:** `GET /api/spend?period=month&from=<Jan 1>&to=<today>&group_by=provider`, `GET /api/spend?period=year&from=<5 years ago Jan 1>&group_by=provider`, `GET /api/charges?status=suggested`, `PATCH /api/charges/{id}` (confirm/dismiss, editor), `POST /api/charges` (editor), `GET /api/accounts` (for the add-charge form), `GET /api/renewals?within_days=7`.

**Behaviour:**
- Headline: **This month** and **This year** money out in the display currency (converted totals), each with the billed amounts per currency underneath ("¥300.00 · $212.00"); when any charge lacks a rate, say "N charges not converted" instead of hiding them.
- Bar chart: money out per month this year, stacked by provider, converted to the display currency (shadcn chart). Table under it: per provider, this month and this year, billed per currency first, converted second.
- "Suggested top-ups" callout when `status=suggested` charges exist: amount, account, date, description, Confirm / Dismiss (editors).
- "Renews this week" strip from `/api/renewals?within_days=7` (links to Quotas).
- "Record a charge" dialog (editors): account, kind (top-up, subscription, invoice, refund), amount as a decimal in the account's currency (convert to minor units with `currencyExponent`), date, description.
- Note under the chart: "Token-cost estimates are on the Usage page; this page shows money that left your account."

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
