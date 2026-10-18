// SPDX-License-Identifier: AGPL-3.0-or-later

// Package settings reads and writes the single org's settings: display
// currency, FX source and the SMTP server alerts are sent through.
package settings

import (
	"context"
	"errors"
	"fmt"
	"net/mail"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/notify"
	"github.com/atakanatamert/aicm/internal/secrets"
)

// Errors returned by the service.
var (
	ErrNotFound  = errors.New("settings not found")
	ErrForbidden = errors.New("settings update needs an admin")
	ErrInvalid   = errors.New("invalid settings")
)

// Settings is the editable configuration plus non-secret password metadata.
type Settings struct {
	DisplayCurrency, FXSource string
	SMTP                      struct {
		Host, Username, From, TLS string
		Port                      int
		Password                  secrets.Info
		PasswordSet               bool
	}
}

// Update is a partial settings write. A nil field leaves that value alone.
type Update struct {
	DisplayCurrency, FXSource *string
	SMTP                      *struct {
		Host, Username, From, TLS *string
		Port                      *int
		Password                  *string // "" clears it; a value is stored through secrets.Store
	}
}

// Service reads and writes org settings.
type Service struct {
	pool  *pgxpool.Pool
	q     *db.Queries
	store secrets.Store
}

// NewService builds the settings service.
func NewService(pool *pgxpool.Pool, store secrets.Store) *Service {
	return &Service{pool: pool, q: db.New(pool), store: store}
}

// Get returns the org's settings. The password is metadata only; use SMTP
// for the value right before a send.
func (s *Service) Get(ctx context.Context, orgID string) (Settings, error) {
	row, err := s.q.GetOrgSettings(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("loading settings: %w", err)
	}

	out := Settings{DisplayCurrency: row.DisplayCurrency, FXSource: row.FxSource}
	out.SMTP.Host = row.SmtpHost
	out.SMTP.Port = int(row.SmtpPort)
	out.SMTP.Username = row.SmtpUsername
	out.SMTP.TLS = row.SmtpTls
	out.SMTP.From = row.SmtpFrom
	if row.SmtpPasswordSecretID != nil {
		info, err := s.store.Info(ctx, orgID, *row.SmtpPasswordSecretID)
		switch {
		case errors.Is(err, secrets.ErrNotFound):
			// The referenced secret is gone; report the password as unset.
		case err != nil:
			return Settings{}, fmt.Errorf("loading SMTP password info: %w", err)
		default:
			out.SMTP.Password = info
			out.SMTP.PasswordSet = true
		}
	}
	return out, nil
}

