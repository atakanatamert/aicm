// SPDX-License-Identifier: SUL-1.0

// Package audit writes the append-only audit log. Every mutation calls
// Write once, inside the same transaction as the change when there is one,
// so a change and its audit row commit together.
//
// audit imports nothing from the feature packages, so every package
// (including auth) can call it. auth.Principal.Actor() builds the Actor.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"

	"github.com/atakanatamert/aicm/internal/db"
)

// Actor is who made a change. Kind matches audit_log.actor_kind: user,
// api_token, agent or system (the default when empty).
type Actor struct {
	OrgID string
	Kind  string
	ID    string
	Label string
}

// Entry describes one change.
type Entry struct {
	// Action is "<target>.<verb>", for example "account.update",
	// "user.create", "secret.set", "charge.confirm".
	Action string
	// TargetKind is the table-like name: "account", "user", "charge", …
	TargetKind string
	TargetID   string
	// Before and After are the changed fields (structs or maps). Use nil
	// for creates (Before) and deletes (After). Values under keys that look
	// secret are replaced with "[redacted]" before storage.
	Before any
	After  any
}

type ipKey struct{}

// WithClientIP returns ctx carrying the request's client IP. The auth
// middleware sets it; Write stores it with every row.
func WithClientIP(ctx context.Context, ip netip.Addr) context.Context {
	return context.WithValue(ctx, ipKey{}, ip)
}

// Write records e for actor a. q is the transaction (db.New(tx)) or the
// pool's queries.
func Write(ctx context.Context, q *db.Queries, a Actor, e Entry) error {
	before, err := encode(e.Before)
	if err != nil {
		return fmt.Errorf("encoding audit before: %w", err)
	}
	after, err := encode(e.After)
	if err != nil {
		return fmt.Errorf("encoding audit after: %w", err)
	}
	var actorID *string
	if a.ID != "" {
		actorID = new(a.ID)
	}
	kind := a.Kind
	if kind == "" {
		kind = "system"
	}
	params := db.InsertAuditLogParams{
		OrgID:      a.OrgID,
		ActorKind:  kind,
		ActorID:    actorID,
		ActorLabel: a.Label,
		Action:     e.Action,
		TargetKind: e.TargetKind,
		TargetID:   e.TargetID,
		Before:     before,
		After:      after,
	}
	if ip, ok := ctx.Value(ipKey{}).(netip.Addr); ok && ip.IsValid() {
		params.Ip = new(ip)
	}
	if err := q.InsertAuditLog(ctx, params); err != nil {
		return fmt.Errorf("writing audit log: %w", err)
	}
	return nil
}

// secretish reports whether a field name holds a credential.
func secretish(key string) bool {
	k := strings.ToLower(key)
	for _, s := range []string{"password", "secret", "token", "hash", "ciphertext", "nonce", "api_key", "apikey", "master_key", "value"} {
		if strings.Contains(k, s) {
			return true
		}
	}
	return false
}

// encode marshals v to JSON with secret-looking fields redacted.
func encode(v any) (json.RawMessage, error) {
	if v == nil {
		return nil, nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("marshalling: %w", err)
	}
	var generic any
	if err := json.Unmarshal(b, &generic); err != nil {
		return nil, fmt.Errorf("unmarshalling: %w", err)
	}
	b, err = json.Marshal(redact(generic))
	if err != nil {
		return nil, fmt.Errorf("marshalling redacted: %w", err)
	}
	return b, nil
}

func redact(v any) any {
	switch t := v.(type) {
	case map[string]any:
		for k, val := range t {
			if secretish(k) && val != nil {
				t[k] = "[redacted]"
			} else {
				t[k] = redact(val)
			}
		}
		return t
	case []any:
		for i := range t {
			t[i] = redact(t[i])
		}
		return t
	default:
		return v
	}
}
