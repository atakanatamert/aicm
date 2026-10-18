# 34-ui-polish – Fixes from the Impeccable critique

**Writer:** GLM 5.3 Flash
**Depends on:** 25, 26, 27a, 27b, 28a, 28b
**License of touched packages:** AGPL-3.0-or-later

## Goal

The UI keeps its single urgency accent for things the user can act on, says why a card is urgent, speaks in the user's words, and reads the same way on every screen and on a phone. Every item below comes from the dual-assessment critique of the merged screens (Nielsen total 25/40).

## Files to touch

- `web/src/index.css` (the `--urgent` token only)
- `web/src/lib/provider-colors.ts` (new)
- `web/src/components/app-shell.tsx`, `web/src/components/page-header.tsx`
- `web/src/routes/{overview,quotas,alerts,machines,settings}.tsx`
- `web/src/components/{overview,quotas,alerts,machines,settings}/*`
- `web/src/components/accounts/*` and `web/src/components/usage/*` only where an item below names them

Touch nothing else. No Go changes, no API changes.

## Read first

- `CLAUDE.md` (Frontend conventions), `docs/PLAN.md` "Stack and UI", "Quota model and scoring rule"
- `web/src/api/client.ts`, `web/src/api/schema.d.ts`, `web/src/lib/format.ts`, `web/src/hooks/use-me.ts`, `web/src/hooks/use-provider-names.ts`
- The files listed under each item

## Changes

### 1. Urgency: one meaning, a stated reason, its own colour

- `index.css`: give `--urgent` its own hue, distinct from `--destructive` in both themes (an amber/orange that passes 3:1 against the card background as a border and 4.5:1 as text). Fix the stale comment on it.
- Urgent applies only to: an infeasible account (`feasible === false`), headroom < 0.3, a renewal within 7 days **with auto-renew off**, a renewal tomorrow or today, an unread critical alert. Ordinary auto-renewing renewals 2–7 days out stay neutral (`renewal-card.tsx`, `renewals-strip.tsx`).
- Every urgent card states why in one line of text next to the border (never colour alone), built from the score fields: infeasible → "Runs out before it resets at current use"; headroom < 0.3 → "Less than 30% left before reset" (use `hours_to_empty` when present: "Runs out in about N h"); renewal → "Renews tomorrow" / "Auto-renew is off: renews in N days". (`quota-card.tsx`, `renewal-card.tsx`)
- Destructive icon buttons (revoke ✕) render in `muted-foreground` and turn destructive on hover/focus (`machine-table.tsx`, `enroll-tokens.tsx`, settings token list).
- The Alerts nav badge uses `--urgent` only when an unread critical alert exists, otherwise the neutral badge (`app-shell.tsx`).

### 2. Words the user knows

- `meter-row.tsx`: replace the raw `source` badge with freshness text: "read 12 min ago · agent" using `captured_at` and a lookup `Record<MeterSource, string>` (`api` → "API", `rollout`/`app_server`/`statusline`/`oauth_usage` → "agent", `manual` → "entered manually"). Percent meters drop "limit 100%".
- `quota-card.tsx`: subtitle "synced 3 h ago" / "not synced yet"; when never synced but readings exist, "updated 12 min ago" from the newest `captured_at`. Split the score line into "Tightest: <meter label>" and, only when the expiring share ≥ 0.1, "Unused before reset: <meter label>, N%".
- `audit-tab.tsx`: show action as a sentence ("Created charge", "Updated account") from a lookup table with the raw key in `title`; show the target's name when the row has one, the short id otherwise.
- Organization tab currency help: "Pick any currency; exchange rates exist for the common ones."
- Email tab: put "Send test email" after "Save changes" and say "Uses the saved settings".

### 3. Consistency

