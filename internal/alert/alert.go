// SPDX-License-Identifier: AGPL-3.0-or-later

// Package alert evaluates alert rules, writes the in-app inbox and exposes
// the rule editor. Evaluation lives in evaluate.go; the hourly River job in
// job.go. Email is task 14b, which installs its sender through SetHook.
package alert

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/quota"
)

// Errors returned by the service.
var (
	ErrNotFound  = errors.New("alert not found")
	ErrForbidden = errors.New("alert rule needs an admin")
	ErrInvalid   = errors.New("invalid alert rule")
	ErrBadCursor = errors.New("invalid cursor")
)

// defaultLimit is the inbox page size when the caller passes none.
const defaultLimit = 50

// maxLimit caps one inbox page.
const maxLimit = 200

// kinds lists every rule kind the schema accepts.
var kinds = map[string]struct{}{
	"renewal":         {},
	"promo_ending":    {},
	"low_balance":     {},
	"meter_exhausted": {},
	"quota_waste":     {},
	"sync_failing":    {},
	"agent_offline":   {},
}

// NewAlert is passed to the hook after an alert row is inserted for the
// first time (never for a duplicate event_key).
type NewAlert struct {
	Alert db.Alert
}

// Hook is called once per newly inserted alert. Task 14b installs the email
// sender here.
type Hook func(ctx context.Context, a NewAlert) error

// RuleParams is the stored rule configuration. Which fields apply depends on
// the rule's kind; validateRule enforces that.
type RuleParams struct {
	DaysBefore       []int    `json:"days_before,omitempty"`
	BelowPercent     *float64 `json:"below_percent,omitempty"`
	DaysLeft         *float64 `json:"days_left,omitempty"`
	UsedPercent      *float64 `json:"used_percent,omitempty"`
	UnusedPercent    *float64 `json:"unused_percent,omitempty"`
	HoursBeforeReset *float64 `json:"hours_before_reset,omitempty"`
	AfterHours       *float64 `json:"after_hours,omitempty"`
}

// RuleWrite is one upsert. AccountID nil writes the org default, which needs
// an admin.
type RuleWrite struct {
	AccountID    *string
	Kind         string
	Enabled      bool
	Params       RuleParams
	SnoozedUntil *time.Time
}

// Service reads the inbox and rules and runs rule evaluation.
type Service struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	quota     *quota.Service
	providers []provider.Provider
	log       *slog.Logger
	hook      Hook
}

// NewService builds the alert service.
func NewService(pool *pgxpool.Pool, log *slog.Logger, quotaSvc *quota.Service, providers []provider.Provider) *Service {
	return &Service{pool: pool, q: db.New(pool), quota: quotaSvc, providers: providers, log: log}
}

// SetHook installs the sender called for every newly inserted alert.
func (s *Service) SetHook(h Hook) {
	s.hook = h
}

