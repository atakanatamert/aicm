# Security policy

## Supported versions

aicm is before 1.0. Only the latest release (including the latest pre-release) of the hub (`aicm`) and the agent (`aicm-agent`) gets security fixes. Fixes ship as a new release, not as patches to older ones, so please update before reporting and check whether the problem is still there.

| Version | Supported |
| --- | --- |
| Latest release | Yes |
| Anything older | No |

## Reporting a vulnerability

Report vulnerabilities privately through a GitHub security advisory:

https://github.com/atakanatamert/aicm/security/advisories/new

Please do not open a public issue, pull request or discussion for a security problem, and do not post details anywhere public until a fix is released.

Include as much of this as you can:

- The affected component (hub, agent, installer, MCP server, web UI) and version (`aicm version`, `aicm-agent version`).
- How the hub or agent was installed (Docker Compose, release binary, Homebrew, install script) and the operating system.
- What an attacker can do, and what access they need first (none, a signed-in user, an admin, a machine on the same network, local access to the agent's machine).
- Steps to reproduce, or a proof of concept.
- Any logs or requests that show the problem, with real API keys, tokens, passwords and the master key removed.

## What to expect

aicm is maintained by one person in their spare time, so please be patient:

- I aim to acknowledge a report within a few days.
- I will tell you whether I can reproduce it and keep you posted while a fix is prepared.
- Once a fix is released, the advisory is published. You are credited unless you would rather not be.

## Scope

A few boundaries matter most in aicm, and reports about them are especially welcome:

- **Stored provider keys.** The hub stores provider API and admin keys encrypted with per-organization data keys, which are in turn encrypted with the master key (`master.key`). The API returns at most the last four characters of a key. Anything that exposes a stored key in plain text, through the API, the UI, logs, exports, backups or the audit log, is in scope.
- **Subscription login tokens.** The agent reads local harness files (Claude Code, Codex, omp) read-only. Subscription OAuth tokens for Claude, ChatGPT and Gemini never leave the machine the agent runs on, are never sent to the hub and are never refreshed by the agent. Any path that sends such a token to the hub, writes to a harness's credentials or refreshes a token is in scope.
- **Access control.** Users only see the accounts they have been granted. Anything that lets a user read or change an account, machine, user or setting they should not have access to is in scope.
- **Agent link and enrollment.** Enrollment tokens, the agent's link to the hub and agent self-update.

Out of scope:

- Problems that need an attacker who already controls the hub's host, its database or the master key file.
- Weaknesses in a provider's own API or in Claude Code, Codex or omp themselves; report those to their vendors.
- Missing hardening headers or best-practice suggestions without a demonstrated impact. These are still welcome as a normal issue.
