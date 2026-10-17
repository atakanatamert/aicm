# aicm – instructions for every coding agent

`aicm` is a placeholder name. Read `docs/PLAN.md` for the design and `docs/DECISIONS.md` for settled decisions. If you were handed a task card in `docs/tasks/`, that card is your whole job. Touch only the files it lists.

## Layout

```text
cmd/aicm/            hub binary (AGPL): serve, migrate, rotate-key, create-admin, providers --markdown
cmd/aicm-agent/      agent binary (Apache-2.0): run, enroll, mcp, update
internal/<feature>/  one package per feature: types, service, sqlc queries together
internal/provider/<id>/   one package per provider adapter
internal/harness/<id>/    one package per local harness importer (used by the agent)
internal/agentlink/  hub<->agent protocol (Apache-2.0)
internal/mcpserver/  MCP tools (Apache-2.0)
migrations/          goose SQL migrations
web/                 React + Vite app, built to web/dist and embedded with go:embed
docs/                plan, decisions, research, task cards
```

## Go conventions

- Go 1.26+ (River and goose require it). Use stdlib `net/http` routing (method and path patterns). Allowed libraries: `pgx/v5`, `sqlc`, `goose`, `riverqueue/river`, `wneessen/go-mail`, `coder/websocket`, the official `modelcontextprotocol/go-sdk`, `golang.org/x/crypto`. Ask before adding any other dependency.
- No ORM, no DI container, no global state other than the logger. Wire dependencies by hand in `cmd/*/main.go`.
- Every query filters by `org_id`. Every table that holds tenant data has an `org_id`.
- Errors: wrap them with `fmt.Errorf("doing x: %w", err)`. Handlers return JSON errors `{ "error": { "code", "message" } }`.
- Logging: `log/slog` with structured fields. Never log secrets, tokens or full API keys.
- Money: integer minor units plus an ISO 4217 currency code. Never store money as a float. Convert only for display, at the rate of the record's own date.
- Time: store UTC `timestamptz`. Meter `resets_at` values are absolute times, not durations.
<!-- REUSE-IgnoreStart -->
- Every Go source file starts with a one-line SPDX header: `// SPDX-License-Identifier: AGPL-3.0-or-later` (hub) or `// SPDX-License-Identifier: Apache-2.0` (everything the agent binary links: `cmd/aicm-agent`, `internal/agentlink`, `internal/mcpserver`, `internal/harness`, `internal/meter`, `internal/version`). Apache packages must never import AGPL packages. `REUSE.toml` supplies copyright and covers non-Go files.
<!-- REUSE-IgnoreEnd -->
- SQL: schema lives in `migrations/` (goose). Queries live in `internal/db/queries/<feature>.sql`; `sqlc generate` writes Go into `internal/db`. Feature packages call `internal/db`.

## Security rules

- Subscription OAuth tokens (Claude, ChatGPT, Gemini) never leave the agent's machine and are never sent to the hub.
- The agent reads harness files read-only and never refreshes an OAuth token.
- Secrets go through `internal/secrets` only. API responses expose at most the last 4 characters.
- Every mutation writes one `audit_log` row.

## Tests

Write tests only for: crypto, permissions (grants), money math, quota scoring, log parsers and provider adapters. Adapters and parsers use golden tests: a recorded fixture in `testdata/` goes in, and the expected records come out. Don't write tests for handlers or UI components.

## Frontend conventions

- React + TypeScript + Vite, shadcn/ui components, shadcn charts (Recharts v3), Motion for small transitions, Hairline only for empty states and the login screen.
- Data comes through TanStack Query and the client generated from `api/openapi.yaml`. Don't hand-write fetch calls.
- Quiet, neutral UI with one accent for urgency. Show provider meters in the provider's own words and units.

## Before you say a task is done

- `go build ./... && go vet ./... && golangci-lint run`
- `sqlc generate` leaves no diff, and `go test ./...` passes
- For `web/`: `pnpm build` and `pnpm lint` pass
- Your diff touches only the files listed in your task card
