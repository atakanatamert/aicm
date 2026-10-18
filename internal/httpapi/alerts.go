// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/atakanatamert/aicm/internal/alert"
	"github.com/atakanatamert/aicm/internal/db"
)

// alertError maps the alert service's sentinel errors to HTTP responses.
func alertError(err error) error {
	switch {
	case errors.Is(err, alert.ErrNotFound):
		return NotFound("alert or account not found")
	case errors.Is(err, alert.ErrForbidden):
		return ErrForbidden
	case errors.Is(err, alert.ErrInvalid):
		return BadRequest(err.Error())
	case errors.Is(err, alert.ErrBadCursor):
		return BadRequest("invalid cursor")
	}
	return err
}

// toAlert maps one inbox row. The links live in data.links.
func toAlert(a db.Alert) (Alert, error) {
	out := Alert{
		Id:           a.ID,
		Kind:         AlertKind(a.Kind),
		Severity:     AlertSeverity(a.Severity),
		Title:        a.Title,
		Body:         a.Body,
		AccountId:    a.AccountID,
		MachineId:    a.MachineID,
		CreatedAt:    a.CreatedAt,
		Read:         a.ReadAt != nil,
		EmailedAt:    a.EmailedAt,
		SnoozedUntil: a.SnoozedUntil,
		ResolvedAt:   a.ResolvedAt,
	}
	if len(a.Data) > 0 && string(a.Data) != "null" {
		var data struct {
			Links *Links `json:"links"`
		}
		if err := json.Unmarshal(a.Data, &data); err != nil {
			return Alert{}, fmt.Errorf("decoding alert data: %w", err)
		}
		out.Links = data.Links
	}
	return out, nil
}

// fromAlertParams maps the API shape to the stored rule params.
func fromAlertParams(p AlertParams) alert.RuleParams {
	out := alert.RuleParams{
		BelowPercent:     p.BelowPercent,
		DaysLeft:         p.DaysLeft,
		UsedPercent:      p.UsedPercent,
		UnusedPercent:    p.UnusedPercent,
		HoursBeforeReset: p.HoursBeforeReset,
		AfterHours:       p.AfterHours,
	}
	if p.DaysBefore != nil {
		out.DaysBefore = *p.DaysBefore
	}
	return out
}

// toAlertParams decodes a stored rule's params.
func toAlertParams(raw json.RawMessage) (AlertParams, error) {
	var out AlertParams
	if len(raw) == 0 || string(raw) == "null" {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("decoding alert rule params: %w", err)
	}
	return out, nil
}

func toAlertRule(r db.AlertRule) (AlertRule, error) {
	params, err := toAlertParams(r.Params)
	if err != nil {
		return AlertRule{}, err
	}
	return AlertRule{
		Id:           r.ID,
		AccountId:    r.AccountID,
		Kind:         AlertKind(r.Kind),
		Enabled:      r.Enabled,
		Params:       params,
		SnoozedUntil: r.SnoozedUntil,
	}, nil
}

// ListAlerts returns the inbox newest first.
func (s *Server) ListAlerts(ctx context.Context, req ListAlertsRequestObject) (ListAlertsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	limit := 0
	if req.Params.Limit != nil {
		limit = *req.Params.Limit
	}
	cursor := ""
	if req.Params.Cursor != nil {
		cursor = *req.Params.Cursor
	}
	rows, unread, next, err := s.Alerts.List(ctx, p, req.Params.Unread, cursor, limit)
	if err != nil {
		return nil, alertError(err)
	}
	out := AlertPage{Items: make([]Alert, 0, len(rows)), UnreadCount: unread}
	for _, row := range rows {
		a, err := toAlert(row)
		if err != nil {
			return nil, err
		}
		out.Items = append(out.Items, a)
	}
	if next != "" {
		out.NextCursor = &next
	}
	return ListAlerts200JSONResponse(out), nil
}

// MarkAllAlertsRead marks every visible unread alert read.
func (s *Server) MarkAllAlertsRead(ctx context.Context, _ MarkAllAlertsReadRequestObject) (MarkAllAlertsReadResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Alerts.MarkAllRead(ctx, p); err != nil {
		return nil, alertError(err)
	}
	return MarkAllAlertsRead204Response{}, nil
}

// UpdateAlert marks one alert read or snoozes it.
func (s *Server) UpdateAlert(ctx context.Context, req UpdateAlertRequestObject) (UpdateAlertResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	if req.Body.Read == nil && req.Body.SnoozedUntil == nil {
		return nil, BadRequest("read or snoozed_until is required")
	}
	row, err := s.Alerts.Update(ctx, p, req.AlertId, req.Body.Read, req.Body.SnoozedUntil)
	if err != nil {
		return nil, alertError(err)
	}
	a, err := toAlert(row)
	if err != nil {
		return nil, err
	}
	return UpdateAlert200JSONResponse(a), nil
}

// ListAlertRules returns org defaults plus optional per-account overrides.
func (s *Server) ListAlertRules(ctx context.Context, req ListAlertRulesRequestObject) (ListAlertRulesResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Alerts.Rules(ctx, p, req.Params.AccountId)
	if err != nil {
		return nil, alertError(err)
	}
	out := make(ListAlertRules200JSONResponse, 0, len(rows))
	for _, row := range rows {
		r, err := toAlertRule(row)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// UpsertAlertRule creates or replaces the rule for (account_id, kind).
func (s *Server) UpsertAlertRule(ctx context.Context, req UpsertAlertRuleRequestObject) (UpsertAlertRuleResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	row, err := s.Alerts.UpsertRule(ctx, p, alert.RuleWrite{
		AccountID:    req.Body.AccountId,
		Kind:         string(req.Body.Kind),
		Enabled:      req.Body.Enabled,
		Params:       fromAlertParams(req.Body.Params),
		SnoozedUntil: req.Body.SnoozedUntil,
	})
	if err != nil {
		return nil, alertError(err)
	}
	r, err := toAlertRule(row)
	if err != nil {
		return nil, err
	}
	return UpsertAlertRule200JSONResponse(r), nil
}
