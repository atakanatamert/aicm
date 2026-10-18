// SPDX-License-Identifier: AGPL-3.0-or-later

// Package httpapi is the hub's HTTP layer. Routing, request decoding and
// response types are generated from api/openapi.yaml into api.gen.go
// (go generate ./internal/httpapi). Server implements the generated
// StrictServerInterface with one file per feature (accounts.go, auth.go, …).
// Handlers parse nothing by hand: they get typed request objects, call the
// feature's service and return typed responses or an *APIError.
//
// Operations not built yet live in unimplemented.go and answer 501. A task
// that implements an operation moves its method from there into the
// feature file.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/agenthub"
	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/alert"
	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/charge"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/jobs"
	"github.com/atakanatamert/aicm/internal/machine"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/quota"
	"github.com/atakanatamert/aicm/internal/secrets"
	"github.com/atakanatamert/aicm/internal/settings"
	"github.com/atakanatamert/aicm/internal/syncer"
	"github.com/atakanatamert/aicm/internal/usage"
	"github.com/atakanatamert/aicm/internal/version"
)

// Pinger reports whether the database is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the services handlers need. Later tasks add fields.
type Deps struct {
	Log       *slog.Logger
	DB        Pinger
	Jobs      *jobs.Client
	Secrets   secrets.Store
	Auth      *auth.Service
	Usage     *usage.Service
	Quota     *quota.Service
	Audit     *audit.Service
	Sync      *syncer.Service
	Accounts  *account.Service
	FX        *fx.Service
	Charges   *charge.Service
	Providers []provider.Provider // the compiled-in catalog, in display order
	Machines  *machine.Service
	AgentHub  *agenthub.Hub
	Alerts    *alert.Service
	Settings  *settings.Service
	Install   http.Handler // public install scripts; nil disables them
	// MCP serves the MCP tools at /mcp when set.
	MCP http.Handler
	// Web serves the embedded UI for every other GET path.
	Web http.Handler
}

// Server implements StrictServerInterface.
type Server struct {
	Deps
}

var _ StrictServerInterface = (*Server)(nil)

// NewRouter returns the hub's HTTP handler.
func NewRouter(d Deps) http.Handler {
	s := &Server{Deps: d}
	authorize := auth.Authorize(WriteError)
	strict := NewStrictHandlerWithOptions(s, []StrictMiddlewareFunc{
		func(f StrictHandlerFunc, operationID string) StrictHandlerFunc {
			h := authorize(auth.StrictHandlerFunc(f), operationID)
			return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request any) (any, error) {
				return h(ctx, w, r, request)
			}
		},
	}, StrictHTTPServerOptions{
		RequestErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		},
		ResponseErrorHandlerFunc: s.writeHandlerError,
	})
	// Patterns carry methods throughout: a method-less pattern such as "/mcp"
	// would overlap "GET /" without either being more specific, which
	// net/http rejects.
	base := http.NewServeMux()
	if d.Web != nil {
		// API routes are more specific and keep winning; the UI serves
		// index.html for the rest so client-side routes survive a reload.
		base.Handle("GET /", d.Web)
	}
	// Unknown /api/... paths still get the JSON 404 envelope.
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodPut} {
		base.HandleFunc(method+" /api/", func(w http.ResponseWriter, _ *http.Request) {
			WriteError(w, http.StatusNotFound, "not_found", "unknown API endpoint")
		})
	}
	if d.AgentHub != nil {
		// The agent endpoint authenticates with the agentlink handshake and
		// lives outside the generated router, so the role middleware never
		// sees it.
		base.Handle("GET "+agentlink.Path, d.AgentHub)
	}
	if d.Install != nil {
		// The install scripts are public: the token in the query-free
		// command is the credential, not a hub session.
		base.Handle("GET /install.sh", d.Install)
		base.Handle("GET /install.ps1", d.Install)
	}
	if d.MCP != nil {
		// MCP authenticates each request with its own bearer token and loops
		// every tool call back through the API, so it also sits outside the
		// generated router. Streamable HTTP uses POST, GET (stream) and DELETE.
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
			base.Handle(method+" /mcp", d.MCP)
		}
	}
	h := HandlerWithOptions(strict, StdHTTPServerOptions{
		BaseRouter: base,
		ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
			WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		},
	})
	if d.Auth != nil {
		return d.Auth.HTTPMiddleware(h)
	}
	return h
}

// GetHealth reports that the process is up. It never touches the database.
func (s *Server) GetHealth(_ context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	return GetHealth200JSONResponse{Status: "ok", Version: new(version.Version)}, nil
}

// GetReady reports whether the hub can serve traffic (database reachable).
func (s *Server) GetReady(ctx context.Context, _ GetReadyRequestObject) (GetReadyResponseObject, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	if err := s.DB.Ping(ctx); err != nil {
		s.Log.WarnContext(ctx, "readiness check failed", "err", err)
		return nil, &APIError{Status: http.StatusServiceUnavailable, Code: "db_unavailable", Message: "database is not reachable"}
	}
	return GetReady200JSONResponse{Status: "ready"}, nil
}

// APIError is what handlers return to send a specific status and code.
// Any other error becomes a logged 500 with code "internal".
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Common errors.
var (
	ErrNotImplemented = &APIError{Status: http.StatusNotImplemented, Code: "not_implemented", Message: "this endpoint is not implemented yet"}
	ErrUnauthorized   = &APIError{Status: http.StatusUnauthorized, Code: "unauthorized", Message: "login required"}
	ErrForbidden      = &APIError{Status: http.StatusForbidden, Code: "forbidden", Message: "your role does not allow this"}
)

// NotFound, BadRequest and Conflict build the usual client errors.
func NotFound(msg string) *APIError {
	return &APIError{Status: http.StatusNotFound, Code: "not_found", Message: msg}
}

// BadRequest is a 400 with code validation_failed.
func BadRequest(msg string) *APIError {
	return &APIError{Status: http.StatusBadRequest, Code: "validation_failed", Message: msg}
}

// Conflict is a 409 with code conflict.
func Conflict(msg string) *APIError {
	return &APIError{Status: http.StatusConflict, Code: "conflict", Message: msg}
}

func (s *Server) writeHandlerError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthenticated):
		WriteError(w, http.StatusUnauthorized, "unauthorized", "login required")
		return
	case errors.Is(err, auth.ErrForbidden):
		WriteError(w, http.StatusForbidden, "forbidden", "your role does not allow this")
		return
	}
	var ae *APIError
	if errors.As(err, &ae) {
		WriteError(w, ae.Status, ae.Code, ae.Message)
		return
	}
	if r.Context().Err() != nil {
		return // the client went away; nobody reads the response
	}
	s.Log.ErrorContext(r.Context(), "handler failed", "method", r.Method, "path", r.URL.Path, "err", err)
	WriteError(w, http.StatusInternalServerError, "internal", "internal error")
}

// WriteJSON writes v as JSON with the given status. Only for responses the
// generated code doesn't cover (cookies, CSV, WebSocket upgrade errors).
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the standard envelope {"error": {"code", "message"}}.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, Error{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}
