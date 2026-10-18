// SPDX-License-Identifier: Apache-2.0

package agent

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/harness"
	"github.com/atakanatamert/aicm/internal/version"
)

const (
	defaultScanInterval = 60 * time.Second
	discoveryInterval   = 10 * time.Minute
	statusInterval      = 5 * time.Minute
	defaultPollInterval = 15 * time.Minute
	maxPollInterval     = time.Hour

	backoffMin       = time.Second
	backoffMax       = 5 * time.Minute
	backoffResetTime = 60 * time.Second

	accountDismissed = "dismissed"
	authKindAPIKey   = "api_key"
)

// updateInterval is how often Run checks for a new release.
const updateInterval = 24 * time.Hour

// Agent owns one machine's link to the hub.
type Agent struct {
	dataDir   string
	harnesses []harness.Harness
	spool     *Spool
	cursors   *Cursors
	log       *slog.Logger

	cfg   Config
	token string

	mu         sync.Mutex
	conn       *agentlink.Conn
	current    agentlink.AgentConfig
	discovered map[string]discoveredAccount

	flushMu sync.Mutex

	// Scan scheduling is owned by the scan goroutine.
	nextDiscovery time.Time
	nextStatus    time.Time
	nextPoll      map[string]time.Time
	pollInterval  map[string]time.Duration
	lastAccounts  string
	status        map[string]agentlink.HarnessStatus

	updated atomic.Bool
}

// discoveredAccount records which harness saw an account and whether it can
// answer a secret request for it.
type discoveredAccount struct {
	harness   string
	hasAPIKey bool
}

