# 01 – Repo skeleton, binaries, config, Docker, licenses, CI

**Writer:** Opus
**Depends on:** none
**License of touched packages:** AGPL-3.0-or-later (hub), Apache-2.0 (agent)

## Goal

Phase 0 contract. See `docs/tasks/README.md` row 1.

## Files

`go.mod` (module `github.com/atakanatamert/aicm`, Go 1.26), `internal/version` (`const Name`), `internal/config`, `internal/httpapi` (router, `/healthz`, `/readyz`, `WriteJSON`/`WriteError`), `cmd/aicm`, `cmd/aicm-agent`, `Dockerfile`, `compose.yaml`, `.env.example`, `LICENSE`, `LICENSES/`, per-directory Apache `LICENSE` files, `REUSE.toml`, `scripts/check-licenses.sh`, `.golangci.yml`, `.github/workflows/ci.yml`, `.github/workflows/cla.yml`, `CLA.md` (draft), `CONTRIBUTING.md`.

## Done when

- [x] `docker compose up` serves `/healthz` and `/readyz` (verified locally with docker-compose 2 and colima).
- [x] `go build`, `go vet`, `golangci-lint run`, `go test -race ./...`, `reuse lint`, `scripts/check-licenses.sh` clean

## Outcome (filled in by Opus)

Done by Opus (contract task), squash-merged to `main`. Decisions taken:

- Go directive is 1.26 because River v0.49 and goose v3.29 require it; CLAUDE.md updated.
- Dockerfile avoids BuildKit-only cache mounts so plain `docker build` works.
- `scripts/check-licenses.sh` enforces SPDX headers per directory and fails if `cmd/aicm-agent` links any AGPL package.
- CLA workflow is gated on repo variable `CLA_ENABLED=true` (off). `CLA.md` is a draft modelled on the Apache ICLA, pending Atakan.
- Everything the agent links is Apache-2.0: `internal/harness`, `internal/meter`, `internal/version` were added to the Apache list (license compatibility; flagged to Atakan).
