// SPDX-License-Identifier: SUL-1.0

// Package mcpserver exposes the hub's data to coding agents over the Model
// Context Protocol. Every tool calls the hub's HTTP API with the caller's API
// token through internal/apiclient, so roles, grants and the audit log apply
// unchanged. The same tool set is served over stdio by `aicm-agent mcp` and
// over streamable HTTP at the hub's /mcp.
package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/atakanatamert/aicm/internal/apiclient"
	"github.com/atakanatamert/aicm/internal/version"
)

// New returns an MCP server whose tools call the hub at baseURL. token
// returns the bearer token for the current call (stdio: fixed; HTTP: from
// the incoming request's Authorization header).
func New(baseURL string, token func(ctx context.Context) (string, error)) *mcp.Server {
	hub, err := apiclient.NewClientWithResponses(baseURL,
		apiclient.WithRequestEditorFn(func(ctx context.Context, req *http.Request) error {
			tok, err := token(ctx)
			if err != nil {
				return fmt.Errorf("getting api token: %w", err)
			}
			if tok == "" {
				return errors.New("no api token")
			}
			req.Header.Set("Authorization", "Bearer "+tok)
			return nil
		}))
	if err != nil {
		// The client cannot fail today: the base URL is used verbatim and the
		// only option never errors. A bad configuration should stop startup.
		panic(fmt.Sprintf("mcpserver: building hub client for %q: %v", baseURL, err))
	}

	srv := mcp.NewServer(&mcp.Implementation{
		Name:    version.Name,
		Title:   "aicm",
		Version: version.Version,
	}, nil)
	registerTools(srv, &toolset{hub: hub})
	return srv
}

// HTTPHandler serves streamable HTTP MCP. It takes the bearer token from
// each request's Authorization header and answers 401 without one.
func HTTPHandler(baseURL string) http.Handler {
	stream := mcp.NewStreamableHTTPHandler(func(r *http.Request) *mcp.Server {
		tok, _ := bearerToken(r)
		return New(baseURL, func(context.Context) (string, error) { return tok, nil })
	}, &mcp.StreamableHTTPOptions{
		// Stateless requests keep one server (and therefore one bearer token)
		// per HTTP request. The hub's own bearer auth is the access boundary,
		// so the SDK's localhost DNS-rebinding guard would only break proxies.
		Stateless:                  true,
		DisableLocalhostProtection: true,
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearerToken(r); !ok {
			writeUnauthorized(w)
			return
		}
		stream.ServeHTTP(w, r)
	})
}

// writeUnauthorized writes the hub's standard error envelope. The MCP handler
// sits below the generated router, so it writes this response itself.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":{"code":"unauthorized","message":"bearer token required"}}`))
}

// bearerToken returns the token from "Authorization: Bearer <token>".
func bearerToken(r *http.Request) (string, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		return "", false
	}
	tok = strings.TrimSpace(tok)
	return tok, tok != ""
}
