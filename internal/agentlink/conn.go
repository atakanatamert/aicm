// SPDX-License-Identifier: Apache-2.0

package agentlink

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

// Envelope wraps every message.
type Envelope struct {
	Type Type `json:"type"`
	// ID is unique per sender. Acks refer to it; agents keep spooled
	// messages under it until acked, and resend them with the same ID.
	ID      string          `json:"id"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Conn sends and receives envelopes over a WebSocket. Send is safe for
// concurrent use; Recv must be called from one goroutine.
type Conn struct {
	ws *websocket.Conn
}

// NewConn wraps an established WebSocket and sets the read limit.
func NewConn(ws *websocket.Conn) *Conn {
	ws.SetReadLimit(MaxMessageBytes)
	return &Conn{ws: ws}
}

// WebSocket returns the underlying connection, for Ping and Close.
func (c *Conn) WebSocket() *websocket.Conn { return c.ws }

// Send writes one message. An empty id gets a fresh one, which is returned.
func (c *Conn) Send(ctx context.Context, typ Type, id string, payload any) (string, error) {
	if id == "" {
		id = NewID()
	}
	p, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encoding %s payload: %w", typ, err)
	}
	b, err := json.Marshal(Envelope{Type: typ, ID: id, Payload: p})
	if err != nil {
		return "", fmt.Errorf("encoding %s envelope: %w", typ, err)
	}
	if len(b) > MaxMessageBytes {
		return "", fmt.Errorf("%s message is %d bytes, over the %d limit", typ, len(b), MaxMessageBytes)
	}
	if err := c.ws.Write(ctx, websocket.MessageText, b); err != nil {
		return "", fmt.Errorf("writing %s: %w", typ, err)
	}
	return id, nil
}

// Recv reads one message.
func (c *Conn) Recv(ctx context.Context) (Envelope, error) {
	typ, b, err := c.ws.Read(ctx)
	if err != nil {
		return Envelope{}, fmt.Errorf("reading message: %w", err)
	}
	if typ != websocket.MessageText {
		return Envelope{}, errors.New("reading message: binary frames are not used")
	}
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return Envelope{}, fmt.Errorf("decoding envelope: %w", err)
	}
	if e.Type == "" || e.ID == "" {
		return Envelope{}, errors.New("decoding envelope: type and id are required")
	}
	return e, nil
}

// Decode unmarshals an envelope's payload.
func Decode[T any](e Envelope) (T, error) {
	var v T
	if err := json.Unmarshal(e.Payload, &v); err != nil {
		return v, fmt.Errorf("decoding %s payload: %w", e.Type, err)
	}
	return v, nil
}

// Fail sends an Error message and closes the connection.
func (c *Conn) Fail(ctx context.Context, code, message string) {
	_, _ = c.Send(ctx, TypeError, "", Error{Code: code, Message: message})
	_ = c.ws.Close(websocket.StatusPolicyViolation, code)
}

// Error lets a received Error be returned as a Go error.
func (e *Error) Error() string { return "agentlink: " + e.Code + ": " + e.Message }

// NewID returns a random 128-bit hex id.
func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