// New opens the agent state in dataDir. hs may be empty; harness importers
// are added by later tasks.
func New(dataDir string, hs []harness.Harness, log *slog.Logger) (*Agent, error) {
	if dataDir == "" {
		return nil, errors.New("agent data dir is required")
	}
	if log == nil {
		log = slog.Default()
	}
	if err := os.MkdirAll(dataDir, dirMode); err != nil {
		return nil, fmt.Errorf("creating agent data dir: %w", err)
	}
	spool, err := OpenSpool(filepath.Join(dataDir, "spool"))
	if err != nil {
		return nil, err
	}
	a := &Agent{
		dataDir:      dataDir,
		harnesses:    hs,
		spool:        spool,
		cursors:      OpenCursors(dataDir),
		log:          log,
		discovered:   map[string]discoveredAccount{},
		nextStatus:   time.Now().Add(statusInterval),
		nextPoll:     map[string]time.Time{},
		pollInterval: map[string]time.Duration{},
		status:       map[string]agentlink.HarnessStatus{},
		current: agentlink.AgentConfig{
			Accounts:            []agentlink.AccountConfig{},
			ScanIntervalSeconds: int(defaultScanInterval / time.Second),
		},
	}
	cfg, err := Load(dataDir)
	switch {
	case err == nil:
		a.cfg = cfg
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	token, err := loadMachineToken(dataDir)
	switch {
	case err == nil:
		a.token = token
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	return a, nil
}

// Enrolled reports whether the agent has a machine token and a config.
func (a *Agent) Enrolled() bool {
	return a.token != "" && a.cfg.HubURL != ""
}

// Enroll pins the hub key, obtains a machine token and stores both. The
// enrollment token is never written to disk. Enrolling again replaces the
// stored config and token.
func (a *Agent) Enroll(ctx context.Context, hubURL, orgID, hubKey, token string) error {
	hubURL = strings.TrimRight(strings.TrimSpace(hubURL), "/")
	orgID = strings.TrimSpace(orgID)
	hubKey = strings.TrimSpace(hubKey)
	token = strings.TrimSpace(token)
	switch {
	case hubURL == "":
		return errors.New("hub URL is required")
	case orgID == "":
		return errors.New("org id is required")
	case hubKey == "":
		return errors.New("hub public key is required")
	case !strings.HasPrefix(token, agentlink.EnrollTokenPrefix):
		return fmt.Errorf("enrollment token must start with %q", agentlink.EnrollTokenPrefix)
	}
	keyBytes, err := base64.StdEncoding.DecodeString(hubKey)
	if err != nil {
		return fmt.Errorf("decoding hub public key: %w", err)
	}
	if len(keyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("hub public key must be %d bytes, got %d", ed25519.PublicKeySize, len(keyBytes))
	}
	fingerprint, err := Fingerprint()
	if err != nil {
		return err
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	ws, err := dial(ctx, hubURL)
	if err != nil {
		return err
	}
	c := agentlink.NewConn(ws)
	defer ws.CloseNow() //nolint:errcheck // best effort after the handshake
	welcome, err := agentlink.ClientHandshake(ctx, c, ed25519.PublicKey(keyBytes), agentlink.Hello{
		OrgID:        orgID,
		Fingerprint:  fingerprint,
		Hostname:     hostname,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: version.Version,
	}, agentlink.Auth{EnrollToken: token})
	if err != nil {
		return fmt.Errorf("enrolling with hub: %w", err)
	}
	if welcome.MachineToken == "" {
		return errors.New("hub did not issue a machine token")
	}
	cfg := Config{HubURL: hubURL, OrgID: orgID, HubPublicKey: hubKey, MachineID: welcome.MachineID}
	if err := cfg.Save(a.dataDir); err != nil {
		return err
	}
	if err := saveMachineToken(a.dataDir, welcome.MachineToken); err != nil {
		return err
	}
	a.mu.Lock()
	a.cfg = cfg
	a.token = welcome.MachineToken
	a.current = welcome.Config
	a.mu.Unlock()
	a.log.Info("enrolled machine", "machine_id", welcome.MachineID, "hub", hubURL)
	return nil
}

// Run keeps one connection to the hub, scanning harnesses in between. It
// returns when ctx ends, when the hub rejects the credential for good, or
// after installing a successful self-update.
func (a *Agent) Run(ctx context.Context) error {
	if a.token == "" || a.cfg.HubURL == "" {
		return errors.New("agent is not enrolled; run `aicm-agent enroll` first")
	}
	if !strings.HasPrefix(a.token, agentlink.MachineTokenPrefix) {
		return errors.New("machine token is invalid; run `aicm-agent enroll` again")
	}
	keyBytes, err := base64.StdEncoding.DecodeString(a.cfg.HubPublicKey)
	if err != nil {
		return fmt.Errorf("decoding pinned hub public key: %w", err)
	}
	if len(keyBytes) != ed25519.PublicKeySize {
		return fmt.Errorf("pinned hub public key must be %d bytes, got %d", ed25519.PublicKeySize, len(keyBytes))
	}
	hubKey := ed25519.PublicKey(keyBytes)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	go a.checkUpdateOnce(ctx, cancel)

	scanDone := make(chan struct{})
	go func() {
		defer close(scanDone)
		a.scanLoop(ctx)
	}()

	err = a.connectLoop(ctx, hubKey)
	cancel()
	<-scanDone
	if a.updated.Load() {
		return nil
	}
	return err
}

// connectLoop dials and serves until ctx ends or a fatal handshake error.
func (a *Agent) connectLoop(ctx context.Context, hubKey ed25519.PublicKey) error {
	backoff := backoffMin
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		started := time.Now()
		c, welcome, err := a.connect(ctx, hubKey)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if fatal(err) {
				return err
			}
			a.log.Warn("hub connection failed", "err", err, "retry_in", backoff)
			if !sleep(ctx, jitter(backoff)) {
				return nil
			}
			backoff = min(backoff*2, backoffMax)
			continue
		}
		if welcome.MachineToken != "" {
			if err := saveMachineToken(a.dataDir, welcome.MachineToken); err != nil {
				_ = c.WebSocket().CloseNow()
				return err
			}
			a.mu.Lock()
			a.token = welcome.MachineToken
			a.mu.Unlock()
		}
		a.log.Info("connected to hub", "machine_id", welcome.MachineID)
		a.setConnection(c, welcome.Config)
		a.flush(ctx)

		done := make(chan struct{})
		go func() {
			defer close(done)
			a.readLoop(ctx, c)
		}()
		select {
		case <-ctx.Done():
			_ = c.WebSocket().Close(websocket.StatusGoingAway, "shutting down")
		case <-done:
		}
		<-done
		a.clearConnection(c)
		_ = c.WebSocket().CloseNow()
		if ctx.Err() != nil {
			return nil
		}
		if time.Since(started) >= backoffResetTime {
			backoff = backoffMin
		}
		delay := jitter(backoff)
		a.log.Warn("hub connection lost", "retry_in", delay)
		if !sleep(ctx, delay) {
			return nil
		}
		backoff = min(backoff*2, backoffMax)
	}
}

// connect dials the hub and runs the machine handshake.
func (a *Agent) connect(ctx context.Context, hubKey ed25519.PublicKey) (*agentlink.Conn, agentlink.Welcome, error) {
	fingerprint, err := Fingerprint()
	if err != nil {
		return nil, agentlink.Welcome{}, err
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "unknown"
	}
	ws, err := dial(ctx, a.cfg.HubURL)
	if err != nil {
		return nil, agentlink.Welcome{}, err
	}
	c := agentlink.NewConn(ws)
	welcome, err := agentlink.ClientHandshake(ctx, c, hubKey, agentlink.Hello{
		OrgID:        a.cfg.OrgID,
		Fingerprint:  fingerprint,
		Hostname:     hostname,
		OS:           runtime.GOOS,
		Arch:         runtime.GOARCH,
		AgentVersion: version.Version,
	}, agentlink.Auth{MachineToken: a.token})
	if err != nil {
		_ = ws.CloseNow()
		return nil, agentlink.Welcome{}, fmt.Errorf("hub handshake: %w", err)
	}
	return c, welcome, nil
}

// readLoop handles every message from the hub until the connection closes.
func (a *Agent) readLoop(ctx context.Context, c *agentlink.Conn) {
	for {
		e, err := c.Recv(ctx)
		if err != nil {
			if ctx.Err() == nil {
				a.log.Debug("agent link closed", "err", err)
			}
			return
		}
		switch e.Type {
		case agentlink.TypeAck:
			a.handleAck(e)
		case agentlink.TypeConfig:
			cfg, err := agentlink.Decode[agentlink.AgentConfig](e)
			if err != nil {
				a.log.Warn("decoding hub config", "id", e.ID, "err", err)
				continue
			}
			a.mu.Lock()
			a.current = cfg
			a.mu.Unlock()
		case agentlink.TypeSecretRequest:
			a.handleSecretRequest(ctx, c, e)
		case agentlink.TypeError:
			le, err := agentlink.Decode[agentlink.Error](e)
			if err != nil {
				a.log.Warn("decoding hub error", "id", e.ID, "err", err)
				continue
			}
			a.log.Error("hub reported an error", "code", le.Code, "message", le.Message)
		default:
			a.log.Debug("ignoring hub message", "type", e.Type, "id", e.ID)
		}
	}
}

// handleAck drops one message from the spool and logs a rejection.
func (a *Agent) handleAck(e agentlink.Envelope) {
	ack, err := agentlink.Decode[agentlink.Ack](e)
	if err != nil {
		a.log.Warn("decoding ack", "id", e.ID, "err", err)
		return
	}
	if err := a.spool.Ack(ack.ID); err != nil {
		a.log.Warn("dropping acked message failed", "id", ack.ID, "err", err)
	}
	if ack.Error != "" {
		a.log.Warn("hub rejected message", "id", ack.ID, "error", ack.Error)
	}
}

// handleSecretRequest answers a request for a plain API key, or refuses it.
func (a *Agent) handleSecretRequest(ctx context.Context, c *agentlink.Conn, e agentlink.Envelope) {
	req, err := agentlink.Decode[agentlink.SecretRequest](e)
	if err != nil {
		a.log.Warn("decoding secret request", "id", e.ID, "err", err)
		return
	}
	reply := agentlink.SecretReply{RequestID: req.RequestID}
	value, err := a.readAPIKey(ctx, req.Account)
	if err != nil {
		reply.Error = err.Error()
		a.log.Warn("refused secret request", "provider", req.Account.ProviderID, "reason", err)
	} else {
		reply.Value = value
	}
	if _, err := c.Send(ctx, agentlink.TypeSecretReply, "", reply); err != nil {
		a.log.Warn("sending secret reply", "request_id", req.RequestID, "err", err)
	}
}

// readAPIKey finds the harness that reported an API-key account and asks it
// for the key. It never returns a key for another kind of account.
func (a *Agent) readAPIKey(ctx context.Context, ref agentlink.AccountRef) (string, error) {
	a.mu.Lock()
	found, ok := a.discovered[refKey(ref)]
	a.mu.Unlock()
	if !ok || !found.hasAPIKey {
		return "", errors.New("no harness reported an API key for this account")
	}
	for _, h := range a.harnesses {
		if h.ID() != found.harness {
			continue
		}
		kr, ok := h.(harness.KeyReader)
		if !ok {
			break
		}
		key, err := kr.ReadAPIKey(ctx, ref)
		if err != nil {
			return "", fmt.Errorf("reading API key: %w", err)
		}
		return key, nil
	}
	return "", errors.New("harness cannot provide API keys")
}

// scanLoop scans on the hub's interval, offline or not.
func (a *Agent) scanLoop(ctx context.Context) {
	for {
		a.scanOnce(ctx)
		if ctx.Err() != nil {
			return
		}
		timer := time.NewTimer(a.scanInterval())
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

// scanOnce runs discovery, harness scans, due polls and a status report, then
// flushes new messages when connected.
func (a *Agent) scanOnce(ctx context.Context) {
	now := time.Now()
	if a.nextDiscovery.IsZero() || !now.Before(a.nextDiscovery) {
		a.discover(ctx)
		a.nextDiscovery = time.Now().Add(discoveryInterval)
	}
	for _, h := range a.harnesses {
		a.scanHarness(ctx, h)
	}
	a.pollHarnesses(ctx)
	if !time.Now().Before(a.nextStatus) {
		a.sendStatus()
		a.nextStatus = time.Now().Add(statusInterval)
	}
	a.flush(ctx)
}

// discover reports the union of every harness's accounts when it changed.
func (a *Agent) discover(ctx context.Context) {
	all := []agentlink.DiscoveredAccount{}
	index := map[string]discoveredAccount{}
	for _, h := range a.harnesses {
		accounts, err := h.Discover(ctx)
		if err != nil {
			a.log.Warn("discovering accounts failed", "harness", h.ID(), "err", err)
			continue
		}
		a.setFound(h.ID(), len(accounts) > 0)
		for _, acc := range accounts {
			all = append(all, acc)
			if acc.AuthKind == authKindAPIKey && acc.HasAPIKey {
				index[refKey(acc.Ref)] = discoveredAccount{harness: h.ID(), hasAPIKey: true}
			}
		}
	}
	a.mu.Lock()
	a.discovered = index
	a.mu.Unlock()

	body, err := json.Marshal(agentlink.Accounts{Accounts: all})
	if err != nil {
		a.log.Warn("encoding accounts", "err", err)
		return
	}
	if string(body) == a.lastAccounts {
		return
	}
	if _, err := a.spool.Add(agentlink.TypeAccounts, agentlink.Accounts{Accounts: all}); err != nil {
		a.log.Warn("spooling accounts", "err", err)
		return
	}
	a.lastAccounts = string(body)
}

// scanHarness reads one harness and spools its batches. Cursors advance only
// after every batch is safely spooled.
func (a *Agent) scanHarness(ctx context.Context, h harness.Harness) {
	id := h.ID()
	all, err := a.cursors.Load()
	if err != nil {
		a.log.Warn("loading cursors failed", "harness", id, "err", err)
		all = map[string]harness.Cursors{}
	}
	started := time.Now()
	batch, scanErr := h.Scan(ctx, all[id])
	if scanErr != nil {
		a.log.Warn("scanning harness failed", "harness", id, "err", scanErr)
		if batch.Cursors == nil {
			a.setScan(id, started, scanErr)
			return
		}
		// A harness may return what it could read plus an error about the
		// rest (for example skipped compressed files): keep the partial batch.
	}
	if err := a.spoolBatch(id, batch); err != nil {
		a.log.Warn("spooling scan failed", "harness", id, "err", err)
		a.setScan(id, started, err)
		return
	}
	if err := a.cursors.Save(id, batch.Cursors); err != nil {
		a.log.Warn("saving cursors failed", "harness", id, "err", err)
	}
	a.setScan(id, started, scanErr)
}

// pollHarnesses polls due pollers, backing off after errors.
func (a *Agent) pollHarnesses(ctx context.Context) {
	cfg := a.config()
	now := time.Now()
	for _, h := range a.harnesses {
		p, ok := h.(harness.Poller)
		if !ok {
			continue
		}
		id := h.ID()
		if next, ok := a.nextPoll[id]; ok && now.Before(next) {
			continue
		}
		reports, err := p.Poll(ctx, cfg)
		if err != nil {
			a.log.Warn("polling harness failed", "harness", id, "err", err)
			base := a.pollInterval[id]
			if base <= 0 {
				base = p.PollInterval()
			}
			if base <= 0 {
				base = defaultPollInterval
			}
			a.pollInterval[id] = min(base*2, maxPollInterval)
		} else {
			if err := a.spoolMeters(id, reports); err != nil {
				a.log.Warn("spooling poll failed", "harness", id, "err", err)
			}
			interval := p.PollInterval()
			if interval <= 0 {
				interval = defaultPollInterval
			}
			a.pollInterval[id] = interval
		}
		a.nextPoll[id] = time.Now().Add(a.pollInterval[id])
	}
}

// spoolBatch splits a scan batch into messages and spools each one.
func (a *Agent) spoolBatch(id string, batch harness.Batch) error {
	dismissed := dismissedSet(a.config())
	events := make([]agentlink.UsageEvent, 0, len(batch.Usage))
	for _, ev := range batch.Usage {
		if !dismissed[refKey(ev.Account)] {
			events = append(events, ev)
		}
	}
	reports, dropped := filterReports(batch.Meters, dismissed)
	if dropped > 0 {
		a.log.Warn("dropping invalid meter readings", "harness", id, "count", dropped)
	}
	for _, chunk := range chunks(events, agentlink.MaxBatchItems) {
		if _, err := a.spool.Add(agentlink.TypeUsage, agentlink.Usage{Events: chunk}); err != nil {
			return err
		}
	}
	for _, chunk := range chunks(reports, agentlink.MaxBatchItems) {
		if _, err := a.spool.Add(agentlink.TypeMeters, agentlink.Meters{Reports: chunk}); err != nil {
			return err
		}
	}
	return nil
}

// spoolMeters filters and spools poll results.
func (a *Agent) spoolMeters(id string, reports []agentlink.MeterReport) error {
	filtered, dropped := filterReports(reports, dismissedSet(a.config()))
	if dropped > 0 {
		a.log.Warn("dropping invalid meter readings", "harness", id, "count", dropped)
	}
	for _, chunk := range chunks(filtered, agentlink.MaxBatchItems) {
		if _, err := a.spool.Add(agentlink.TypeMeters, agentlink.Meters{Reports: chunk}); err != nil {
			return err
		}
	}
	return nil
}

// sendStatus spools one health report for every harness.
func (a *Agent) sendStatus() {
	st := agentlink.Status{Harnesses: make([]agentlink.HarnessStatus, 0, len(a.harnesses))}
	for _, h := range a.harnesses {
		s := a.status[h.ID()]
		s.ID = h.ID()
		st.Harnesses = append(st.Harnesses, s)
	}
	if _, err := a.spool.Add(agentlink.TypeStatus, st); err != nil {
		a.log.Warn("spooling status failed", "err", err)
	}
}

// flush sends every spooled message over the current connection. Messages
// stay spooled until acked.
func (a *Agent) flush(ctx context.Context) {
	a.flushMu.Lock()
	defer a.flushMu.Unlock()
	a.mu.Lock()
	c := a.conn
	a.mu.Unlock()
	if c == nil {
		return
	}
	pending, err := a.spool.Pending()
	if err != nil {
		a.log.Warn("reading spool failed", "err", err)
		return
	}
	for _, e := range pending {
		if _, err := c.Send(ctx, e.Type, e.ID, e.Payload); err != nil {
			if ctx.Err() == nil {
				a.log.Warn("sending spooled message failed", "type", e.Type, "id", e.ID, "err", err)
			}
			return
		}
	}
}

// setConnection installs a live connection and the hub's config.
func (a *Agent) setConnection(c *agentlink.Conn, cfg agentlink.AgentConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.conn = c
	a.current = cfg
}

// clearConnection removes the connection if it is still the current one.
func (a *Agent) clearConnection(c *agentlink.Conn) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == c {
		a.conn = nil
	}
}

// config returns the hub's latest config.
func (a *Agent) config() agentlink.AgentConfig {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.current
}

// setConfig replaces the hub config without a connection.
func (a *Agent) setConfig(cfg agentlink.AgentConfig) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.current = cfg
}

// scanInterval is the hub's scan interval, defaulting to 60 s.
func (a *Agent) scanInterval() time.Duration {
	seconds := a.config().ScanIntervalSeconds
	if seconds <= 0 {
		return defaultScanInterval
	}
	return time.Duration(seconds) * time.Second
}

// setScan records the outcome of one harness scan.
func (a *Agent) setScan(id string, at time.Time, err error) {
	s := a.status[id]
	s.ID = id
	s.LastScanAt = at
	s.Error = ""
	if err != nil {
		s.Error = err.Error()
	}
	a.status[id] = s
}

// setFound records whether a harness found any accounts.
func (a *Agent) setFound(id string, found bool) {
	s := a.status[id]
	s.ID = id
	s.Found = found
	a.status[id] = s
}

// checkUpdateOnce checks for a release and cancels Run after a successful
// update so the service manager starts the new binary.
func (a *Agent) checkUpdateOnce(ctx context.Context, cancel context.CancelFunc) {
	if version.Version == "dev" {
		return
	}
	stamp := filepath.Join(a.dataDir, "update-check")
	if info, err := os.Stat(stamp); err == nil && time.Since(info.ModTime()) < updateInterval {
		return
	}
	if err := writeFileAtomic(stamp, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), fileMode); err != nil {
		a.log.Warn("recording update check failed", "err", err)
	}
	latest, err := Update(ctx, &http.Client{Timeout: 5 * time.Minute})
	if err != nil {
		if ctx.Err() == nil {
			a.log.Warn("update check failed", "err", err)
		}
		return
	}
	if latest == "" {
		return
	}
	a.log.Info("updated agent; exiting so it can restart", "version", latest)
	a.updated.Store(true)
	cancel()
}

