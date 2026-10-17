# Local harness storage: research notes

Researched 2026-10-17. Confidence: H = docs or source code, M = secondary sources.

Rules for every importer:
- Treat every harness file as read-only.
- Never refresh an OAuth token, because that can log the official client out.
- Never send subscription tokens to the hub.
- Plain API keys found in a harness (for example a DeepSeek key in omp) are copied to the hub's encrypted store only after the user confirms them in the import preview.

| Harness | Credentials | Plan or account | Usage logs | Path overrides | Conf. |
| --- | --- | --- | --- | --- | --- |
| Claude Code | macOS Keychain `Claude Code-credentials` (hashed suffix when `CLAUDE_CONFIG_DIR` is set); otherwise `~/.claude/.credentials.json` → `claudeAiOauth{accessToken, refreshToken, expiresAt, subscriptionType, rateLimitTier}` | `subscriptionType` and `rateLimitTier`; account in `~/.claude.json` → `oauthAccount{emailAddress, accountUuid, organizationUuid}` | `~/.claude/projects/*/*.jsonl` and `~/.config/claude/projects/`. Assistant lines have `message.model`, `message.id`, `requestId` and `message.usage{input_tokens, output_tokens, cache_creation_input_tokens, cache_read_input_tokens}`. Dedupe on message.id + requestId. | `CLAUDE_CONFIG_DIR` (may be comma-separated) | H |
| Codex CLI | `$CODEX_HOME/auth.json` or the OS keyring in newer builds (`cli_auth_credentials_store`) | `id_token` JWT claim `https://api.openai.com/auth` → `chatgpt_plan_type`, `chatgpt_account_id` | `sessions/YYYY/MM/DD/rollout-*.jsonl`, `token_count` events (see `quota-capture.md`) | `CODEX_HOME` | H |
| omp (oh-my-pi) | `~/.omp/agent/agent.db` (SQLite), `auth.json`, `auth-broker.json`; multiple providers | Per provider in `agent.db` (schema undocumented) | `~/.omp/agent/sessions/**/*.jsonl`, pi-style per-message usage | `PI_CODING_AGENT_DIR`, `PI_CONFIG_DIR`; profiles under `~/.omp/profiles/<p>/agent` | M |
| pi | `~/.pi/agent/auth.json`, keyed by provider (api key or `{access, refresh, expires}`) | Provider entries | `~/.pi/agent/sessions/*/*.jsonl` with `usage{input, output, cacheRead, cacheWrite, cost}` | `PI_CODING_AGENT_DIR` | H/M |
| Gemini CLI | Keychain `gemini-cli-oauth` or `~/.gemini/oauth_creds.json` | Email in `~/.gemini/google_accounts.json` | `~/.gemini/tmp/<hash>/chats/*.json` with `tokens{input, output, cached, thoughts, tool}` | `GEMINI_CLI_HOME` | H |
| opencode | `~/.local/share/opencode/auth.json` | Provider entries | `~/.local/share/opencode/opencode.db` (SQLite); legacy `storage/message/**.json` | XDG | H/M |
| Kimi Code CLI | `~/.kimi-code/credentials/*.json` | In credentials | `~/.kimi-code/sessions/**/wire.jsonl` | `KIMI_CODE_HOME` | H |
| Cline, Roo, Kilo | VS Code SecretStorage (encrypted, not readable) | – | `globalStorage/<ext>/tasks/*/ui_messages.json` with tokensIn, tokensOut and cost | – | M |
| Crush | `crush.json` with env references | – | `.crush/crush.db` `sessions` table (prompt_tokens, completion_tokens, cost) | `CRUSH_GLOBAL_DATA` | H |
| Goose | OS keyring or `~/.config/goose/secrets.yaml` | – | `~/.local/share/goose/sessions/sessions.db` | – | M |
| Amp | `~/.local/share/amp/secrets.json` | – | `~/.local/share/amp/threads/*.json` | – | M |
| Copilot CLI | `gh` token chain or keychain | – | `~/.copilot/session-state/`, `otel/*.jsonl` | `COPILOT_HOME` | M |
| Cursor | `state.vscdb` `cursorAuth/accessToken`, `stripeMembershipType` | Plan in `stripeMembershipType` | No token counts on disk | – | M |
| Aider | `.env`, `.aider.conf.yml` | – | Only `Tokens: … Cost: …` lines in `.aider.chat.history.md` | – | M |

MVP importers are Claude Code, Codex and omp. The rest come later.

## Prior art to port from (all MIT; keep attribution)

- [tokscale](https://github.com/junhoyeo/tokscale): parsers for about 55 clients. This is the best reference for exact paths.
- [ccusage](https://github.com/ryoppippi/ccusage): Claude Code, Codex, opencode and pi parsers. It is a monorepo, so check each package's LICENSE.
- CodexBar (steipete): quota-probe strategies for Claude and Codex.
- Claude-Code-Usage-Monitor: statusline capture pattern (`--statusline` writer).
- ccstatusline: statusline `rate_limits` extraction with an OAuth fallback cache.
- codeburn: per-provider parser docs.
