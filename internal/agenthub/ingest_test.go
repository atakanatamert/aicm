// SPDX-License-Identifier: AGPL-3.0-or-later

package agenthub

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/machine"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/pricing"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/registry"
	"github.com/atakanatamert/aicm/internal/quota"
	"github.com/atakanatamert/aicm/internal/secrets"
	"github.com/atakanatamert/aicm/internal/usage"
)

type hubFixture struct {
	hub    *Hub
	pool   *pgxpool.Pool
	orgID  string
	p      auth.Principal
	srv    *httptest.Server
	pub    ed25519.PublicKey
	enroll string
}

func newHubFixture(t *testing.T) *hubFixture {
	t.Helper()
	pool := dbtest.New(t)
	ctx := context.Background()
	orgID := dbtest.DefaultOrgID(t, pool)

	var master [32]byte
	if _, err := rand.Read(master[:]); err != nil {
		t.Fatalf("generating master key: %v", err)
	}
	store, err := secrets.NewBuiltin(pool, master[:])
	if err != nil {
		t.Fatalf("building secret store: %v", err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	httpc := provider.NewHTTPClient(log, nil)
	providers := registry.All(httpc)
	prices := pricing.NewService(pool, httpc, log)
	catalog := func(id string) (provider.Info, bool) {
		p, ok := registry.ByID(providers, id)
		if !ok {
			return provider.Info{}, false
		}
		return p.Info(), true
	}
	usageSvc := usage.NewService(pool, prices, log)
	quotaSvc := quota.NewService(pool, log, catalog, quota.PriceFromTable(prices))
	accounts := account.NewService(pool, store, providers, log)
	machines := machine.NewService(pool, store, "https://hub.example", log)
	hub := New(pool, log, machines, usageSvc, quotaSvc, accounts, providers)
	srv := httptest.NewServer(hub)
	t.Cleanup(srv.Close)

	pub, err := machines.HubPublicKey(ctx, orgID)
	if err != nil {
		t.Fatalf("loading hub public key: %v", err)
	}
	user, err := db.New(pool).InsertUser(ctx, db.InsertUserParams{
		OrgID:        orgID,
		Email:        "admin@example.com",
		Name:         "Admin",
		PasswordHash: "test",
		Role:         string(auth.RoleAdmin),
	})
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	p := auth.Principal{
		OrgID:   orgID,
		UserID:  user.ID,
		Role:    auth.RoleAdmin,
		Kind:    auth.ActorUser,
		ActorID: user.ID,
		Label:   user.Email,
	}
	created, err := machines.CreateEnrollToken(ctx, p, false, time.Hour)
	if err != nil {
		t.Fatalf("creating enroll token: %v", err)
	}
	return &hubFixture{hub: hub, pool: pool, orgID: orgID, p: p, srv: srv, pub: pub, enroll: created.Raw}
}

func (f *hubFixture) connect(t *testing.T, hostname, fingerprint string) (*agentlink.Conn, agentlink.Welcome) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(f.srv.URL, "http") + agentlink.Path
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{agentlink.Subprotocol}}) //nolint:bodyclose // coder/websocket owns the response body
	if err != nil {
		t.Fatalf("dialing hub: %v", err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	c := agentlink.NewConn(ws)
	w, err := agentlink.ClientHandshake(ctx, c, f.pub, agentlink.Hello{
		OrgID:        f.orgID,
		Fingerprint:  agentlink.Fingerprint(fingerprint),
		Hostname:     hostname,
		OS:           "darwin",
		Arch:         "arm64",
		AgentVersion: "test",
	}, agentlink.Auth{EnrollToken: f.enroll})
	if err != nil {
		t.Fatalf("agent handshake: %v", err)
	}
	return c, w
}

// sendAck sends one message and returns its ack, skipping pushed configs.
func sendAck(t *testing.T, c *agentlink.Conn, typ agentlink.Type, id string, payload any) agentlink.Ack {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Send(ctx, typ, id, payload); err != nil {
		t.Fatalf("sending %s: %v", typ, err)
	}
	for {
		e, err := c.Recv(ctx)
		if err != nil {
			t.Fatalf("receiving ack for %s: %v", typ, err)
		}
		if e.Type == agentlink.TypeConfig {
			continue
		}
		if e.Type != agentlink.TypeAck {
			t.Fatalf("received %s, want ack", e.Type)
		}
		ack, err := agentlink.Decode[agentlink.Ack](e)
		if err != nil {
			t.Fatalf("decoding ack: %v", err)
		}
		if ack.ID != id {
			t.Fatalf("ack id = %q, want %q", ack.ID, id)
		}
		return ack
	}
}

func recvConfig(t *testing.T, c *agentlink.Conn) agentlink.AgentConfig {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		e, err := c.Recv(ctx)
		if err != nil {
			t.Fatalf("receiving config: %v", err)
		}
		if e.Type != agentlink.TypeConfig {
			continue
		}
		cfg, err := agentlink.Decode[agentlink.AgentConfig](e)
		if err != nil {
			t.Fatalf("decoding config: %v", err)
		}
		return cfg
	}
}

