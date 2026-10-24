// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
)

const testOrg = "org-1"

// fakeHub is an in-memory machine registry with the same handshake rules as
// the real hub. It records every envelope it receives.
type fakeHub struct {
	key        ed25519.PrivateKey
	enrollHash []byte
	cfg        agentlink.AgentConfig
	ack        bool

	mu          sync.Mutex
	machines    map[string]fakeMachine // by machine token hash
	envelopes   []agentlink.Envelope
	usage       []agentlink.UsageEvent
	seen        map[string]bool // envelope ids whose data was already recorded
	connections int
	conns       []*websocket.Conn
}

type fakeMachine struct {
	id, fingerprint string
}

func newFakeHub(t *testing.T, enrollToken string) *fakeHub {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	return &fakeHub{
		key:        priv,
		enrollHash: agentlink.HashToken(enrollToken),
		cfg:        agentlink.AgentConfig{Accounts: []agentlink.AccountConfig{}, ScanIntervalSeconds: 60},
		ack:        true,
		machines:   map[string]fakeMachine{},
		seen:       map[string]bool{},
	}
}

func (h *fakeHub) SigningKey(_ context.Context, orgID string) (ed25519.PrivateKey, error) {
	if orgID != testOrg {
		return nil, errors.New("unknown org")
	}
	return h.key, nil
}

func (h *fakeHub) Authenticate(_ context.Context, hello agentlink.Hello, auth agentlink.Auth) (agentlink.Welcome, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	switch {
	case auth.EnrollToken != "":
		if !bytes.Equal(agentlink.HashToken(auth.EnrollToken), h.enrollHash) {
			return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeUnauthorized, Message: "invalid enroll token"}
		}
		tok := agentlink.NewToken(agentlink.MachineTokenPrefix)
		m := fakeMachine{id: "machine-" + hello.Fingerprint[:8], fingerprint: hello.Fingerprint}
		h.machines[string(agentlink.HashToken(tok))] = m
		return agentlink.Welcome{MachineID: m.id, MachineToken: tok, Config: h.cfg}, nil
	case auth.MachineToken != "":
		m, ok := h.machines[string(agentlink.HashToken(auth.MachineToken))]
		if !ok {
			return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeUnauthorized, Message: "unknown machine token"}
		}
		if m.fingerprint != hello.Fingerprint {
			return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeFingerprint, Message: "fingerprint changed"}
		}
		return agentlink.Welcome{MachineID: m.id, Config: h.cfg}, nil
	default:
		return agentlink.Welcome{}, &agentlink.Error{Code: agentlink.CodeUnauthorized, Message: "missing credential"}
	}
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{agentlink.Subprotocol}})
	if err != nil {
		return
	}
	c := agentlink.NewConn(ws)
	defer ws.CloseNow() //nolint:errcheck // test cleanup
	if _, _, err := agentlink.ServerHandshake(r.Context(), c, h); err != nil {
		return
	}
	h.mu.Lock()
	h.connections++
	h.conns = append(h.conns, ws)
	h.mu.Unlock()
	for {
		e, err := c.Recv(r.Context())
		if err != nil {
			return
		}
		h.mu.Lock()
		h.envelopes = append(h.envelopes, e)
		switch e.Type {
		case agentlink.TypeUsage:
			// At-least-once delivery can repeat an envelope; the real hub
			// dedupes, so record each id once.
			if !h.seen[e.ID] {
				h.seen[e.ID] = true
				u, err := agentlink.Decode[agentlink.Usage](e)
				if err == nil {
					h.usage = append(h.usage, u.Events...)
				}
			}
		}
		ack := h.ack
		h.mu.Unlock()
		if ack {
			if _, err := c.Send(r.Context(), agentlink.TypeAck, "", agentlink.Ack{ID: e.ID}); err != nil {
				return
			}
		}
	}
}

// closeConnections drops every live connection, forcing a reconnect.
func (h *fakeHub) closeConnections() {
	h.mu.Lock()
	conns := append([]*websocket.Conn(nil), h.conns...)
	h.mu.Unlock()
	for _, c := range conns {
		_ = c.Close(websocket.StatusGoingAway, "test reconnect")
	}
}

// setAck toggles acknowledgements and clears what was recorded so far.
func (h *fakeHub) reset(ack bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.ack = ack
	h.envelopes = nil
	h.usage = nil
	h.seen = map[string]bool{}
}

func (h *fakeHub) usageSnapshot() []agentlink.UsageEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]agentlink.UsageEvent(nil), h.usage...)
}

