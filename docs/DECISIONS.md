# Decisions

Settled with Atakan on 2026-10-17. Changing any of these needs Atakan's OK. Add new decisions at the bottom with a date.

| # | Decision | Why |
| --- | --- | --- |
| 1 | Web app first: Go hub with embedded React UI, Docker + Postgres. Desktop shell (Wails) only later, if wanted. | The stack (multi-user, roles, Postgres) is a server; one container serves API and UI. |
| 2 | React + Vite + TypeScript. No Next.js, no HTMX. | Logged-in dashboard; no SSR or SEO need. |
| 3 | UI: shadcn/ui, shadcn charts (Recharts v3), Motion, Hairline for illustration. Impeccable as a design-review step. | Clean and minimal with a little polish. |
| 4 | Go: stdlib `net/http`, `pgx` + `sqlc`, `goose`, River, go-mail, `slog`, official MCP Go SDK, `coder/websocket`. Feature packages, no DDD layers, no ORM. | Clean structure is the priority. |
| 5 | Tests only for crypto, permissions, money math, scoring, parsers, adapters. | Avoid test bloat. |
| 6 | Roles admin, editor, viewer, plus per-account grants to users or groups. Email/password login, API tokens. No SSO in the open core. | Simplest model that limits who sees which accounts. |
| 7 | One `aicm-agent` per machine, Beszel-style: outbound WebSocket, universal enrollment token, hub signature check, machine fingerprint, user-level service, self-update. | Painless setup on many machines; works behind NAT. |
| 8 | No subscription OAuth tokens on the hub. Claude quota from a statusline wrapper; Codex quota from rollout logs and `codex app-server`. Claude `/api/oauth/usage` poll is opt-in, agent-only, at most every 15 min. | Anthropic's terms forbid third parties collecting or intermediating Claude.ai tokens; minimize ban risk. |
| 9 | Unofficial quota endpoints are opt-in per account. Gemini CLI subscription quota is left out (Google bans third-party OAuth use). | Atakan's call: needed for the product, but minimal use. |
| 10 | Quotas are provider-native meters (provider's names, units, reset rules). Only the ranking score is derived. | Atakan's call: don't normalize providers into one shape. |
| 11 | Scoring: headroom = min(1, time-to-empty / time-to-reset) per meter; an account scores by its weakest meter. Rank: feasible, then most expiring quota, then lowest marginal cost, then highest headroom. 24 h hourly forecast. | Atakan's burn-rate idea; explainable to agents. |
| 12 | Money out (fees, top-ups, invoices) is the headline, in billed currency. Token-cost estimates are secondary. | Atakan's call. |
| 13 | Currency conversion in v1 through a `RateSource` interface: Frankfurter by default, ECB fallback, rate at each record's date. | Cheap to build; modular. |
| 14 | Balance-only providers are snapshotted hourly. Spend = start − end + top-ups. Upward jumps become suggested top-up charges. | Those APIs have no history. |
| 15 | Secrets: built-in envelope encryption (XChaCha20-Poly1305), per-org data keys, master key from env or file, key versions, `rotate-key`. `SecretStore` interface for OpenBao or KMS later. Not Infisical in v1. | Secure without extra infrastructure. |
| 16 | Alerts by email first (SMTP via go-mail) through a `Notifier` interface; in-app inbox; River jobs with uniqueness keys. | Reminders are the most important feature. |
| 17 | Audit log (append-only), CSV export and MCP (stdio via the agent, and `/mcp` over HTTP on the hub) are in v1. | Atakan's call. |
| 18 | Providers in MVP: OpenRouter, Anthropic API, OpenAI API, xAI, DeepSeek, Moonshot, Z.ai coding plan, GitHub Copilot. Google AI Studio is manual with deep links (no API). Everything else is catalog links plus manual entry; more on community request. | Major platforms first. |
| 19 | Hub license AGPL-3.0-or-later. Agent, MCP server and agent-link protocol Apache-2.0 (assumed, pending Atakan's confirmation). | Deter closed resellers; let companies install the agent. |
| 20 | CLA from the first outside PR (assumed, pending confirmation). Trademark the final name. | Keeps the hosted version and commercial licensing possible. |
| 21 | Hosted version later: `org_id` on every tenant table from day one; Cloud-only code in separate services or a commercial `ee/` folder; nothing a solo user or small team needs is held back from self-host. | Supabase or Grafana-style business without hurting trust. |
| 22 | README supported-providers table is generated from the code catalog (`aicm providers --markdown`); CI fails when it is stale. | It must never drift. |
| 23 | Reuse MIT and Apache projects (tokscale, ccusage, CodexBar, Claude-Code-Usage-Monitor, ccstatusline, codeburn, Frankfurter, Beszel, Hairline, LiteLLM price file) with attribution in `THIRD_PARTY_NOTICES.md`. Nothing from LiteLLM `enterprise/`. River unmodified only. | Licenses checked 2026-10-17. |
| 24 | Name `aicm` is a placeholder. | Pending Atakan. |
| 25 | API code is generated from `api/openapi.yaml`: Go server (strict, std `net/http`) and Go client with `oapi-codegen` v2 (runtime dep `oapi-codegen/runtime` plus `google/uuid`, `go-jsonmerge`); TypeScript with `openapi-typescript`, `openapi-fetch`, `openapi-react-query`. (2026-10-18) | Flash models fill in typed handlers instead of inventing request parsing; UI and Go types never drift from the spec. |
| 26 | Everything the agent binary links is Apache-2.0: `cmd/aicm-agent`, `internal/agentlink`, `internal/mcpserver`, `internal/apiclient`, `internal/harness`, `internal/meter`, `internal/version`. `scripts/check-licenses.sh` fails CI if the agent links an AGPL package. Applies Decision 19. (2026-10-18) | An Apache binary cannot link AGPL code. |
| 27 | Writers are reached through the omp CLI: DeepSeek Flash as `deepseek/deepseek-flash`, GLM 5.3 Flash as `zai/glm-5.3-flash`, one git worktree and branch per task. (2026-10-18) | Isolated parallel tasks; diffs reviewable per branch. |
