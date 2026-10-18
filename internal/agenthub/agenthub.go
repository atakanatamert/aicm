// SPDX-License-Identifier: AGPL-3.0-or-later

// Package agenthub terminates the agent WebSocket, runs the signed handshake
// and ingests what enrolled machines push: discovered accounts, usage and
// meter readings. It also owns the review actions and the live-connection
// registry the Machines page reads.
package agenthub

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/machine"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/quota"
	"github.com/atakanatamert/aicm/internal/usage"
)

// Errors returned by the review actions.
var (
	ErrNotFound     = errors.New("account not found")
	ErrNotPending   = errors.New("account is not pending")
	ErrAgentOffline = errors.New("no connected machine can provide this key")
)

// pingInterval and pingTimeout are the liveness probe cadence.
const (
	pingInterval    = 30 * time.Second
	pingTimeout     = 10 * time.Second
	secretTimeout   = 15 * time.Second
	scanInterval    = 60
	lastSeenRefresh = time.Minute
)

// Hub is the live agent registry and the ingest entry point.
type Hub struct {
	pool      *pgxpool.Pool
	q         *db.Queries
	machines  *machine.Service
	usage     *usage.Service
	quota     *quota.Service
	accounts  *account.Service
	providers []provider.Provider
	log       *slog.Logger

	mu   sync.RWMutex
	live map[string]*session
}

// New builds the hub. pool is used for multi-statement ingest transactions;
// queries are built from it here.
func New(pool *pgxpool.Pool, log *slog.Logger, machines *machine.Service, usageSvc *usage.Service, quotaSvc *quota.Service, accounts *account.Service, providers []provider.Provider) *Hub {
	return &Hub{
		pool:      pool,
		q:         db.New(pool),
		machines:  machines,
		usage:     usageSvc,
		quota:     quotaSvc,
		accounts:  accounts,
		providers: providers,
		log:       log,
		live:      map[string]*session{},
	}
}

// session is one live agent connection.
type session struct {
	hub         *Hub
	conn        *agentlink.Conn
	ws          *websocket.Conn
	orgID       string
	machineID   string
	machineName string

	mu        sync.Mutex
	harnesses []string
	lastSeen  time.Time
	pending   map[string]chan agentlink.SecretReply

	done chan struct{}
	once sync.Once
}

func newSession(h *Hub, conn *agentlink.Conn, orgID, machineID, machineName string) *session {
	return &session{
		hub:         h,
		conn:        conn,
		ws:          conn.WebSocket(),
		orgID:       orgID,
		machineID:   machineID,
		machineName: machineName,
		harnesses:   []string{},
		pending:     map[string]chan agentlink.SecretReply{},
		done:        make(chan struct{}),
	}
}

// ServeHTTP accepts one agent connection, runs the handshake and then the
// message loop. The endpoint is mounted outside the generated router; the
// agent authenticates with the agentlink handshake, never a session.
func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !hasSubprotocol(r.Header.Get("Sec-WebSocket-Protocol"), agentlink.Subprotocol) {
		writeJSONError(w, http.StatusBadRequest, "bad_request", "subprotocol "+agentlink.Subprotocol+" is required")
		return
	}
	ws, err := websocket.Accept(w, r, &websocket.AcceptOptions{
		Subprotocols: []string{agentlink.Subprotocol},
	})
	if err != nil {
		h.log.WarnContext(r.Context(), "accepting agent websocket", "err", err)
		return
	}
	defer ws.CloseNow() //nolint:errcheck // best-effort cleanup

	conn := agentlink.NewConn(ws)
	hello, welcome, err := agentlink.ServerHandshake(r.Context(), conn, handshakeHub{h: h})
	if err != nil {
		h.log.WarnContext(r.Context(), "agent handshake failed", "err", err)
		return
	}

	name := hello.Hostname
	if name == "" {
		name = welcome.MachineID
	}
	s := newSession(h, conn, hello.OrgID, welcome.MachineID, name)
	h.register(s)
	defer h.unregister(s)
	h.log.Info("agent connected", "machine_id", s.machineID, "org_id", s.orgID, "hostname", hello.Hostname)
	s.run(r.Context())
}

// handshakeHub fills the Welcome config from the hub before it is sent.
type handshakeHub struct{ h *Hub }

func (x handshakeHub) SigningKey(ctx context.Context, orgID string) (ed25519.PrivateKey, error) {
	return x.h.machines.SigningKey(ctx, orgID)
}

