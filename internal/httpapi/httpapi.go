// SPDX-License-Identifier: AGPL-3.0-or-later

// Package httpapi is the hub's HTTP layer: the router, the JSON helpers and
// one file of handlers per feature. Handlers parse the request, call the
// feature's service and write JSON. Business logic stays in the feature
// packages.
package httpapi

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/atakanatamert/aicm/internal/version"
)

// Pinger reports whether the database is reachable. *pgxpool.Pool satisfies it.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Deps are the services the router needs. Later tasks add fields.
type Deps struct {
	Log *slog.Logger
	DB  Pinger
}

// NewRouter returns the hub's HTTP handler.
func NewRouter(d Deps) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealthz)
	mux.HandleFunc("GET /readyz", d.handleReadyz)
	return mux
}

// handleHealthz reports that the process is up. It never touches the database.
func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": version.Version})
}

// handleReadyz reports whether the hub can serve traffic (database reachable).
func (d Deps) handleReadyz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := d.DB.Ping(ctx); err != nil {
		d.Log.WarnContext(ctx, "readiness check failed", "err", err)
		WriteError(w, http.StatusServiceUnavailable, "db_unavailable", "database is not reachable")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

// ErrorBody is the JSON error envelope every handler returns:
// {"error": {"code": "...", "message": "..."}}.
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the inner error object. Code is a stable snake_case string.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON writes v as JSON with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// WriteError writes the standard JSON error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, ErrorBody{Error: ErrorDetail{Code: code, Message: message}})
}
