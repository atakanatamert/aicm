// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/atakanatamert/aicm/internal/auth"
)

// LoginTotp finishes a login that returned a 202 challenge.
func (s *Server) LoginTotp(ctx context.Context, req LoginTotpRequestObject) (LoginTotpResponseObject, error) {
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	_, userAgent, ip := auth.RequestMeta(ctx)
	session, err := s.Auth.LoginTOTP(ctx, req.Body.Challenge, req.Body.Code, userAgent, ip)
	if err != nil {
		return nil, mapTotpError(err)
	}
	me, err := s.currentUser(ctx, session.Principal)
	if err != nil {
		return nil, err
	}
	return totpLoginOK{me: me, cookie: s.Auth.SessionCookie(session.Token, session.ExpiresAt)}, nil
}

// GetMyTotp reports whether the caller has two-factor on.
func (s *Server) GetMyTotp(ctx context.Context, _ GetMyTotpRequestObject) (GetMyTotpResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	status, err := s.Auth.GetTOTPStatus(ctx, p)
	if err != nil {
		return nil, mapTotpError(err)
	}
	return GetMyTotp200JSONResponse{
		Enabled:           status.Enabled,
		EnabledAt:         status.EnabledAt,
		RecoveryCodesLeft: status.RecoveryCodesLeft,
	}, nil
}

// SetupMyTotp starts two-factor setup and returns a secret to scan.
func (s *Server) SetupMyTotp(ctx context.Context, _ SetupMyTotpRequestObject) (SetupMyTotpResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	secret, otpauthURL, err := s.Auth.SetupTOTP(ctx, p)
	if err != nil {
		return nil, mapTotpError(err)
	}
	return SetupMyTotp200JSONResponse{Secret: secret, OtpauthUrl: otpauthURL}, nil
}

// EnableMyTotp confirms a code and returns the recovery codes once.
func (s *Server) EnableMyTotp(ctx context.Context, req EnableMyTotpRequestObject) (EnableMyTotpResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	codes, err := s.Auth.EnableTOTP(ctx, p, req.Body.Code)
	if err != nil {
		return nil, mapTotpError(err)
	}
	return EnableMyTotp200JSONResponse{RecoveryCodes: codes}, nil
}

// DisableMyTotp turns two-factor off after checking the password and a code.
func (s *Server) DisableMyTotp(ctx context.Context, req DisableMyTotpRequestObject) (DisableMyTotpResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	if err := s.Auth.DisableTOTP(ctx, p, req.Body.Password, req.Body.Code); err != nil {
		return nil, mapTotpError(err)
	}
	return DisableMyTotp204Response{}, nil
}

// ResetUserTotp lets an admin turn off another user's two-factor.
func (s *Server) ResetUserTotp(ctx context.Context, req ResetUserTotpRequestObject) (ResetUserTotpResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Auth.ResetUserTOTP(ctx, p, req.UserId); err != nil {
		return nil, mapTotpError(err)
	}
	return ResetUserTotp204Response{}, nil
}

// totpLoginOK sets the session cookie before writing the Me body.
type totpLoginOK struct {
	me     Me
	cookie *http.Cookie
}

func (r totpLoginOK) VisitLoginTotpResponse(w http.ResponseWriter) error {
	return writeMeWithCookie(w, r.me, r.cookie)
}

// mapTotpError turns two-factor sentinels into the API's error envelope.
func mapTotpError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidCode):
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_code", Message: err.Error()}
	case errors.Is(err, auth.ErrChallengeExpired):
		return &APIError{Status: http.StatusUnauthorized, Code: "challenge_expired", Message: err.Error()}
	case errors.Is(err, auth.ErrTotpAlreadyEnabled), errors.Is(err, auth.ErrTotpNotSetup):
		return Conflict(err.Error())
	case errors.Is(err, auth.ErrNotFound):
		return NotFound(err.Error())
	}
	return mapAuthError(err)
}
