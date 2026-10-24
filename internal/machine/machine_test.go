// SPDX-License-Identifier: SUL-1.0

package machine

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/secrets"
)

type fixture struct {
	svc   *Service
	pool  *pgxpool.Pool
	store *secrets.Builtin
	orgID string
	p     auth.Principal
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	pool := dbtest.New(t)
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
	user, err := db.New(pool).InsertUser(context.Background(), db.InsertUserParams{
		OrgID:        orgID,
		Email:        "admin@example.com",
		Name:         "Admin",
		PasswordHash: "test",
		Role:         string(auth.RoleAdmin),
	})
	if err != nil {
		t.Fatalf("creating test user: %v", err)
	}
	return &fixture{
		svc:   NewService(pool, store, "https://hub.example", log),
		pool:  pool,
		store: store,
		orgID: orgID,
		p: auth.Principal{
			OrgID:   orgID,
			UserID:  user.ID,
			Role:    auth.RoleAdmin,
			Kind:    auth.ActorUser,
			ActorID: user.ID,
			Label:   user.Email,
		},
	}
}

// serveHub runs the hub side of the handshake against the real service.
func serveHub(t *testing.T, svc *Service) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{agentlink.Subprotocol}})
		if err != nil {
			return
		}
		defer ws.CloseNow() //nolint:errcheck // test cleanup
		c := agentlink.NewConn(ws)
		_, _, _ = agentlink.ServerHandshake(r.Context(), c, svc)
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http") + agentlink.Path
}

func dial(t *testing.T, ctx context.Context, url string) *agentlink.Conn {
	t.Helper()
	ws, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{agentlink.Subprotocol}}) //nolint:bodyclose // coder/websocket owns the response body
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ws.CloseNow() })
	return agentlink.NewConn(ws)
}

func testHello(orgID, fingerprint string) agentlink.Hello {
	return agentlink.Hello{
		OrgID:        orgID,
		Fingerprint:  fingerprint,
		Hostname:     "laptop",
		OS:           "darwin",
		Arch:         "arm64",
		AgentVersion: "test",
	}
}

func expectCode(t *testing.T, err error, code string) {
	t.Helper()
	var le *agentlink.Error
	if !errors.As(err, &le) || le.Code != code {
		t.Fatalf("err = %v, want agentlink code %q", err, code)
	}
}

func TestEnrollAndReconnect(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := serveHub(t, f.svc)
	pub, err := f.svc.HubPublicKey(ctx, f.orgID)
	if err != nil {
		t.Fatalf("hub public key: %v", err)
	}
	created, err := f.svc.CreateEnrollToken(ctx, f.p, false, time.Hour)
	if err != nil {
		t.Fatalf("creating enroll token: %v", err)
	}
	fingerprint := agentlink.Fingerprint("machine-uuid-1")

	w, err := agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, fingerprint), agentlink.Auth{EnrollToken: created.Raw})
	if err != nil {
		t.Fatalf("enroll handshake: %v", err)
	}
	if w.MachineID == "" || !strings.HasPrefix(w.MachineToken, agentlink.MachineTokenPrefix) {
		t.Fatalf("welcome = %+v, want machine id and machine token", w)
	}
	if w.Config.ScanIntervalSeconds != 60 {
		t.Errorf("scan interval = %d, want 60", w.Config.ScanIntervalSeconds)
	}

	rows, err := db.New(f.pool).ListMachines(ctx, f.orgID)
	if err != nil {
		t.Fatalf("listing machines: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("machines = %d, want 1", len(rows))
	}
	if rows[0].ID != w.MachineID {
		t.Errorf("machine id = %q, want %q", rows[0].ID, w.MachineID)
	}
	if !bytes.Equal(rows[0].TokenHash, agentlink.HashToken(w.MachineToken)) {
		t.Error("stored token hash does not match the issued machine token")
	}

	w2, err := agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, fingerprint), agentlink.Auth{MachineToken: w.MachineToken})
	if err != nil {
		t.Fatalf("reconnect handshake: %v", err)
	}
	if w2.MachineID != w.MachineID || w2.MachineToken != "" {
		t.Fatalf("reconnect welcome = %+v, want same machine and no new token", w2)
	}

	rows, err = db.New(f.pool).ListMachines(ctx, f.orgID)
	if err != nil {
		t.Fatalf("listing machines after reconnect: %v", err)
	}
	if len(rows) != 1 || !bytes.Equal(rows[0].TokenHash, agentlink.HashToken(w.MachineToken)) {
		t.Error("reconnect created a machine or rotated the token")
	}
}

