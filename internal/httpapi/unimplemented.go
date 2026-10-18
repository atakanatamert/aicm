// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import "context"

// Operations not implemented yet. Each answers 501 not_implemented. A task
// that implements one moves the method into its feature file.

func (s *Server) ListAlertRules(context.Context, ListAlertRulesRequestObject) (ListAlertRulesResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpsertAlertRule(context.Context, UpsertAlertRuleRequestObject) (UpsertAlertRuleResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListAlerts(context.Context, ListAlertsRequestObject) (ListAlertsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) MarkAllAlertsRead(context.Context, MarkAllAlertsReadRequestObject) (MarkAllAlertsReadResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateAlert(context.Context, UpdateAlertRequestObject) (UpdateAlertResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ExportReadings(context.Context, ExportReadingsRequestObject) (ExportReadingsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ExportUsage(context.Context, ExportUsageRequestObject) (ExportUsageResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) GetSettings(context.Context, GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateSettings(context.Context, UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) SendTestEmail(context.Context, SendTestEmailRequestObject) (SendTestEmailResponseObject, error) {
	return nil, ErrNotImplemented
}
