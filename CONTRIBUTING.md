# Contributing

aicm is pre-alpha. Read [docs/PLAN.md](docs/PLAN.md) for the design and [CLAUDE.md](CLAUDE.md) for code conventions; both apply to humans and coding agents alike.

## Contributor License Agreement

Outside contributions will require signing the [CLA](CLA.md) once it is confirmed. The CLA check runs on every pull request when enabled.

## Development

Requirements: Go 1.26+, Docker, [sqlc](https://sqlc.dev), [golangci-lint](https://golangci-lint.run) v2.

```sh
cp .env.example .env
docker compose up --build        # hub on http://localhost:8080
go build ./... && go vet ./... && golangci-lint run
go test ./...                    # DB tests need AICM_TEST_DATABASE_URL
sqlc generate                    # after editing migrations/ or internal/db/queries/
./scripts/check-licenses.sh      # SPDX headers and the Apache/AGPL boundary
```

Commits follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`, `chore:` …), one task per branch, squash-merged.

## Licenses

The hub is AGPL-3.0-or-later. The agent and everything it links (`cmd/aicm-agent`, `internal/agentlink`, `internal/mcpserver`, `internal/harness`, `internal/meter`, `internal/version`) is Apache-2.0 and must not import hub packages; `scripts/check-licenses.sh` enforces this. Code ported from another project needs an entry in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) in the same change.
