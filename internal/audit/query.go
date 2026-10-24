// SPDX-License-Identifier: SUL-1.0

package audit

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/db"
)

// ErrBadCursor reports a cursor that is not a base64url-encoded row id.
var ErrBadCursor = errors.New("invalid cursor")

// Service lists audit log entries.
type Service struct {
	pool    *pgxpool.Pool
	queries *db.Queries
}

// NewService builds the audit service.
func NewService(pool *pgxpool.Pool) *Service {
	return &Service{pool: pool, queries: db.New(pool)}
}

// Filter narrows List. ActorID, TargetKind and TargetID are optional exact
// matches. Cursor is opaque; Limit is 1..500.
type Filter struct {
	ActorID    string
	TargetKind string
	TargetID   string
	Cursor     string
	Limit      int
}

// List returns entries newest first and the cursor for the next page ("" at
// the end). The cursor is the last row's id, base64url-encoded; rows with
// id < cursor follow.
func (s *Service) List(ctx context.Context, orgID string, f Filter) ([]db.AuditLog, string, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var cursor *int64
	if f.Cursor != "" {
		raw, err := base64.RawURLEncoding.DecodeString(f.Cursor)
		if err != nil {
			return nil, "", ErrBadCursor
		}
		id, err := strconv.ParseInt(string(raw), 10, 64)
		if err != nil || id < 0 {
			return nil, "", ErrBadCursor
		}
		cursor = &id
	}
	rows, err := s.queries.ListAuditLog(ctx, db.ListAuditLogParams{
		OrgID:      orgID,
		ActorID:    optional(f.ActorID),
		TargetKind: optional(f.TargetKind),
		TargetID:   optional(f.TargetID),
		Cursor:     cursor,
		RowLimit:   int32(limit + 1), //nolint:gosec // limit is at most 500
	})
	if err != nil {
		return nil, "", fmt.Errorf("listing audit log: %w", err)
	}
	next := ""
	if len(rows) > limit {
		rows = rows[:limit]
		next = encodeCursor(rows[len(rows)-1].ID)
	}
	return rows, next, nil
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func encodeCursor(id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(id, 10)))
}