func TestRejections(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := serveHub(t, f.svc)
	pub, err := f.svc.HubPublicKey(ctx, f.orgID)
	if err != nil {
		t.Fatalf("hub public key: %v", err)
	}

	t.Run("expired enroll token", func(t *testing.T) {
		created, err := f.svc.CreateEnrollToken(ctx, f.p, false, -time.Minute)
		if err != nil {
			t.Fatalf("creating enroll token: %v", err)
		}
		_, err = agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, agentlink.Fingerprint("expired")), agentlink.Auth{EnrollToken: created.Raw})
		expectCode(t, err, agentlink.CodeUnauthorized)
	})

	t.Run("revoked enroll token", func(t *testing.T) {
		created, err := f.svc.CreateEnrollToken(ctx, f.p, false, time.Hour)
		if err != nil {
			t.Fatalf("creating enroll token: %v", err)
		}
		if err := f.svc.RevokeEnrollToken(ctx, f.p, created.Token.ID); err != nil {
			t.Fatalf("revoking enroll token: %v", err)
		}
		_, err = agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, agentlink.Fingerprint("revoked-token")), agentlink.Auth{EnrollToken: created.Raw})
		expectCode(t, err, agentlink.CodeUnauthorized)
	})

	t.Run("revoked machine", func(t *testing.T) {
		created, err := f.svc.CreateEnrollToken(ctx, f.p, false, time.Hour)
		if err != nil {
			t.Fatalf("creating enroll token: %v", err)
		}
		fingerprint := agentlink.Fingerprint("revoked-machine")
		w, err := agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, fingerprint), agentlink.Auth{EnrollToken: created.Raw})
		if err != nil {
			t.Fatalf("enroll handshake: %v", err)
		}
		if _, err := f.svc.RevokeMachine(ctx, f.p, w.MachineID); err != nil {
			t.Fatalf("revoking machine: %v", err)
		}
		_, err = agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, fingerprint), agentlink.Auth{MachineToken: w.MachineToken})
		expectCode(t, err, agentlink.CodeRevoked)
	})

	t.Run("different fingerprint", func(t *testing.T) {
		created, err := f.svc.CreateEnrollToken(ctx, f.p, false, time.Hour)
		if err != nil {
			t.Fatalf("creating enroll token: %v", err)
		}
		w, err := agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, agentlink.Fingerprint("fingerprint-a")), agentlink.Auth{EnrollToken: created.Raw})
		if err != nil {
			t.Fatalf("enroll handshake: %v", err)
		}
		_, err = agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, testHello(f.orgID, agentlink.Fingerprint("fingerprint-b")), agentlink.Auth{MachineToken: w.MachineToken})
		expectCode(t, err, agentlink.CodeFingerprint)
	})

	t.Run("unknown org", func(t *testing.T) {
		created, err := f.svc.CreateEnrollToken(ctx, f.p, false, time.Hour)
		if err != nil {
			t.Fatalf("creating enroll token: %v", err)
		}
		hello := testHello("00000000-0000-0000-0000-000000000000", agentlink.Fingerprint("unknown-org"))
		_, err = agentlink.ClientHandshake(ctx, dial(t, ctx, url), pub, hello, agentlink.Auth{EnrollToken: created.Raw})
		expectCode(t, err, agentlink.CodeUnauthorized)
	})
}

func TestHubKeyStable(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pub1, err := f.svc.HubPublicKey(ctx, f.orgID)
	if err != nil {
		t.Fatalf("first hub public key: %v", err)
	}
	pub2, err := f.svc.HubPublicKey(ctx, f.orgID)
	if err != nil {
		t.Fatalf("second hub public key: %v", err)
	}
	if !bytes.Equal(pub1, pub2) {
		t.Fatal("two calls returned different hub public keys")
	}

	row, err := db.New(f.pool).GetHubKey(ctx, f.orgID)
	if err != nil {
		t.Fatalf("loading hub key: %v", err)
	}
	if !bytes.Equal(row.PublicKey, pub1) {
		t.Error("stored public key differs from HubPublicKey")
	}
	if len(row.PublicKey) != ed25519.PublicKeySize {
		t.Errorf("public key length = %d, want %d", len(row.PublicKey), ed25519.PublicKeySize)
	}

	encodedSeed, err := f.store.Reveal(ctx, f.orgID, row.SecretID)
	if err != nil {
		t.Fatalf("revealing hub key seed: %v", err)
	}
	seed, err := base64.StdEncoding.DecodeString(encodedSeed)
	if err != nil {
		t.Fatalf("decoding seed: %v", err)
	}
	if len(seed) != ed25519.SeedSize {
		t.Fatalf("seed length = %d, want %d", len(seed), ed25519.SeedSize)
	}
	if bytes.Equal(seed, row.PublicKey) {
		t.Error("hub_keys.public_key holds the private seed")
	}

	priv, err := f.svc.SigningKey(ctx, f.orgID)
	if err != nil {
		t.Fatalf("signing key: %v", err)
	}
	if !bytes.Equal(priv.Public().(ed25519.PublicKey), pub1) {
		t.Error("SigningKey public half differs from HubPublicKey")
	}

	var count int
	if err := f.pool.QueryRow(ctx, "SELECT count(*) FROM hub_keys WHERE org_id = $1", f.orgID).Scan(&count); err != nil {
		t.Fatalf("counting hub keys: %v", err)
	}
	if count != 1 {
		t.Errorf("hub_keys rows = %d, want 1", count)
	}
}
