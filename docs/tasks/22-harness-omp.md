# 22 – Harness: omp (oh-my-pi)

**Writer:** DeepSeek Flash
**Depends on:** 05
**License of touched packages:** Apache-2.0

## Goal

`internal/harness/omp` discovers the API-key providers configured in omp, reads per-message usage (tokens and omp's own cost) from omp session logs, and can hand a plain API key to the hub when the user imports it.

## Files to touch

- `internal/harness/omp/omp.go` (new): `Harness`, Discover, KeyReader
- `internal/harness/omp/sessions.go` (new): session parser
- `internal/harness/omp/omp_test.go` (new): golden tests
- `internal/harness/omp/testdata/*` (new): synthetic fixtures and goldens

Touch nothing else. Opus wires the harness into `cmd/aicm-agent` after merging; the constructor receives the home and data directories as arguments. Apache-2.0 SPDX headers; standard library plus `internal/agentlink`, `internal/harness`, `internal/meter`, `internal/agent`. No SQLite driver: `agent.db` is out of scope.

## Read first

- `CLAUDE.md` (security rules), `docs/research/harnesses.md` (omp, pi rows)
- `internal/harness/harness.go` (`KeyReader`), `internal/agentlink/protocol.go` (`AccountRef` rule for API keys: `key:` + first 16 hex chars of SHA-256)
- The real files on this machine, **read-only and for format only**: `~/.omp/agent/auth.json` (look at key names and structure, never print values), `~/.omp/agent/sessions/` (a few lines of one file). Do not copy any real content into fixtures.

## Contract

```go
package omp

const ID = "omp"
// New uses PI_CODING_AGENT_DIR, else <home>/.omp/agent. Profiles under
// <home>/.omp/profiles/<p>/agent are scanned too.
func New(home string) *Harness
var _ harness.Harness = (*Harness)(nil)
var _ harness.KeyReader = (*Harness)(nil)
```

- **Discover:** from `auth.json`, every provider entry that holds a plain API key → `DiscoveredAccount{Ref: {ProviderID: <hub provider id>, ExternalID: "key:" + hex(sha256(key))[:16]}, Harness: "omp", Label: "<provider> key …<last 4>", AuthKind: "api_key", HasAPIKey: true}`. Map omp provider names to hub catalog ids where they match (`anthropic`, `openai`, `openrouter`, `deepseek`, `xai`, `moonshot`, `zai`; list the mapping table in the code); skip providers without a catalog id. OAuth entries (subscriptions) are skipped entirely: they are covered by the Claude Code and Codex harnesses.
- **ReadAPIKey:** returns the key for a ref this harness reported; anything else → error. The key is returned only to the agent core for a `secret_reply`; never log it.
- **Sessions:** `sessions/**/*.jsonl` with cursors (same rules as the other harnesses). Assistant messages with `usage` → `UsageEvent{Account: the ref of the provider's API key (skip when the message used an OAuth provider or a provider without a discovered key), Source: "omp", MessageID: <session id> + ":" + <message id or line timestamp>, Model, InputTokens: input, OutputTokens: output, CacheReadTokens: cacheRead, CacheWriteTokens: cacheWrite, CostMicro: cost.total in micro USD when present (money as integer: parse the JSON number's text with strconv and scale by 1e6 with rounding; no float multiplication), CostCurrency: "USD"}`. Verify the exact field names against the real session files' structure.

## Done when

- [ ] `omp_test.go` golden tests with **synthetic** fixtures: `TestDiscoverGolden` (auth.json with two API keys, one OAuth entry, one unknown provider → two accounts; golden has no key), `TestReadAPIKey` (reported ref → key; unknown ref → error), `TestSessionsGolden` (session file with user/assistant/tool lines, an OAuth-provider message that is skipped, malformed and partial lines → events golden; re-scan yields nothing).
- [ ] Smoke on this machine (read-only): a throwaway program in `/tmp` prints counts of discovered accounts and scanned events only.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.

## Outcome (filled in by Opus)
