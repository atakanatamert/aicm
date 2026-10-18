# 18b – Hub: agent WebSocket endpoint, ingest, discovered-account review

**Writer:** DeepSeek Flash
**Depends on:** 09, 12, 13, 18a
**License of touched packages:** AGPL-3.0-or-later

## Goal

Agents connect to `GET /api/agent/connect`, complete the signed handshake, and push discovered accounts, usage and meter readings, which the hub stores against the right account (one account across many machines) and acknowledges. Users confirm or dismiss discovered accounts; confirming can import a plain API key from the agent. The Machines page shows which machines are online and which harnesses they run.

## Files to touch

- `internal/agenthub/agenthub.go` (new): connection registry, WebSocket handler, message loop
- `internal/agenthub/ingest.go` (new): account resolution and ingest
- `internal/agenthub/ingest_test.go` (new)
- `internal/db/queries/agenthub.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/discovered.go` (new): `ConfirmDiscoveredAccount`, `DismissDiscoveredAccount`
- `internal/httpapi/machines.go` (edit: fill `online` and `harnesses` from the hub)
- `internal/httpapi/unimplemented.go` (edit: delete the two methods)
- `internal/httpapi/httpapi.go` (edit: `Deps.AgentHub *agenthub.Hub`; mount `GET /api/agent/connect` on the base mux before the generated routes; it is not in the OpenAPI spec)
- `internal/auth/roles.go` is unchanged: the agent endpoint authenticates with the agentlink handshake, not a session, and must bypass the role middleware because it is registered outside the generated router
- `cmd/aicm/main.go` (edit: build the hub, pass it in `Deps`, close all connections on shutdown)

Touch nothing else.

## Read first

- `CLAUDE.md`, `docs/agentlink.md`
- `internal/agentlink/*.go` (protocol types, `Conn`, `ServerHandshake`, `Decode`, `MaxBatchItems`), `internal/agentlink/handshake_test.go` (a server loop)
- `internal/machine/machine.go` (`Service` implements `agentlink.Hub`; `Authenticate` returns `Welcome`)
- `internal/usage/usage.go` (`RecordAgentEvents`), `internal/quota/quota.go` (`RecordReadings`)
- `internal/account/account.go` (`SetSecret`), `internal/provider/registry/registry.go`, `internal/provider/manual/manual.go` (`claude`, `chatgpt` entries)
- `migrations/00001_init.sql`: `accounts` (`status`, `external_id`, unique `accounts_external_key`), `account_machines`
- `internal/audit/audit.go`, `internal/auth/principal.go`
- `internal/httpapi/machines.go`, `internal/httpapi/accounts.go`

## Contract

```go
package agenthub

type Hub struct { /* pool, queries, machines, usage, quota, accounts, providers, log; mu + live map[machineID]*session */ }

func New(pool *pgxpool.Pool, log *slog.Logger, machines *machine.Service, usage *usage.Service, quota *quota.Service, accounts *account.Service, providers []provider.Provider) *Hub

// ServeHTTP accepts the WebSocket (subprotocol agentlink.Subprotocol
// required, else 400 JSON error), runs agentlink.ServerHandshake with the
// machine service, fills Welcome.Config (see Config), registers the session
// and runs the message loop until the connection closes.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request)

// Online reports whether the machine has a live connection; Harnesses
// returns the harness ids from its last status message.
func (h *Hub) Online(machineID string) bool
func (h *Hub) Harnesses(machineID string) []string

// Config builds the AgentConfig for an org: every account with an
// external_id (status, allow_unofficial), ScanIntervalSeconds 60.
func (h *Hub) Config(ctx context.Context, orgID string) (agentlink.AgentConfig, error)
// PushConfig sends TypeConfig to every live machine of the org.
func (h *Hub) PushConfig(ctx context.Context, orgID string)

// Confirm moves a pending account to active (optionally renaming it). With
// importKey it asks an online machine that reported the account with
// has_api_key for the key (TypeSecretRequest, 15 s timeout), stores it with
// accounts.SetSecret(validate: true), and fails with ErrAgentOffline when
// no such machine is connected. Pushes config afterwards.
func (h *Hub) Confirm(ctx context.Context, p auth.Principal, accountID string, name *string, importKey bool) (db.Account, error)
// Dismiss sets status dismissed and pushes config.
func (h *Hub) Dismiss(ctx context.Context, p auth.Principal, accountID string) (db.Account, error)

// Shutdown closes every live connection with StatusGoingAway.
func (h *Hub) Shutdown()

var (
	ErrNotFound     = errors.New("account not found")
	ErrNotPending   = errors.New("account is not pending")
	ErrAgentOffline = errors.New("no connected machine can provide this key")
)
```

