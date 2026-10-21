# Feature gaps before the first release

Merged from two research passes on 2026-10-21 (sources, quotes and links in `feature-gaps-devtools.md` and `feature-gaps-spendops.md`). Sizes: S a few days, M one to two weeks, L several weeks. "Reuses" names the existing part of aicm the feature builds on.

## Recommended for v0.1.0

| # | Feature | Why (evidence) | Size | Reuses |
|---|---|---|---|---|
| 1 | Alert channels: generic webhook, ntfy, Telegram, Discord, Slack | Self-hosters expect them (Wallos ships six channels; LiteLLM four); listing them as hosted-only reads badly on r/selfhosted | S–M | `notify.Notifier` |
| 2 | "Limits are back" alert after a window was exhausted, and vendor-wide surprise resets | codex#21073 (86 reactions), CodexBar#263; whole sites exist to announce resets | S | meter readings |
| 3 | Monthly budget per org and per account, threshold alerts, month-end projection | OpenRouter: "Does OpenRouter alert me before I hit a budget? Not yet"; Cursor, LiteLLM, Portkey, Helicone have it | M | spend report, alerts |
| 4 | Runaway-spend alert: usage far above the account's normal rate, or top-ups much more often than usual | $82k stolen Gemini key in 48 h; "credits funded twice in 24 h"; no self-hosted tool does it | M (S for the top-up rule) | usage rollups, detected top-ups |
| 5 | Wallet runway in the UI ("runs out around Nov 3 at this pace") | Copilot users: quota "depleted in less than two days" | S | `hours_to_empty` already scored |
| 6 | Wrong-wallet warning: Claude Code or Codex turns billed to an API key or paid extra usage while a subscription is active | Claude Code#62338 ($447 billed to API instead of Max), OpenAI forum; no competitor does it | M | agent logs + account kinds |
| 7 | Cost by project (working directory / repo) from agent logs | ccusage, codeburn (11.5k stars) and agency requests on the Cursor forum | M | agent logs carry the directory |

## Next, after the release

| Feature | Size | Note |
|---|---|---|
| TOTP 2FA, then OIDC sign-in | S / M | PLAN says "not planned"; r/selfhosted treats SSO as basic, and the hub holds admin keys |
| Prepaid credit expiry per top-up, with an alert | S–M | OpenAI, Anthropic and Gemini credits expire after 12 months |
| Claude extra usage and Codex credits as spend | S | the usage poll already decodes `extra_usage` |
| Prometheus `/metrics` | S | Grafana stacks for Claude Code already exist |
| Idle subscriptions ("you used Cursor twice this month") | S | |
| Weekly spend digest by email | S | LiteLLM, Helicone |
| Installable web app (PWA) for phones | S | |
| iCal feed of renewals and expiries | S | escape input (Wallos CVE-2026-77353) |
| More sources: Cursor, Kimi Code plan, Ollama Cloud, opencode logs, Copilot CLI | S–M each | |
| Charged vs expected reconciliation | M | aicm alone has both the ledger and token estimates |
| Meter vs token drain detection ("my plan drains when I'm away") | M | aicm alone has both sides for one account |
| Banked limit resets (inventory, expiry) | S–M | needs a source for Claude's |
| Read-only provider spend caps; later a kill switch | S / M–L | OpenAI and Anthropic now have cap APIs |
| Plan right-sizing ("Max 20x used like Max 5x") | S–M | |
| Bedrock / Vertex / Azure billing, LiteLLM and Open WebUI spend | M each | |
| Native menu bar app | L | `aicm-agent statusline` and a JSON endpoint first |

## Already covered, demand confirmed

Several accounts per provider, one hub for many machines, provider-native 5-hour and weekly meters, quota-waste alert, `recommend_account` over MCP (CodexBar declined routing), multi-currency, renewal reminders with cancel links, low-balance alerts, sync-failure alerts, roles and audit log, backups, CSV export, data kept local, Docker images and self-update.

## Pitfalls competitors hit

Repeated macOS Keychain prompts (CodexBar's most-reacted bug class), polling that spends the user's own quota, assuming windows are fixed (Codex removed its 5-hour window for a while; banked resets move the weekly date), and cluttered overviews.

## Decisions needed

1. Alert channels in the self-hosted hub (PLAN lists push, Slack and Telegram as hosted-only advantages).
2. TOTP and OIDC sign-in (PLAN: not planned).
3. Budgets and anomaly alerts before the release (PLAN: after MVP).
