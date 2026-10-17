# 05 – agentlink protocol and harness interface

**Writer:** Opus
**Depends on:** 01
**License of touched packages:** Apache-2.0

## Goal

Phase 0 contract. See `docs/tasks/README.md` row 5.

## Files

`internal/agentlink/{protocol,conn,handshake}.go` + loopback test, `internal/harness/harness.go`, `docs/agentlink.md`, `org_id` added to `EnrollTokenCreated` in the spec.

## Done when

- [x] Loopback test: enroll, push usage and meters with acks, reconnect with machine token; impostor hub never receives the token; wrong token, fingerprint mismatch and unknown org are rejected.
- [x] `go build`, `go vet`, `golangci-lint run`, `go test -race ./...`, `reuse lint`, `scripts/check-licenses.sh` clean

## Outcome (filled in by Opus)

Done by Opus (contract task), squash-merged to `main`. Decisions taken:

- Handshake is hello → signed challenge → auth → welcome. The agent sends its token only after verifying the hub's Ed25519 signature over org, fingerprint and both nonces.
- Enrollment issues a per-machine token (stored hashed); the universal token is only used once per machine.
- Hub keys are per org (`hub_keys`), so `hello` carries `org_id`, and the enroll command includes it.