// dial opens the agent WebSocket. Plain ws:// is allowed only to loopback.
func dial(ctx context.Context, hubURL string) (*websocket.Conn, error) {
	u, err := url.Parse(hubURL)
	if err != nil {
		return nil, fmt.Errorf("parsing hub URL: %w", err)
	}
	if u.Host == "" {
		return nil, fmt.Errorf("hub URL %q has no host", hubURL)
	}
	switch u.Scheme {
	case "https", "wss":
		u.Scheme = "wss"
	case "http", "ws":
		if !loopback(u.Hostname()) {
			return nil, fmt.Errorf("plain ws:// is only allowed to loopback hosts; use https:// or wss://")
		}
		u.Scheme = "ws"
	default:
		return nil, fmt.Errorf("hub URL scheme %q must be http, https, ws or wss", u.Scheme)
	}
	u.Path = strings.TrimRight(u.Path, "/") + agentlink.Path
	ws, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{Subprotocols: []string{agentlink.Subprotocol}}) //nolint:bodyclose // coder/websocket owns the response body
	if err != nil {
		return nil, fmt.Errorf("connecting to hub: %w", err)
	}
	return ws, nil
}

// loopback reports whether host is a local address.
func loopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// fatal reports whether a handshake error should stop Run for good.
func fatal(err error) bool {
	if errors.Is(err, agentlink.ErrBadHubSignature) {
		return true
	}
	var le *agentlink.Error
	if errors.As(err, &le) {
		switch le.Code {
		case agentlink.CodeUnauthorized, agentlink.CodeFingerprint, agentlink.CodeRevoked, agentlink.CodeBadHubSignature:
			return true
		}
	}
	return false
}

