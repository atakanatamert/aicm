// SPDX-License-Identifier: SUL-1.0

// Package auth handles users, sessions, API tokens, roles and per-account
// grants. This file is the contract every feature uses: the Principal the
// auth middleware puts in the request context.
package auth

import (
	"context"

	"github.com/atakanatamert/aicm/internal/audit"
)

// Role is a user's org-wide role.
type Role string

// Roles, weakest first.
const (
	RoleViewer Role = "viewer"
	RoleEditor Role = "editor"
	RoleAdmin  Role = "admin"
)

func (r Role) rank() int {
	switch r {
	case RoleViewer:
		return 1
	case RoleEditor:
		return 2
	case RoleAdmin:
		return 3
	}
	return 0
}

// AtLeast reports whether r grants everything min grants.
func (r Role) AtLeast(min Role) bool { return r.rank() > 0 && r.rank() >= min.rank() }

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r.rank() > 0 }

// ActorKind matches audit_log.actor_kind.
type ActorKind string

// Actor kinds.
const (
	ActorUser     ActorKind = "user"
	ActorAPIToken ActorKind = "api_token"
	ActorAgent    ActorKind = "agent"
	ActorSystem   ActorKind = "system"
)

// Principal is who is making a request.
type Principal struct {
	OrgID string
	// UserID is the acting user. API tokens act as their user. Empty for
	// agent and system actors.
	UserID string
	Role   Role
	Kind   ActorKind
	// ActorID is the user id, API token id or machine id, for the audit log.
	ActorID string
	// Label is a readable actor name for the audit log (email, token name,
	// machine name).
	Label string
}

// IsAdmin reports whether the principal sees every account in the org.
func (p Principal) IsAdmin() bool { return p.Role == RoleAdmin }

// Actor is the principal as the audit log records it.
func (p Principal) Actor() audit.Actor {
	return audit.Actor{OrgID: p.OrgID, Kind: string(p.Kind), ID: p.ActorID, Label: p.Label}
}

type ctxKey struct{}

// WithPrincipal returns ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// FromContext returns the principal the auth middleware stored. Handlers
// behind the middleware can rely on ok being true.
func FromContext(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}