### Ingest rules (`ingest.go`)

- **Resolve an `AccountRef`** for an org and machine: look up `accounts` by `(org_id, provider_id, external_id)`. If missing, create it with `status = 'pending'`, `name` = the discovered label (or the provider's catalog name), `auth_kind` = the discovered auth kind (default `agent_capture`), `currency` = the catalog provider's currency. Unknown `provider_id` (not in the catalog) → the item is skipped and counted in the ack error text. Upsert `account_machines (account, machine, harness, details, last_seen_at)`. Use `INSERT … ON CONFLICT … DO UPDATE … RETURNING` so concurrent agents can't create duplicates. One account seen on five machines stays one account.
- `accounts` message: resolve every account, write one audit row per **newly created** account (`account.discover`, system principal labelled with the machine name). Ack.
- `usage` message: more than `agentlink.MaxBatchItems` events → ack with error. Resolve refs; drop events for dismissed accounts; call `usage.RecordAgentEvents` with the machine id. Ack after it returns.
- `meters` message: same size check; for each report resolve the ref, skip dismissed, then `quota.RecordReadings(ctx, org, account, report.Def, []meter.Reading{report.Reading}, &machineID)`. Ack.
- `status` message: keep the harness ids in memory for `Harnesses`. Ack.
- `secret_reply`: deliver to the waiting `Confirm` (by request id). Ack.
- Unknown message types: ack with error `unknown type`.
- Every incoming message updates `machines.last_seen_at`, at most once per minute per machine.
- The hub pings each connection every 30 s (`websocket.Conn.Ping` with a 10 s timeout) and drops it on failure.
- A second connection for a machine that is already connected replaces the first (close the old one).

### HTTP

- `GET /api/agent/connect` is registered on the base mux in `NewRouter` (`mux.Handle("GET "+agentlink.Path, d.AgentHub)`) when `d.AgentHub != nil`.
- `ConfirmDiscoveredAccount`: 404 unknown/hidden account, 409 when not pending, 409 `agent_offline` for `ErrAgentOffline`, 422 when the provider rejects the imported key. Returns the `Account`.
- `DismissDiscoveredAccount`: 404 / 409 as above.
- `ListMachines`: `online` and `harnesses` from the hub.
- Audit: `account.confirm`, `account.dismiss` (plus `secret.set` from `SetSecret` when a key is imported).

## Done when

- [ ] `ingest_test.go` (with `dbtest.New`, real services, and a real WebSocket via `httptest` + `agentlink.ClientHandshake` as in `internal/machine/machine_test.go`):
  - `TestSameAccountTwoMachines`: two enrolled machines report the same `claude` account ref → one `accounts` row (pending) and two `account_machines` rows.
  - `TestUsageAndMetersAcked`: a `usage` batch and a `meters` batch are acked by id; the event lands in `usage_events`, the reading in `quota_readings` with the machine id; sending the same batches again stores nothing new and is still acked.
  - `TestDismissedDropsData`: after `Dismiss`, the agent receives a `config` message with status dismissed, and new usage for that account is acked but not stored.
  - `TestOversizedBatch`: `MaxBatchItems + 1` events → ack with error, nothing stored.
- [ ] Smoke: run the hub; create an enroll token over the API; run a tiny Go program (write it in `/tmp`, not in the repo) that enrolls with `agentlink.ClientHandshake` and sends one `accounts` message; the account appears in `GET /api/accounts?status=pending`; `POST /api/accounts/{id}/confirm` → 200 active; `GET /api/machines` shows the machine online.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes; no OAuth token or plain key is logged.

## Outcome (filled in by Opus)
