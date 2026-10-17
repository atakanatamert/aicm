# Subscription quota capture: research notes

Researched 2026-10-17. Confidence: **H** = official docs or source code, **M** = secondary sources. Re-check field names against a live account before finalizing any parser.

## Claude Code (Pro / Max)

**Rule:** Anthropic's [legal and compliance page](https://code.claude.com/docs/en/legal-and-compliance) says: "developers may not collect, store, or intermediate Claude.ai credentials or session tokens." The hub never receives a Claude token.

### 1. Statusline stdin (primary, passive) – H

Docs: https://code.claude.com/docs/en/statusline (verified 2026-10-17)

```json
"rate_limits": {
  "five_hour": { "used_percentage": 23.5, "resets_at": 1738425600 },
  "seven_day": { "used_percentage": 41.2, "resets_at": 1738857600 }
}
```

- `used_percentage` runs 0–100. `resets_at` is in Unix epoch seconds.
- Present only for claude.ai Pro and Max, and only after the session's first API response. Each window can be absent, and Claude Code drops a window once its `resets_at` passes.
- Behind a Claude apps gateway there is also `rate_limits.spend_limit` (`used_percentage`, `resets_at`, `used_usd`, `limit_usd`, `period`).
- Added in v2.1.80. The field has gone missing in some releases (issues #40094, #60612). A reported bug once put the `resets_at` epoch into `used_percentage`, so validate that it is between 0 and 100.
- Only one `statusLine` command is allowed. The agent installs a wrapper that:
  - runs the user's existing statusline and passes its output through unchanged
  - appends `{captured_at, five_hour, seven_day}` to a local spool file
- It does **not** include the per-model weekly windows (Opus, Sonnet) or extra-usage credits.

### 2. Headless runs – H

`claude -p --output-format stream-json` and the Agent SDK emit a `rate_limit_event` (since v2.1.45):

```json
{"type":"rate_limit_event","rate_limit_info":{"status":"allowed|allowed_warning|rejected","resetsAt":0,"utilization":0}}
```

It carries only the binding window.

### 3. Hooks (alert triggers only) – H

- `StopFailure` with `error: "rate_limit"`
- `Notification` types `quota_auto_resume_fired`, `quota_auto_resume_stale` and `quota_auto_resume_disabled` (v2.1.234+)

No hook carries utilization numbers. OpenTelemetry has no rate-limit metrics.

### 4. Usage endpoint (opt-in fallback, agent only) – H for shape

Request: `GET https://api.anthropic.com/api/oauth/usage` with `Authorization: Bearer <local OAuth token>` and `anthropic-beta: oauth-2025-04-20`.

Response fields:
- `five_hour`, `seven_day`, `seven_day_oauth_apps`, `seven_day_opus` and `seven_day_sonnet`, each `{utilization: 0-100, resets_at: ISO8601}` or null
- `extra_usage {is_enabled, monthly_limit, used_credits, utilization}`

How to call it:
- The endpoint rate-limits aggressively. Poll at most every 15 minutes, and only when the statusline data is more than 30 minutes old.
- Honour `Retry-After`, back off exponentially, and keep one cache per machine.
- Read the token from the macOS Keychain (`Claude Code-credentials`) or from `~/.claude/.credentials.json` (`claudeAiOauth.accessToken`). Read only; never refresh it.

## Codex CLI (ChatGPT plans)

### 1. Rollout logs (primary, passive) – H

Files: `$CODEX_HOME/sessions/YYYY/MM/DD/rollout-*.jsonl` (default `~/.codex`). Some builds may compress them as `.jsonl.zst`, so handle both.

Look for `event_msg` with `payload.type == "token_count"`:

```json
"rate_limits": {
  "limit_id": "...", "limit_name": "...",
  "primary":   { "used_percent": 12.0, "window_minutes": 300,   "resets_at": 0 },
  "secondary": { "used_percent": 40.0, "window_minutes": 10080, "resets_at": 0 },
  "credits": { "has_credits": true, "unlimited": false, "balance": "..." },
  "plan_type": "pro"
}
```

- `rate_limits` can be null, for example under API-key auth.
- `info.total_token_usage` is cumulative, so take deltas. `last_token_usage` holds the per-turn usage.

### 2. Official app-server (secondary) – H

Spawn `codex app-server` locally and use JSON-RPC:
- Call `account/rateLimits/read` with `{ "excludeResetCreditDetails": true }`.
- It returns `rateLimits.primary` and `rateLimits.secondary` `{usedPercent, windowDurationMins, resetsAt}`, plus `credits` and `planType`.
- Subscribe to the `account/rateLimits/updated` notification.
- Poll every 10–15 minutes while idle.

Never read `auth.json` for this. The official binary uses its own auth.

Source: https://github.com/openai/codex/blob/main/codex-rs/app-server-protocol/src/protocol/v2/account.rs

## Gemini CLI

Left out. Google's terms ban third-party use of Gemini CLI OAuth. Use local usage estimates from `~/.gemini/tmp/*/chats/*.json` only.

## API-key coding plans (hub polls, low risk)

- **Z.ai GLM Coding Plan** – H:
  - `GET https://api.z.ai/api/monitor/usage/quota/limit` (`open.bigmodel.cn` in China), with the raw API key in `Authorization` and no `Bearer`.
  - Limits are typed `TOKENS_LIMIT`, `TIME_LIMIT` or `CREDIT_LIMIT`, with `unit`, `number`, `percentage`, `currentValue`, `remaining` and `nextResetTime`.
  - `/api/biz/subscription/list` gives the plan and its renewal date.
  - The vendor's own `zai-org/zai-coding-plugins` uses these endpoints.
- **Kimi coding** – H: `GET https://api.kimi.com/coding/v1/usages` with a Bearer key. Returns `usage {limit, used, remaining}` and `limits[] {detail, window {duration, timeUnit}}`.
- **MiniMax** – M: `GET https://api.minimax.io/v1/token_plan/remains` with a Bearer key. Returns `model_remains[]` with interval and weekly counts.
- **GitHub Copilot** – H:
  - Official: `GET /users/{u}/settings/billing/ai_credit/usage` (and `premium_request/usage`) with a fine-grained PAT that has "Plan: read". It reports usage only, so remaining = allowance − net.
  - Opt-in fallback: `api.github.com/copilot_internal/user`.

## Every reading stores

`source` ∈ {`statusline`, `stream_json`, `rollout`, `app_server`, `api`, `manual`} and `captured_at`. A reading counts as stale after its `resets_at`, at which point the window is back to 0.
