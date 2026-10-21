// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/atakanatamert/aicm/internal/auth"
)

// GetSetupStatus reports whether first-run setup is still needed.
func (s *Server) GetSetupStatus(ctx context.Context, _ GetSetupStatusRequestObject) (GetSetupStatusResponseObject, error) {
	needed, err := s.Auth.SetupNeeded(ctx)
	if err != nil {
		return nil, err
	}
	return GetSetupStatus200JSONResponse{Needed: needed}, nil
}

// CompleteSetup creates the first admin, logs them in and sets the cookie.
func (s *Server) CompleteSetup(ctx context.Context, req CompleteSetupRequestObject) (CompleteSetupResponseObject, error) {
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	email := strings.TrimSpace(req.Body.Email)
	if email == "" {
		return nil, BadRequest("email is required")
	}
	if err := auth.ValidatePassword(req.Body.Password); err != nil {
		return nil, BadRequest(err.Error())
	}
	name := ""
	if req.Body.Name != nil {
		name = strings.TrimSpace(*req.Body.Name)
	}
	_, userAgent, ip := auth.RequestMeta(ctx)
	session, err := s.Auth.CompleteSetup(ctx, email, name, req.Body.Password, userAgent, ip)
	if err != nil {
		return nil, mapAuthError(err)
	}
	me, err := s.currentUser(ctx, session.Principal)
	if err != nil {
		return nil, err
	}
	return setupOK{me: me, cookie: s.Auth.SessionCookie(session.Token, session.ExpiresAt)}, nil
}

// Login verifies the password and sets the session cookie.
func (s *Server) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	_, userAgent, ip := auth.RequestMeta(ctx)
	result, err := s.Auth.Login(ctx, req.Body.Email, req.Body.Password, userAgent, ip)
	if err != nil {
		return nil, mapAuthError(err)
	}
	if result.TOTPRequired() {
		return Login202JSONResponse{
			TotpRequired: true,
			Challenge:    result.Challenge,
		}, nil
	}
	me, err := s.currentUser(ctx, result.Session.Principal)
	if err != nil {
		return nil, err
	}
	return loginOK{me: me, cookie: s.Auth.SessionCookie(result.Session.Token, result.Session.ExpiresAt)}, nil
}

// Logout deletes the session and clears the cookie.
func (s *Server) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	sessionToken, _, _ := auth.RequestMeta(ctx)
	if err := s.Auth.Logout(ctx, sessionToken); err != nil {
		return nil, err
	}
	return logoutOK{cookie: s.Auth.ClearedSessionCookie()}, nil
}

// GetMe returns the current user and org.
func (s *Server) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	me, err := s.currentUser(ctx, p)
	if err != nil {
		return nil, mapAuthError(err)
	}
	return GetMe200JSONResponse(me), nil
}

// ChangePassword sets a new password and ends the user's other sessions.
func (s *Server) ChangePassword(ctx context.Context, req ChangePasswordRequestObject) (ChangePasswordResponseObject, error) {
	if req.Body == nil {
		return nil, BadRequest("request body is required")
	}
	if err := auth.ValidatePassword(req.Body.NewPassword); err != nil {
		return nil, BadRequest(err.Error())
	}
	p, ok := auth.FromContext(ctx)
	if !ok {
		return nil, auth.ErrUnauthenticated
	}
	keepSessionToken, _, _ := auth.RequestMeta(ctx)
	if err := s.Auth.ChangePassword(ctx, p, req.Body.CurrentPassword, req.Body.NewPassword, keepSessionToken); err != nil {
		return nil, mapAuthError(err)
	}
	return ChangePassword204Response{}, nil
}

// currentUser loads the principal's user and org into the API shape.
func (s *Server) currentUser(ctx context.Context, p auth.Principal) (Me, error) {
	user, org, settings, err := s.Auth.Me(ctx, p)
	if err != nil {
		return Me{}, mapAuthError(err)
	}
	var me Me
	me.User = toUser(user)
	me.Org.Id = org.ID
	me.Org.Name = org.Name
	me.Org.DisplayCurrency = settings.DisplayCurrency
	return me, nil
}

// mapAuthError turns auth sentinels into the API's error envelope.
func mapAuthError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidLogin):
		return &APIError{Status: http.StatusUnauthorized, Code: "invalid_login", Message: err.Error()}
	case errors.Is(err, auth.ErrTooManyAttempts):
		return &APIError{Status: http.StatusTooManyRequests, Code: "too_many_attempts", Message: err.Error()}
	case errors.Is(err, auth.ErrSetupDone):
		return &APIError{Status: http.StatusConflict, Code: "setup_done", Message: err.Error()}
	case errors.Is(err, auth.ErrUnauthenticated):
		return ErrUnauthorized
	case errors.Is(err, auth.ErrForbidden):
		return ErrForbidden
	}
	return err
}

// loginOK sets the session cookie before writing the Me body.
type loginOK struct {
	me     Me
	cookie *http.Cookie
}

func (r loginOK) VisitLoginResponse(w http.ResponseWriter) error {
	return writeMeWithCookie(w, r.me, r.cookie)
}

// setupOK sets the session cookie before writing the Me body.
type setupOK struct {
	me     Me
	cookie *http.Cookie
}

func (r setupOK) VisitCompleteSetupResponse(w http.ResponseWriter) error {
	return writeMeWithCookie(w, r.me, r.cookie)
}

func writeMeWithCookie(w http.ResponseWriter, me Me, cookie *http.Cookie) error {
	http.SetCookie(w, cookie)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(me); err != nil {
		return fmt.Errorf("writing response: %w", err)
	}
	return nil
}

// logoutOK clears the session cookie before writing the 204.
type logoutOK struct {
	cookie *http.Cookie
}

func (r logoutOK) VisitLogoutResponse(w http.ResponseWriter) error {
	http.SetCookie(w, r.cookie)
	w.WriteHeader(http.StatusNoContent)
	return nil
}