func (f *hubFixture) accountCount(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	if err := f.pool.QueryRow(context.Background(), where, args...).Scan(&n); err != nil {
		t.Fatalf("querying count: %v", err)
	}
	return n
}

func TestSameAccountTwoMachines(t *testing.T) {
	f := newHubFixture(t)
	c1, _ := f.connect(t, "laptop-1", "fingerprint-1")
	c2, _ := f.connect(t, "laptop-2", "fingerprint-2")

	ref := agentlink.AccountRef{ProviderID: "claude", ExternalID: "account-uuid-1"}
	snapshot := agentlink.Accounts{Accounts: []agentlink.DiscoveredAccount{{
		Ref:       ref,
		Harness:   "claudecode",
		Label:     "me@example.com",
		Plan:      "max",
		AuthKind:  "agent_capture",
		HasAPIKey: true,
		Details:   map[string]string{"tier": "default"},
	}}}
	for i, c := range []*agentlink.Conn{c1, c2} {
		ack := sendAck(t, c, agentlink.TypeAccounts, fmt.Sprintf("snap-%d", i), snapshot)
		if ack.Error != "" {
			t.Fatalf("accounts ack error: %s", ack.Error)
		}
	}

	if n := f.accountCount(t, `SELECT count(*) FROM accounts WHERE org_id = $1`, f.orgID); n != 1 {
		t.Fatalf("accounts = %d, want 1", n)
	}
	if n := f.accountCount(t, `SELECT count(*) FROM account_machines WHERE org_id = $1`, f.orgID); n != 2 {
		t.Fatalf("account_machines = %d, want 2", n)
	}
	var status string
	if err := f.pool.QueryRow(context.Background(), `SELECT status FROM accounts WHERE org_id = $1`, f.orgID).Scan(&status); err != nil {
		t.Fatalf("reading status: %v", err)
	}
	if status != "pending" {
		t.Errorf("status = %q, want pending", status)
	}
}

