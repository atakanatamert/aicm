# 18a – Hub: enrollment tokens, machines API, hub signing key

**Writer:** DeepSeek Flash
**Depends on:** 05, 06, 07
**License of touched packages:** AGPL-3.0-or-later

## Goal

Admins create universal enrollment tokens and get the one-line install commands; the hub has an Ed25519 signing key per org (private half stored through the secret store) and implements `agentlink.Hub` so agents can enroll and reconnect. Admins can list, rename and revoke machines. The WebSocket endpoint itself is task 18b.

## Files to touch

- `internal/machine/machine.go` (new): service (tokens, machines, hub key, `agentlink.Hub`)
- `internal/machine/machine_test.go` (new)
- `internal/db/queries/machines.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/machines.go` (new): `ListEnrollTokens`, `CreateEnrollToken`, `RevokeEnrollToken`, `ListMachines`, `UpdateMachine`
- `internal/httpapi/unimplemented.go` (edit: delete those five)
- `internal/httpapi/httpapi.go` (edit: `Deps.Machines *machine.Service`)
- `cmd/aicm/main.go` (edit: build the service)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `docs/agentlink.md` and `internal/agentlink/handshake.go` (`Hub` interface, `NewToken`, `HashToken`, token prefixes, error codes), `internal/agentlink/handshake_test.go` (`fakeHub` shows the rules)
- `migrations/00001_init.sql`: `hub_keys`, `enroll_tokens`, `machines`
- `internal/secrets/secrets.go`, `internal/audit/audit.go`, `internal/auth/principal.go`
- `api/openapi.yaml`: machines operations, `Machine`, `EnrollToken`, `EnrollTokenCreated`
- `internal/config/config.go` (`PublicURL`), `internal/httpapi/accounts.go` for handler style

## Contract

Audit rows: `audit.Write(ctx, q, p.Actor(), audit.Entry{Action, TargetKind, TargetID, Before, After})` in the same transaction; system work uses `auth.Principal{OrgID: …, Kind: auth.ActorSystem, Label: "…"}.Actor()`.

```go
package machine

type Service struct { /* pool, queries, secrets, publicURL, log */ }
func NewService(pool *pgxpool.Pool, store secrets.Store, publicURL string, log *slog.Logger) *Service

var _ agentlink.Hub = (*Service)(nil)

// HubPublicKey returns the org's Ed25519 public key, creating the key pair on
// first use (ed25519.GenerateKey; private key seed stored with store.Put as
// base64; public key in hub_keys.public_key). Concurrent first calls must not
// create two keys (INSERT … ON CONFLICT (org_id) DO NOTHING, then re-read; if
// you lost the race, delete the secret you created).
func (s *Service) HubPublicKey(ctx context.Context, orgID string) (ed25519.PublicKey, error)
// SigningKey implements agentlink.Hub (reveals the seed, ed25519.NewKeyFromSeed).
func (s *Service) SigningKey(ctx context.Context, orgID string) (ed25519.PrivateKey, error)

// Authenticate implements agentlink.Hub:
//  - EnrollToken: hash it, find an enroll token in hello.OrgID that is not
//    revoked and not expired (expires_at NULL = permanent); bump use_count and
//    last_used_at. If a machine with this fingerprint exists in the org and is
//    not revoked, rotate its token; otherwise create the machine (name =
//    hostname). Return Welcome{MachineID, MachineToken: new aicm_mt_ token}.
//    Invalid → &agentlink.Error{Code: agentlink.CodeUnauthorized}.
//  - MachineToken: find the machine by token hash; revoked →
//    CodeRevoked; fingerprint differs → CodeFingerprint; else update
//    last_seen_at, agent_version, os, arch, hostname and return Welcome{MachineID}.
//  - Welcome.Config.ScanIntervalSeconds = 60; Accounts left empty here (task 18b fills it).
func (s *Service) Authenticate(ctx context.Context, hello agentlink.Hello, auth agentlink.Auth) (agentlink.Welcome, error)

type CreatedToken struct {
	Token db.EnrollToken
	Raw   string // shown once
	HubURL, OrgID, HubPublicKey string // base64 std of the public key
	Commands Commands
}
type Commands struct{ Sh, Brew, PowerShell, Docker string }
func (s *Service) CreateEnrollToken(ctx context.Context, p auth.Principal, permanent bool, ttl time.Duration) (CreatedToken, error)
func (s *Service) ListEnrollTokens(ctx context.Context, p auth.Principal) ([]db.EnrollToken, error)
func (s *Service) RevokeEnrollToken(ctx context.Context, p auth.Principal, id string) error
func (s *Service) ListMachines(ctx context.Context, p auth.Principal) ([]db.Machine, error)
func (s *Service) RenameMachine(ctx context.Context, p auth.Principal, id, name string) (db.Machine, error)
func (s *Service) RevokeMachine(ctx context.Context, p auth.Principal, id string) (db.Machine, error)
```

Install commands (exact; `{hub}` = public URL without trailing slash, `{org}`, `{key}` = base64 public key, `{token}` = raw token; quote values for the shell):

```
sh:         curl -fsSL {hub}/install.sh | sh -s -- --hub '{hub}' --org '{org}' --key '{key}' --token '{token}'
brew:       brew install atakanatamert/tap/aicm-agent && aicm-agent enroll --hub '{hub}' --org '{org}' --key '{key}' --token '{token}' && brew services start aicm-agent
powershell: & ([scriptblock]::Create((irm '{hub}/install.ps1'))) -Hub '{hub}' -Org '{org}' -Key '{key}' -Token '{token}'
docker:     docker run -d --name aicm-agent --restart unless-stopped -v "$HOME:/host-home:ro" -e AICM_AGENT_HOME=/host-home ghcr.io/atakanatamert/aicm-agent:latest run --hub '{hub}' --org '{org}' --key '{key}' --token '{token}'
```

Take the binary name from `version.AgentBinary` and the repo path `atakanatamert/aicm` from one constant in `machine.go` so a rename stays a search-and-replace. (`install.sh`/`install.ps1` are served by task 19.)

Audit: `enroll_token.create` (after: id, expires_at; never the token), `enroll_token.revoke`, `machine.rename`, `machine.revoke`, and `machine.enroll` when `Authenticate` creates a machine (system principal labelled with the hostname).

Handlers: `Machine.online` is false in this task (18b adds live connections); `harnesses` empty. `UpdateMachine` with `revoked: true` revokes; with `name` renames; both may be present.

## Done when

- [ ] `machine_test.go` (with `dbtest.New` and a real `secrets.Builtin` on a random master key):
  - `TestEnrollAndReconnect`: run a full `agentlink.ClientHandshake` / `ServerHandshake` pair over `httptest` (copy the dial helper from `internal/agentlink/handshake_test.go`) against `Service` as the `Hub`: enroll with a fresh token → machine row and token; reconnect with the machine token → same machine id, no new token.
  - `TestRejections`: expired enroll token, revoked enroll token, revoked machine (`machine_revoked`), different fingerprint (`fingerprint_mismatch`), unknown org.
  - `TestHubKeyStable`: two calls to `HubPublicKey` return the same key; the private seed is not stored in `hub_keys`.
- [ ] Smoke: `POST /api/enroll-tokens` as admin returns the token and four commands; `GET /api/enroll-tokens` never contains the token; as viewer → 403.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
