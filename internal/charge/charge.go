// SPDX-License-Identifier: SUL-1.0

// Package charge records money out: subscription fees, top-ups, invoices and
// refunds, each in its billed currency, plus the spend report that converts
// every charge at its own date's rate.
package charge

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
)

// Errors.
var (
	ErrNotFound  = errors.New("charge not found")
	ErrInvalid   = errors.New("invalid charge")
	ErrConflict  = errors.New("charge status can only change from suggested")
	ErrBadCursor = errors.New("invalid cursor")
)

var (
	kinds    = map[string]struct{}{"subscription": {}, "topup": {}, "invoice": {}, "refund": {}}
	statuses = map[string]struct{}{"confirmed": {}, "suggested": {}, "dismissed": {}}
)

// Service reads and writes charges.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
	fx   *fx.Service
	log  *slog.Logger
}

// NewService returns a charge service.
func NewService(pool *pgxpool.Pool, fxSvc *fx.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, q: db.New(pool), fx: fxSvc, log: log}
}

// CreateInput is one payment entered by a user.
type CreateInput struct {
	AccountID              string
	Kind                   string // subscription | topup | invoice | refund
	Amount                 money.Amount
	OccurredOn             time.Time // UTC midnight
	PeriodStart, PeriodEnd *time.Time
	Description            string
}