// Update applies u and records one audit row. The password value never
// reaches the audit log.
func (s *Service) Update(ctx context.Context, p auth.Principal, u Update) (Settings, error) {
	if !p.IsAdmin() {
		return Settings{}, ErrForbidden
	}
	if err := validateUpdate(u); err != nil {
		return Settings{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Settings{}, fmt.Errorf("starting settings update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetOrgSettings(ctx, p.OrgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Settings{}, ErrNotFound
	}
	if err != nil {
		return Settings{}, fmt.Errorf("loading settings: %w", err)
	}
	next := before

	if u.DisplayCurrency != nil {
		next.DisplayCurrency = *u.DisplayCurrency
	}
	if u.FXSource != nil {
		next.FxSource = *u.FXSource
	}
	passwordSet := before.SmtpPasswordSecretID != nil
	var clearedSecretID string
	if u.SMTP != nil {
		if u.SMTP.Host != nil {
			next.SmtpHost = *u.SMTP.Host
		}
		if u.SMTP.Port != nil {
			next.SmtpPort = int32(*u.SMTP.Port) //nolint:gosec // validated 1..65535
		}
		if u.SMTP.Username != nil {
			next.SmtpUsername = *u.SMTP.Username
		}
		if u.SMTP.TLS != nil {
			next.SmtpTls = *u.SMTP.TLS
		}
		if u.SMTP.From != nil {
			next.SmtpFrom = *u.SMTP.From
		}
		if u.SMTP.Password != nil {
			switch {
			case *u.SMTP.Password == "":
				if before.SmtpPasswordSecretID != nil {
					clearedSecretID = *before.SmtpPasswordSecretID
				}
				next.SmtpPasswordSecretID = nil
				passwordSet = false
			case before.SmtpPasswordSecretID != nil:
				if _, err := s.store.Replace(ctx, p.OrgID, *before.SmtpPasswordSecretID, *u.SMTP.Password); err != nil {
					return Settings{}, fmt.Errorf("replacing SMTP password: %w", err)
				}
				passwordSet = true
			default:
				info, err := s.store.Put(ctx, p.OrgID, *u.SMTP.Password)
				if err != nil {
					return Settings{}, fmt.Errorf("storing SMTP password: %w", err)
				}
				next.SmtpPasswordSecretID = &info.ID
				passwordSet = true
			}
		}
	}

	row, err := q.UpdateOrgSettings(ctx, db.UpdateOrgSettingsParams{
		DisplayCurrency:      next.DisplayCurrency,
		FxSource:             next.FxSource,
		SmtpHost:             next.SmtpHost,
		SmtpPort:             next.SmtpPort,
		SmtpUsername:         next.SmtpUsername,
		SmtpPasswordSecretID: next.SmtpPasswordSecretID,
		SmtpTls:              next.SmtpTls,
		SmtpFrom:             next.SmtpFrom,
		OrgID:                p.OrgID,
	})
	if err != nil {
		return Settings{}, fmt.Errorf("updating settings: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "settings.update",
		TargetKind: "settings",
		TargetID:   p.OrgID,
		Before:     settingsSnapshot(before, before.SmtpPasswordSecretID != nil),
		After:      settingsSnapshot(row, passwordSet),
	}); err != nil {
		return Settings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Settings{}, fmt.Errorf("committing settings update: %w", err)
	}

	if clearedSecretID != "" {
		if err := s.store.Delete(ctx, p.OrgID, clearedSecretID); err != nil && !errors.Is(err, secrets.ErrNotFound) {
			return Settings{}, fmt.Errorf("deleting cleared SMTP password: %w", err)
		}
	}
	return s.Get(ctx, p.OrgID)
}

// SMTP returns the full config including the revealed password, for the
// mail worker and the test email only. The bool is false when the org has
// not configured both host and from.
func (s *Service) SMTP(ctx context.Context, orgID string) (notify.SMTPConfig, bool, error) {
	row, err := s.q.GetOrgSettings(ctx, orgID)
	if errors.Is(err, pgx.ErrNoRows) {
		return notify.SMTPConfig{}, false, ErrNotFound
	}
	if err != nil {
		return notify.SMTPConfig{}, false, fmt.Errorf("loading SMTP settings: %w", err)
	}
	if row.SmtpHost == "" || row.SmtpFrom == "" {
		return notify.SMTPConfig{}, false, nil
	}
	cfg := notify.SMTPConfig{
		Host:     row.SmtpHost,
		Port:     int(row.SmtpPort),
		Username: row.SmtpUsername,
		From:     row.SmtpFrom,
		TLS:      row.SmtpTls,
	}
	if row.SmtpPasswordSecretID != nil {
		password, err := s.store.Reveal(ctx, orgID, *row.SmtpPasswordSecretID)
		if err != nil {
			return notify.SMTPConfig{}, false, fmt.Errorf("revealing SMTP password: %w", err)
		}
		cfg.Password = password
	}
	return cfg, true, nil
}

func settingsSnapshot(o db.OrgSetting, passwordSet bool) map[string]any {
	snapshot := map[string]any{
		"display_currency": o.DisplayCurrency,
		"fx_source":        o.FxSource,
		"smtp_host":        o.SmtpHost,
		"smtp_port":        o.SmtpPort,
		"smtp_username":    o.SmtpUsername,
		"smtp_tls":         o.SmtpTls,
		"smtp_from":        o.SmtpFrom,
		"password_set":     passwordSet,
	}
	if passwordSet {
		snapshot["password"] = "[redacted]"
	}
	return snapshot
}

func validateUpdate(u Update) error {
	if u.DisplayCurrency != nil && !money.ValidCurrency(*u.DisplayCurrency) {
		return fmt.Errorf("%w: display_currency must be a three-letter ISO 4217 code", ErrInvalid)
	}
	if u.FXSource != nil {
		switch *u.FXSource {
		case "frankfurter", "ecb":
		default:
			return fmt.Errorf("%w: fx_source must be frankfurter or ecb", ErrInvalid)
		}
	}
	if u.SMTP == nil {
		return nil
	}
	if u.SMTP.Port != nil && (*u.SMTP.Port < 1 || *u.SMTP.Port > 65535) {
		return fmt.Errorf("%w: port must be between 1 and 65535", ErrInvalid)
	}
	if u.SMTP.TLS != nil {
		switch *u.SMTP.TLS {
		case "none", "starttls", "tls":
		default:
			return fmt.Errorf("%w: tls must be none, starttls or tls", ErrInvalid)
		}
	}
	if u.SMTP.From != nil && *u.SMTP.From != "" {
		if _, err := mail.ParseAddress(*u.SMTP.From); err != nil {
			return fmt.Errorf("%w: from must be a valid email address", ErrInvalid)
		}
	}
	return nil
}
