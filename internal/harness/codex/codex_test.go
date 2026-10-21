// SPDX-License-Identifier: Apache-2.0

package codex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
)

const testAccountID = "acct_abc123"

func TestDiscoverGolden(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	token := writeAuth(t, filepath.Join(home, ".codex"))

	h := New(home)
	got, err := h.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	compareGolden(t, got, "testdata/discover.golden.json")

	golden, err := os.ReadFile("testdata/discover.golden.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{token, "secret-access-token", "secret-refresh-token"} {
		if bytes.Contains(golden, []byte(secret)) {
			t.Fatalf("golden contains a credential")
		}
	}
}

func TestRolloutGolden(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	codexHome := filepath.Join(home, ".codex")
	writeAuth(t, codexHome)

	sessionID := "11111111-2222-3333-4444-555555555555"
	day := filepath.Join(codexHome, "sessions", "2026", "03", "04")
	if err := os.MkdirAll(day, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(day, "rollout-2026-03-04T10-00-00-"+sessionID+".jsonl")

	lines := []string{
		`{"timestamp":"2026-03-04T10:00:00.000Z","type":"session_meta","payload":{"session_id":"` + sessionID + `","id":"` + sessionID + `","timestamp":"2026-03-04T10:00:00Z","cwd":"/work/project","originator":"codex_cli_rs","cli_version":"0.1.0"}}`,
		`{"timestamp":"2026-03-04T10:00:01.000Z","type":"turn_context","payload":{"model":"gpt-5.1-codex","cwd":"/work/project","approval_policy":"never","sandbox_policy":{"mode":"read-only"}}}`,
		`{"timestamp":"2026-03-04T10:00:02.000Z","type":"event_msg","payload":{"type":"token_count","info":null,"rate_limits":{"primary":{"used_percent":5,"window_minutes":300,"resets_in_seconds":18000}}}}`,
		`{"timestamp":"2026-03-04T10:00:03.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":1000,"cached_input_tokens":400,"output_tokens":50,"reasoning_output_tokens":10}},"rate_limits":null}}`,
		`{"timestamp":"2026-03-04T10:00:04.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":2000,"cached_input_tokens":800,"output_tokens":100,"reasoning_output_tokens":20}},"rate_limits":{"primary":{"used_percent":12,"window_minutes":300,"resets_at":1772636400},"secondary":{"used_percent":40,"window_minutes":10080,"resets_in_seconds":604800},"credits":{"has_credits":true,"unlimited":false,"balance":"12.5"},"plan_type":"plus"}}}`,
		`{"timestamp":`,
	}
	partial := `{"timestamp":"2026-03-04T10:00:05.000Z","type":"event_msg","payload":{"type":"token_count","info":{"last_token_usage":{"input_tokens":9`
	content := strings.Join(lines, "\n") + "\n" + partial
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	zst := filepath.Join(day, "rollout-2026-03-04T10-00-00-aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee.jsonl.zst")
	if err := os.WriteFile(zst, []byte("not a zstd frame"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := New(home)
	batch, err := h.Scan(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "1 compressed rollouts not supported yet") {
		t.Fatalf("Scan error = %v, want compressed rollouts warning", err)
	}

	compareGolden(t, batch.Usage, "testdata/rollout_usage.golden.json")
	compareGolden(t, batch.Meters, "testdata/rollout_meters.golden.json")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if batch.Cursors[path].Offset >= info.Size() {
		t.Fatalf("cursor offset %d advanced over the partial line (size %d)", batch.Cursors[path].Offset, info.Size())
	}

	again, err := h.Scan(context.Background(), batch.Cursors)
	if err == nil || !strings.Contains(err.Error(), "compressed rollouts not supported yet") {
		t.Fatalf("second Scan error = %v, want compressed rollouts warning", err)
	}
	if len(again.Usage) != 0 || len(again.Meters) != 0 {
		t.Fatalf("second Scan returned %d events and %d meters, want none", len(again.Usage), len(again.Meters))
	}
}

func TestAppServerGolden(t *testing.T) {
	t.Setenv("CODEX_HOME", "")
	home := t.TempDir()
	writeAuth(t, filepath.Join(home, ".codex"))

	script, err := os.ReadFile("testdata/fake-codex.sh")
	if err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(binDir, "codex"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))

	fixed := time.Date(2026, 3, 4, 10, 5, 0, 0, time.UTC)
	h := New(home)
	h.now = func() time.Time { return fixed }

	got, err := h.Poll(context.Background(), agentlink.AgentConfig{})
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	compareGolden(t, got, "testdata/appserver_meters.golden.json")
}

func writeAuth(t *testing.T, codexHome string) string {
	t.Helper()
	if err := os.MkdirAll(codexHome, 0o755); err != nil {
		t.Fatal(err)
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none","typ":"JWT"}`))
	payload, err := json.Marshal(map[string]any{
		"email": "test@example.com",
		"https://api.openai.com/auth": map[string]any{
			"chatgpt_account_id":                testAccountID,
			"chatgpt_plan_type":                 "plus",
			"chatgpt_subscription_active_start": "2026-06-09T08:48:55+00:00",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	signature := base64.RawURLEncoding.EncodeToString([]byte("fake-signature"))
	token := header + "." + base64.RawURLEncoding.EncodeToString(payload) + "." + signature

	auth, err := json.Marshal(map[string]any{
		"OPENAI_API_KEY": nil,
		"tokens": map[string]any{
			"id_token":      token,
			"access_token":  "secret-access-token",
			"refresh_token": "secret-refresh-token",
			"account_id":    testAccountID,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "auth.json"), auth, 0o600); err != nil {
		t.Fatal(err)
	}
	return token
}

func compareGolden(t *testing.T, got any, path string) {
	t.Helper()
	gotJSON, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("marshaling result: %v", err)
	}
	wantJSON, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden: %v", err)
	}
	var gotValue, wantValue any
	if err := json.Unmarshal(gotJSON, &gotValue); err != nil {
		t.Fatalf("decoding result: %v", err)
	}
	if err := json.Unmarshal(wantJSON, &wantValue); err != nil {
		t.Fatalf("decoding golden %s: %v", path, err)
	}
	if !reflect.DeepEqual(gotValue, wantValue) {
		t.Fatalf("golden mismatch for %s\ngot:  %s\nwant: %s", path, gotJSON, wantJSON)
	}
}
