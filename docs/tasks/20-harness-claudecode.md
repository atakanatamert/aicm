# 20 – Harness: Claude Code (accounts, usage logs, statusline capture)

**Writer:** DeepSeek Flash
**Depends on:** 05
**License of touched packages:** Apache-2.0

## Goal

`internal/harness/claudecode` discovers the Claude.ai account(s) on the machine, reads per-request token usage from Claude Code's session logs, and turns statusline captures into 5-hour and weekly meter readings. Export `RunStatusline(stdin io.Reader, stdout io.Writer, dataDir string) int` (the wrapper Claude Code calls; returns the exit code), methods `(h *Harness) Install(executable string) error` and `(h *Harness) Uninstall() error`; Opus maps them to `aicm-agent statusline`, `statusline install`, `statusline uninstall`. They chain the user's existing statusline unchanged.

## Files to touch

- `internal/harness/claudecode/claudecode.go` (new): `Harness` implementation
- `internal/harness/claudecode/logs.go` (new): log parser
- `internal/harness/claudecode/statusline.go` (new): wrapper, capture spool, install/uninstall
- `internal/harness/claudecode/claudecode_test.go` (new): golden tests
- `internal/harness/claudecode/testdata/*` (new): synthetic fixtures and goldens
- `THIRD_PARTY_NOTICES.md` (edit only if you port code, see below)

Touch nothing else. Opus wires the harness into `cmd/aicm-agent` after merging; the constructor receives the home and data directories as arguments. Apache-2.0 SPDX headers; import only `internal/agentlink`, `internal/harness`, `internal/meter`, `internal/version` and the standard library.

## Read first

- `CLAUDE.md` (security rules!), `docs/research/harnesses.md`, `docs/research/quota-capture.md` (Claude Code sections), `docs/agentlink.md`
- `internal/harness/harness.go`, `internal/agentlink/protocol.go` (`DiscoveredAccount`, `UsageEvent`, `MeterReport`, `AccountRef`)
- `internal/meter/meter.go`, `internal/provider/manual/manual.go` (`claude` meters: `five_hour`, `seven_day`)
- Statusline docs: https://code.claude.com/docs/en/statusline (verify the `rate_limits` shape; report differences)
- Prior art (MIT, attribution required if you port): ccusage (`apps/ccusage`), Claude-Code-Usage-Monitor, ccstatusline

## Contract

```go
package claudecode

const ID = "claudecode"

// New finds config dirs from CLAUDE_CONFIG_DIR (comma-separated) or, when
// unset, <home>/.claude and <home>/.config/claude (whichever exist).
func New(home, dataDir string) *Harness

var _ harness.Harness = (*Harness)(nil)
```

### Security (non-negotiable)

- Every harness file is opened read-only. The only writes are `statusline install/uninstall` editing `settings.json` after the user ran that command, and the agent's own files in the data dir.
- Never refresh, log, store or send an OAuth token. The credentials blob (macOS Keychain item `Claude Code-credentials` via `security find-generic-password -s "Claude Code-credentials" -w`, else `<configdir>/.credentials.json`) may be read **only** to take `claudeAiOauth.subscriptionType` and `rateLimitTier`; discard everything else immediately. If reading it fails, the plan is just empty.

### Discover

For each config dir: account from `.claude.json` (in `<home>/.claude.json` for the default dir, `<configdir>/.claude.json` when `CLAUDE_CONFIG_DIR` is used) → `oauthAccount.accountUuid`, `emailAddress`, `organizationUuid`. Emit `DiscoveredAccount{Ref: {ProviderID: "claude", ExternalID: accountUuid}, Harness: "claudecode", Label: email, Plan: subscriptionType, AuthKind: "agent_capture", Details: {"rate_limit_tier": …, "organization_uuid": …}}`. A dir without `oauthAccount` (API-key use) is skipped and its usage is not reported (note it in your summary). No config dir → `(nil, nil)`.

### Usage logs (`logs.go`)