func (x handshakeHub) Authenticate(ctx context.Context, hello agentlink.Hello, a agentlink.Auth) (agentlink.Welcome, error) {
	w, err := x.h.machines.Authenticate(ctx, hello, a)
	if err != nil {
		return w, err
	}
	cfg, err := x.h.Config(ctx, hello.OrgID)
	if err != nil {
		return agentlink.Welcome{}, fmt.Errorf("building agent config: %w", err)
	}
	w.Config = cfg
	return w, nil
}

// register publishes s and replaces (closes) an older connection for the
// same machine.
func (h *Hub) register(s *session) {
	h.mu.Lock()
	old := h.live[s.machineID]
	h.live[s.machineID] = s
	h.mu.Unlock()
	if old != nil && old != s {
		old.close(websocket.StatusGoingAway, "replaced by a newer connection")
	}
}

// unregister removes s when it is still the registered connection.
func (h *Hub) unregister(s *session) {
	h.mu.Lock()
	if h.live[s.machineID] == s {
		delete(h.live, s.machineID)
	}
	h.mu.Unlock()
	s.stop()
	h.log.Info("agent disconnected", "machine_id", s.machineID, "org_id", s.orgID)
}

// run reads messages until the connection closes. Every message is acked
// after its handler committed (or rejected) it.
func (s *session) run(ctx context.Context) {
	pingCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	go s.pingLoop(pingCtx)

	for {
		e, err := s.conn.Recv(ctx)
		if err != nil {
			return
		}
		s.touch(ctx)
		ackErr := s.hub.handle(ctx, s, e)
		if _, err := s.conn.Send(ctx, agentlink.TypeAck, "", agentlink.Ack{ID: e.ID, Error: ackErr}); err != nil {
			return
		}
	}
}

// pingLoop probes the agent every 30 s and drops the connection on failure.
func (s *session) pingLoop(ctx context.Context) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, pingTimeout)
			err := s.ws.Ping(pingCtx)
			cancel()
			if err != nil {
				s.close(websocket.StatusGoingAway, "ping failed")
				return
			}
		}
	}
}

// touch refreshes machines.last_seen_at at most once a minute.
func (s *session) touch(ctx context.Context) {
	s.mu.Lock()
	if time.Since(s.lastSeen) < lastSeenRefresh {
		s.mu.Unlock()
		return
	}
	s.lastSeen = time.Now()
	s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.hub.q.TouchMachineSeen(ctx, db.TouchMachineSeenParams{OrgID: s.orgID, ID: s.machineID}); err != nil {
		s.hub.log.WarnContext(ctx, "recording agent last seen", "machine_id", s.machineID, "err", err)
	}
}

// setHarnesses keeps the last status message's harness ids in memory.
func (s *session) setHarnesses(ids []string) {
	s.mu.Lock()
	s.harnesses = append([]string(nil), ids...)
	s.mu.Unlock()
}

// harnessesCopy returns the last status message's harness ids.
func (s *session) harnessesCopy() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.harnesses))
	copy(out, s.harnesses)
	return out
}

func (s *session) addPending(id string, ch chan agentlink.SecretReply) {
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()
}

func (s *session) removePending(id string) {
	s.mu.Lock()
	delete(s.pending, id)
	s.mu.Unlock()
}

func (s *session) deliver(reply agentlink.SecretReply) {
	s.mu.Lock()
	ch, ok := s.pending[reply.RequestID]
	if ok {
		delete(s.pending, reply.RequestID)
	}
	s.mu.Unlock()
	if !ok {
		return
	}
	select {
	case ch <- reply:
	default:
	}
}

// stop signals waiters that this connection is gone.
func (s *session) stop() {
	s.once.Do(func() { close(s.done) })
}

// close stops the session and closes the WebSocket with code.
func (s *session) close(code websocket.StatusCode, reason string) {
	s.stop()
	_ = s.ws.Close(code, reason)
}

// Shutdown closes every live connection with StatusGoingAway.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	sessions := make([]*session, 0, len(h.live))
	for _, s := range h.live {
		sessions = append(sessions, s)
	}
	h.live = map[string]*session{}
	h.mu.Unlock()

	var wg sync.WaitGroup
	for _, s := range sessions {
		wg.Add(1)
		go func(s *session) {
			defer wg.Done()
			s.close(websocket.StatusGoingAway, "hub shutting down")
		}(s)
	}
	wg.Wait()
}

// Online reports whether the machine has a live connection.
func (h *Hub) Online(machineID string) bool {
	h.mu.RLock()
	_, ok := h.live[machineID]
	h.mu.RUnlock()
	return ok
}

