# 03 – OpenAPI spec for UI, MCP and export

**Writer:** Opus
**Depends on:** 02
**License of touched packages:** AGPL-3.0-or-later

## Goal

Phase 0 contract. See `docs/tasks/README.md` row 3.

## Files

`api/openapi.yaml`, `redocly.yaml`, CI `openapi` job.

## Done when

- [x] `redocly lint` passes. Go types + std-http strict server (oapi-codegen v2.8) and TS types (openapi-typescript 7) generated into a scratch dir and compiled; not committed.
- [x] `go build`, `go vet`, `golangci-lint run`, `go test -race ./...`, `reuse lint`, `scripts/check-licenses.sh` clean

## Outcome (filled in by Opus)

Done by Opus (contract task), squash-merged to `main`. Decisions taken:

- OpenAPI 3.0.3 for tool support. Each operation carries `x-aicm-role` (public, viewer, editor, admin) for task 07.
- First-run admin via `POST /api/setup` (no manual SQL or exec step).
- Codegen tooling is not committed: Go generation needs `github.com/oapi-codegen/runtime` (+ `google/uuid`), which is not on the allowed list. Waiting on Atakan.
