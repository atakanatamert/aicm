// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"
	"net/http"

	"github.com/atakanatamert/aicm/internal/auth"
)

// GetOidcConfig tells the login page whether to show the sign-on button.
func (s *Server) GetOidcConfig(_ context.Context, _ GetOidcConfigRequestObject) (GetOidcConfigResponseObject, error) {
	enabled := s.Auth != nil && s.Auth.OIDCEnabled()
	response := GetOidcConfig200JSONResponse{Enabled: enabled}
	if enabled {
		response.Name = new(s.Auth.OIDCName())
		response.StartUrl = new("/auth/oidc/start")
	}
	return response, nil
}

// oidcStart redirects the browser to the provider. Discovery is lazy, so a
// provider that is down sends the user back to the login page instead of
// breaking the hub.
func (s *Server) oidcStart(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil || !s.Auth.OIDCEnabled() {
		redirectLoginError(w, r, "sso_unavailable")
		return
	}
	authURL, cookie, err := s.Auth.OIDCStart(r.Context())
	if err != nil {
		redirectLoginError(w, r, "sso_unavailable")
		return
	}
	http.SetCookie(w, cookie)
	http.Redirect(w, r, authURL, http.StatusFound)
}

// oidcCallback verifies the flow and creates the session, then sends the
// browser to the app. Failures land on the login page with a reason.
func (s *Server) oidcCallback(w http.ResponseWriter, r *http.Request) {
	if s.Auth == nil {
		redirectLoginError(w, r, "sso_unavailable")
		return
	}
	cookieValue := ""
	if cookie, err := r.Cookie(auth.OIDCStateCookieName); err == nil {
		cookieValue = cookie.Value
	}
	_, userAgent, ip := auth.RequestMeta(r.Context())
	session, err := s.Auth.OIDCFinish(r.Context(),
		r.URL.Query().Get("code"), r.URL.Query().Get("state"), cookieValue, userAgent, ip)
	http.SetCookie(w, s.Auth.ClearedOIDCStateCookie())
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrSSOUnknownUser):
			redirectLoginError(w, r, "sso_unknown_user")
		case errors.Is(err, auth.ErrOIDCUnverified):
			redirectLoginError(w, r, "sso_unverified")
		default:
			// Do not log the exchange error: provider messages can echo
			// request parameters, and the authorization code is a credential.
			redirectLoginError(w, r, "sso_unavailable")
		}
		return
	}
	http.SetCookie(w, s.Auth.SessionCookie(session.Token, session.ExpiresAt))
	http.Redirect(w, r, "/", http.StatusFound)
}

func redirectLoginError(w http.ResponseWriter, r *http.Request, reason string) {
	http.Redirect(w, r, "/login?error="+reason, http.StatusFound)
}