- `lib/provider-colors.ts`: a `Record<string, string>` from provider id to a CSS colour (same in both themes, chosen to read on light and dark), with a neutral fallback. The Overview chart and Usage chart colour bars by provider id, not rank.
- Every place that shows a billed amount with a converted one uses `formatBilledAndConverted` (`provider-table.tsx` stops always showing "≈" when the currency matches).
- One content container on every route: `mx-auto w-full max-w-6xl px-4 sm:px-6`; remove inner duplicate padding (`settings.tsx` + its tabs).
- Remove the duplicated page title in the top bar (`app-shell.tsx`) or replace it with nothing; the `PageHeader` h1 stays.
- `PageHeader` gains an optional `actions` slot; "Record a charge" (Overview), "Add a machine" (Machines) and "Add account" (Accounts) move into it.

### 4. Alerts layout and safety

- Alerts: two tabs, "Inbox" and "Rules", backed by `?tab=` in the URL; Inbox is the default.
- Rules: one form with one Save bar for the org defaults and one per account override block (not a Save per row).
- Alert rows: provider links read "Manage subscription ↗", "Cancel subscription ↗", "Top up ↗", "Billing ↗" (external-link icon, `target="_blank" rel="noreferrer"`); Mark read and Snooze stay the visible actions; on mobile the provider links collapse into a "Links" dropdown.
- External provider links everywhere (quota cards, renewal cards, account pages) carry the same external-link icon.

### 5. Mobile and accessibility

- Machines and the Overview provider table render stacked rows below `sm` (name, status text, last seen; hostname only when it differs from the name).
- Card titles on Quotas and Overview are `h3`.
- Buttons on cards use `size="sm"` (not `xs`).
- Settings tabs are backed by `?tab=` and wrap on narrow screens.
- Online/offline in Machines: the status text is coloured (`text-foreground` online, `muted` offline) and the dot has `aria-hidden`.
- Unread dot in alert rows: `role="img"` with its `aria-label`.

### 6. Smaller fixes

- Overview chart: start at the first month that has data (at most 12 months), Y axis ticks carry the currency symbol.
- Enroll tokens: expired tokens show an "Expired" badge and no Revoke.
- Users tab: disabling a user or demoting an admin asks for confirmation.
- Hairline figures inside cards are capped at `max-w-[200px]`.
- Quotas: meters section first, renewals second (Overview already shows renewals).

### Rules for every UI task

- React + TypeScript, shadcn/ui components (add missing ones with `pnpm dlx shadcn@latest add <name>` from `web/`; remove any `cn` dependency the CLI adds and import `cn` from `@/lib/utils`), Tailwind v4 tokens from `index.css`. Motion only for meters filling and small transitions (≤150 ms).
- All data through `query.useQuery` / `query.useMutation` with typed paths; invalidate with keys shaped `["get", "/api/..."]`. No `fetch`, no hand-written types.
- Viewers (`!canEdit`) see no edit controls at all. Admin-only controls need `isAdmin`.
- Static lookup tables are `Record`s.

## Done when

- [ ] `pnpm -C web lint` and `pnpm -C web build` pass with zero warnings; `go build ./...` passes.
- [ ] Smoke in a real browser against a local hub with seeded data: each numbered item above is visible; desktop (1280×800) and mobile (390×844) screenshots in light and dark described in the summary.
- [ ] As a viewer, edit controls are absent.
- [ ] The verification checklist in `docs/DELEGATION.md` passes (Opus re-runs the Impeccable critique).

## Outcome (filled in by Opus)

Merged in `d6bace0` after the first attempt (GLM, ~43 min). Opus finished what the file list excluded (shared container and header action on Accounts, Usage and account detail; external-link icons on account detail) and a second critique pass: Nielsen 25 → 26/40 by the independent reviewer before the last fixes. Fixed after it: renewal card footer overflow (page scrolled sideways), alert bodies without pasted URLs and with readable dates (server side), Accounts uses the shared renewal urgency rule with its reason, Record a charge has no preselected account, dark `--chart-3` moved off the urgency hue, "about N days" past 48 h, Archive button outline. Left for later: one primary provider link per card with the rest in a menu, sort/filter on Accounts and Alerts, harness names on Machines from account sightings, `h2` section headings.