// Harnesses returns the harness ids from the machine's last status message.
func (h *Hub) Harnesses(machineID string) []string {
	h.mu.RLock()
	s := h.live[machineID]
	h.mu.RUnlock()
	if s == nil {
		return []string{}
	}
	return s.harnessesCopy()
}

// Config builds the config sent to every agent of an org.
func (h *Hub) Config(ctx context.Context, orgID string) (agentlink.AgentConfig, error) {
	rows, err := h.q.ListAgentAccounts(ctx, orgID)
	if err != nil {
		return agentlink.AgentConfig{}, fmt.Errorf("listing agent accounts: %w", err)
	}
	cfg := agentlink.AgentConfig{
		Accounts:            make([]agentlink.AccountConfig, 0, len(rows)),
		ScanIntervalSeconds: scanInterval,
	}
	for _, a := range rows {
		if a.ExternalID == nil {
			continue
		}
		cfg.Accounts = append(cfg.Accounts, agentlink.AccountConfig{
			Ref: agentlink.AccountRef{
				ProviderID: a.ProviderID,
				ExternalID: *a.ExternalID,
			},
			Status:          a.Status,
			AllowUnofficial: a.AllowUnofficial,
		})
	}
	return cfg, nil
}

// PushConfig sends TypeConfig to every live machine of the org.
func (h *Hub) PushConfig(ctx context.Context, orgID string) {
	cfg, err := h.Config(ctx, orgID)
	if err != nil {
		h.log.WarnContext(ctx, "building config to push", "org_id", orgID, "err", err)
		return
	}
	h.mu.RLock()
	sessions := make([]*session, 0, len(h.live))
	for _, s := range h.live {
		if s.orgID == orgID {
			sessions = append(sessions, s)
		}
	}
	h.mu.RUnlock()

	for _, s := range sessions {
		go func(s *session) {
			sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			defer cancel()
			if _, err := s.conn.Send(sendCtx, agentlink.TypeConfig, "", cfg); err != nil {
				h.log.WarnContext(sendCtx, "pushing agent config", "machine_id", s.machineID, "err", err)
			}
		}(s)
	}
}

