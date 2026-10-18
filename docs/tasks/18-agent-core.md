# 18 – Agent core: enroll, link, spool, cursors, self-update

**Writer:** DeepSeek Flash
**Depends on:** 05 (protocol), 18a/18b (hub side, for the end-to-end smoke)
**License of touched packages:** Apache-2.0

## Goal

`aicm-agent enroll` pins the hub key and obtains a machine token; `aicm-agent run` keeps one outbound WebSocket to the hub, scans the registered harnesses, spools every outgoing message on disk until the hub acks it, resends unacked messages after a reconnect, honours hub config, answers key requests, and reconnects with backoff. `aicm-agent update` replaces the binary with the latest GitHub release after checking its SHA-256. Harness importers themselves are tasks 20–22; this task runs whatever `[]harness.Harness` it is given (none yet).

## Files to touch

- `internal/agent/config.go` (new): data dir, config file, machine id, fingerprint
- `internal/agent/spool.go` (new): on-disk outbox
- `internal/agent/cursors.go` (new): cursor store
- `internal/agent/agent.go` (new): enroll, run loop, message handling
- `internal/agent/update.go` (new): self-update
- `internal/agent/agent_test.go` (new)
- `cmd/aicm-agent/main.go` (edit: `enroll`, `run`, `update` commands)

Touch nothing else. Everything here is Apache-2.0 (SPDX `Apache-2.0`) and must not import any hub package: only `internal/agentlink`, `internal/harness`, `internal/meter`, `internal/version`, the standard library, `coder/websocket`. `./scripts/check-licenses.sh` enforces this.

## Read first

- `CLAUDE.md` (security rules), `docs/agentlink.md`
- `internal/agentlink/*.go` (`ClientHandshake`, `Conn`, message types, `Fingerprint`, `MaxBatchItems`, `NewID`)
- `internal/agentlink/handshake_test.go` (fake hub you can reuse in the test)
- `internal/harness/harness.go` (`Harness`, `Poller`, `KeyReader`, `Batch`, `Cursors`)
- `internal/machine/machine.go` (`Commands`: the flags the install commands pass)
- `cmd/aicm-agent/main.go`

## Contract

### Paths and config (`config.go`)

- Data dir: `$AICM_AGENT_DATA`, else `os.UserConfigDir()/aicm-agent`. Created 0700.
- Home for harness files: `$AICM_AGENT_HOME`, else `os.UserHomeDir()` (the Docker command mounts the host home there). Harnesses get it later; expose `func HomeDir() (string, error)`.
- `config.json` (0600): `{"hub_url", "org_id", "hub_public_key" (base64), "machine_id"}`. `machine.token` (0600) holds the machine token. Write files atomically (temp file + rename).
- Machine id for the fingerprint: Linux `/etc/machine-id` (fallback `/var/lib/dbus/machine-id`); macOS `ioreg -rd1 -c IOPlatformExpertDevice` → `IOPlatformUUID`; Windows `reg query HKLM\SOFTWARE\Microsoft\Cryptography /v MachineGuid`. If none works, generate a random id once and store it as `machine-id` in the data dir. `agentlink.Fingerprint(id)`.

```go
type Config struct { HubURL, OrgID, HubPublicKey, MachineID string }
func Load(dataDir string) (Config, error)
func (c Config) Save(dataDir string) error
func DataDir() (string, error)
func HomeDir() (string, error)
func Fingerprint() (string, error)
```

### Enroll

`aicm-agent enroll --hub URL --org ID --key BASE64 --token aicm_et_…`: validates the inputs, dials `wss://` (or `ws://` only for loopback hosts) `HUB/api/agent/connect` with subprotocol `agentlink.Subprotocol`, runs `ClientHandshake` with the enroll token, saves config and the machine token, prints `enrolled as machine <id>`, exits. Re-running enroll on an enrolled machine replaces config and token. The enroll token is never written to disk.

`aicm-agent run` with the same four flags enrolls first when there is no machine token (Docker mode), then runs.

### Spool (`spool.go`)

```go
// Spool is a durable outbox: one file per message in dataDir/spool,
// named "<20-digit sequence>-<envelope id>.json", holding {type, id, payload}.
type Spool struct{ /* dir, mu, next seq */ }
func OpenSpool(dir string) (*Spool, error)
func (s *Spool) Add(typ agentlink.Type, payload any) (id string, err error) // fsync file and dir
func (s *Spool) Pending() ([]agentlink.Envelope, error)                    // in sequence order
func (s *Spool) Ack(id string) error                                      // delete; unknown id is not an error
```

Cap: when the spool holds more than 10,000 files, drop the oldest and log a warning with the count.

### Cursors (`cursors.go`)

`cursors.json` maps harness id → `harness.Cursors`, saved atomically. `Load() (map[string]harness.Cursors, error)`, `Save(id string, c harness.Cursors) error`.

