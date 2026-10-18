# 29 – MCP server: `aicm-agent mcp` (stdio) and hub `/mcp` (streamable HTTP)

**Writer:** DeepSeek Flash
**Depends on:** 03, 13, 18 (agent config), and the hub endpoints the tools call (09, 10, 12, 14a)
**License of touched packages:** Apache-2.0 (`internal/mcpserver`, `cmd/aicm-agent`), AGPL-3.0-or-later (hub wiring)

## Goal

Coding agents can ask aicm which account to use, read quotas, renewals, spend and usage, and make the few allowed writes, through MCP. The same tool set is served two ways: `aicm-agent mcp` over stdio (for Claude Code, Codex and omp on a machine with the agent), and `POST /mcp` on the hub over streamable HTTP. Every tool calls the hub's HTTP API through the generated client with the caller's API token, so roles, grants and the audit log apply unchanged.

## Files to touch

- `internal/mcpserver/server.go` (new): builds the MCP server and registers tools
- `internal/mcpserver/tools.go` (new): tool handlers
- `cmd/aicm-agent/main.go` (edit: `mcp` command)
- `internal/httpapi/httpapi.go` (edit: mount `/mcp` on the base mux when `Deps.MCP` is set)
- `cmd/aicm/main.go` (edit: build the HTTP MCP handler with the hub's own base URL)
- `go.mod`, `go.sum` (add `github.com/modelcontextprotocol/go-sdk`)

Touch nothing else. `internal/mcpserver` is Apache-2.0 and may import only `internal/apiclient`, `internal/version`, the MCP SDK and the standard library.

## Read first

- `CLAUDE.md`, `docs/PLAN.md` section "MCP tool surface" (the tool table is the contract)
- `internal/apiclient/client.gen.go` (`NewClientWithResponses`, `WithRequestEditorFn`, the `…WithResponse` methods and their `JSON200` fields)
- `api/openapi.yaml` (parameters of each operation the tools call)
- MCP Go SDK: https://pkg.go.dev/github.com/modelcontextprotocol/go-sdk/mcp (`mcp.NewServer`, `mcp.AddTool` with typed input/output structs, `mcp.StdioTransport`, `mcp.NewStreamableHTTPHandler`)
- `internal/agent/config.go` (hub URL from the agent's config)

## Contract

```go
package mcpserver

// New returns an MCP server whose tools call the hub at baseURL. token
// returns the bearer token for the current call (stdio: fixed; HTTP: from
// the incoming request's Authorization header).
func New(baseURL string, token func(ctx context.Context) (string, error)) *mcp.Server

// HTTPHandler serves streamable HTTP MCP. It takes the bearer token from
// each request's Authorization header and answers 401 without one.
func HTTPHandler(baseURL string) http.Handler
```

### Tools (names, kinds and roles exactly as in PLAN.md)

| Tool | Calls | Input |
| --- | --- | --- |
| `list_accounts` | `GET /api/accounts` | `status?`, `provider_id?` |
| `get_quotas` | `GET /api/quotas` | `account_id?` |
| `recommend_account` | `GET /api/recommend` | `model_family?`, `size?` (small/medium/large), `horizon_hours?`, `limit?` |
| `forecast_capacity` | `GET /api/forecast` | `account_id?`, `hours?` (default 24) |
| `list_renewals` | `GET /api/renewals` | `within_days?` |
| `get_spend` | `GET /api/spend` | `period` (month/year), `from?`, `to?`, `group_by?`, `currency?` |
| `get_usage` | `GET /api/usage` | `group_by`, `from?`, `to?`, `account_id?`, `provider_id?` |
| `get_links` | `GET /api/accounts/{id}` → its `links` | `account_id` |
| `add_account` | `POST /api/accounts` | `provider_id`, `name`, `plan_name?`, `price_amount?` (decimal string, converted to minor units with the account currency's exponent), `currency?`, `cycle?`, `renews_on?`, `notes?` — never a secret |
| `update_account` | `PATCH /api/accounts/{id}` | `account_id`, `name?`, plan fields as above, `notes?` |
| `record_charge` | `POST /api/charges` | `account_id`, `kind`, `amount` (decimal string), `currency`, `occurred_on`, `description?` |
| `sync_account` | `POST /api/accounts/{id}/sync` | `account_id` |
| `snooze_alert` | `PATCH /api/alerts/{id}` | `alert_id`, `until` (RFC 3339) |
| `archive_account` | `POST /api/accounts/{id}/archive` | `account_id`, `confirm` (must be `true`, else the tool returns an error without calling the hub) |

- Each tool has a one-sentence description that says what it returns and that numbers are in the provider's own units; `recommend_account`'s description says to read `reasons`.
- Results: return the hub's JSON response as structured output (the generated type) plus a short text summary (e.g. "3 accounts ranked; best: Claude Max (62% of weekly window expires in 31 h)").
- Hub errors: return an MCP tool error with the hub's error code and message (401/403/404/409/429 included). Never a stack trace.
- No tool reads or returns a secret, and no tool deletes data.
- Decimal amounts → minor units: parse with `strconv`-free digit scanning (copy the approach of `internal/money.ParseDecimal`; `internal/money` is AGPL and cannot be imported here), exponent 0 for JPY/KRW/VND…, 3 for BHD/KWD/OMR…, else 2.

### `aicm-agent mcp`

Flags/env: hub URL from `--hub` or `AICM_HUB_URL`, else the agent's `config.json`; token from `AICM_TOKEN` or `--token-file` (file read once; refuse a world-readable file). Serves `mcp.StdioTransport` until stdin closes. Logs go to stderr only.

### Hub `/mcp`

`NewRouter` mounts `d.MCP` at `/mcp` (all methods) on the base mux. `cmd/aicm/main.go` builds `mcpserver.HTTPHandler("http://127.0.0.1" + listen port)` so tool calls loop back to the hub's own API with the caller's token.

## Done when

- [ ] Smoke (stdio): on a hub with data (setup, an account, a meter via SQL), create an API token, then run `AICM_TOKEN=… go run ./cmd/aicm-agent mcp --hub http://127.0.0.1:<port>` and drive it with a small JSON-RPC script in `/tmp` (initialize, tools/list, tools/call `recommend_account`, `list_renewals`, `archive_account` without confirm → error). Show the tool list and one result in your summary.
- [ ] Smoke (HTTP): the same `tools/list` and `recommend_account` against `POST /mcp` with `Authorization: Bearer …`; without the header → 401; a viewer token calling `add_account` → tool error `forbidden`.
- [ ] Claude Code check: print the exact `claude mcp add` command line for the stdio server in your summary (do not run it).
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `./scripts/check-licenses.sh` passes.

## Outcome (filled in by Opus)
