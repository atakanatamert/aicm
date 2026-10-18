// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atakanatamert/aicm/internal/audit"
)

// ListAuditLog returns the org's audit entries, newest first.
func (s *Server) ListAuditLog(ctx context.Context, req ListAuditLogRequestObject) (ListAuditLogResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	f := audit.Filter{
		ActorID:    stringValue(req.Params.ActorId),
		TargetKind: stringValue(req.Params.TargetKind),
		TargetID:   stringValue(req.Params.TargetId),
		Limit:      100,
	}
	if req.Params.Cursor != nil {
		f.Cursor = *req.Params.Cursor
	}
	if req.Params.Limit != nil {
		f.Limit = *req.Params.Limit
	}
	if f.Limit < 1 || f.Limit > 500 {
		return nil, BadRequest("limit must be between 1 and 500")
	}
	rows, next, err := s.Audit.List(ctx, p.OrgID, f)
	if err != nil {
		if errors.Is(err, audit.ErrBadCursor) {
			return nil, BadRequest("invalid cursor")
		}
		return nil, err
	}
	out := ListAuditLog200JSONResponse{Items: make([]AuditEntry, len(rows))}
	for i, row := range rows {
		before, err := auditObject(row.Before)
		if err != nil {
			return nil, err
		}
		after, err := auditObject(row.After)
		if err != nil {
			return nil, err
		}
		out.Items[i] = AuditEntry{
			Id:         row.ID,
			OccurredAt: row.OccurredAt,
			ActorKind:  AuditEntryActorKind(row.ActorKind),
			ActorId:    row.ActorID,
			ActorLabel: row.ActorLabel,
			Action:     row.Action,
			TargetKind: row.TargetKind,
			TargetId:   row.TargetID,
			Before:     before,
			After:      after,
		}
	}
	if next != "" {
		out.NextCursor = &next
	}
	return out, nil
}

// auditObject decodes one before/after column. A missing value stays absent.
func auditObject(raw json.RawMessage) (*map[string]interface{}, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var m map[string]interface{}
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("decoding audit json: %w", err)
	}
	return &m, nil
}

func stringValue(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
