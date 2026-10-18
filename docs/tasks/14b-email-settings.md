# 14b – Email notifier, alert emails, org settings

**Writer:** DeepSeek Flash
**Depends on:** 06, 14a
**License of touched packages:** AGPL-3.0-or-later

## Goal

Every new alert is emailed exactly once, even across hub restarts, through the org's own SMTP server via a `Notifier` interface (go-mail first; Slack or ntfy later without touching alert logic). Admins edit org settings: display currency, FX source and SMTP (password write-only), and can send a test email.

## Files to touch

- `internal/notify/notify.go` (new): `Notifier`, `Message`, SMTP implementation
- `internal/notify/email.go` (new): alert → message rendering (plain text + HTML)
- `internal/notify/job.go` (new): River worker that sends one alert email
- `internal/settings/settings.go` (new): org settings service
- `internal/db/queries/settings.sql` (new), then `~/go/bin/sqlc generate`
- `internal/httpapi/settings.go` (new): `GetSettings`, `UpdateSettings`, `SendTestEmail`
- `internal/httpapi/unimplemented.go` (edit: delete those three)
- `internal/httpapi/httpapi.go` (edit: `Deps.Settings *settings.Service`)
- `cmd/aicm/main.go` (edit: build services, set the alert hook, register the worker)
- `go.mod`, `go.sum` (add `github.com/wneessen/go-mail`)

Touch nothing else.

## Read first

- `CLAUDE.md`, `docs/PLAN.md` section "Alerts, audit log and CSV export"
- `internal/alert/alert.go` (`Hook`, `NewAlert`, `SetHook`), `migrations/00001_init.sql` (`alerts.emailed_at`, `org_settings`)
- `internal/secrets/secrets.go` (`Put`, `Replace`, `Reveal`, `Info`)
- `internal/auth/principal.go`, `internal/audit/audit.go`, `internal/version/version.go`, `internal/config/config.go` (`PublicURL`)
- `api/openapi.yaml`: `getSettings`, `updateSettings`, `sendTestEmail`, `Settings`, `SettingsUpdate`, `SmtpSettings`
- go-mail docs: https://pkg.go.dev/github.com/wneessen/go-mail

## Contract

```go
package notify

type Message struct {
	To       []string
	Subject  string
	Text     string
	HTML     string
}

// Notifier delivers a message. SMTP is the only implementation in v1.
type Notifier interface {
	Send(ctx context.Context, m Message) error
}

type SMTPConfig struct {
	Host, Username, Password, From string
	Port int
	TLS  string // none | starttls | tls
}
func NewSMTP(c SMTPConfig) Notifier // go-mail client per Send; 30 s timeout; never log the password

// SendAlertArgs emails one alert. Unique by alert id, so a restart or a
// retry never queues a second email for the same event.
type SendAlertArgs struct { OrgID, AlertID string }
func (SendAlertArgs) Kind() string { return "alert_email" }
func (SendAlertArgs) InsertOpts() river.InsertOpts // MaxAttempts 8, UniqueOpts{ByArgs: true}

type SendAlertWorker struct {
	river.WorkerDefaults[SendAlertArgs]
	Pool     *pgxpool.Pool
	Settings *settings.Service
	PublicURL string
	Log      *slog.Logger
}
// Work: load the alert; if emailed_at is set, return nil. Load SMTP config;
// if not configured (empty host or from), log once at info and return nil
// (the alert stays in the inbox). Recipients: enabled users of the org who
// can see the alert's account (account_visible; all enabled users for
// alerts without an account). Send one message to all recipients (Bcc when
// more than one), then set emailed_at = now() WHERE emailed_at IS NULL.

// Hook returns an alert.Hook that inserts SendAlertArgs with the River
// client from the context (the hook runs inside the evaluation job).
func Hook() alert.Hook
```

Email rendering (`email.go`): subject = alert title; text and HTML body = alert body, then one line or button per link in `alert.data` (`manage_subscription` "Manage subscription", `cancel` "Cancel", `top_up` "Top up", `billing` "Billing"), then "Open in aicm: {PublicURL}/alerts". HTML is a simple inline-styled single column (no external images, no tracking). Use `html/template` for the HTML and `text/template` for text. Product name from `version.Name`.

```go
package settings

type Settings struct {
	DisplayCurrency, FXSource string
	SMTP struct {
		Host, Username, From, TLS string
		Port int
		Password secrets.Info; PasswordSet bool
	}
}
type Update struct {
	DisplayCurrency, FXSource *string
	SMTP *struct {
		Host, Username, From, TLS *string
		Port *int
		Password *string // "" clears it; a value is stored through secrets.Store
	}
}
func NewService(pool *pgxpool.Pool, store secrets.Store) *Service
func (s *Service) Get(ctx context.Context, orgID string) (Settings, error)
func (s *Service) Update(ctx context.Context, p auth.Principal, u Update) (Settings, error) // audit "settings.update", password redacted
// SMTP returns the full config including the revealed password, for the mail worker and the test email only.
func (s *Service) SMTP(ctx context.Context, orgID string) (notify.SMTPConfig, bool, error)
```

Validation: `display_currency` must pass `money.ValidCurrency`; `fx_source` in {frankfurter, ecb}; `port` 1..65535; `tls` in {none, starttls, tls}; `from` must parse with `net/mail.ParseAddress`.

Handlers: `GetSettings` (viewer: SMTP host/port/from shown, username and password info only for admins; `version` from `version.Version`, `source_url` = `https://github.com/atakanatamert/aicm` + `/tree/` + version when it starts with `v`, else the repo URL), `UpdateSettings` (admin), `SendTestEmail` (admin; 502 `smtp_failed` with the SMTP error text on failure, never the password).

Import cycle note: `settings` imports `notify` for `SMTPConfig`, and `notify`'s worker takes `*settings.Service`. Break it by keeping `SMTPConfig` in `notify`, having `settings.SMTP` return `notify.SMTPConfig`, and giving the worker an interface `type SMTPSource interface { SMTP(ctx, orgID) (SMTPConfig, bool, error) }` instead of `*settings.Service`.

Wiring: `alerts.SetHook(notify.Hook())`; `jobs.Register(reg, &notify.SendAlertWorker{…})`.

## Done when

- [ ] Smoke with a local SMTP catcher (run `docker run -d --rm --name aicm-mailpit -p 1025:1025 -p 8025:8025 axllent/mailpit`, stop it afterwards): configure SMTP over `PATCH /api/settings` (host localhost, port 1025, tls none); `POST /api/settings/smtp/test` → 204 and the message shows in `http://localhost:8025/api/v1/messages`; seed a renewal 7 days out with SQL, wait for the alert job (or restart the hub, which runs it on start) → exactly one email; restart the hub twice → still exactly one email for that alert; `GET /api/settings` never returns the password.
- [ ] `go build ./... && go vet ./... && ~/go/bin/golangci-lint run` clean; `~/go/bin/sqlc generate` no diff; `./scripts/check-licenses.sh` passes.
- [ ] The verification checklist in `docs/DELEGATION.md` passes. No new tests are required.

## Outcome (filled in by Opus)

Merged in `b8a97a7` after the first attempt. Smoke with mailpit: test email, exactly one email per alert across two forced re-evaluations and restarts; password never returned.
