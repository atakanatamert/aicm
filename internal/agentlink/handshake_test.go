// SPDX-License-Identifier: SUL-1.0

package agentlink

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/atakanatamert/aicm/internal/meter"
)

// fakeHub is an in-memory machine registry with the same rules the real
// hub service must follow.
type fakeHub struct {
	key ed25519.PrivateKey

	mu          sync.Mutex
	enrollHash  []byte
	machines    map[string]fakeMachine // by machine token hash (string)
	authCalls   int
	acked       []string
	gotUsage    []UsageEvent
	gotReadings []MeterReport
}

type fakeMachine struct {
	id, fingerprint string
}

const testOrg = "org-1"

func newFakeHub(t *testing.T, enrollToken string) *fakeHub {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeHub{key: priv, enrollHash: HashToken(enrollToken), machines: map[string]fakeMachine{}}
}

func (h *fakeHub) SigningKey(_ context.Context, orgID string) (ed25519.PrivateKey, error) {
	if orgID != testOrg {
		return nil, errors.New("unknown org")
	}
	return h.key, nil
}

func (h *fakeHub) Authenticate(_ context.Context, hello Hello, auth Auth) (Welcome, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.authCalls++
	switch {
	case auth.EnrollToken != "":
		if !bytes.Equal(HashToken(auth.EnrollToken), h.enrollHash) {
			return Welcome{}, &Error{Code: CodeUnauthorized, Message: "invalid enroll token"}
		}
		tok := NewToken(MachineTokenPrefix)
		m := fakeMachine{id: "machine-" + hello.Fingerprint[:8], fingerprint: hello.Fingerprint}
		h.machines[string(HashToken(tok))] = m
		return Welcome{MachineID: m.id, MachineToken: tok}, nil
	default:
		m, ok := h.machines[string(HashToken(auth.MachineToken))]
		if !ok {
			return Welcome{}, &Error{Code: CodeUnauthorized, Message: "unknown machine token"}
		}
		if m.fingerprint != hello.Fingerprint {
			return Welcome{}, &Error{Code: CodeFingerprint, Message: "fingerprint changed"}
		}
		return Welcome{MachineID: m.id}, nil
	}
}

// serve runs the hub side: handshake, then ack every data message.
func (h *fakeHub) serve(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{Subprotocol}})
	if err != nil {
		return
	}
	c := NewConn(ws)
	defer ws.CloseNow() //nolint:errcheck // test cleanup
	if _, _, err := ServerHandshake(r.Context(), c, h); err != nil {
		return
	}
	for {
		e, err := c.Recv(r.Context())
		if err != nil {
			return
		}
		h.mu.Lock()
		switch e.Type {
		case TypeUsage:
			u, _ := Decode[Usage](e)
			h.gotUsage = append(h.gotUsage, u.Events...)
		case TypeMeters:
			m, _ := Decode[Meters](e)
			h.gotReadings = append(h.gotReadings, m.Reports...)
		}
		h.acked = append(h.acked, e.ID)
		h.mu.Unlock()
		if _, err := c.Send(r.Context(), TypeAck, "", Ack{ID: e.ID}); err != nil {
			return
		}
	}
}

func dial(t *testing.T, ctx context.Context, url string) *Conn {
	t.Helper()
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{Subprotocol}}) //nolint:bodyclose // coder/websocket owns the response body
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return NewConn(ws)
}

func testHello(fp string) Hello {
	return Hello{OrgID: testOrg, Fingerprint: fp, Hostname: "laptop", OS: "darwin", Arch: "arm64", AgentVersion: "test"}
}

