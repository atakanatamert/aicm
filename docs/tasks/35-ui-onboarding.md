# 35-ui-onboarding – First-run import and the last critique fixes

**Writer:** GLM 5.3 Flash
**Depends on:** 34
**License of touched packages:** AGPL-3.0-or-later

## Goal

A new hub leads straight to import: right after setup the user sees one command that installs the agent on this computer, watches the machine connect, and lands on the discovered accounts to confirm. Atakan tried the app and found "no obvious way to start import"; this card fixes that. It also finishes the open items from the second Impeccable critique.

## Files to touch

- `web/src/components/onboarding/*` (new)
- `web/src/components/machines/add-machine-dialog.tsx` (extract the command tabs into a shared component)
- `web/src/routes/overview.tsx`, `web/src/routes/accounts.tsx`, `web/src/routes/quotas.tsx`, `web/src/routes/machines.tsx`, `web/src/routes/setup.tsx`
- `web/src/components/accounts/*`, `web/src/components/alerts/*`, `web/src/components/quotas/*`, `web/src/components/overview/*`, `web/src/components/machines/*`, `web/src/routes/account-detail.tsx`, `web/src/components/account-detail/*`
- `web/src/lib/harness-names.ts` (new)

Touch nothing else. No Go or API changes.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` "Harness import" and "The agent"
- `web/src/api/client.ts`, `web/src/api/schema.d.ts`, `web/src/hooks/use-me.ts`, `web/src/lib/format.ts`
- `web/src/components/machines/add-machine-dialog.tsx`, `web/src/components/accounts/review.tsx`
- `api/openapi.yaml`: `POST /api/enroll-tokens`, `GET /api/machines`, `GET /api/accounts?status=pending`, `GET /api/accounts`

## Changes

### 1. First-run import (the main item)

- `components/onboarding/get-started.tsx`: a panel shown on Overview whenever the org has **no accounts (any status) and no machines**. It replaces Overview's empty state. Title "Import your AI accounts". Two numbered steps:
  1. **"Install the agent on this computer"** (admins): a button "Show the install command" creates a 1-hour token (`POST /api/enroll-tokens`, `permanent: false`) and shows the commands in the shared tabs component. Preselect the tab for the visitor's OS (`navigator.userAgent`: Windows → PowerShell, otherwise macOS/Linux). One sentence above: "It reads Claude Code, Codex and omp on this computer, read-only, and sends usage and limits here. Your logins stay on this computer." While waiting, poll `GET /api/machines` every 5 s and show "Waiting for the agent to connect…". When a machine appears: "Connected: <name>", then poll pending accounts every 5 s and show "Found N accounts" with a primary button "Review and confirm" → `/accounts` (the review section is there).
  2. **"Add API accounts"**: one line and a button "Add account" → `/accounts?add=1`; `accounts.tsx` opens the Add account dialog when `add=1` is in the URL.
  - Editors who are not admins see step 1 as "Ask an admin to add this computer" with no button. Viewers do not see the panel (they see the normal empty state).
- After the setup form succeeds, the user lands on Overview, which shows this panel (no extra page).
- `components/machines/enroll-commands.tsx`: the four command tabs with Copy buttons and the "shown once" note, extracted from `add-machine-dialog.tsx`; the dialog uses it too.
- Accounts empty state and Quotas empty state: one sentence plus a link "Import from your computer" → `/` (the panel) when there are no machines; keep their current text otherwise.
- Machines empty state: the Add a machine button stays; add the same sentence about what the agent reads.

### 2. One primary provider link

- Quota card, renewal card, alert row and account detail header show **one** primary provider link chosen by context, and the rest in an "Open in provider" dropdown menu (`pnpm dlx shadcn@latest add dropdown-menu` if missing). Links with the same `href` appear once (keep the first label in this order: Top up, Manage subscription, Billing, Usage, Cancel subscription).
- Primary link by context: low balance or wallet meter → Top up; renewal → Manage subscription; otherwise Usage. Alert rows: low_balance → Top up, renewal/promo_ending → Manage subscription, others → none (all in the menu).
- "Cancel subscription" is never the primary link.

### 3. Sort and filter

- Accounts table: a search box (name or provider, case-insensitive) and sortable headers for Name, Provider, Renews on (click toggles asc/desc; default Name asc). Keep the state in the URL (`?q=`, `?sort=`).
- Alerts inbox: filters for account (select, "All accounts") and kind (select, "All kinds"), applied client-side to loaded pages, kept in the URL.

### 4. Harness names and headings

- `lib/harness-names.ts`: `Record<string, string>` → `claudecode` "Claude Code", `codex` "Codex", `omp` "omp", fallback to the id. Use it on Machines, account detail machines card and the review cards.
- Section headings: every page section title under the page `h1` is an `h2` (Overview "This month"/"This year" group, Quotas "Quotas"/"Renewals", Alerts "Rules" blocks, account detail section cards); card titles stay `h3`.

### Rules for every UI task

- React + TypeScript, shadcn/ui components (remove any `cn` dependency the CLI adds and import `cn` from `@/lib/utils`), Tailwind v4 tokens from `index.css`. Motion only for small transitions (≤150 ms).
- All data through `query.useQuery` / `query.useMutation` with typed paths; invalidate with keys shaped `["get", "/api/..."]`. No `fetch`, no hand-written types.
- Viewers (`!canEdit`) see no edit controls. Admin-only controls need `isAdmin`.
- `--urgent` only for the cases in card 34, always with a reason in text.
- Static lookup tables are `Record`s.

## Done when

- [ ] `pnpm -C web lint` and `pnpm -C web build` pass with zero warnings; `go build ./...` passes.
- [ ] Fresh database: run setup in the browser, land on Overview, see the panel, click "Show the install command", and see the commands with the OS tab preselected. Then simulate the agent: insert a machine row and a pending account with `account_machines` via SQL (describe the SQL), and watch the panel switch to "Connected" and "Found 1 account" without a reload, and "Review and confirm" open Accounts with the review card.
- [ ] Every item in sections 2–4 visible in the browser, desktop 1280×800 and mobile 390×844, light and dark; describe what you saw.
- [ ] As a viewer, no panel and no edit controls.

## Outcome (filled in by Opus)

Merged in `48f4453` after the first attempt (GLM, ~34 min). The panel stays while the org has no active or dismissed account (pending accounts are the flow itself), so it does not vanish the moment the machine connects.