// Confirm moves a pending account to active, optionally renaming it. With
// importKey it asks a connected machine that reported the key for it.
func (h *Hub) Confirm(ctx context.Context, p auth.Principal, accountID string, name *string, importKey bool) (db.Account, error) {
	a, err := h.accounts.Get(ctx, p, accountID)
	switch {
	case errors.Is(err, account.ErrNotFound):
		return db.Account{}, ErrNotFound
	case err != nil:
		return db.Account{}, err
	}
	if a.Status != "pending" {
		return db.Account{}, ErrNotPending
	}

	var trimmed *string
	if name != nil {
		n := strings.TrimSpace(*name)
		if n == "" {
			return db.Account{}, fmt.Errorf("%w: name is required", account.ErrInvalid)
		}
		trimmed = &n
	}

	if importKey {
		if a.ExternalID == nil {
			return db.Account{}, ErrAgentOffline
		}
		ref := agentlink.AccountRef{ProviderID: a.ProviderID, ExternalID: *a.ExternalID}
		value, err := h.requestKey(ctx, p.OrgID, accountID, ref)
		if err != nil {
			return db.Account{}, err
		}
		if _, err := h.accounts.SetSecret(ctx, p, accountID, value, true); err != nil {
			return db.Account{}, err
		}
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return db.Account{}, fmt.Errorf("starting account confirm: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := h.q.WithTx(tx)

	updated, err := q.ConfirmAccount(ctx, db.ConfirmAccountParams{
		Name:  trimmed,
		OrgID: p.OrgID,
		ID:    accountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Account{}, ErrNotPending
	}
	if err != nil {
		return db.Account{}, fmt.Errorf("confirming account: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "account.confirm",
		TargetKind: "account",
		TargetID:   updated.ID,
		Before:     map[string]any{"status": "pending"},
		After:      map[string]any{"status": updated.Status, "name": updated.Name},
	}); err != nil {
		return db.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Account{}, fmt.Errorf("committing account confirm: %w", err)
	}

	h.PushConfig(ctx, p.OrgID)
	return updated, nil
}

// Dismiss marks a pending account dismissed and tells agents to stop sending
// its data.
func (h *Hub) Dismiss(ctx context.Context, p auth.Principal, accountID string) (db.Account, error) {
	a, err := h.accounts.Get(ctx, p, accountID)
	switch {
	case errors.Is(err, account.ErrNotFound):
		return db.Account{}, ErrNotFound
	case err != nil:
		return db.Account{}, err
	}
	if a.Status != "pending" {
		return db.Account{}, ErrNotPending
	}

	tx, err := h.pool.Begin(ctx)
	if err != nil {
		return db.Account{}, fmt.Errorf("starting account dismiss: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := h.q.WithTx(tx)

	updated, err := q.DismissAccount(ctx, db.DismissAccountParams{OrgID: p.OrgID, ID: accountID})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Account{}, ErrNotPending
	}
	if err != nil {
		return db.Account{}, fmt.Errorf("dismissing account: %w", err)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "account.dismiss",
		TargetKind: "account",
		TargetID:   updated.ID,
		Before:     map[string]any{"status": "pending"},
		After:      map[string]any{"status": updated.Status},
	}); err != nil {
		return db.Account{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return db.Account{}, fmt.Errorf("committing account dismiss: %w", err)
	}

	h.PushConfig(ctx, p.OrgID)
	return updated, nil
}

// requestKey asks one connected machine that reported has_api_key for the
// account's plain API key.
func (h *Hub) requestKey(ctx context.Context, orgID, accountID string, ref agentlink.AccountRef) (string, error) {
	rows, err := h.q.ListAccountKeyMachines(ctx, db.ListAccountKeyMachinesParams{OrgID: orgID, AccountID: accountID})
	if err != nil {
		return "", fmt.Errorf("finding a machine with the key: %w", err)
	}

	h.mu.RLock()
	var s *session
	for _, machineID := range rows {
		if c := h.live[machineID]; c != nil && c.orgID == orgID {
			s = c
			break
		}
	}
	h.mu.RUnlock()
	if s == nil {
		return "", ErrAgentOffline
	}

	reqID := agentlink.NewID()
	ch := make(chan agentlink.SecretReply, 1)
	s.addPending(reqID, ch)
	defer s.removePending(reqID)

	if _, err := s.conn.Send(ctx, agentlink.TypeSecretRequest, reqID, agentlink.SecretRequest{RequestID: reqID, Account: ref}); err != nil {
		return "", fmt.Errorf("requesting the key: %w", err)
	}

	timer := time.NewTimer(secretTimeout)
	defer timer.Stop()
	select {
	case reply := <-ch:
		if reply.Error != "" {
			return "", errors.New(reply.Error)
		}
		if reply.Value == "" {
			return "", errors.New("agent returned an empty key")
		}
		return reply.Value, nil
	case <-timer.C:
		return "", fmt.Errorf("%w: timed out waiting for the agent", ErrAgentOffline)
	case <-s.done:
		return "", ErrAgentOffline
	case <-ctx.Done():
		return "", fmt.Errorf("waiting for the agent key: %w", ctx.Err())
	}
}

// handle dispatches one data message and returns the ack error, if any.
func (h *Hub) handle(ctx context.Context, s *session, e agentlink.Envelope) string {
	switch e.Type {
	case agentlink.TypeAccounts:
		return h.ingestAccounts(ctx, s, e)
	case agentlink.TypeUsage:
		return h.ingestUsage(ctx, s, e)
	case agentlink.TypeMeters:
		return h.ingestMeters(ctx, s, e)
	case agentlink.TypeStatus:
		return h.ingestStatus(ctx, s, e)
	case agentlink.TypeSecretReply:
		reply, err := agentlink.Decode[agentlink.SecretReply](e)
		if err != nil {
			return err.Error()
		}
		s.deliver(reply)
		return ""
	default:
		return "unknown type"
	}
}

// ingestStatus keeps the harness list for the Machines page.
func (h *Hub) ingestStatus(_ context.Context, s *session, e agentlink.Envelope) string {
	st, err := agentlink.Decode[agentlink.Status](e)
	if err != nil {
		return err.Error()
	}
	ids := make([]string, 0, len(st.Harnesses))
	for _, hs := range st.Harnesses {
		if hs.ID != "" {
			ids = append(ids, hs.ID)
		}
	}
	s.setHarnesses(ids)
	return ""
}

// hasSubprotocol reports whether header lists want.
func hasSubprotocol(header, want string) bool {
	for _, part := range strings.Split(header, ",") {
		if strings.TrimSpace(part) == want {
			return true
		}
	}
	return false
}

// writeJSONError writes the hub's standard error envelope.
func writeJSONError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}{Error: struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}{Code: code, Message: message}})
}
