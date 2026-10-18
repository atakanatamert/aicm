# 21 – Harness: Codex CLI (accounts, rollout usage, rate limits, app-server poll)

**Writer:** DeepSeek Flash
**Depends on:** 05
**License of touched packages:** Apache-2.0

## Goal

`internal/harness/codex` discovers the ChatGPT account used by Codex, reads per-turn token usage and the primary/secondary rate-limit windows from Codex rollout logs, and polls the official `codex app-server` for fresh windows when Codex hasn't run for a while.

## Files to touch

- `internal/harness/codex/codex.go` (new): `Harness`, Discover
- `internal/harness/codex/rollout.go` (new): rollout parser
- `internal/harness/codex/appserver.go` (new): `harness.Poller` via `codex app-server`
- `internal/harness/codex/codex_test.go` (new): golden tests
- `internal/harness/codex/testdata/*` (new): synthetic fixtures and goldens
- `THIRD_PARTY_NOTICES.md` (edit only if you port code)

Touch nothing else. Opus wires the harness into `cmd/aicm-agent` after merging; the constructor receives the home and data directories as arguments. Apache-2.0 SPDX headers; import only `internal/agentlink`, `internal/harness`, `internal/meter`, `internal/version` and the standard library (no zstd library: skip `.jsonl.zst` files and count them in `HarnessStatus.Error` as "compressed rollouts not supported yet").

## Read first

- `CLAUDE.md` (security rules!), `docs/research/harnesses.md`, `docs/research/quota-capture.md` (Codex sections)
- `internal/harness/harness.go`, `internal/agentlink/protocol.go`, `internal/meter/meter.go`, `internal/provider/manual/manual.go` (`chatgpt` meters: `primary`, `secondary`, `credits`)
- Codex source for exact shapes: https://github.com/openai/codex — `codex-rs/protocol` (rollout `RolloutItem`, `EventMsg::TokenCount`, `TokenCountEvent`, `RateLimitSnapshot`), `codex-rs/app-server-protocol/src/protocol/v2/account.rs` (`account/rateLimits/read`, `account/read`). Cite the files you used in your summary.

## Contract

```go
package codex

const ID = "codex"
// New uses CODEX_HOME, else <home>/.codex.
func New(home string) *Harness
var _ harness.Harness = (*Harness)(nil)
var _ harness.Poller = (*Harness)(nil)
```

### Security

- Read-only. `auth.json` may be read only to decode the **payload** of `tokens.id_token` (base64url, no signature check, no network) and take the `https://api.openai.com/auth` claims `chatgpt_account_id`, `chatgpt_plan_type`, and the top-level `email`. Never read, log or send the access or refresh token. When `auth.json` is absent (keyring builds), use the app-server `account/read` result for the account id and plan if the protocol exposes them; otherwise report no account.
- The poller starts the official `codex` binary from PATH; it never handles credentials itself.

### Discover

`DiscoveredAccount{Ref: {ProviderID: "chatgpt", ExternalID: chatgpt_account_id}, Harness: "codex", Label: email, Plan: chatgpt_plan_type, AuthKind: "agent_capture"}`. API-key auth (`OPENAI_API_KEY` in auth.json, no id_token) → no account, usage not reported (say so in your summary).

### Rollouts (`rollout.go`)

Files `<codex home>/sessions/YYYY/MM/DD/rollout-*.jsonl`, read with cursors as in the Claude harness (inode/offset/size, complete lines only). Track the current model from `turn_context` lines. For `event_msg` lines whose payload type is `token_count`:
- usage: from `info.last_token_usage` (per turn) → `UsageEvent{Account, Source: "codex", MessageID: <session id from the file's session_meta or file name> + ":" + <line timestamp>, OccurredAt: line timestamp, Model, InputTokens: input_tokens − cached_input_tokens, CacheReadTokens: cached_input_tokens, OutputTokens: output_tokens, ReasoningTokens: reasoning_output_tokens}`. Skip when `info` is null.
- meters: when `rate_limits` is not null: `primary` → meter `primary`, `secondary` → `secondary`, `Used` = `used_percent`, `Limit` 100, `ResetsAt` from `resets_at` (epoch seconds; older builds send `resets_in_seconds` relative to the line timestamp: support both), and the `Def` copied from the catalog with `WindowMinutes` from `window_minutes` and a label from it: 300 → "5-hour limit", 10080 → "Weekly limit", otherwise "<n>-minute window". `Source: meter.SourceRollout`. `credits` (when present, `balance` as a decimal string, unlimited → skip) → meter `credits`.
- Only the newest reading per meter per scan is reported.

### App-server poll (`appserver.go`)

`PollInterval()` = 15 min. `Poll` runs only when the newest rollout reading is older than 30 min. Start `codex app-server` (find `codex` with `exec.LookPath`; not found → no readings, no error), speak JSON-RPC 2.0 over stdio (newline-delimited JSON; check the source for the framing): `initialize` (client info name `aicm-agent`, version from `version.Version`), then `account/rateLimits/read` with `{"excludeResetCreditDetails": true}` → `rateLimits.primary` / `secondary` (`usedPercent`, `windowDurationMins`, `resetsAt`) and `credits` → `MeterReport`s with `Source: meter.SourceAppServer`. Kill the process after the call; whole poll times out after 20 s.

## Done when

- [ ] `codex_test.go` golden tests with **synthetic** fixtures (made-up ids, `test@example.com`, a JWT you build in the test with a fake signature):
  - `TestDiscoverGolden`: auth.json with an id_token → discovered account; golden has no token.
  - `TestRolloutGolden`: a rollout with session_meta, turn_context, three token_count events (one with `info: null`, one with `rate_limits: null`, one with both windows), a malformed and a partial line → events and meter reports golden; re-scan with returned cursors yields nothing; `.jsonl.zst` file is skipped.
  - `TestAppServerGolden`: run the poller against a fake `codex` script in `testdata/` placed first on `PATH` (a tiny shell script that prints canned JSON-RPC responses) → meter reports golden.
- [ ] Smoke on this machine (read-only): a throwaway program in `/tmp` calls `Discover`, one `Scan`, and `Poll` if `codex` is installed, and prints counts only.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.

## Outcome (filled in by Opus)

Merged in `0aad775` after the first attempt. Opus changed credits handling afterwards: a reading only when `has_credits` is true (a zero balance on accounts without credits was being scored as an exhausted meter).
