# 27a-ui-accounts – Accounts list, add account, discovered-account review, usage page

**Writer:** GLM 5.3 Flash
**Depends on:** 9, 12, 18b, 24
**License of touched packages:** AGPL-3.0-or-later

## Goal

Users see and add accounts, review accounts the agents discovered, and see token usage with cost estimates.

## Files to touch

- `web/src/routes/accounts.tsx`
- `web/src/routes/usage.tsx` (replace the placeholders)
- `web/src/components/accounts/*` (new)
- `web/src/components/usage/*` (new)

Touch nothing else.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` sections "Stack and UI", "Quota model and scoring rule", "Money: currency, conversion and balance history"
- `web/src/api/client.ts` (`query.useQuery`, `query.useMutation`, `errorOf`), `web/src/api/schema.d.ts` (types; never hand-write response types)
- `web/src/lib/format.ts` (`formatMoney`, `formatBilledAndConverted`, `formatMicro`, `formatMeterValue`, `formatRelative`, `formatDate`), `web/src/hooks/use-me.ts` (`canEdit`, `isAdmin`, `displayCurrency`)
- `web/src/components/page-header.tsx`, `web/src/components/empty-state.tsx`, `web/src/components/app-shell.tsx`, `web/src/components/ui/*`, `web/src/routes/router.tsx`, `web/src/index.css` (theme tokens, `--urgent`)
- `api/openapi.yaml` for the operations listed below

## Contract

**Operations:** `GET /api/accounts` (and `?status=pending`), `GET /api/providers`, `POST /api/accounts`, `POST /api/accounts/{id}/confirm` (with `import_key`), `POST /api/accounts/{id}/dismiss`, `GET /api/usage`, `GET /api/export/usage.csv`.

**Behaviour (Accounts):**
- "Found on your machines" review section when pending accounts exist: provider, label (email), plan, which harness; Confirm (optionally renaming; "Import API key" checkbox only for `auth_kind = api_key`) and Dismiss. 409 `agent_offline` → "Open the machine that found this key and try again".
- Account table: name, provider, plan, price (billed), renews on, key (last 4 or "agent" / "manual"), last sync status (error text on hover, urgent dot when failing). Row → `/accounts/:id` (task 27b builds that page; link to it now).
- "Add account" dialog (editors): pick a provider from the catalog (grouped: API keys, subscriptions/manual), name, then fields from the provider: key input (password field, never echoed) when its auth kind needs one, the provider's `config_fields`, plan (name, price + currency defaulting to the provider currency, cycle, renews on, auto-renew, promo), "allow unofficial endpoints" checkbox with a one-line warning when the provider is unofficial. Show 422 `invalid_credentials` as "The provider rejected this key".

**Behaviour (Usage):**
- Range picker (this month default, last 30 days, this year), group by day / model / account / provider; table with requests, input, output, cache read/write, reasoning tokens and estimated cost (`formatMicro`, "reported" vs "estimated" badge); a line chart for group-by-day. "Export CSV" button linking to `/api/export/usage.csv` with the same filters.
- Header note: "Estimates from token counts × list prices. Money out is on the Overview."

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

Merged in `5736ba8` after the first attempt (GLM). The first run hit the 60-minute job limit before reporting; a `-c` continuation finished the smoke and the summary. The review cards call `GET /api/accounts/{id}/machines` for the harness name (not in the card list, but an existing spec operation). Duplicate shadcn files resolved to main's copies.
