# 37 – Alert channels: webhook, ntfy, Telegram, Discord, Slack

**Writer:** DeepSeek Flash
**Depends on:** 36 (contracts: migration 00007, spec)
**License of touched packages:** AGPL-3.0-or-later

## Goal

Admins add channels besides email under Settings. Every new alert goes to each enabled channel whose filters match, once, surviving restarts and retries. "Send test" proves a channel works.

## Files to touch

- `internal/notify/channels.go` (new): channel service (CRUD with secrets, audit) and filters
- `internal/notify/senders.go` (new): one sender per kind, all through `net/http` with a 10 s timeout
- `internal/notify/deliver.go` (new): River job `alert_deliver` (args `OrgID, AlertID, ChannelID`, unique by args, `MaxAttempts: 8`) and its worker
- `internal/notify/job.go` (edit): `Hook()` also queues one `alert_deliver` job per matching enabled channel
- `internal/db/queries/notify_channels.sql` (new), then sqlc
- `internal/httpapi/channels.go` (new): `ListNotificationChannels`, `CreateNotificationChannel`, `UpdateNotificationChannel`, `DeleteNotificationChannel`, `TestNotificationChannel`
- `internal/httpapi/unimplemented.go` (edit), `internal/httpapi/httpapi.go` (edit only to add a `Channels` dependency), `cmd/aicm/main.go` (edit: wire the service and register the worker)

## Read first

- `api/openapi.yaml`: `ChannelKind` (its description defines each kind's config and secret), `NotificationChannel*`, the five operations
- `migrations/00007_release_features.sql`: `notification_channels`, `alert_deliveries`
- `internal/notify/job.go`, `internal/notify/email.go` (message text and links), `internal/alert/alert.go` (`Hook`, `NewAlert`), `internal/settings` (how SMTP secrets are stored)

## Contract

- **Filters:** a channel gets an alert when `enabled`, severity ≥ `min_severity` (info < warning < critical), and `kinds` is empty or contains the alert's kind.
- **Validation (400):** `name` non-empty; webhook/discord/slack: secret must be an `https://` URL (allow `http://` only for hosts `localhost`, `127.0.0.1` or names without a dot, for LAN tools); ntfy: `config.topic` required, `config.server` optional `https://` URL defaulting to `https://ntfy.sh`; telegram: secret (bot token) and `config.chat_id` required. Unknown config keys rejected.
- **Messages** (same title/body/links as email; link to the alert in the hub: `<public URL>/alerts`):
  - webhook: `POST` JSON `{"id","kind","severity","title","body","account_id","created_at","url","links":{...}}`, `Content-Type: application/json`, `User-Agent: aicm/<version>`.
  - ntfy: `POST <server>/<topic>` body = alert body plus the hub URL; headers `Title`, `Priority` (critical 5, warning 4, info 3), `Tags` (critical `rotating_light`, warning `warning`, info `information_source`), `Click: <hub alerts URL>`, `Authorization: Bearer <token>` when set.
  - telegram: `POST https://api.telegram.org/bot<token>/sendMessage` JSON `{"chat_id","text","disable_web_page_preview":true}`; text = `*title*\n\nbody\n\nurl` with `parse_mode: "MarkdownV2"` and proper escaping, or plain text without parse_mode (simplest correct).
  - discord: `POST <url>` JSON `{"content": "**title**\nbody\n<url>"}` (2000-char limit; truncate body).
  - slack: `POST <url>` JSON `{"text": "*title*\nbody\n<url>"}`.
  - Any non-2xx is an error carrying the status and at most 200 bytes of the body (never the URL or token).
- **Delivery:** the worker skips when a row exists in `alert_deliveries` for (alert, channel) or the channel is gone/disabled; on success inserts the row and sets `last_sent_at`, clears `last_error`; on failure sets `last_error` and returns the error so River retries. A disabled or deleted channel never sends.
- **Test:** `POST .../test` sends a fixed message ("aicm test message") synchronously; on error answer 422 with the provider's status/body excerpt in `error.message`; record `last_error`/`last_sent_at` the same way. Audit row `notification_channel.test`.
- **Responses:** `secret` is `{set, last4}` from `secrets.Info`; config returned as stored.
- **Audit actions:** `notification_channel.create`, `.update`, `.delete`, `.test`; never put the secret in before/after.


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

- [ ] All five operations work for an admin; editors and viewers get 403 (the role table already says admin).
- [ ] Smoke with a local catcher: run a tiny HTTP server (for example `python3 -m http.server` replacement script that prints POSTs) and create a webhook channel pointing at it; "test" arrives; trigger a real alert (seed an account with a renewal tomorrow and run the alert evaluation, or insert a meter reading over 90%) and see exactly one delivery even after restarting the hub; a channel with `min_severity: critical` does not receive a warning; a disabled channel receives nothing.
- [ ] ntfy against `https://ntfy.sh` with a random topic (read it back with `curl -s https://ntfy.sh/<topic>/json?poll=1`).

## Outcome (filled in by Opus)

Merged in `3946571` after the first attempt. Opus kept transport errors useful without leaking URLs (the url.Error cause only) and checked a webhook test end to end.
