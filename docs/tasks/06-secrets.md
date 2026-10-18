# 06 – Secret store: envelope encryption, master key, rotate-key

**Writer:** DeepSeek Flash
**Depends on:** 02
**License of touched packages:** AGPL-3.0-or-later

## Goal

`internal/secrets` stores provider keys and the SMTP password encrypted. Each org has a data key (DEK) wrapped by the master key; each secret is sealed with its org's DEK. `aicm rotate-key` re-wraps every DEK with a new master key. A fresh `docker compose up` works with no manual key step.

## Files to touch

- `internal/secrets/secrets.go` (new)
- `internal/secrets/secrets_test.go` (new)
- `internal/db/queries/secrets.sql` (new), then run `sqlc generate` (updates `internal/db/`)
- `internal/config/config.go` (edit: master key loading)
- `cmd/aicm/main.go` (edit: `rotate-key` command; build the store in `serve`)
- `internal/httpapi/httpapi.go` (edit: add one field `Secrets secrets.Store` to `Deps`)
- `compose.yaml`, `.env.example`, `Dockerfile` (edit: master key file on a volume)
- `go.mod`, `go.sum` (add `golang.org/x/crypto`)

Touch nothing else.

## Read first

- `CLAUDE.md`
- `migrations/00001_init.sql`: tables `org_keys`, `secrets`
- `internal/dbtest/dbtest.go`: use it in the test
- `internal/config/config.go`, `cmd/aicm/main.go`: existing style

## Contract

### Master key (config)

Add to `config.Hub`:

```go
// MasterKey is the 32-byte key that wraps every org data key.
MasterKey []byte
```

Load order in `LoadHub`:
1. `AICM_MASTER_KEY`: base64 (std encoding) of exactly 32 bytes.
2. else `AICM_MASTER_KEY_FILE`: file containing base64 of 32 bytes (surrounding whitespace trimmed).
3. If `AICM_MASTER_KEY_FILE` is set, the file does not exist, and `AICM_MASTER_KEY_GENERATE=true`: generate 32 random bytes with `crypto/rand`, write base64 + "\n" to the file with mode 0600 (create parent dir 0700), and use it. Log nothing about the key itself.
4. Otherwise `LoadHub` returns an error: `"AICM_MASTER_KEY or AICM_MASTER_KEY_FILE is required"`.

Wrong length or bad base64 is an error. Never log the key.

Move the config read for the master key into a helper `func LoadMasterKey() ([]byte, error)` in `config.go` so `rotate-key` can reuse it.

`compose.yaml` hub service gets:
```yaml
    environment:
      AICM_MASTER_KEY_FILE: /var/lib/aicm/master.key
      AICM_MASTER_KEY_GENERATE: "true"
    volumes:
      - hub-data:/var/lib/aicm
```
and a top-level `hub-data:` volume. The image runs as uid 65532 and the directory must exist in the image with that owner, or Docker creates the volume owned by root. In the `Dockerfile`: in the build stage add `RUN mkdir -p /out/data`; in the final stage add `COPY --from=build --chown=65532:65532 /out/data /var/lib/aicm`.

`.env.example` gets a commented block:
```
# Master key for stored secrets. By default the hub generates one into the
# hub-data volume on first start. BACK IT UP: losing it means re-entering
# every provider key. To supply your own: openssl rand -base64 32
# AICM_MASTER_KEY=
```

### Package API

```go
package secrets

// Store is the only way code reads or writes secrets. A future OpenBao or
// KMS backend implements the same interface.
type Store interface {
	// Put encrypts plaintext as a new secret in the org and returns its id.
	Put(ctx context.Context, orgID, plaintext string) (Info, error)
	// Replace re-encrypts an existing secret with a new value.
	Replace(ctx context.Context, orgID, secretID, plaintext string) (Info, error)
	// Reveal decrypts a secret. Call it only right before using the value
	// (sync workers, SMTP). Never log or return the result to a client.
	Reveal(ctx context.Context, orgID, secretID string) (string, error)
	// Info returns non-secret metadata.
	Info(ctx context.Context, orgID, secretID string) (Info, error)
	// Delete removes a secret.
	Delete(ctx context.Context, orgID, secretID string) error
}

// Info is safe to show: at most the last four characters.
type Info struct {
	ID        string
	Last4     string // "" when the plaintext is shorter than 8 characters
	UpdatedAt time.Time
}

// ErrNotFound is returned (wrapped) for an unknown secret id in the org.
var ErrNotFound = errors.New("secret not found")

// ErrWrongMasterKey means a data key could not be unwrapped: the master key
// is not the one the data was encrypted with.
var ErrWrongMasterKey = errors.New("master key does not match stored data keys")

// Builtin is the default Store: XChaCha20-Poly1305 envelope encryption in Postgres.
type Builtin struct { /* pool, master key */ }

func NewBuiltin(pool *pgxpool.Pool, masterKey []byte) (*Builtin, error) // error if len != 32

var _ Store = (*Builtin)(nil)

// RotateMasterKey re-wraps every org data key (all orgs) from oldKey to
// newKey in one transaction and sets master_key_version to the previous
// maximum + 1. It returns the number of data keys re-wrapped. Secrets
// themselves are not re-encrypted (their DEKs don't change).
func RotateMasterKey(ctx context.Context, pool *pgxpool.Pool, oldKey, newKey []byte) (int, error)
```

