# Contributing

Read [docs/PLAN.md](docs/PLAN.md) for the design and [CLAUDE.md](CLAUDE.md) for code conventions; both apply to humans and coding agents alike.

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
./scripts/check-licenses.sh      # SPDX header on every Go file
```

Commits follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`, `chore:` …), one task per branch, squash-merged.

## License

All code is under the [Sustainable Use License 1.0](LICENSE) (SPDX `SUL-1.0`), and contributions are accepted under the [CLA](CLA.md), which lets the maintainer also offer aicm commercially. Code ported from another project needs an entry in [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) in the same change.
