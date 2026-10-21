# 40 – Agent: project per usage event, billing mode and extra usage per account

**Writer:** DeepSeek Flash
**Depends on:** 36 (contracts)
**License of touched packages:** Apache-2.0 (agent side only; never import AGPL packages)

## Goal

Every usage event the agent sends names its project (repository), so the hub can show cost by project. Discovered subscription accounts tell the hub when the harness on this machine is set up to bill an API key instead of the subscription, and whether paid extra usage is on.

## Files to touch

- `internal/harness/project.go` (new, shared): `Project(cwd string) string` — walk up from `cwd` to the first directory containing `.git` (directory or file) and return that directory's base name; when none is found (or the path no longer exists) return `filepath.Base(cwd)`; `""` for empty input, `/`, the home directory itself, or the OS temp dir. Cache results in a small mutex-guarded map (paths repeat thousands of times).
- `internal/harness/claudecode/*.go`, `internal/harness/codex/*.go`, `internal/harness/omp/*.go`: fill `UsageEvent.Project` from the session's working directory, and add the details below in `Discover`
- tests: the existing golden tests and fixtures in each harness's `testdata/` (add `cwd` to fixture lines; goldens gain `"project"`), plus a table test for `harness.Project` using `t.TempDir()` with a nested `.git`

## Read first

- `internal/agentlink/protocol.go`: `UsageEvent.Project`, `DetailBillingMode`, `DetailExtraUsage`; `docs/DECISIONS.md` #32 (base name only, never a path)
- Each harness's session parser and its golden test
- Real log shapes on this machine (read-only): Claude Code lines carry `"cwd"` on each entry (`~/.claude/projects/*/*.jsonl`); Codex rollouts carry `"cwd"` in the `session_meta` line at the top (`~/.codex/sessions/**/rollout-*.jsonl`); omp session files start with a `"type":"session"` line carrying `"cwd"`. Check a few lines before coding; do not print anything else from them.

## Contract

- **Project:** Claude Code: the entry's own `cwd`. Codex: the `session_meta` cwd of the file (remember it per file; a resumed scan that starts mid-file must still know it, so store the session's cwd in the cursor or re-read the first line). omp: the `session` header's cwd (same rule). Never send a full path.
- **billing_mode** on Claude Code accounts: `api_key` when `~/.claude/settings.json` (and the config dir's own `settings.json`) has `env.ANTHROPIC_API_KEY`, `env.ANTHROPIC_AUTH_TOKEN` or `apiKeyHelper` set, or `.claude.json` has a non-empty `customApiKeyResponses.approved`; otherwise `subscription`. Read only these keys; never read or send the values.
- **billing_mode** on Codex accounts: `api_key` when `$CODEX_HOME/auth.json` has a non-empty `OPENAI_API_KEY` and its `auth_mode` (if present) is not `chatgpt`; otherwise `subscription`. Do not decode tokens for this.
- **extra_usage** on Claude Code accounts: `on`/`off` from `.claude.json` `oauthAccount.hasExtraUsageEnabled` when present; absent otherwise. Codex: leave absent.
- omp: no billing details (its logins are subscriptions by definition).


### Rules for every Go task

- Read `CLAUDE.md` first. Go 1.26 (`new(expr)` for pointers). No new dependencies unless this card names them.
- Every query filters by `org_id`; account-scoped reads use `account_visible(...)` (`migrations/00002_account_visible.sql`). Every mutation writes exactly one `audit_log` row with `audit.Write(ctx, q, principal.Actor(), audit.Entry{...})`, in the same transaction.
- Secrets only through `secrets.Store` (`internal/secrets/secrets.go`); API responses show at most `last4`; never log a secret, webhook URL or token.
- Queries live in `internal/db/queries/<feature>.sql`; run `~/go/bin/sqlc generate`. Never edit `*.gen.go` or `internal/db/*.sql.go` by hand.
- HTTP handlers are methods on `httpapi.Server` in one file per feature. The operations this card implements currently answer 501 from `internal/httpapi/unimplemented.go`: delete each method you implement from that file (delete the file if it becomes empty).
- Errors wrapped `fmt.Errorf("doing x: %w", err)`; handler errors as `*httpapi.APIError` (see `internal/httpapi/errors.go`).
- Tests only where `CLAUDE.md` allows (crypto, permissions, money, scoring, parsers, adapters with golden files). The migration `migrations/00007_release_features.sql` and the spec `api/openapi.yaml` are the contract: do not change them; if you think they are wrong, say so in your summary.
- Before you finish: `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean, `~/go/bin/sqlc generate` leaves no diff, `go test ./...` passes with `AICM_TEST_DATABASE_URL` set, `./scripts/check-licenses.sh` passes. Then smoke-test on a running hub (fresh database on the dev Postgres at `localhost:55432`, user/password `aicm`) and describe exactly what you ran and saw.

## Done when

- [ ] `harness.Project` table test passes (repo root found from a nested dir; `.git` file for worktrees; missing path falls back to base name; home and `/` give "").
- [ ] Golden tests updated and passing; no golden contains a full path or a key value.
- [ ] Throwaway run against this machine's real logs (a temporary `_test.go` you delete afterwards, or a tiny `go run` program outside the repo): print counts of events per project for each harness and the details per discovered account; include the output in your summary.

## Outcome (filled in by Opus)

