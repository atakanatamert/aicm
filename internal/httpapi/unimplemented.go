// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import "context"

// Operations not implemented yet. Each answers 501 not_implemented. A task
// that implements one moves the method into its feature file.

func (s *Server) LoginTotp(context.Context, LoginTotpRequestObject) (LoginTotpResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) GetOidcConfig(context.Context, GetOidcConfigRequestObject) (GetOidcConfigResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ListBudgets(context.Context, ListBudgetsRequestObject) (ListBudgetsResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) PutBudget(context.Context, PutBudgetRequestObject) (PutBudgetResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) DeleteBudget(context.Context, DeleteBudgetRequestObject) (DeleteBudgetResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) DisableMyTotp(context.Context, DisableMyTotpRequestObject) (DisableMyTotpResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) GetMyTotp(context.Context, GetMyTotpRequestObject) (GetMyTotpResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) EnableMyTotp(context.Context, EnableMyTotpRequestObject) (EnableMyTotpResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) SetupMyTotp(context.Context, SetupMyTotpRequestObject) (SetupMyTotpResponseObject, error) {
	return nil, ErrNotImplemented
}

func (s *Server) ResetUserTotp(context.Context, ResetUserTotpRequestObject) (ResetUserTotpResponseObject, error) {
	return nil, ErrNotImplemented
}