// jitter adds ±20% to a backoff delay.
func jitter(d time.Duration) time.Duration {
	factor := 0.8 + 0.4*rand.Float64() //nolint:gosec // jitter needs no crypto randomness
	return time.Duration(float64(d) * factor)
}

// sleep waits for d or until ctx ends, reporting whether the wait finished.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// dismissedSet indexes the account refs the hub dismissed.
func dismissedSet(cfg agentlink.AgentConfig) map[string]bool {
	out := make(map[string]bool, len(cfg.Accounts))
	for _, acc := range cfg.Accounts {
		if acc.Status == accountDismissed {
			out[refKey(acc.Ref)] = true
		}
	}
	return out
}

// filterReports drops dismissed accounts and readings that fail validation.
func filterReports(reports []agentlink.MeterReport, dismissed map[string]bool) ([]agentlink.MeterReport, int) {
	out := make([]agentlink.MeterReport, 0, len(reports))
	dropped := 0
	for _, r := range reports {
		if dismissed[refKey(r.Account)] {
			continue
		}
		if err := r.Def.Validate(); err != nil {
			dropped++
			continue
		}
		if err := r.Reading.Validate(r.Def); err != nil {
			dropped++
			continue
		}
		out = append(out, r)
	}
	return out, dropped
}

// refKey identifies an account in maps.
func refKey(r agentlink.AccountRef) string {
	return r.ProviderID + "\x00" + r.ExternalID
}

// chunks splits items into slices of at most size.
func chunks[T any](items []T, size int) [][]T {
	if len(items) == 0 {
		return nil
	}
	out := make([][]T, 0, (len(items)+size-1)/size)
	for len(items) > 0 {
		n := min(size, len(items))
		out = append(out, items[:n])
		items = items[n:]
	}
	return out
}