// List returns one inbox page newest first, the total unread count and the
// next cursor ("" at the end).
func (s *Service) List(ctx context.Context, p auth.Principal, unread *bool, cursor string, limit int) ([]db.Alert, int, string, error) {
	if limit <= 0 {
		limit = defaultLimit
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	params := db.ListAlertsParams{
		OrgID:    p.OrgID,
		Unread:   unread,
		UserID:   userIDOf(p),
		IsAdmin:  p.IsAdmin(),
		RowLimit: int32(limit + 1), //nolint:gosec // limit is at most 200
	}
	if cursor != "" {
		created, id, err := decodeCursor(cursor)
		if err != nil {
			return nil, 0, "", err
		}
		params.CursorCreated, params.CursorID = &created, &id
	}
	rows, err := s.q.ListAlerts(ctx, params)
	if err != nil {
		return nil, 0, "", fmt.Errorf("listing alerts: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(rows[len(rows)-1])
	}
	count, err := s.q.CountUnreadAlerts(ctx, db.CountUnreadAlertsParams{
		OrgID: p.OrgID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, 0, "", fmt.Errorf("counting unread alerts: %w", err)
	}
	return rows, int(count), next, nil
}

// MarkAllRead marks every visible unread alert read and records one audit
// entry.
func (s *Service) MarkAllRead(ctx context.Context, p auth.Principal) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting alert read-all: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	n, err := q.MarkAllAlertsRead(ctx, db.MarkAllAlertsReadParams{
		OrgID: p.OrgID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return fmt.Errorf("marking alerts read: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "alert.read_all",
		TargetKind: "alert",
		After:      map[string]any{"unread_marked": n},
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing alert read-all: %w", err)
	}
	return nil
}

// Update marks one alert read or snoozes it. A nil field leaves that column
// unchanged.
func (s *Service) Update(ctx context.Context, p auth.Principal, id string, read *bool, snoozedUntil *time.Time) (db.Alert, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Alert{}, fmt.Errorf("starting alert update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetAlert(ctx, db.GetAlertParams{
		OrgID: p.OrgID, ID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Alert{}, ErrNotFound
	}
	if err != nil {
		return db.Alert{}, fmt.Errorf("loading alert: %w", err)
	}
	updated, err := q.UpdateAlert(ctx, db.UpdateAlertParams{
		Read:         read,
		SnoozedUntil: snoozedUntil,
		OrgID:        p.OrgID,
		ID:           id,
		UserID:       userIDOf(p),
		IsAdmin:      p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Alert{}, ErrNotFound
	}
	if err != nil {
		return db.Alert{}, fmt.Errorf("updating alert: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "alert.update",
		TargetKind: "alert",
		TargetID:   id,
		Before:     alertSnapshot(before),
		After:      alertSnapshot(updated),
	}); err != nil {
		return db.Alert{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Alert{}, fmt.Errorf("committing alert update: %w", err)
	}
	return updated, nil
}

// Rules returns the org defaults plus, when accountID is set, that account's
// overrides. Account rules follow account_visible.
func (s *Service) Rules(ctx context.Context, p auth.Principal, accountID *string) ([]db.AlertRule, error) {
	rows, err := s.q.ListAlertRulesForOrg(ctx, db.ListAlertRulesForOrgParams{
		OrgID: p.OrgID, AccountID: accountID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing alert rules: %w", err)
	}
	return rows, nil
}

// UpsertRule creates or replaces the rule for (account_id, kind). Writing the
// org default (nil account) needs an admin.
func (s *Service) UpsertRule(ctx context.Context, p auth.Principal, w RuleWrite) (db.AlertRule, error) {
	if err := validateRule(w); err != nil {
		return db.AlertRule{}, err
	}
	if w.AccountID == nil && !p.IsAdmin() {
		return db.AlertRule{}, ErrForbidden
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.AlertRule{}, fmt.Errorf("starting alert rule upsert: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if w.AccountID != nil {
		_, err := q.GetVisibleAccount(ctx, db.GetVisibleAccountParams{
			OrgID: p.OrgID, AccountID: *w.AccountID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return db.AlertRule{}, ErrNotFound
		}
		if err != nil {
			return db.AlertRule{}, fmt.Errorf("loading alert rule account: %w", err)
		}
	}

	before, err := q.GetAlertRule(ctx, db.GetAlertRuleParams{
		OrgID: p.OrgID, AccountID: w.AccountID, Kind: w.Kind,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		before = db.AlertRule{}
	} else if err != nil {
		return db.AlertRule{}, fmt.Errorf("loading alert rule: %w", err)
	}
	hadBefore := before.ID != ""

	params, err := json.Marshal(w.Params)
	if err != nil {
		return db.AlertRule{}, fmt.Errorf("encoding alert rule params: %w", err)
	}
	row, err := q.UpsertAlertRule(ctx, db.UpsertAlertRuleParams{
		OrgID:        p.OrgID,
		AccountID:    w.AccountID,
		Kind:         w.Kind,
		Enabled:      w.Enabled,
		Params:       params,
		SnoozedUntil: w.SnoozedUntil,
	})
	if err != nil {
		return db.AlertRule{}, fmt.Errorf("upserting alert rule: %w", err)
	}

	var beforeValue any
	if hadBefore {
		beforeValue = ruleSnapshot(before)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "alert_rule.upsert",
		TargetKind: "alert_rule",
		TargetID:   row.ID,
		Before:     beforeValue,
		After:      ruleSnapshot(row),
	}); err != nil {
		return db.AlertRule{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.AlertRule{}, fmt.Errorf("committing alert rule upsert: %w", err)
	}
	return row, nil
}

func ruleSnapshot(r db.AlertRule) map[string]any {
	return map[string]any{
		"kind":          r.Kind,
		"account_id":    r.AccountID,
		"enabled":       r.Enabled,
		"params":        r.Params,
		"snoozed_until": r.SnoozedUntil,
	}
}

func alertSnapshot(a db.Alert) map[string]any {
	return map[string]any{
		"kind":          a.Kind,
		"event_key":     a.EventKey,
		"severity":      a.Severity,
		"read_at":       a.ReadAt,
		"snoozed_until": a.SnoozedUntil,
	}
}

// validateRule checks the params a kind accepts and their ranges.
func validateRule(w RuleWrite) error {
	if _, ok := kinds[w.Kind]; !ok {
		return fmt.Errorf("%w: unknown kind %q", ErrInvalid, w.Kind)
	}
	p := w.Params
	invalid := func(format string, args ...any) error {
		return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
	}
	switch w.Kind {
	case "renewal", "promo_ending":
		if p.BelowPercent != nil || p.DaysLeft != nil || p.UsedPercent != nil ||
			p.UnusedPercent != nil || p.HoursBeforeReset != nil || p.AfterHours != nil {
			return invalid("kind %s only accepts days_before", w.Kind)
		}
		for _, d := range p.DaysBefore {
			if d < 0 {
				return invalid("days_before must be 0 or more")
			}
		}
	case "low_balance":
		if p.DaysBefore != nil || p.UsedPercent != nil || p.UnusedPercent != nil ||
			p.HoursBeforeReset != nil || p.AfterHours != nil {
			return invalid("kind low_balance only accepts below_percent and days_left")
		}
		if err := percent("below_percent", p.BelowPercent); err != nil {
			return err
		}
		if err := nonNegative("days_left", p.DaysLeft); err != nil {
			return err
		}
	case "meter_exhausted":
		if p.DaysBefore != nil || p.BelowPercent != nil || p.DaysLeft != nil ||
			p.UnusedPercent != nil || p.HoursBeforeReset != nil || p.AfterHours != nil {
			return invalid("kind meter_exhausted only accepts used_percent")
		}
		if err := percent("used_percent", p.UsedPercent); err != nil {
			return err
		}
	case "quota_waste":
		if p.DaysBefore != nil || p.BelowPercent != nil || p.DaysLeft != nil ||
			p.UsedPercent != nil || p.AfterHours != nil {
			return invalid("kind quota_waste only accepts unused_percent and hours_before_reset")
		}
		if err := percent("unused_percent", p.UnusedPercent); err != nil {
			return err
		}
		if err := nonNegative("hours_before_reset", p.HoursBeforeReset); err != nil {
			return err
		}
	case "sync_failing", "agent_offline":
		if p.DaysBefore != nil || p.BelowPercent != nil || p.DaysLeft != nil ||
			p.UsedPercent != nil || p.UnusedPercent != nil || p.HoursBeforeReset != nil {
			return invalid("kind %s only accepts after_hours", w.Kind)
		}
		if err := nonNegative("after_hours", p.AfterHours); err != nil {
			return err
		}
	default:
		return invalid("unknown kind %q", w.Kind)
	}
	return nil
}

func percent(name string, v *float64) error {
	if v == nil {
		return nil
	}
	if !finite(*v) || *v < 0 || *v > 100 {
		return fmt.Errorf("%w: %s must be between 0 and 100", ErrInvalid, name)
	}
	return nil
}

func nonNegative(name string, v *float64) error {
	if v == nil {
		return nil
	}
	if !finite(*v) || *v < 0 {
		return fmt.Errorf("%w: %s must be 0 or more", ErrInvalid, name)
	}
	return nil
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }

// userIDOf returns a uuid for account_visible. Principals without a user id
// see only what an admin sees.
func userIDOf(p auth.Principal) string {
	if p.UserID == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return p.UserID
}

func encodeCursor(a db.Alert) string {
	raw := a.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + a.ID
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	when, id, ok := strings.Cut(string(raw), "|")
	if !ok || !validUUID(id) {
		return time.Time{}, "", ErrBadCursor
	}
	created, err := time.Parse(time.RFC3339Nano, when)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	return created, id, nil
}

func validUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i := range len(s) {
		c := s[i]
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
			continue
		}
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}
