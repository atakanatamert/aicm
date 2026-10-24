// SPDX-License-Identifier: SUL-1.0

package agentlink

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"
)

// HandshakeTimeout bounds the whole handshake on both sides.
const HandshakeTimeout = 15 * time.Second

// Token prefixes. Tokens are prefix + 43 base64url characters (32 bytes).
const (
	EnrollTokenPrefix  = "aicm_et_"
	MachineTokenPrefix = "aicm_mt_"
)

// NewToken returns a random token with the given prefix.
func NewToken(prefix string) string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return prefix + base64.RawURLEncoding.EncodeToString(b[:])
}

// HashToken is how the hub stores enroll and machine tokens.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// Fingerprint derives the machine fingerprint the agent sends from a stable
// machine id (for example /etc/machine-id or IOPlatformUUID).
func Fingerprint(machineID string) string {
	h := sha256.Sum256([]byte("aicm-agent-fingerprint\x00" + machineID))
	return hex.EncodeToString(h[:])
}

// NewNonce returns 32 random bytes, base64.
func NewNonce() string {
	var b [32]byte
	_, _ = rand.Read(b[:])
	return base64.StdEncoding.EncodeToString(b[:])
}

// Transcript is the byte string the hub signs. It binds the org, the
// machine fingerprint and both nonces, so a signature can't be replayed to
// another agent or connection.
func Transcript(h Hello, hubNonce string) []byte {
	return []byte(strings.Join([]string{Subprotocol, h.OrgID, h.Fingerprint, h.Nonce, hubNonce}, "\n"))
}

// ErrBadHubSignature means the hub could not prove it holds the pinned key.
var ErrBadHubSignature = errors.New("hub signature does not match the pinned hub key")

// ClientHandshake runs the agent side over a fresh connection: Hello, verify
// the hub's Challenge against hubKey, then Auth. The credential is sent only
// after the signature checks out.
func ClientHandshake(ctx context.Context, c *Conn, hubKey ed25519.PublicKey, hello Hello, auth Auth) (Welcome, error) {
	ctx, cancel := context.WithTimeout(ctx, HandshakeTimeout)
	defer cancel()

	if len(hubKey) != ed25519.PublicKeySize {
		return Welcome{}, errors.New("hub public key has the wrong length")
	}
	if (auth.EnrollToken == "") == (auth.MachineToken == "") {
		return Welcome{}, errors.New("auth needs exactly one of enroll token or machine token")
	}
	hello.Version = Version
	if hello.Nonce == "" {
		hello.Nonce = NewNonce()
	}
	if _, err := c.Send(ctx, TypeHello, "", hello); err != nil {
		return Welcome{}, err
	}

	ch, err := expect[Challenge](ctx, c, TypeChallenge)
	if err != nil {
		return Welcome{}, err
	}
	sig, err := base64.StdEncoding.DecodeString(ch.Signature)
	if err != nil || !ed25519.Verify(hubKey, Transcript(hello, ch.HubNonce), sig) {
		c.Fail(ctx, CodeBadHubSignature, "signature check failed")
		return Welcome{}, ErrBadHubSignature
	}

	if _, err := c.Send(ctx, TypeAuth, "", auth); err != nil {
		return Welcome{}, err
	}
	return expect[Welcome](ctx, c, TypeWelcome)
}

// Hub is what ServerHandshake needs from the hub's machine service.
type Hub interface {
	// SigningKey returns the org's hub key. Unknown orgs return an error.
	SigningKey(ctx context.Context, orgID string) (ed25519.PrivateKey, error)
	// Authenticate checks the credential and fingerprint, creates the
	// machine on first enrollment (issuing a machine token) and returns the
	// Welcome. Return an *Error to send a specific code to the agent.
	Authenticate(ctx context.Context, hello Hello, auth Auth) (Welcome, error)
}

// ServerHandshake runs the hub side. On success the connection is ready for
// data messages; on failure an Error has been sent and the connection closed.
func ServerHandshake(ctx context.Context, c *Conn, hub Hub) (Hello, Welcome, error) {
	ctx, cancel := context.WithTimeout(ctx, HandshakeTimeout)
	defer cancel()

	hello, err := expect[Hello](ctx, c, TypeHello)
	if err != nil {
		c.Fail(ctx, CodeBadMessage, "expected hello")
		return Hello{}, Welcome{}, err
	}
	if hello.Version != Version {
		c.Fail(ctx, CodeUnsupported, fmt.Sprintf("hub speaks agentlink version %d", Version))
		return hello, Welcome{}, fmt.Errorf("agent speaks version %d", hello.Version)
	}
	if nonce, err := base64.StdEncoding.DecodeString(hello.Nonce); err != nil || len(nonce) != 32 {
		c.Fail(ctx, CodeBadMessage, "nonce must be 32 bytes, base64")
		return hello, Welcome{}, errors.New("bad agent nonce")
	}
	if hello.Fingerprint == "" || hello.OrgID == "" {
		c.Fail(ctx, CodeBadMessage, "org_id and fingerprint are required")
		return hello, Welcome{}, errors.New("hello without org or fingerprint")
	}
	key, err := hub.SigningKey(ctx, hello.OrgID)
	if err != nil {
		c.Fail(ctx, CodeUnauthorized, "unknown org")
		return hello, Welcome{}, fmt.Errorf("loading hub key: %w", err)
	}
	hubNonce := NewNonce()
	sig := ed25519.Sign(key, Transcript(hello, hubNonce))
	if _, err := c.Send(ctx, TypeChallenge, "", Challenge{HubNonce: hubNonce, Signature: base64.StdEncoding.EncodeToString(sig)}); err != nil {
		return hello, Welcome{}, err
	}

	auth, err := expect[Auth](ctx, c, TypeAuth)
	if err != nil {
		c.Fail(ctx, CodeBadMessage, "expected auth")
		return hello, Welcome{}, err
	}
	welcome, err := hub.Authenticate(ctx, hello, auth)
	if err != nil {
		var le *Error
		if errors.As(err, &le) {
			c.Fail(ctx, le.Code, le.Message)
		} else {
			c.Fail(ctx, CodeInternal, "authentication failed")
		}
		return hello, Welcome{}, fmt.Errorf("authenticating agent: %w", err)
	}
	if welcome.ServerTime.IsZero() {
		welcome.ServerTime = time.Now().UTC()
	}
	if _, err := c.Send(ctx, TypeWelcome, "", welcome); err != nil {
		return hello, Welcome{}, err
	}
	return hello, welcome, nil
}

// expect reads one message of type want. A received Error is returned as
// *Error.
func expect[T any](ctx context.Context, c *Conn, want Type) (T, error) {
	var zero T
	e, err := c.Recv(ctx)
	if err != nil {
		return zero, err
	}
	if e.Type == TypeError {
		le, err := Decode[Error](e)
		if err != nil {
			return zero, err
		}
		return zero, &le
	}
	if e.Type != want {
		return zero, fmt.Errorf("expected %s, got %s", want, e.Type)
	}
	return Decode[T](e)
}
