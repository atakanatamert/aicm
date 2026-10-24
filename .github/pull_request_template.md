## What and why

<!-- What this change does and why it is needed. Keep it short; the diff shows the how. -->

## Linked issue

<!-- "Closes #123", or "None" for small fixes. -->

## Checklist

- [ ] The title follows [Conventional Commits](https://www.conventionalcommits.org/), for example `fix(agent): ...` or `feat(provider): ...`.
- [ ] `go build ./... && go vet ./... && golangci-lint run` pass.
- [ ] `sqlc generate` leaves no diff, and `go test ./...` passes.
- [ ] Generated code is regenerated, not edited by hand (`go generate ./internal/httpapi` after changing `api/openapi.yaml`).
- [ ] For changes in `web/`: `pnpm build` and `pnpm lint` pass.
- [ ] Tests are added only where the repo asks for them: crypto, permissions, money math, quota scoring, log parsers and provider adapters (golden tests with a recorded fixture in `testdata/`).
- [ ] No real API keys, tokens or account data in the diff, fixtures or screenshots.
- [ ] User-visible changes have a line under `## [Unreleased]` in `CHANGELOG.md`.

## CLA

The first time you open a pull request, you'll be asked to sign the CLA. The pull request can be merged once it is signed.