func (h *fakeHub) envelopeSnapshot() []agentlink.Envelope {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]agentlink.Envelope(nil), h.envelopes...)
}

func (h *fakeHub) connectionCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.connections
}

// fakeHarness returns its events on the first scan and nothing afterwards.
type fakeHarness struct {
	id       string
	accounts []agentlink.DiscoveredAccount
	events   []agentlink.UsageEvent
	reports  []agentlink.MeterReport
	cursors  harness.Cursors
}

func (h *fakeHarness) ID() string { return h.id }

func (h *fakeHarness) Discover(context.Context) ([]agentlink.DiscoveredAccount, error) {
	return h.accounts, nil
}

func (h *fakeHarness) Scan(_ context.Context, cur harness.Cursors) (harness.Batch, error) {
	if len(cur) > 0 {
		return harness.Batch{Cursors: cur}, nil
	}
	cursors := h.cursors
	if cursors == nil {
		cursors = harness.Cursors{"log": {Offset: 1}}
	}
	return harness.Batch{Usage: h.events, Meters: h.reports, Cursors: cursors}, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func testRef(external string) agentlink.AccountRef {
	return agentlink.AccountRef{ProviderID: "claude", ExternalID: external}
}

func testEvent(external, messageID string) agentlink.UsageEvent {
	return agentlink.UsageEvent{
		Account:    testRef(external),
		Source:     "fake",
		MessageID:  messageID,
		OccurredAt: time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC),
		Model:      "claude-opus-4-1",
	}
}

func testEvents() []agentlink.UsageEvent {
	return []agentlink.UsageEvent{
		testEvent("acct-1", "msg-1"),
		testEvent("acct-1", "msg-2"),
		testEvent("acct-1", "msg-3"),
	}
}