Files: `<configdir>/projects/*/*.jsonl`. Read appended lines using the cursor (`harness.Cursor`: inode, offset, size, mod time; restart at 0 when the inode changes or the size shrinks; skip unchanged files without opening; only complete lines, never a partial last line). Assistant lines with `message.usage` become `UsageEvent{Account: the dir's ref, Source: "claudecode", MessageID: message.id + ":" + requestId, OccurredAt: timestamp, Model: message.model, InputTokens: input_tokens, OutputTokens: output_tokens, CacheWriteTokens: cache_creation_input_tokens, CacheReadTokens: cache_read_input_tokens}`. Skip lines without `message.id` or `requestId`, and `<synthetic>` models. Dedupe within a batch on MessageID (Claude Code repeats lines for streamed messages; keep the last). Malformed lines are skipped and counted.

### Statusline capture (`statusline.go`)

- `aicm-agent statusline` (the wrapper): read all of stdin; if it contains `rate_limits.five_hour` / `seven_day` with `used_percentage` in 0..100 and `resets_at` (epoch seconds), append one JSON line `{"captured_at", "transcript_path", "five_hour": {...}, "seven_day": {...}}` to `<dataDir>/claude-statusline.jsonl` (create 0600; skip silently on any error). Then run the user's original command (from `<dataDir>/statusline.json`, `{"command": "...", "padding": …}`) through `sh -c` (Windows `cmd /C`) with the same stdin, copy its stdout and exit code. With no original command, print nothing. Total overhead must stay small: no network, no locks held after the append.
- `aicm-agent statusline install`: for each config dir's `settings.json`, back it up as `settings.json.aicm-backup-<unix time>`, store the existing `statusLine` object in `statusline.json` (data dir), and set `statusLine` to `{"type": "command", "command": "<absolute path of aicm-agent> statusline"}` keeping every other key and the original `padding`. Idempotent: running it twice doesn't wrap the wrapper. `uninstall` restores the stored `statusLine` (or removes the key when there was none).
- Scan reads `claude-statusline.jsonl` with its own cursor and emits `MeterReport`s for `five_hour` (window 300) and `seven_day` (window 10080) with the defs from the `claude` catalog entry, `Source: meter.SourceStatusline`, `Used` = `used_percentage`, `Limit` 100, `ResetsAt` from the epoch. The account is the config dir that contains `transcript_path`. Readings failing `Reading.Validate` are dropped (the known bug that put the epoch into `used_percentage`).

## Done when

- [ ] `claudecode_test.go` golden tests with **synthetic** fixtures (never copy real logs or emails; use `test@example.com`, made-up UUIDs):
  - `TestDiscoverGolden`: a fake home with `.claude.json` and `.credentials.json` → discovered account golden; the golden contains no token.
  - `TestScanLogsGolden`: two project files with assistant/user/system lines, a repeated streamed message, a malformed line and a partial last line → usage events golden; a second `Scan` with the returned cursors yields nothing; appending one line yields exactly that event; truncating the file restarts from 0.
  - `TestStatuslineCaptureGolden`: feed the documented stdin example (plus one with `used_percentage: 1738425600`) through `RunStatusline` with a fake original command `echo hi`: stdout is `hi`, the spool has one valid line, and `Scan` turns it into two meter reports (golden).
  - `TestInstallUninstall`: install on a settings.json with an existing statusLine and other keys → wrapper set, original kept in `statusline.json`, other keys untouched; install again → unchanged; uninstall → original restored byte-for-byte equivalent (compare decoded JSON).
- [ ] Smoke on this machine (read-only, nothing sent anywhere): `go run ./cmd/aicm-agent` with a throwaway `main` in `/tmp` that calls `Discover` and one `Scan` and prints **counts only** (no emails, no content).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.
- [ ] If you ported logic from an MIT project, add its row to `THIRD_PARTY_NOTICES.md` with the copyright line.

## Outcome (filled in by Opus)
