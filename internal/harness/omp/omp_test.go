// SPDX-License-Identifier: Apache-2.0

package omp

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakanatamert/aicm/internal/agentlink"
	harnesspkg "github.com/atakanatamert/aicm/internal/harness"
)

const syntheticHome = "testdata/home"

func newTestHarness(t *testing.T) *Harness {
	t.Helper()
	t.Setenv("PI_CODING_AGENT_DIR", "")
	home, err := filepath.Abs(syntheticHome)
	if err != nil {
		t.Fatalf("resolving testdata home: %v", err)
	}
	return New(home)
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name)) //nolint:gosec // fixed test fixture
	if err != nil {
		t.Fatalf("reading golden %s: %v", name, err)
	}
	return bytes.TrimSpace(data)
}

func marshalGolden(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshaling golden value: %v", err)
	}
	return data
}

func TestDiscoverGolden(t *testing.T) {
	harness := newTestHarness(t)
	accounts, err := harness.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	// auth.json holds the anthropic and deepseek keys; agent.db holds the zai
	// key (one coding-plan and one pay-as-you-go API account) plus a disabled
	// key, an OAuth row and a provider without a catalog id, none of which
	// may be reported.
	// The subscription logins (Claude, ChatGPT) are reported by account id;
	// their tokens never appear.
	if len(accounts) != 6 {
		t.Fatalf("Discover returned %d accounts, want 6", len(accounts))
	}
	got := marshalGolden(t, accounts)
	want := readGolden(t, "discover.golden.json")
	if !bytes.Equal(got, want) {
		t.Fatalf("discovered accounts mismatch\n got: %s\nwant: %s", got, want)
	}
	for _, secret := range []string{"synthetic-anthropic", "synthetic-deepseek", "zai-synthetic", "synthetic-access", "synthetic-refresh"} {
		if bytes.Contains(want, []byte(secret)) {
			t.Fatalf("discover golden contains a credential (%s)", secret)
		}
	}
}

func TestReadAPIKey(t *testing.T) {
	harness := newTestHarness(t)
	accounts, err := harness.Discover(context.Background())
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}

	var anthropic agentlink.AccountRef
	for _, account := range accounts {
		if account.Ref.ProviderID == "anthropic" {
			anthropic = account.Ref
		}
	}
	if anthropic.ProviderID == "" {
		t.Fatal("anthropic account not discovered")
	}

	key, err := harness.ReadAPIKey(context.Background(), anthropic)
	if err != nil {
		t.Fatalf("ReadAPIKey(reported ref): %v", err)
	}
	if want := "sk-ant-synthetic-anthropic-0001"; key != want {
		t.Fatalf("ReadAPIKey = %q, want %q", key, want)
	}

	unknown := agentlink.AccountRef{ProviderID: "anthropic", ExternalID: "key:0000000000000000"}
	if _, err := harness.ReadAPIKey(context.Background(), unknown); err == nil {
		t.Fatal("ReadAPIKey(unknown ref) returned no error")
	}
	oauth := agentlink.AccountRef{ProviderID: "openai-codex", ExternalID: "key:0000000000000000"}
	if _, err := harness.ReadAPIKey(context.Background(), oauth); err == nil {
		t.Fatal("ReadAPIKey(OAuth ref) returned no error")
	}
}

func TestCostMicro(t *testing.T) {
	tests := []struct {
		in   string
		want int64
		ok   bool
	}{
		{in: "0.0125775", want: 12578, ok: true},
		{in: "0.0000015", want: 2, ok: true},
		{in: "1e-06", want: 1, ok: true},
		{in: "1.2345678e-3", want: 1235, ok: true},
		{in: "0", want: 0, ok: true},
		{in: "-0.0000015", want: -2, ok: true},
		{in: "", ok: false},
		{in: "not-a-number", ok: false},
	}
	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			got, ok := costMicro(&sessionCost{Total: json.Number(test.in)})
			if ok != test.ok || got != test.want {
				t.Fatalf("costMicro(%q) = (%d, %v), want (%d, %v)", test.in, got, ok, test.want, test.ok)
			}
		})
	}
}

func TestSessionsGolden(t *testing.T) {
	harness := newTestHarness(t)
	batch, err := harness.Scan(context.Background(), nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(batch.Meters) != 0 {
		t.Fatalf("Scan returned %d meters, want 0", len(batch.Meters))
	}
	if len(batch.Cursors) != 2 {
		t.Fatalf("Scan returned %d cursors, want 2 (one session file, one key fingerprint)", len(batch.Cursors))
	}

	got := marshalGolden(t, batch.Usage)
	want := readGolden(t, "sessions.golden.json")
	if !bytes.Equal(got, want) {
		t.Fatalf("usage events mismatch\n got: %s\nwant: %s", got, want)
	}
	// msg-a3 ran on omp's ChatGPT login: it belongs to that subscription.
	// msg-a4 is a provider without a key or login: not reported.
	if !strings.Contains(string(got), "msg-a3") || !strings.Contains(string(got), `"provider_id": "chatgpt"`) {
		t.Fatal("the ChatGPT-login turn was not attributed to the subscription")
	}
	if strings.Contains(string(got), "msg-a4") {
		t.Fatal("a message without a key or login was reported")
	}

	again, err := harness.Scan(context.Background(), batch.Cursors)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(again.Usage) != 0 {
		t.Fatalf("second Scan returned %d events, want 0", len(again.Usage))
	}

	// Cursors saved while a key was missing: lines for that provider were
	// skipped, so a different key set must read every file again.
	stale := maps.Clone(batch.Cursors)
	stale[keysCursor] = harnesspkg.Cursor{Offset: stale[keysCursor].Offset + 1}
	rescan, err := harness.Scan(context.Background(), stale)
	if err != nil {
		t.Fatalf("Scan after key change: %v", err)
	}
	if len(rescan.Usage) != len(batch.Usage) {
		t.Fatalf("Scan after key change returned %d events, want %d", len(rescan.Usage), len(batch.Usage))
	}
}
