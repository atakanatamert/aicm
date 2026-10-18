// SPDX-License-Identifier: AGPL-3.0-or-later

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"

	"github.com/atakanatamert/aicm/internal/audit"
)

// StrictHandlerFunc mirrors the generated strict handler type. It is an alias
// so httpapi can adapt its own named type without an import cycle.
type StrictHandlerFunc = func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error)

type requestMetaKey struct{}

type requestMeta struct {
	sessionToken string
	userAgent    string
	ip           netip.Addr
}

// RequestMeta returns the raw session cookie value (empty for API token
// requests), the User-Agent and the client IP recorded by HTTPMiddleware.
func RequestMeta(ctx context.Context) (sessionToken, userAgent string, ip netip.Addr) {
	m, _ := ctx.Value(requestMetaKey{}).(requestMeta)
	return m.sessionToken, m.userAgent, m.ip
}

// HTTPMiddleware authenticates the request and stores the Principal in the
// context when credentials are present. It never rejects unauthenticated
// requests by itself.
//
// Credentials are tried in order: "Authorization: Bearer aicm_pat_…", then
// the aicm_session cookie. Cookie-authenticated unsafe requests are rejected
// when an Origin header does not match the hub's public URL (CSRF guard).
func (s *Service) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		var (
			principal  Principal
			ok         bool
			token      string
			fromCookie bool
		)

		if bearer, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); found && bearer != "" {
			token = strings.TrimSpace(bearer)
			principal, ok = s.authenticate(ctx, token, true)
		} else if cookie, err := r.Cookie(SessionCookieName); err == nil && cookie.Value != "" {
			token = cookie.Value
			fromCookie = true
			principal, ok = s.authenticate(ctx, token, false)
		}

		if fromCookie && isUnsafeMethod(r.Method) {
			if origin := r.Header.Get("Origin"); origin != "" && origin != s.publicOrigin {
				writeAuthError(w, http.StatusForbidden, "bad_origin", "origin does not match the hub's public URL")
				return
			}
		}

		meta := requestMeta{
			userAgent: r.UserAgent(),
			ip:        remoteIP(r.RemoteAddr),
		}
		if fromCookie {
			meta.sessionToken = token
		}
		ctx = context.WithValue(ctx, requestMetaKey{}, meta)
		ctx = audit.WithClientIP(ctx, meta.ip)
		if ok {
			ctx = WithPrincipal(ctx, principal)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Service) authenticate(ctx context.Context, token string, bearer bool) (Principal, bool) {
	var (
		p   Principal
		err error
	)
	if bearer {
		p, err = s.AuthenticateAPIToken(ctx, token)
	} else {
		p, err = s.AuthenticateSession(ctx, token)
	}
	if err != nil {
		if !errors.Is(err, ErrUnauthenticated) && ctx.Err() == nil {
			s.log.WarnContext(ctx, "authenticating request failed", "err", err)
		}
		return Principal{}, false
	}
	return p, true
}

func isUnsafeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

func remoteIP(remoteAddr string) netip.Addr {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip
}

func writeAuthError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}

// Authorize returns a strict middleware that enforces OperationRoles.
// Public operations pass; missing or weaker principals return
// ErrUnauthenticated or ErrForbidden, which the HTTP layer maps to 401 and
// 403. Unknown operation ids are denied.
func Authorize(_ func(w http.ResponseWriter, status int, code, msg string)) func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
	return func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
		// The generated strict handler passes its exported method name
		// ("GetSetupStatus"); the table is keyed by the spec's operationId.
		if operationID != "" && operationID[0] >= 'A' && operationID[0] <= 'Z' {
			operationID = string(operationID[0]+'a'-'A') + operationID[1:]
		}
		min, known := OperationRoles[operationID]
		return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
			if !known {
				return nil, ErrForbidden
			}
			if min == "" {
				return f(ctx, w, r, request)
			}
			p, ok := FromContext(ctx)
			if !ok {
				return nil, ErrUnauthenticated
			}
			if !p.Role.AtLeast(min) {
				return nil, ErrForbidden
			}
			return f(ctx, w, r, request)
		}
	}
}
