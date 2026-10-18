// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import "context"

// Operations not implemented yet. Each answers 501 not_implemented. A task
// that implements one moves the method into its feature file.

func (s *Server) ListAccounts(context.Context, ListAccountsRequestObject) (ListAccountsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) CreateAccount(context.Context, CreateAccountRequestObject) (CreateAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) GetAccount(context.Context, GetAccountRequestObject) (GetAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateAccount(context.Context, UpdateAccountRequestObject) (UpdateAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ArchiveAccount(context.Context, ArchiveAccountRequestObject) (ArchiveAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ConfirmDiscoveredAccount(context.Context, ConfirmDiscoveredAccountRequestObject) (ConfirmDiscoveredAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) DismissDiscoveredAccount(context.Context, DismissDiscoveredAccountRequestObject) (DismissDiscoveredAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) GetAccountGrants(context.Context, GetAccountGrantsRequestObject) (GetAccountGrantsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) SetAccountGrants(context.Context, SetAccountGrantsRequestObject) (SetAccountGrantsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListAccountMachines(context.Context, ListAccountMachinesRequestObject) (ListAccountMachinesResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) SetAccountSecret(context.Context, SetAccountSecretRequestObject) (SetAccountSecretResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) SyncAccount(context.Context, SyncAccountRequestObject) (SyncAccountResponseObject, error) {
	return nil, ErrNotImplemented
}

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

func (s *Server) ListAuditLog(context.Context, ListAuditLogRequestObject) (ListAuditLogResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListCharges(context.Context, ListChargesRequestObject) (ListChargesResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) CreateCharge(context.Context, CreateChargeRequestObject) (CreateChargeResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateCharge(context.Context, UpdateChargeRequestObject) (UpdateChargeResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListEnrollTokens(context.Context, ListEnrollTokensRequestObject) (ListEnrollTokensResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) CreateEnrollToken(context.Context, CreateEnrollTokenRequestObject) (CreateEnrollTokenResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) RevokeEnrollToken(context.Context, RevokeEnrollTokenRequestObject) (RevokeEnrollTokenResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ExportCharges(context.Context, ExportChargesRequestObject) (ExportChargesResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ExportReadings(context.Context, ExportReadingsRequestObject) (ExportReadingsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ExportUsage(context.Context, ExportUsageRequestObject) (ExportUsageResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListGroups(context.Context, ListGroupsRequestObject) (ListGroupsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) CreateGroup(context.Context, CreateGroupRequestObject) (CreateGroupResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) DeleteGroup(context.Context, DeleteGroupRequestObject) (DeleteGroupResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateGroup(context.Context, UpdateGroupRequestObject) (UpdateGroupResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListMachines(context.Context, ListMachinesRequestObject) (ListMachinesResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateMachine(context.Context, UpdateMachineRequestObject) (UpdateMachineResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListApiTokens(context.Context, ListApiTokensRequestObject) (ListApiTokensResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) CreateApiToken(context.Context, CreateApiTokenRequestObject) (CreateApiTokenResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) RevokeApiToken(context.Context, RevokeApiTokenRequestObject) (RevokeApiTokenResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListProviders(context.Context, ListProvidersRequestObject) (ListProvidersResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListRenewals(context.Context, ListRenewalsRequestObject) (ListRenewalsResponseObject, error) {
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

func (s *Server) GetSpend(context.Context, GetSpendRequestObject) (GetSpendResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListUsers(context.Context, ListUsersRequestObject) (ListUsersResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) CreateUser(context.Context, CreateUserRequestObject) (CreateUserResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) UpdateUser(context.Context, UpdateUserRequestObject) (UpdateUserResponseObject, error) {
	return nil, ErrNotImplemented
}