func TestUsageAndMetersAcked(t *testing.T) {
	f := newHubFixture(t)
	c, w := f.connect(t, "laptop", "fingerprint-usage")

	ref := agentlink.AccountRef{ProviderID: "claude", ExternalID: "account-uuid-usage"}
	at := time.Now().UTC().Truncate(time.Second)
	batch := agentlink.Usage{Events: []agentlink.UsageEvent{{
		Account:      ref,
		Source:       "claudecode",
		MessageID:    "message-1",
		OccurredAt:   at,
		Model:        "claude-sonnet-4-5",
		InputTokens:  100,
		OutputTokens: 40,
	}}}
	if ack := sendAck(t, c, agentlink.TypeUsage, "usage-1", batch); ack.Error != "" {
		t.Fatalf("usage ack error: %s", ack.Error)
	}

	used := 12.5
	def := meter.Def{Key: "five_hour", Label: "5-hour limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 300}
	reports := agentlink.Meters{Reports: []agentlink.MeterReport{{
		Account: ref,
		Def:     def,
		Reading: meter.Reading{Key: def.Key, Used: &used, CapturedAt: at, Source: meter.SourceStatusline},
	}}}
	if ack := sendAck(t, c, agentlink.TypeMeters, "meters-1", reports); ack.Error != "" {
		t.Fatalf("meters ack error: %s", ack.Error)
	}

	if n := f.accountCount(t, `SELECT count(*) FROM usage_events WHERE org_id = $1`, f.orgID); n != 1 {
		t.Fatalf("usage_events = %d, want 1", n)
	}
	var machineID string
	if err := f.pool.QueryRow(context.Background(), `SELECT machine_id::text FROM quota_readings WHERE org_id = $1`, f.orgID).Scan(&machineID); err != nil {
		t.Fatalf("reading quota_readings: %v", err)
	}
	if machineID != w.MachineID {
		t.Errorf("reading machine_id = %q, want %q", machineID, w.MachineID)
	}

	// At-least-once redelivery: same natural keys, nothing stored twice.
	if ack := sendAck(t, c, agentlink.TypeUsage, "usage-2", batch); ack.Error != "" {
		t.Fatalf("redelivered usage ack error: %s", ack.Error)
	}
	if ack := sendAck(t, c, agentlink.TypeMeters, "meters-2", reports); ack.Error != "" {
		t.Fatalf("redelivered meters ack error: %s", ack.Error)
	}
	if n := f.accountCount(t, `SELECT count(*) FROM usage_events WHERE org_id = $1`, f.orgID); n != 1 {
		t.Errorf("usage_events after redelivery = %d, want 1", n)
	}
	if n := f.accountCount(t, `SELECT count(*) FROM quota_readings WHERE org_id = $1`, f.orgID); n != 1 {
		t.Errorf("quota_readings after redelivery = %d, want 1", n)
	}
}

func TestDismissedDropsData(t *testing.T) {
	f := newHubFixture(t)
	c, _ := f.connect(t, "laptop", "fingerprint-dismiss")

	ref := agentlink.AccountRef{ProviderID: "claude", ExternalID: "account-uuid-dismiss"}
	snapshot := agentlink.Accounts{Accounts: []agentlink.DiscoveredAccount{{
		Ref:      ref,
		Harness:  "claudecode",
		Label:    "dismiss@example.com",
		AuthKind: "agent_capture",
	}}}
	if ack := sendAck(t, c, agentlink.TypeAccounts, "snap", snapshot); ack.Error != "" {
		t.Fatalf("accounts ack error: %s", ack.Error)
	}

	var accountID string
	if err := f.pool.QueryRow(context.Background(), `SELECT id FROM accounts WHERE org_id = $1`, f.orgID).Scan(&accountID); err != nil {
		t.Fatalf("reading account: %v", err)
	}
	if _, err := f.hub.Dismiss(context.Background(), f.p, accountID); err != nil {
		t.Fatalf("dismissing account: %v", err)
	}

	cfg := recvConfig(t, c)
	found := false
	for _, a := range cfg.Accounts {
		if a.Ref == ref {
			found = true
			if a.Status != "dismissed" {
				t.Errorf("config status = %q, want dismissed", a.Status)
			}
		}
	}
	if !found {
		t.Fatalf("config has no entry for %+v: %+v", ref, cfg.Accounts)
	}

	batch := agentlink.Usage{Events: []agentlink.UsageEvent{{
		Account:     ref,
		Source:      "claudecode",
		MessageID:   "after-dismiss",
		OccurredAt:  time.Now().UTC().Truncate(time.Second),
		Model:       "claude-sonnet-4-5",
		InputTokens: 10,
	}}}
	if ack := sendAck(t, c, agentlink.TypeUsage, "usage-after-dismiss", batch); ack.Error != "" {
		t.Fatalf("usage ack error: %s", ack.Error)
	}
	if n := f.accountCount(t, `SELECT count(*) FROM usage_events WHERE org_id = $1`, f.orgID); n != 0 {
		t.Errorf("usage_events for dismissed account = %d, want 0", n)
	}
}

func TestOversizedBatch(t *testing.T) {
	f := newHubFixture(t)
	c, _ := f.connect(t, "laptop", "fingerprint-oversized")

	ref := agentlink.AccountRef{ProviderID: "claude", ExternalID: "account-uuid-oversized"}
	at := time.Now().UTC().Truncate(time.Second)
	events := make([]agentlink.UsageEvent, agentlink.MaxBatchItems+1)
	for i := range events {
		events[i] = agentlink.UsageEvent{
			Account:    ref,
			Source:     "claudecode",
			MessageID:  fmt.Sprintf("message-%d", i),
			OccurredAt: at,
			Model:      "claude-sonnet-4-5",
		}
	}
	ack := sendAck(t, c, agentlink.TypeUsage, "oversized", agentlink.Usage{Events: events})
	if ack.Error == "" {
		t.Fatal("oversized batch was acked without an error")
	}
	if n := f.accountCount(t, `SELECT count(*) FROM usage_events WHERE org_id = $1`, f.orgID); n != 0 {
		t.Errorf("usage_events = %d, want 0", n)
	}
}