// Create records a manual, confirmed charge.
func (s *Service) Create(ctx context.Context, p auth.Principal, in CreateInput) (db.Charge, error) {
	if _, ok := kinds[in.Kind]; !ok {
		return db.Charge{}, fmt.Errorf("%w: unknown kind %q", ErrInvalid, in.Kind)
	}
	if in.Amount.Minor <= 0 || !money.ValidCurrency(in.Amount.Currency) {
		return db.Charge{}, fmt.Errorf("%w: amount must be positive in a valid currency", ErrInvalid)
	}
	if in.OccurredOn.IsZero() {
		return db.Charge{}, fmt.Errorf("%w: occurred_on is required", ErrInvalid)
	}
	if in.PeriodStart != nil && in.PeriodEnd != nil && in.PeriodEnd.Before(*in.PeriodStart) {
		return db.Charge{}, fmt.Errorf("%w: period_end is before period_start", ErrInvalid)
	}
	if _, err := s.visibleAccount(ctx, p, in.AccountID); err != nil {
		return db.Charge{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Charge{}, fmt.Errorf("starting charge create: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	occurred := dayUTC(in.OccurredOn)
	var createdBy *string
	if p.UserID != "" {
		createdBy = new(p.UserID)
	}
	created, err := q.InsertCharge(ctx, db.InsertChargeParams{
		OrgID:       p.OrgID,
		AccountID:   in.AccountID,
		Kind:        in.Kind,
		Status:      "confirmed",
		Source:      "manual",
		AmountMinor: in.Amount.Minor,
		Currency:    in.Amount.Currency,
		OccurredOn:  occurred,
		PeriodStart: optDay(in.PeriodStart),
		PeriodEnd:   optDay(in.PeriodEnd),
		Description: in.Description,
		CreatedBy:   createdBy,
	})
	if err != nil {
		return db.Charge{}, fmt.Errorf("inserting charge: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "charge.create",
		TargetKind: "charge",
		TargetID:   created.ID,
		After:      created,
	}); err != nil {
		return db.Charge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Charge{}, fmt.Errorf("committing charge create: %w", err)
	}
	return created, nil
}

// UpdateInput changes only the fields that are set.
type UpdateInput struct {
	Status      *string // confirmed | dismissed (only from suggested)
	Amount      *money.Amount
	OccurredOn  *time.Time
	Description *string
}

// Update edits a charge or confirms/dismisses a suggested one.
func (s *Service) Update(ctx context.Context, p auth.Principal, id string, in UpdateInput) (db.Charge, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return db.Charge{}, fmt.Errorf("starting charge update: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	current, err := q.GetVisibleCharge(ctx, db.GetVisibleChargeParams{
		OrgID: p.OrgID, ChargeID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Charge{}, ErrNotFound
	}
	if err != nil {
		return db.Charge{}, fmt.Errorf("loading charge: %w", err)
	}

	status := current.Status
	action := "charge.update"
	if in.Status != nil {
		target := *in.Status
		if target != "confirmed" && target != "dismissed" {
			return db.Charge{}, fmt.Errorf("%w: status must be confirmed or dismissed", ErrInvalid)
		}
		if current.Status != "suggested" {
			return db.Charge{}, ErrConflict
		}
		status = target
		if target == "confirmed" {
			action = "charge.confirm"
		} else {
			action = "charge.dismiss"
		}
	}
	amountMinor, currency := current.AmountMinor, current.Currency
	if in.Amount != nil {
		if in.Amount.Minor <= 0 || !money.ValidCurrency(in.Amount.Currency) {
			return db.Charge{}, fmt.Errorf("%w: amount must be positive in a valid currency", ErrInvalid)
		}
		amountMinor, currency = in.Amount.Minor, in.Amount.Currency
	}
	occurred := current.OccurredOn
	if in.OccurredOn != nil {
		occurred = dayUTC(*in.OccurredOn)
	}
	description := current.Description
	if in.Description != nil {
		description = *in.Description
	}

	updated, err := q.UpdateCharge(ctx, db.UpdateChargeParams{
		Status:      status,
		AmountMinor: amountMinor,
		Currency:    currency,
		OccurredOn:  occurred,
		Description: description,
		OrgID:       p.OrgID,
		ChargeID:    id,
		UserID:      userIDOf(p),
		IsAdmin:     p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Charge{}, ErrNotFound
	}
	if err != nil {
		return db.Charge{}, fmt.Errorf("updating charge: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     action,
		TargetKind: "charge",
		TargetID:   updated.ID,
		Before:     current,
		After:      updated,
	}); err != nil {
		return db.Charge{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Charge{}, fmt.Errorf("committing charge update: %w", err)
	}
	return updated, nil
}

// ListFilter narrows List. Status defaults to confirmed.
type ListFilter struct {
	From, To  *time.Time
	AccountID string
	Kind      string
	Status    string
	Cursor    string
	Limit     int
}

// List returns charges newest first (occurred_on DESC, id DESC) and the
// cursor for the next page ("" at the end).
func (s *Service) List(ctx context.Context, p auth.Principal, f ListFilter) ([]db.ListChargesRow, string, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	params, err := s.listParams(p, f)
	if err != nil {
		return nil, "", err
	}
	rowLimit := int32(limit + 1) //nolint:gosec // limit is at most 500
	params.RowLimit = &rowLimit

	rows, err := s.q.ListCharges(ctx, params)
	if err != nil {
		return nil, "", fmt.Errorf("listing charges: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next = encodeCursor(last.OccurredOn, last.ID)
	}
	return rows, next, nil
}

// ExportRows returns every matching charge without pagination, for the CSV
// export.
func (s *Service) ExportRows(ctx context.Context, p auth.Principal, f ListFilter) ([]db.ListChargesRow, error) {
	params, err := s.listParams(p, f)
	if err != nil {
		return nil, err
	}
	rows, err := s.q.ListCharges(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("listing charges for export: %w", err)
	}
	return rows, nil
}

// listParams validates a filter and builds the query arguments.
func (s *Service) listParams(p auth.Principal, f ListFilter) (db.ListChargesParams, error) {
	status := f.Status
	if status == "" {
		status = "confirmed"
	}
	if _, ok := statuses[status]; !ok {
		return db.ListChargesParams{}, fmt.Errorf("%w: unknown status %q", ErrInvalid, status)
	}
	params := db.ListChargesParams{
		OrgID:   p.OrgID,
		UserID:  userIDOf(p),
		IsAdmin: p.IsAdmin(),
		Status:  status,
	}
	if f.Kind != "" {
		if _, ok := kinds[f.Kind]; !ok {
			return db.ListChargesParams{}, fmt.Errorf("%w: unknown kind %q", ErrInvalid, f.Kind)
		}
		params.Kind = new(f.Kind)
	}
	if f.AccountID != "" {
		params.AccountID = new(f.AccountID)
	}
	if f.From != nil {
		params.FromDay = optDay(f.From)
	}
	if f.To != nil {
		params.ToDay = optDay(f.To)
	}
	if f.Cursor != "" {
		cursorDate, cursorID, err := decodeCursor(f.Cursor)
		if err != nil {
			return db.ListChargesParams{}, err
		}
		params.CursorDate, params.CursorID = &cursorDate, &cursorID
	}
	return params, nil
}

func (s *Service) visibleAccount(ctx context.Context, p auth.Principal, accountID string) (db.Account, error) {
	a, err := s.q.GetVisibleAccount(ctx, db.GetVisibleAccountParams{
		OrgID: p.OrgID, AccountID: accountID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err != nil {
		return a, fmt.Errorf("loading account: %w", err)
	}
	return a, nil
}

// userIDOf returns a uuid for account_visible; non-user principals have none
// and only see what admins see if their role is admin.
func userIDOf(p auth.Principal) string {
	if p.UserID == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return p.UserID
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func optDay(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	d := dayUTC(*t)
	return &d
}

func encodeCursor(occurred time.Time, id string) string {
	raw := occurred.UTC().Format(time.DateOnly) + "|" + id
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func decodeCursor(s string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	date, id, ok := strings.Cut(string(raw), "|")
	if !ok || !validUUID(id) {
		return time.Time{}, "", ErrBadCursor
	}
	occurred, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return time.Time{}, "", ErrBadCursor
	}
	return occurred, id, nil
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
