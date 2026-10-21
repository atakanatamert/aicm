# Task index

Task cards live next to this file as `NN-short-name.md`, written by Opus from `TEMPLATE.md` when a task is about to start. Order and dependencies come from `docs/PLAN.md` ("Build order and delegation").

Tasks 1–5 are the contracts Opus writes before any delegation. Large tasks were split (7b, 14a/b, 18a/b, 15–17 one card per adapter) and two were added (S: sync engine, 33: exports). Each card's Outcome section records the merge commit, attempts and decisions.

| # | Phase | Task | Writer | Needs | Status |
| --- | --- | --- | --- | --- | --- |
| 1 | Contracts | Repo skeleton, binaries, config, Docker, licenses, CI | Opus | none | merged |
| 2 | Contracts | Migrations, sqlc, River setup | Opus | 1 | merged |
| 3 | Contracts | OpenAPI spec (+ generated Go server/client) | Opus | 2 | merged |
| 4 | Contracts | Provider/meter/money contracts, HTTP client, fixtures | Opus | 1 | merged |
| 5 | Contracts | agentlink protocol, harness interface | Opus | 1 | merged |
| 6 | Core | `secrets`: envelope encryption, rotate-key | DeepSeek | 2 | merged |
| 7 | Core | `auth`: passwords, sessions, middleware, roles, setup, login | DeepSeek | 2, 3 | merged |
| 7b | Core | Users, groups, API tokens, grants | DeepSeek | 7 | merged |
| 8 | Core | `audit` list endpoint, rows for existing mutations | DeepSeek | 7 | merged |
| 9 | Core | `account` CRUD, plans, keys, catalog, renewals, roll-forward | DeepSeek | 7 | merged |
| 10 | Core | `charge`: renewal fees, top-ups, spend, charges CSV | DeepSeek | 9 | merged |
| 11 | Core | `fx`: Frankfurter + ECB, conversion | DeepSeek | 2 | merged |
| 12 | Core | `usage` rollups, `pricing` import | DeepSeek | 2 | merged |
| 13 | Core | `quota`: meters, scoring, recommend, forecast | Opus | 4 | merged |
| S | Hub sync | Sync engine: dispatcher, per-account jobs, sync now | Opus | 9, 12, 13 | merged |
| 14a | Core | `alert`: evaluation, inbox, rules API | DeepSeek | 9, 13 | merged |
| 14b | Core | `notify`: SMTP, one email per alert; org settings | DeepSeek | 14a | merged |
| 15a–c | Hub sync | Adapters: OpenRouter, DeepSeek, Moonshot | DeepSeek | 4 | merged |
| 16a–c | Hub sync | Adapters: Anthropic, OpenAI, xAI | DeepSeek | 4 | merged |
| 17a–b | Hub sync | Adapters: Z.ai, GitHub Copilot | DeepSeek | 4 | merged |
| 18 | Agent | Agent core: enroll, link, spool, cursors, self-update | DeepSeek | 5 | merged |
| 18a | Agent | Hub: enroll tokens, machines API, hub key | DeepSeek | 5, 6, 7 | merged |
| 18b | Agent | Hub: agent WebSocket, ingest, confirm/dismiss | DeepSeek | 18a | merged |
| 19 | Agent | Install scripts (sh, PowerShell), hub-served | DeepSeek | 18 | merged |
| 20 | Agent | `harness/claudecode` + statusline wrapper | DeepSeek | 5 | merged |
| 21 | Agent | `harness/codex` + app-server poll | DeepSeek | 5 | merged |
| 22 | Agent | `harness/omp` | DeepSeek | 5 | merged |
| 23 | Agent | Opt-in Claude usage poll | DeepSeek | 20 | merged |
| 24 | UI | Shell, login, theme, generated client, embedding | GLM | 3 | merged |
| 25 | UI | Quotas and renewals page | GLM | 13, 24 | merged |
| 26 | UI | Overview: money out | GLM | 10, 11, 24 | merged |
| 27a | UI | Accounts, add account, discovered review, Usage | GLM | 9, 12, 24 | merged |
| 27b | UI | Account detail | GLM | 27a | merged |
| 34 | UI | Fixes from the Impeccable critique | GLM | 25–28b | merged |
| 35 | UI | First-run import, one primary provider link, sort and filter, harness names | GLM | 34 | merged |
| 36 | Contracts | Migration 00007, spec, agent protocol fields, decisions 30–32 | Opus | 35 | merged |
| 37 | Hub | Alert channels: webhook, ntfy, Telegram, Discord, Slack | DeepSeek | 36 | merged |
| 38 | Hub | Budgets and alerts: budget, limits back, runaway spend, wrong wallet | DeepSeek | 36 | merged |
| 40 | Agent | Project per usage event; billing mode and extra usage details | DeepSeek | 36 | merged |
| 41 | Hub | Usage by project | DeepSeek | 36 | merged |
| 42 | Hub | Two-factor (TOTP) and OIDC sign-in | DeepSeek | 36 | merged |
| 43a | UI | Channels, budgets, new alert kinds, wallet runway | GLM | 37, 38 | merged |
| 43b | UI | Two-factor, SSO button, usage by project | GLM | 41, 42 | merged |
| 28a | UI | Alerts inbox and rules, Machines | GLM | 14a, 18a, 24 | merged |
| 28b | UI | Settings | GLM | 7b, 8, 14b, 24 | merged |
| 29 | MCP | `aicm-agent mcp` and hub `/mcp` | DeepSeek | 3, 13 | merged |
| 30 | Release | GoReleaser, GHCR, Homebrew tap, SBOM | Opus | 19 | merged |
| 31 | Catalog | Manual and agent-captured providers | DeepSeek | 4 | merged |
| 32 | Release | README providers table + CI drift check | DeepSeek | 31 | merged |
| 33 | Core | Usage and readings CSV exports | DeepSeek | 12, 13 | merged |
