// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/agenthub"
	"github.com/atakanatamert/aicm/internal/provider"
)

// ConfirmDiscoveredAccount accepts a pending account reported by an agent.
// With import_key it asks a connected machine for a plain API key.
func (s *Server) ConfirmDiscoveredAccount(ctx context.Context, req ConfirmDiscoveredAccountRequestObject) (ConfirmDiscoveredAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.AgentHub == nil {
		return nil, ErrNotImplemented
	}
	var (
		name      *string
		importKey bool
	)
	if req.Body != nil {
		name = req.Body.Name
		importKey = req.Body.ImportKey != nil && *req.Body.ImportKey
	}
	a, err := s.AgentHub.Confirm(ctx, p, req.AccountId, name, importKey)
	if err != nil {
		return nil, discoveredError(err)
	}
	resp, err := s.accountResponse(ctx, p, a)
	if err != nil {
		return nil, accountError(err)
	}
	return ConfirmDiscoveredAccount200JSONResponse(resp), nil
}

// ImportAccountKey copies an agent-reported API key into the hub after the
// account was confirmed without importing it.
func (s *Server) ImportAccountKey(ctx context.Context, req ImportAccountKeyRequestObject) (ImportAccountKeyResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.AgentHub == nil {
		return nil, ErrNotImplemented
	}
	a, err := s.AgentHub.ImportKey(ctx, p, req.AccountId)
	if err != nil {
		return nil, discoveredError(err)
	}
	resp, err := s.accountResponse(ctx, p, a)
	if err != nil {
		return nil, accountError(err)
	}
	return ImportAccountKey200JSONResponse(resp), nil
}

// DismissDiscoveredAccount rejects a pending account; agents stop sending
// its data.
func (s *Server) DismissDiscoveredAccount(ctx context.Context, req DismissDiscoveredAccountRequestObject) (DismissDiscoveredAccountResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if s.AgentHub == nil {
		return nil, ErrNotImplemented
	}
	a, err := s.AgentHub.Dismiss(ctx, p, req.AccountId)
	if err != nil {
		return nil, discoveredError(err)
	}
	resp, err := s.accountResponse(ctx, p, a)
	if err != nil {
		return nil, accountError(err)
	}
	return DismissDiscoveredAccount200JSONResponse(resp), nil
}

// discoveredError maps hub review errors to the API's status and code.
func discoveredError(err error) error {
	switch {
	case errors.Is(err, agenthub.ErrNotFound), errors.Is(err, account.ErrNotFound):
		return NotFound("account not found")
	case errors.Is(err, agenthub.ErrNotPending):
		return &APIError{Status: http.StatusConflict, Code: "not_pending", Message: "account is not pending"}
	case errors.Is(err, agenthub.ErrAgentOffline):
		return &APIError{Status: http.StatusConflict, Code: "agent_offline", Message: "no connected machine can provide this key"}
	case errors.Is(err, provider.ErrInvalidCredentials):
		return &APIError{Status: http.StatusUnprocessableEntity, Code: "invalid_credentials", Message: "the provider rejected this key"}
	case errors.Is(err, provider.ErrUnofficialNotAllowed):
		return errUnofficialNotAllowed
	case errors.Is(err, account.ErrInvalid):
		return BadRequest(err.Error())
	}
	return err
}
