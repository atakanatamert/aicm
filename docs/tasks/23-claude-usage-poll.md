# 23 – Opt-in Claude usage poll (agent only)

**Writer:** DeepSeek Flash
**Depends on:** 20
**License of touched packages:** Apache-2.0

## Goal

For Claude accounts whose owner opted in (`allow_unofficial` on the account, delivered to the agent in `AgentConfig`), the agent reads the per-model weekly windows (Opus, Sonnet) and extra-usage credits from `GET https://api.anthropic.com/api/oauth/usage` with the local Claude Code token, on this machine only, at most every 15 minutes, only when the statusline reading is over 30 minutes old, honouring `Retry-After`. Off by default (Decision 8).

## Files to touch

- `internal/harness/claudecode/usagepoll.go` (new): `harness.Poller` implementation on `*Harness`
- `internal/harness/claudecode/usagepoll_test.go` (new)
- `internal/harness/claudecode/testdata/usagepoll*` (new)

Touch nothing else.

## Read first

- `CLAUDE.md` (security rules!), `docs/DECISIONS.md` #8 and #9, `docs/research/quota-capture.md` ("Usage endpoint (opt-in fallback, agent only)")
- `internal/harness/claudecode/*.go` (config dirs, credentials reading, statusline spool, meter defs), `internal/harness/harness.go` (`Poller`), `internal/agentlink/protocol.go` (`AgentConfig.Accounts[].AllowUnofficial`)
- `internal/provider/manual/manual.go` (`claude` meters `five_hour`, `seven_day`, `seven_day_opus`, `seven_day_sonnet`)

## Contract

```go
var _ harness.Poller = (*Harness)(nil)
func (h *Harness) PollInterval() time.Duration // 15 * time.Minute
func (h *Harness) Poll(ctx context.Context, cfg agentlink.AgentConfig) ([]agentlink.MeterReport, error)
```

Rules, all enforced inside `Poll`:
- For each discovered Claude account: skip unless `cfg.Accounts` has `{Ref: {claude, accountUuid}, Status != "dismissed", AllowUnofficial: true}`.
- Skip when the newest statusline capture for that account is less than 30 minutes old.
- Per-account cache in `<dataDir>/claude-usage-poll.json`: last attempt time and `retry_after_until`. Never call more than once per 15 minutes per account, never before `retry_after_until`. A 429 sets `retry_after_until` from `Retry-After` (seconds or HTTP date; default 30 min when absent) and doubles the next minimum gap up to 4 h; a success resets it.
- Token: read the access token the same way Discover reads the plan (macOS Keychain `Claude Code-credentials`, else `<configdir>/.credentials.json` → `claudeAiOauth.accessToken`), hold it in a local variable only for the request, never log, store or return it. If `expiresAt` is in the past, skip (never refresh; Claude Code refreshes its own token).
- Request: `GET https://api.anthropic.com/api/oauth/usage`, headers `Authorization: Bearer <token>`, `anthropic-beta: oauth-2025-04-20`, `User-Agent: aicm-agent/<version>`; 15 s timeout. 401/403 → skip quietly until the next interval (token likely expired).
- Response mapping (`utilization` 0–100, `resets_at` ISO 8601, any window may be null): `five_hour` → `five_hour`, `seven_day` → `seven_day`, `seven_day_opus` → `seven_day_opus`, `seven_day_sonnet` → `seven_day_sonnet`, each `Used` = utilization, `Limit` 100, `Source: meter.SourceAPI`, `CapturedAt` now. `extra_usage` (when `is_enabled`) → no meter in this task; mention it in your summary.
- The base URL is a package variable so the test can point it at `httptest`.

## Done when

- [ ] `usagepoll_test.go` with `httptest` and a fake home (synthetic credentials file with a fake token):
  - `TestPollOffByDefault`: account not opted in → no request made.
  - `TestPollMapsWindows` (golden): opted-in account, stale statusline → one request with the right headers (assert the bearer value equals the fake token and appears nowhere in the returned reports), four meter reports with one null window skipped.
  - `TestPollFloorAndRetryAfter`: a second call within 15 min makes no request; a 429 with `Retry-After: 120` blocks requests until 120 s later (inject a clock); fresh statusline (< 30 min) → no request.
  - `TestExpiredTokenSkipped`: `expiresAt` in the past → no request.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] No real request to Anthropic is made by the tests or by you.

## Outcome (filled in by Opus)

Merged in `9e855e6` after the first attempt. No request to Anthropic was made; `extra_usage` is decoded but not stored as a meter yet.
