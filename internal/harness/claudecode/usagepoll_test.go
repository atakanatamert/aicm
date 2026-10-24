// SPDX-License-Identifier: SUL-1.0

package claudecode

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/version"
)

const usagePollTestToken = "sk-ant-oat01-SYNTHETIC-USAGE-POLL-DO-NOT-USE"

type usagePollTestEnv struct {
	harness   *Harness
	ref       agentlink.AccountRef
	dataDir   string
	configDir string
}

func newUsagePollTestEnv(t *testing.T, expiresAt time.Time) usagePollTestEnv {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", "")

	home := t.TempDir()
	configDir := filepath.Join(home, ".claude")
	dataDir := t.TempDir()
	if err := os.MkdirAll(configDir, 0o700); err != nil {
		t.Fatalf("creating config dir: %v", err)
	}
	accountUUID := "3f2a1b0c-9d8e-4f7a-8b6c-5d4e3f2a1b0c"
	// Default config dirs use <home>/.claude.json; only the credentials file
	// lives in the config dir.
	writeUsagePollJSON(t, filepath.Join(home, ".claude.json"), map[string]any{
		"oauthAccount": map[string]any{
			"accountUuid":  accountUUID,
			"emailAddress": "test@example.com",
		},
	})
	writeUsagePollJSON(t, filepath.Join(configDir, ".credentials.json"), map[string]any{
		"claudeAiOauth": map[string]any{
			"accessToken":      usagePollTestToken,
			"expiresAt":        expiresAt.UnixMilli(),
			"subscriptionType": "max",
		},
	})

	h := New(home, dataDir)
	h.keychain = nil // keep the test off the real Keychain
	return usagePollTestEnv{
		harness:   h,
		ref:       agentlink.AccountRef{ProviderID: "claude", ExternalID: accountUUID},
		dataDir:   dataDir,
		configDir: configDir,
	}
}

func writeUsagePollJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("encoding fixture: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("writing fixture %s: %v", path, err)
	}
}

// writeStatuslineCapture appends one capture at the given time. Its transcript
// path lives under the config dir, so Scan maps it to the account.
func writeStatuslineCapture(t *testing.T, env usagePollTestEnv, at time.Time) {
	t.Helper()
	line, err := json.Marshal(map[string]any{
		"captured_at":     at,
		"transcript_path": filepath.Join(env.configDir, "projects", "-tmp", "session.jsonl"),
	})
	if err != nil {
		t.Fatalf("encoding capture: %v", err)
	}
	path := filepath.Join(env.dataDir, "claude-statusline.jsonl")
	if err := os.WriteFile(path, append(line, '\n'), 0o600); err != nil {
		t.Fatalf("writing statusline spool: %v", err)
	}
}

func setUsagePollClock(t *testing.T, at time.Time) *time.Time {
	t.Helper()
	now := at
	previous := usagePollNow
	usagePollNow = func() time.Time { return now }
	t.Cleanup(func() { usagePollNow = previous })
	return &now
}

func setUsagePollURL(t *testing.T, url string) {
	t.Helper()
	previous := usagePollURL
	usagePollURL = url
	t.Cleanup(func() { usagePollURL = previous })
}

func usagePollConfig(ref agentlink.AccountRef, allow bool) agentlink.AgentConfig {
	return agentlink.AgentConfig{Accounts: []agentlink.AccountConfig{{
		Ref:             ref,
		Status:          "active",
		AllowUnofficial: allow,
	}}}
}

func TestPollOffByDefault(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	setUsagePollClock(t, now)
	env := newUsagePollTestEnv(t, now.Add(24*time.Hour))
	writeStatuslineCapture(t, env, now.Add(-45*time.Minute))

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-10-18T15:00:00Z"}}`))
	}))
	defer server.Close()
	setUsagePollURL(t, server.URL+"/api/oauth/usage")

	reports, err := env.harness.Poll(t.Context(), usagePollConfig(env.ref, false))
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("reports = %d, want 0", len(reports))
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests = %d, want 0", got)
	}
}