func TestLoopbackEnrollPushReconnect(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	enroll := NewToken(EnrollTokenPrefix)
	hub := newFakeHub(t, enroll)
	srv := httptest.NewServer(http.HandlerFunc(hub.serve))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + Path
	pub := hub.key.Public().(ed25519.PublicKey)
	fp := Fingerprint("machine-uuid-1")

	// First contact: enroll and receive a machine token.
	c := dial(t, ctx, url)
	w, err := ClientHandshake(ctx, c, pub, testHello(fp), Auth{EnrollToken: enroll})
	if err != nil {
		t.Fatalf("enroll handshake: %v", err)
	}
	if w.MachineID == "" || !strings.HasPrefix(w.MachineToken, MachineTokenPrefix) {
		t.Fatalf("welcome = %+v, want machine id and token", w)
	}

	// Push one usage batch and one meter batch; each is acked by id.
	ref := AccountRef{ProviderID: "claude", ExternalID: "acct-uuid"}
	usageID, err := c.Send(ctx, TypeUsage, "", Usage{Events: []UsageEvent{{
		Account: ref, Source: "claudecode", MessageID: "msg_1:req_1",
		OccurredAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC), Model: "claude-opus-4-1", InputTokens: 10, OutputTokens: 20,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	expectAck(t, ctx, c, usageID)
	reading := meter.Reading{Key: "five_hour", Used: new(23.5), CapturedAt: time.Now().UTC(), Source: meter.SourceStatusline}
	def := meter.Def{Key: "five_hour", Label: "5-hour limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 300}
	metersID, err := c.Send(ctx, TypeMeters, "", Meters{Reports: []MeterReport{{Account: ref, Def: def, Reading: reading}}})
	if err != nil {
		t.Fatal(err)
	}
	expectAck(t, ctx, c, metersID)
	_ = c.WebSocket().Close(websocket.StatusNormalClosure, "")

	// Later connections use the machine token and get no new token.
	c2 := dial(t, ctx, url)
	w2, err := ClientHandshake(ctx, c2, pub, testHello(fp), Auth{MachineToken: w.MachineToken})
	if err != nil {
		t.Fatalf("reconnect handshake: %v", err)
	}
	if w2.MachineID != w.MachineID || w2.MachineToken != "" {
		t.Fatalf("reconnect welcome = %+v, want same machine and no token", w2)
	}

	hub.mu.Lock()
	defer hub.mu.Unlock()
	if len(hub.gotUsage) != 1 || hub.gotUsage[0].MessageID != "msg_1:req_1" {
		t.Errorf("hub usage = %+v", hub.gotUsage)
	}
	if len(hub.gotReadings) != 1 || *hub.gotReadings[0].Reading.Used != 23.5 {
		t.Errorf("hub readings = %+v", hub.gotReadings)
	}
}

func expectAck(t *testing.T, ctx context.Context, c *Conn, id string) {
	t.Helper()
	e, err := c.Recv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	ack, err := Decode[Ack](e)
	if err != nil || e.Type != TypeAck || ack.ID != id {
		t.Fatalf("got %s %+v, want ack for %s", e.Type, ack, id)
	}
}

func TestHandshakeRejections(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	enroll := NewToken(EnrollTokenPrefix)
	hub := newFakeHub(t, enroll)
	srv := httptest.NewServer(http.HandlerFunc(hub.serve))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + Path
	pub := hub.key.Public().(ed25519.PublicKey)

	t.Run("impostor hub never receives the token", func(t *testing.T) {
		otherPub, _, _ := ed25519.GenerateKey(nil)
		before := hub.authCalls
		_, err := ClientHandshake(ctx, dial(t, ctx, url), otherPub, testHello(Fingerprint("m")), Auth{EnrollToken: enroll})
		if !errors.Is(err, ErrBadHubSignature) {
			t.Fatalf("err = %v, want ErrBadHubSignature", err)
		}
		time.Sleep(50 * time.Millisecond)
		hub.mu.Lock()
		defer hub.mu.Unlock()
		if hub.authCalls != before {
			t.Error("hub received auth after a failed signature check")
		}
	})

	t.Run("wrong enroll token", func(t *testing.T) {
		_, err := ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(Fingerprint("m")), Auth{EnrollToken: NewToken(EnrollTokenPrefix)})
		var le *Error
		if !errors.As(err, &le) || le.Code != CodeUnauthorized {
			t.Fatalf("err = %v, want %s", err, CodeUnauthorized)
		}
	})

	t.Run("machine token from another fingerprint", func(t *testing.T) {
		w, err := ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(Fingerprint("a")), Auth{EnrollToken: enroll})
		if err != nil {
			t.Fatal(err)
		}
		_, err = ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(Fingerprint("b")), Auth{MachineToken: w.MachineToken})
		var le *Error
		if !errors.As(err, &le) || le.Code != CodeFingerprint {
			t.Fatalf("err = %v, want %s", err, CodeFingerprint)
		}
	})

	t.Run("unknown org", func(t *testing.T) {
		h := testHello(Fingerprint("m"))
		h.OrgID = "other"
		_, err := ClientHandshake(ctx, dial(t, ctx, url), pub, h, Auth{EnrollToken: enroll})
		var le *Error
		if !errors.As(err, &le) || le.Code != CodeUnauthorized {
			t.Fatalf("err = %v, want %s", err, CodeUnauthorized)
		}
	})
}

func TestTranscriptBindsEveryField(t *testing.T) {
	_, priv, _ := ed25519.GenerateKey(nil)
	pub := priv.Public().(ed25519.PublicKey)
	h := Hello{OrgID: "o", Fingerprint: "f", Nonce: NewNonce()}
	sig := ed25519.Sign(priv, Transcript(h, "hub-nonce"))
	for name, mut := range map[string]func(*Hello, *string){
		"org":         func(h *Hello, _ *string) { h.OrgID = "o2" },
		"fingerprint": func(h *Hello, _ *string) { h.Fingerprint = "f2" },
		"agent nonce": func(h *Hello, _ *string) { h.Nonce = NewNonce() },
		"hub nonce":   func(_ *Hello, n *string) { *n = "other" },
	} {
		h2, n := h, "hub-nonce"
		mut(&h2, &n)
		if ed25519.Verify(pub, Transcript(h2, n), sig) {
			t.Errorf("signature still verifies after changing %s", name)
		}
	}
}
