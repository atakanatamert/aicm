# 43a-ui-channels-budgets – Alert channels, budgets, new alert kinds, wallet runway

**Writer:** GLM 5.3 Flash
**Depends on:** 37, 38
**License of touched packages:** AGPL-3.0-or-later

## Goal

Admins add alert channels (webhook, ntfy, Telegram, Discord, Slack) and test them; editors set a monthly budget for the org and per account and see month-to-date and projection on the Overview; the four new alert kinds read well in the inbox and can be tuned in Rules; API wallets say when they run out.

## Files to touch

- `web/src/components/settings/notifications-tab.tsx` (new) and `web/src/routes/settings.tsx` (add the "Notifications" tab, admin only)
- `web/src/components/overview/budgets-card.tsx` (new), `web/src/components/overview/budget-dialog.tsx` (new), `web/src/routes/overview.tsx` (show the card)
- `web/src/components/account-detail/*` (a "Budget" section card using the same dialog)
- `web/src/components/alerts/alert-kinds.ts` (labels, descriptions, param fields for `meter_reset`, `budget`, `spend_anomaly`, `wrong_wallet`), `web/src/components/alerts/*` where kinds are listed
- `web/src/components/quotas/quota-card.tsx` and `meter-row.tsx` (wallet runway)

Touch nothing else.

## Read first

- `CLAUDE.md` Frontend conventions; `web/src/api/client.ts`, `web/src/api/schema.d.ts`; `web/src/hooks/use-me.ts`; `web/src/lib/format.ts`
- `api/openapi.yaml`: `NotificationChannel*`, `ChannelKind` (its description defines each kind's fields), `Budget`, `BudgetInput`, `listBudgets`/`putBudget`/`deleteBudget`, `AlertKind`, `AlertParams`
- `web/src/routes/settings.tsx` and one existing tab (for example `email-tab.tsx`) for layout; `web/src/components/alerts/alert-kinds.ts`, `alert-rules.tsx`

## Contract

**Notifications tab (admins)**
- List of channels: kind icon or badge, name, filter summary ("warning and above · all alerts"), status dot (green last sent ok, red `last_error` with the message on hover, grey never sent), enabled switch, Test, Edit, Delete (confirm).
- Add/Edit dialog: kind select (Webhook, ntfy, Telegram, Discord, Slack); fields per kind exactly as `ChannelKind` describes (webhook/discord/slack: URL input type password with a "how to get this URL" help line linking to the vendor docs; ntfy: server (placeholder https://ntfy.sh), topic, optional access token; telegram: bot token and chat id with a help line); minimum severity select; alert kinds multi-select (default all); enabled. On edit the secret field is empty with "leave empty to keep ••••<last4>".
- Test button: spinner, then "Sent" or the error message inline.

**Budgets**
- Overview card "Budgets": org budget first, then account budgets; each row: name, `month_to_date` of `amount` with a bar (accent `--urgent` only at ≥ 100% or projected ≥ amount, with the reason in text), "Projected <amount>" and "N charges not converted" when > 0. Editors: "Set budget" / edit / remove. Empty state: one line and a "Set a monthly budget" button (editors) — no illustration.
- Dialog: scope (whole org or one account), amount with currency select (default display currency), thresholds (default 80, 100; chips with add/remove).
- Account detail: a "Budget" section showing that account's budget (or a set button).

**Alert kinds**
- Labels: `meter_reset` "Limits are back", `budget` "Budget", `spend_anomaly` "Unusual spend", `wrong_wallet` "Billed to the wrong wallet". Rules editor fields: `meter_reset.used_percent`, `budget.thresholds`, `spend_anomaly.usage_multiple` / `min_daily_cost` (USD) / `topups_per_day`, `wrong_wallet` none. Defaults as placeholders from the migration seed (90; 80, 100; 3, 5, 2).

**Wallet runway**
- For currency wallets (`reset_kind` never, unit currency) with `hours_to_empty`: a line under the meter "Runs out around <date> at this pace" (date in the user's locale; "in about N hours" when under 48 h); no line when burn is zero or unknown.

### Rules for every UI task

- React + TypeScript, shadcn/ui components (remove any `cn` dependency the CLI adds and import `cn` from `@/lib/utils`), Tailwind v4 tokens from `index.css`. Motion only for small transitions (≤150 ms).
- All data through `query.useQuery` / `query.useMutation` with typed paths; invalidate with keys shaped `["get", "/api/..."]`. No `fetch`, no hand-written types.
- Viewers (`!canEdit`) see no edit controls. Admin-only controls need `isAdmin`.
- `--urgent` only for urgent, actionable states, always with a reason in text. Status dots use `StatusDot` (`web/src/components/status-dot.tsx`).
- Static lookup tables are `Record`s.

## Done when

- [ ] `pnpm -C web lint` and `pnpm -C web build` clean (zero warnings); `go build ./...` passes.
- [ ] Browser smoke on a local hub with seeded data (describe the seed): add a webhook channel pointing at a local catcher and press Test (arrives); an ntfy channel to a random ntfy.sh topic; set an org budget and an account budget and see month-to-date and projection match `GET /api/budgets`; new alert kinds appear with their labels in the inbox (insert alerts via SQL) and in Rules; a wallet with `hours_to_empty` shows the runway line. Desktop 1280×800 and mobile 390×844, light and dark, described in the summary.
- [ ] As a viewer: no Notifications tab, no budget edit controls.

## Outcome (filled in by Opus)