func startHub(t *testing.T, h *fakeHub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func enroll(t *testing.T, a *Agent, srv *httptest.Server, hub *fakeHub, token string) {
	t.Helper()
	pub := hub.key.Public().(ed25519.PublicKey)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := a.Enroll(ctx, srv.URL, testOrg, base64.StdEncoding.EncodeToString(pub), token); err != nil {
		t.Fatalf("enroll: %v", err)
	}
}

func startAgent(t *testing.T, a *Agent) (context.CancelFunc, chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	return cancel, done
}

func stopAgent(t *testing.T, cancel context.CancelFunc, done chan error) {
	t.Helper()
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("run: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("agent did not stop")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// distinctUsageIDs returns the set of usage envelope ids received.
func distinctUsageIDs(h *fakeHub) map[string]bool {
	ids := map[string]bool{}
	for _, e := range h.envelopeSnapshot() {
		if e.Type == agentlink.TypeUsage {
			ids[e.ID] = true
		}
	}
	return ids
}

// usageEnvelopeCount counts every usage envelope receipt, repeats included.
func usageEnvelopeCount(h *fakeHub) int {
	n := 0
	for _, e := range h.envelopeSnapshot() {
		if e.Type == agentlink.TypeUsage {
			n++
		}
	}
	return n
}

func TestSpoolSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	enrollToken := agentlink.NewToken(agentlink.EnrollTokenPrefix)
	hub := newFakeHub(t, enrollToken)
	hub.reset(false) // accept the handshake but never ack
	srv := startHub(t, hub)

	fake := &fakeHarness{id: "fake", events: testEvents()}
	a, err := New(dir, []harness.Harness{fake}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, a, srv, hub, enrollToken)

	cancel, done := startAgent(t, a)
	waitFor(t, "the first hub to receive three usage events", func() bool {
		return len(hub.usageSnapshot()) == 3
	})
	ids := distinctUsageIDs(hub)
	if len(ids) != 1 {
		t.Fatalf("usage envelope ids = %v, want one batch", ids)
	}
	var firstID string
	for id := range ids {
		firstID = id
	}
	stopAgent(t, cancel, done)

	// The unacked usage message is still on disk under its original id.
	pending, err := a.spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	var spooledIDs []string
	for _, e := range pending {
		if e.Type == agentlink.TypeUsage {
			spooledIDs = append(spooledIDs, e.ID)
		}
	}
	if len(spooledIDs) != 1 || spooledIDs[0] != firstID {
		t.Fatalf("spooled usage ids = %v, want %s", spooledIDs, firstID)
	}

	// A new agent on the same data dir reconnects to a hub that acks.
	hub.reset(true)
	a2, err := New(dir, []harness.Harness{fake}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	cancel2, done2 := startAgent(t, a2)
	waitFor(t, "the second hub to receive the spooled batch", func() bool {
		return len(hub.usageSnapshot()) == 3
	})
	if got := distinctUsageIDs(hub); len(got) != 1 || !got[firstID] {
		t.Fatalf("resent usage ids = %v, want exactly %s", got, firstID)
	}
	waitFor(t, "the spool to empty", func() bool {
		p, err := a2.spool.Pending()
		return err == nil && len(p) == 0
	})
	stopAgent(t, cancel2, done2)
}

func TestNoDuplicateAfterAck(t *testing.T) {
	dir := t.TempDir()
	enrollToken := agentlink.NewToken(agentlink.EnrollTokenPrefix)
	hub := newFakeHub(t, enrollToken)
	srv := startHub(t, hub)

	fake := &fakeHarness{id: "fake", events: testEvents()}
	a, err := New(dir, []harness.Harness{fake}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	enroll(t, a, srv, hub, enrollToken)

	cancel, done := startAgent(t, a)
	waitFor(t, "three acked usage events", func() bool {
		return len(hub.usageSnapshot()) == 3
	})
	waitFor(t, "the spool to empty", func() bool {
		p, err := a.spool.Pending()
		return err == nil && len(p) == 0
	})
	before := usageEnvelopeCount(hub)

	hub.closeConnections()
	waitFor(t, "the agent to reconnect", func() bool {
		return hub.connectionCount() >= 2
	})
	time.Sleep(300 * time.Millisecond)

	if got := hub.usageSnapshot(); len(got) != 3 {
		t.Fatalf("hub received %d usage events after reconnect, want 3", len(got))
	}
	if got := usageEnvelopeCount(hub); got != before {
		t.Fatalf("hub received %d usage batches after reconnect, want the original %d", got, before)
	}
	stopAgent(t, cancel, done)
}

func TestCursorsAdvanceAfterSpool(t *testing.T) {
	t.Run("saved after a successful spool add", func(t *testing.T) {
		dir := t.TempDir()
		fake := &fakeHarness{id: "fake", events: testEvents(), cursors: harness.Cursors{"log": {Offset: 7}}}
		a, err := New(dir, []harness.Harness{fake}, testLogger())
		if err != nil {
			t.Fatal(err)
		}
		a.scanOnce(context.Background())

		all, err := a.cursors.Load()
		if err != nil {
			t.Fatal(err)
		}
		if got := all["fake"]["log"].Offset; got != 7 {
			t.Fatalf("saved cursor offset = %d, want 7", got)
		}
	})

	t.Run("not saved when the spool add fails", func(t *testing.T) {
		dir := t.TempDir()
		fake := &fakeHarness{id: "fake", events: testEvents(), cursors: harness.Cursors{"log": {Offset: 7}}}
		a, err := New(dir, []harness.Harness{fake}, testLogger())
		if err != nil {
			t.Fatal(err)
		}
		// Replace the spool directory with a regular file: every write
		// into it then fails, on every OS and regardless of privileges.
		spoolDir := filepath.Join(dir, "spool")
		if err := os.Remove(spoolDir); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(spoolDir, nil, fileMode); err != nil {
			t.Fatal(err)
		}

		a.scanOnce(context.Background())

		all, err := a.cursors.Load()
		if err != nil {
			t.Fatal(err)
		}
		if len(all) != 0 {
			t.Fatalf("cursors = %v, want none after a failed spool add", all)
		}
		if _, err := os.Stat(filepath.Join(dir, cursorsFile)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("cursors.json exists after a failed spool add: %v", err)
		}
	})
}

func TestDismissedAccountsDropped(t *testing.T) {
	dir := t.TempDir()
	fake := &fakeHarness{id: "fake", events: []agentlink.UsageEvent{
		testEvent("keep", "msg-keep"),
		testEvent("gone", "msg-gone"),
	}}
	a, err := New(dir, []harness.Harness{fake}, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	a.setConfig(agentlink.AgentConfig{
		Accounts: []agentlink.AccountConfig{
			{Ref: testRef("keep"), Status: "active"},
			{Ref: testRef("gone"), Status: "dismissed"},
		},
		ScanIntervalSeconds: 60,
	})
	a.scanOnce(context.Background())

	pending, err := a.spool.Pending()
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range pending {
		if e.Type != agentlink.TypeUsage {
			continue
		}
		u, err := agentlink.Decode[agentlink.Usage](e)
		if err != nil {
			t.Fatal(err)
		}
		for _, ev := range u.Events {
			got = append(got, ev.MessageID)
		}
	}
	if len(got) != 1 || got[0] != "msg-keep" {
		t.Fatalf("spooled usage events = %v, want only msg-keep", got)
	}
}