### Crypto rules (exact)

- AEAD: `chacha20poly1305.NewX` from `golang.org/x/crypto/chacha20poly1305`. Nonces: 24 random bytes from `crypto/rand` per encryption.
- Org DEK: 32 random bytes. Created lazily on the first `Put` for an org as `version = 1`, `active = true`, `master_key_version = 1`. Two concurrent first `Put`s must not create two keys: insert with `ON CONFLICT (org_id, version) DO NOTHING`, then select the active key.
- `org_keys.wrapped_key` = nonce (24 bytes) || Seal(master, nonce, dek, ad) with `ad = "aicm/org-key/v1:" + org_id + ":" + version`.
- Secret id: generate a UUIDv4 string in Go before encrypting (16 random bytes, set version and variant bits, format 8-4-4-4-12 lowercase hex) so it can be bound as associated data.
- `secrets.nonce` = 24 bytes, `secrets.ciphertext` = Seal(dek, nonce, plaintext, ad) with `ad = "aicm/secret/v1:" + org_id + ":" + secret_id`.
- `Replace` keeps the id, writes a fresh nonce and ciphertext, updates `last4` and `updated_at`, and uses the org's current active DEK (update `org_key_id`).
- Unwrap failure → wrap `ErrWrongMasterKey`. Open failure on a secret → plain error `"decrypting secret <id>: authentication failed"`.
- `last4`: the last 4 characters (runes) of the plaintext if it has at least 8 runes, else "".
- Never log plaintext, DEKs or the master key. Zero DEK byte slices after use where practical (`clear(dek)`).
- Every query filters by `org_id`, except the rotation query that must read all orgs (comment it: `-- rotation reads every org by design`).

### SQL (`internal/db/queries/secrets.sql`)

Write queries named: `GetActiveOrgKey`, `GetOrgKey` (by org_id, id), `InsertOrgKey`, `ListOrgKeysForRotation` (`FOR UPDATE`), `UpdateOrgKeyWrap`, `InsertSecret`, `UpdateSecret`, `GetSecret`, `DeleteSecret`. Use `sqlc.arg(name)` for readable parameter names.

### `aicm rotate-key`

```
aicm rotate-key --new-key-file PATH
```
- Old key: `config.LoadMasterKey()`. New key: base64 of 32 bytes read from PATH.
- Runs `secrets.RotateMasterKey`, prints `re-wrapped N data keys; now set AICM_MASTER_KEY(_FILE) to the new key and restart`.
- Add it to the usage text.

`serve` fails fast on a bad master key (config error). It builds `secrets.NewBuiltin(pool, cfg.MasterKey)`, calls `func (b *Builtin) Check(ctx context.Context) error` (unwraps the first org key found, if any; returns nil when there are none, and a wrapped `ErrWrongMasterKey` on mismatch), and passes the store as `httpapi.Deps{..., Secrets: store}`.

## Steps

1. `go get golang.org/x/crypto@latest`.
2. Write the queries, run `sqlc generate` (binary at `~/go/bin/sqlc`).
3. Implement `internal/secrets/secrets.go` exactly per the contract.
4. Config + main.go changes.
5. Tests (below), using `dbtest.New(t)` and `dbtest.DefaultOrgID`.

## Done when

- [ ] `go test ./internal/secrets/` passes with `AICM_TEST_DATABASE_URL=postgres://aicm:aicm@localhost:55432/aicm?sslmode=disable`. Required tests, one function each:
  - `TestRoundTrip`: Put → Reveal returns the plaintext; Info shows the right last4; short secret has empty last4; Replace changes Reveal's result and keeps the id.
  - `TestCiphertextBoundToRow`: copy one secret's nonce+ciphertext onto another secret row with SQL; Reveal of the second fails.
  - `TestWrongMasterKey`: a store with another master key fails Reveal with `errors.Is(err, ErrWrongMasterKey)`.
  - `TestRotateMasterKey`: after rotation the new-key store reveals every secret, the old-key store fails with `ErrWrongMasterKey`, and `master_key_version` went up by one.
  - `TestOrgIsolation`: a secret from org A cannot be revealed or replaced through org B (create org B with SQL in the test).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` leaves no diff; `./scripts/check-licenses.sh` passes.
- [ ] Smoke: `AICM_DATABASE_URL=… AICM_MASTER_KEY_FILE=/tmp/aicm-test.key AICM_MASTER_KEY_GENERATE=true go run ./cmd/aicm serve` creates the key file with mode 0600 and starts; `go run ./cmd/aicm rotate-key --new-key-file <file>` prints the count.
- [ ] The verification checklist in `docs/DELEGATION.md` passes.

## Outcome (filled in by Opus)