### Run loop (`agent.go`)

```go
type Agent struct { /* config, dataDir, harnesses, spool, cursors, log, current AgentConfig */ }
func New(dataDir string, hs []harness.Harness, log *slog.Logger) (*Agent, error)
func (a *Agent) Enroll(ctx context.Context, hubURL, orgID, hubKey, token string) error
func (a *Agent) Run(ctx context.Context) error // returns when ctx ends
```

- Connect: dial, handshake with the machine token (hostname, `runtime.GOOS`, `runtime.GOARCH`, `version.Version`). Errors `unauthorized`, `fingerprint_mismatch`, `machine_revoked`, `bad_hub_signature` stop `Run` with that error (exit code 1; the service manager's restart then backs off). Other failures reconnect with exponential backoff 1 s → 5 min, ±20% jitter, reset after a connection lasts 60 s.
- After connect: apply `Welcome.Config`; send every `Pending()` spool message in order (same envelope ids).
- Reader goroutine: `ack` → `spool.Ack(id)` (log the error text when set); `config` → replace current config; `secret_request` → if a harness implementing `harness.KeyReader` reported the account with `has_api_key`, reply `secret_reply{value}`, else `secret_reply{error}`; unknown types are logged and ignored.
- Scan loop (every `ScanIntervalSeconds`, default 60; first run right after connect and also while disconnected, so data keeps spooling offline):
  1. Every 10 minutes and on start: `Discover` on every harness → one `accounts` message with the union (only when it changed since the last one sent).
  2. `Scan(ctx, cursors[h.ID()])` per harness; drop usage events and meter reports for accounts the current config marks `dismissed`; split into messages of at most `agentlink.MaxBatchItems` items; `spool.Add` each; **then** save the harness's new cursors. If sending fails the messages stay in the spool.
  3. Send newly spooled messages when connected.
  4. Every 5 minutes: one `status` message with `HarnessStatus` per harness.
- Pollers: for harnesses implementing `harness.Poller`, call `Poll` no more often than `PollInterval()`, doubling the interval after an error (max 1 h); spool its meter reports like scan results.
- Never log payloads, tokens or keys; log counts and ids.

### Self-update (`update.go`)

```go
// Update downloads the latest release if it is newer than version.Version,
// verifies its SHA-256 against checksums.txt and replaces the running
// executable atomically. It returns the new version, or "" when current.
func Update(ctx context.Context, client *http.Client) (string, error)
```

- Release source: `https://api.github.com/repos/atakanatamert/aicm/releases/latest` (repo path in one constant). Asset `aicm-agent_<version without v>_<goos>_<goarch>.tar.gz` (`.zip` on Windows) holding the `aicm-agent` binary, plus `checksums.txt` (`<sha256>  <filename>` lines, GoReleaser format).
- Skip when `version.Version` is `dev`.
- Replace: write next to the executable, `chmod 0755`, rename over it (Windows: rename the running exe to `.old` first).
- `aicm-agent update` runs it once and prints the result. `run` checks once a day and, after a successful update, exits 0 so the service manager restarts the new binary.

## Done when

- [ ] `agent_test.go` with the fake hub pattern from `internal/agentlink/handshake_test.go` (`httptest` server, `ServerHandshake`) and a fake `harness.Harness` in the test file:
  - `TestSpoolSurvivesRestart`: the fake hub accepts the handshake but never acks; the agent scans (fake harness returns 3 usage events), stops; a new `Agent` on the same data dir connects to a hub that acks → the hub receives exactly those messages with the **same envelope ids**, and the spool is empty afterwards.
  - `TestNoDuplicateAfterAck`: acked messages are never resent after a reconnect.
  - `TestCursorsAdvanceAfterSpool`: cursors returned by the fake harness are saved only after `spool.Add` succeeded (simulate an `Add` failure by making the spool dir read-only).
  - `TestDismissedAccountsDropped`: config marks an account dismissed → its events are not spooled.
- [ ] Smoke end to end once 18b is merged (otherwise against the fake hub and say so): create an enroll token on a local hub, run `go run ./cmd/aicm-agent enroll …` with the printed flags (use `ws://127.0.0.1:<port>`), then `go run ./cmd/aicm-agent run` with `AICM_AGENT_DATA=/tmp/aicm-agent-test`; `GET /api/machines` shows it online; stop the hub, restart it, the agent reconnects within its backoff.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes (no AGPL import from the agent).
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)

Merged in `6d35f34` after the first attempt. Opus wired the three harnesses and the statusline commands afterwards, and made the agent keep partial scan batches when a harness reports a non-fatal error. End-to-end on this Mac: enrolled, 8,143 Claude Code and 3,583 Codex usage events delivered, Codex windows from the app-server, spool empty after acks.
