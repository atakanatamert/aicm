# Task index

Task cards live next to this file as `NN-short-name.md`, written by Opus from `TEMPLATE.md` when a task is about to start. Order and dependencies come from `docs/PLAN.md` ("Build order and delegation").

Tasks 1–5 are the contracts Opus writes before any delegation. After Atakan approves them, tasks 6–12 run in parallel, then 15–28. Merge in number order.

| # | Phase | Task | Writer | Needs | Done when |
| --- | --- | --- | --- | --- | --- |
| 1 | Contracts | Repo skeleton, both `cmd/` binaries, config, Dockerfile, Compose, license files, CI with REUSE and CLA checks | Opus | none | `docker compose up` serves a health check |
| 2 | Contracts | Migrations for all tables with org\_id, sqlc config, River setup | Opus | 1 | `migrate up` runs clean |
| 3 | Contracts | OpenAPI spec for UI, MCP and export endpoints | Opus | 2 | Spec lints; Go and TS types generate |
| 4 | Contracts | Provider, harness, `MeterDef` and `MeterReading` types; shared HTTP client with fixture recorder | Opus | 1 | A fake adapter passes a golden test |
| 5 | Contracts | `agentlink` protocol: enroll, handshake, message types | Opus | 1 | Spec doc plus a loopback test |
| 6 | Core | `secrets`: envelope encryption, key versions, `rotate-key` | Flash | 2 | Round-trip and rotation tests pass |
| 7 | Core | `auth`: users, sessions, tokens, roles, grants | Flash | 2, 3 | Viewer cannot read an ungranted account (test) |
| 8 | Core | `audit` writes on every mutation, admin view endpoint | Flash | 7 | Each write leaves one row |
| 9 | Core | `account` CRUD, plans, renewal roll-forward | Flash | 7 | Roll-forward test passes |
| 10 | Core | `charge`: fees on renewal, top-ups, top-up detection from balance jumps | Flash | 9 | Money-math tests pass |
| 11 | Core | `fx`: ECB fetch job, rate-at-date lookup | Flash | 2 | Known date converts correctly (test) |
| 12 | Core | `usage` rollups and `pricing` table import | Flash | 2 | Aggregation test passes |
| 13 | Core | `quota`: meters, readings, staleness, scoring, forecast | Opus | 4 | Scoring cases agree with the written rule |
| 14 | Core | `alert`: rules, inbox, go-mail notifier, River jobs with uniqueness | Flash | 9, 13 | One email per event under restarts |
| 15 | Hub sync | Adapters: OpenRouter, DeepSeek, Moonshot (one task each) | Flash | 4 | Golden test passes |
| 16 | Hub sync | Adapters: Anthropic Admin, OpenAI Admin, xAI (one task each) | Flash | 4 | Golden test passes; paging handled |
| 17 | Hub sync | Adapters: Z.ai coding plan, GitHub Copilot billing (one task each) | Flash | 4 | Golden test passes |
| 18 | Agent | Agent core: enroll, WebSocket link, reconnect, spool, self-update | Flash | 5 | Survives hub restart; no duplicate pushes |
| 19 | Agent | Install scripts (sh, Homebrew, PowerShell) as user-level services | Flash | 18 | Fresh macOS and Linux installs enroll |
| 20 | Agent | `harness/claudecode`: discover, tail logs, statusline wrapper that chains the user's own | Flash | 18 | Sample log and sample stdin parse to expected rows |
| 21 | Agent | `harness/codex`: rollout tailing and `codex app-server` rate-limit reads | Flash | 18 | Sample rollout parses; app-server read works locally |
| 22 | Agent | `harness/omp` | Flash | 18 | Sample session parses |
| 23 | Agent | Opt-in Claude usage poll with cache, backoff, 15-minute floor | Flash | 20 | Off by default; respects `Retry-After` |
| 24 | UI | Shell, login, theme, generated client, Hairline on empty states | Flash | 3 | Login works |
| 25 | UI | Quotas and renewals page with provider-native meters and one-click links | Flash | 13, 24 | Sorted by urgency; links open provider pages |
| 26 | UI | Overview: money out by month and year, billed and converted | Flash | 10, 11, 24 | Matches API totals |
| 27 | UI | Accounts, account detail, usage, discovered-account review | Flash | 9, 24 | Viewer sees no edit controls |
| 28 | UI | Alerts inbox, Machines page, Settings (users, grants, SMTP, currency, audit log) | Flash | 14, 18, 24 | All settings save |
| 29 | MCP | `aicm-agent mcp` and hub `/mcp` with every tool | Flash | 3, 13 | Works from Claude Code; viewer blocked on writes |
| 30 | Release | GoReleaser: GHCR images, agent binaries, Homebrew tap, SBOM | Opus | 19 | Snapshot release installs end to end |
| 31 | Catalog | Links and manual meters for every provider in the table | Flash | 4 | Every listed provider has links |
| 32 | Release | README supported-providers table generated from the catalog, plus CI drift check; THIRD\_PARTY\_NOTICES | Flash | 31 | CI fails when the README table is stale |