func TestPollMapsWindows(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	setUsagePollClock(t, now)
	env := newUsagePollTestEnv(t, now.Add(24*time.Hour))
	writeStatuslineCapture(t, env, now.Add(-45*time.Minute))

	body, err := os.ReadFile(filepath.Join("testdata", "usagepoll", "response.json")) //nolint:gosec // testdata path
	if err != nil {
		t.Fatalf("reading response fixture: %v", err)
	}

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/api/oauth/usage" {
			t.Errorf("path = %s, want /api/oauth/usage", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+usagePollTestToken {
			t.Errorf("Authorization = %q, want bearer with the fake token", got)
		}
		if got := r.Header.Get("anthropic-beta"); got != "oauth-2025-04-20" {
			t.Errorf("anthropic-beta = %q, want oauth-2025-04-20", got)
		}
		if got, want := r.Header.Get("User-Agent"), "aicm-agent/"+version.Version; got != want {
			t.Errorf("User-Agent = %q, want %q", got, want)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer server.Close()
	setUsagePollURL(t, server.URL+"/api/oauth/usage")

	reports, err := env.harness.Poll(t.Context(), usagePollConfig(env.ref, true))
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	if len(reports) != 4 {
		t.Fatalf("reports = %d, want 4", len(reports))
	}
	for i, want := range []string{"five_hour", "seven_day", "seven_day_opus", "seven_day_sonnet"} {
		if reports[i].Def.Key != want {
			t.Fatalf("report %d key = %q, want %q", i, reports[i].Def.Key, want)
		}
		if reports[i].Reading.Source != "api" {
			t.Fatalf("report %d source = %q, want api", i, reports[i].Reading.Source)
		}
	}
	compareGolden(t, reports, "usagepoll.golden.json")

	encoded, err := json.Marshal(reports)
	if err != nil {
		t.Fatalf("marshaling reports: %v", err)
	}
	if bytes.Contains(encoded, []byte(usagePollTestToken)) {
		t.Fatal("reports leaked the access token")
	}
	cache, err := os.ReadFile(filepath.Join(env.dataDir, "claude-usage-poll.json")) //nolint:gosec // test temp dir
	if err != nil {
		t.Fatalf("reading usage poll cache: %v", err)
	}
	if bytes.Contains(cache, []byte(usagePollTestToken)) {
		t.Fatal("cache leaked the access token")
	}
}

func TestPollFloorAndRetryAfter(t *testing.T) {
	t0 := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	clock := setUsagePollClock(t, t0)
	env := newUsagePollTestEnv(t, t0.Add(24*time.Hour))

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) == 1 {
			w.Header().Set("Retry-After", "120")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-10-18T15:00:00Z"}}`))
	}))
	defer server.Close()
	setUsagePollURL(t, server.URL+"/api/oauth/usage")

	cfg := usagePollConfig(env.ref, true)
	if _, err := env.harness.Poll(t.Context(), cfg); err != nil {
		t.Fatalf("first Poll: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests after 429 = %d, want 1", got)
	}

	*clock = t0.Add(time.Minute)
	if _, err := env.harness.Poll(t.Context(), cfg); err != nil {
		t.Fatalf("Poll during Retry-After: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests one minute after 429 = %d, want 1", got)
	}

	*clock = t0.Add(119 * time.Second)
	if _, err := env.harness.Poll(t.Context(), cfg); err != nil {
		t.Fatalf("Poll before Retry-After: %v", err)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("requests 119s after 429 = %d, want 1", got)
	}

	*clock = t0.Add(121 * time.Second)
	reports, err := env.harness.Poll(t.Context(), cfg)
	if err != nil {
		t.Fatalf("Poll after Retry-After: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests 121s after 429 = %d, want 2", got)
	}
	if len(reports) != 1 {
		t.Fatalf("reports after retry = %d, want 1", len(reports))
	}

	// A success resets the floor to 15 minutes.
	*clock = t0.Add(121*time.Second + 14*time.Minute)
	if _, err := env.harness.Poll(t.Context(), cfg); err != nil {
		t.Fatalf("Poll within the floor: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests within 15 minutes of success = %d, want 2", got)
	}

	*clock = t0.Add(121*time.Second + 15*time.Minute + time.Second)
	writeStatuslineCapture(t, env, clock.Add(-time.Minute))
	if _, err := env.harness.Poll(t.Context(), cfg); err != nil {
		t.Fatalf("Poll with fresh statusline: %v", err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("requests with a fresh statusline = %d, want 2", got)
	}

	if err := os.Remove(filepath.Join(env.dataDir, "claude-statusline.jsonl")); err != nil {
		t.Fatalf("removing statusline spool: %v", err)
	}
	if _, err := env.harness.Poll(t.Context(), cfg); err != nil {
		t.Fatalf("Poll without statusline: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Fatalf("requests without a statusline = %d, want 3", got)
	}
}

func TestExpiredTokenSkipped(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	setUsagePollClock(t, now)
	env := newUsagePollTestEnv(t, now.Add(-time.Hour))

	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-10-18T15:00:00Z"}}`))
	}))
	defer server.Close()
	setUsagePollURL(t, server.URL+"/api/oauth/usage")

	reports, err := env.harness.Poll(t.Context(), usagePollConfig(env.ref, true))
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(reports) != 0 {
		t.Fatalf("reports = %d, want 0", len(reports))
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests with an expired token = %d, want 0", got)
	}
}
