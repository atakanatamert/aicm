# Changelog

All notable changes to aicm are listed here. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html). Each release covers both binaries: the hub (`aicm`) and the agent (`aicm-agent`).

## [Unreleased]

## [0.1.0] - Unreleased

First release. It collects everything shipped in the pre-releases v0.1.0-rc.1 to v0.1.0-rc.10 plus the Windows fixes made since.

### Added

- Self-hosted hub: one Go server with the web UI built in, running with Postgres through Docker Compose. A new hub asks for the first admin; `aicm create-admin` does the same from the command line.
- Accounts for every subscription and API wallet: plan and list price, renewal day, billed currency, a write-only key and archiving. A catalog of more than 20 providers gives each account its billing, top-up, usage and docs links, and providers without an API can be tracked by hand.
- Automatic sync for OpenRouter, Anthropic API, OpenAI API, xAI API, DeepSeek, Moonshot (Kimi), Z.ai GLM Coding Plan, Z.ai API pay-as-you-go, Google AI Studio (Gemini API), TypeSafe and GitHub Copilot AI credits. One Z.ai key finds both Z.ai accounts, and Sync now on an account shows when the fresh result lands.
- Quotas page split into Subscriptions and API credits, with every meter in the provider's own words and units, renewal cards and a 24-hour forecast.
- Overview with money out by month and year in the billed currency, converted with daily Frankfurter and ECB rates; renewal fees, suggested top-ups, wallet runway and a dialog to record a charge.
- Monthly budgets for the whole organization or for one account.
- Alerts for renewals, low balances, unused quota, budgets, limits coming back, runaway spend and use of the wrong wallet, with an in-app inbox and editable rules.
- Notifications by email over any SMTP server and through webhook, ntfy, Telegram, Discord and Slack channels.
- Usage page by day, model, account, provider and project, opening on the last 30 days. Token usage is priced with LiteLLM's model prices.
- CSV exports for charges, usage and meter readings.
- Users, groups, API tokens and per-account access grants, plus an audit log of every change for admins.
- Two-factor sign-in with any TOTP app and recovery codes, and single sign-on through any OpenID Connect provider.
- MCP server so coding agents can ask which account to use right now: over stdio with `aicm-agent mcp`, and over streamable HTTP at `/mcp` on the hub.
- Machines page with a one-line enrollment command per platform and status dots for machines, account sync, enrollment tokens and users.
- Nightly backup service that writes a database dump together with the master key, keeps the newest 14, can hand the files to a host user and comes with tested restore steps.
- `aicm rotate-key` to rotate the master key.
- `aicm-agent` for macOS, Linux and Windows: enrolls with the hub, keeps an outbound link, spools data on disk while the hub is unreachable and updates itself to the hub's version.
- One-line installers for macOS, Linux and Windows served by the hub, a Homebrew formula and a Docker image. The agent runs as a user-level service and gets the installing shell's PATH so it finds tools like `codex`.
- Claude Code support: accounts, usage from session logs and the 5-hour and weekly limits recorded through the statusline. The installer wraps your existing statusline (`--no-statusline` skips it, `aicm-agent statusline uninstall` reverts it). An opt-in usage poll checks at most every 15 minutes and respects Retry-After.
- Codex support: accounts, usage from rollout files, rate limits and credits, polled through the Codex app server.
- omp support: API keys from omp's database, session usage including side calls, and omp's Claude and ChatGPT logins counted toward those subscriptions.
- Found on your machines: accounts the agent discovers wait for review. Confirming one imports its key and, for subscriptions, fills in the list price, renewal day and estimated past fees.
- The agent tags usage with the project it came from, and records billing mode and extra usage on subscription accounts.
- Release archives with checksums and SBOMs, a hub container image and a README providers table generated from the catalog.

### Changed

- Licensing: all code is now under the Sustainable Use License 1.0 (free to self-host and modify for internal, personal and non-commercial use; selling it or offering it as a paid service is reserved to the maintainer). Pre-releases up to v0.1.0-rc.10 keep their original AGPL-3.0-or-later (hub) and Apache-2.0 (agent) licenses.

### Fixed

- Every checkout uses LF line endings. Backups and the `install.sh` the hub serves were broken when built from a Windows checkout.
- The Claude Code statusline wrapper runs in Claude Code's own shell on Windows (Git Bash or PowerShell).
- The installers and `aicm-agent update` pick the newest published release while only pre-releases exist.
- `aicm-agent mcp --token-file` works on Windows.
- Confirming a found subscription fills in its plan price and renewal right away.
- Usage charts show days without usage as zero instead of skipping them.
- A session started at a drive root no longer shows up as a project.
- A fresh clone builds without building the web UI first.
- Storing a key for a provider behind an unofficial endpoint explains the opt-in instead of failing with a server error, and Z.ai's quota endpoint is allowed by default.
- OpenRouter inference keys sync the balance; the activity endpoint is only used with a management key.
- Unused-quota alerts only fire for windows that have started, at most once a day per account.
- Supplemental meters such as Codex credits and DeepSeek's granted balance never mark an account as unusable.
- An expired or replayed single sign-on attempt sends you back to the login page with an error.

### Security

- Provider keys are encrypted at rest with per-organization data keys under a master key, are never shown again after saving, and the API exposes at most their last four characters.
- Subscription login tokens for Claude, ChatGPT and Gemini never leave the agent's machine. The agent reads Claude Code, Codex and omp files read-only and never refreshes a token.
- Agents enroll with a revocable enrollment token, then authenticate with their own machine token over a handshake signed with the hub's key.
- Passwords are hashed with Argon2id, and secrets are redacted from audit log entries.

[Unreleased]: https://github.com/atakanatamert/aicm/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/atakanatamert/aicm/releases/tag/v0.1.0
