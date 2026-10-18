// SPDX-License-Identifier: Apache-2.0

package omp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakanatamert/aicm/internal/agentlink"
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
	if len(accounts) != 2 {
		t.Fatalf("Discover returned %d accounts, want 2", len(accounts))
	}
	got := marshalGolden(t, accounts)
	want := readGolden(t, "discover.golden.json")
	if !bytes.Equal(got, want) {
		t.Fatalf("discovered accounts mismatch\n got: %s\nwant: %s", got, want)
	}
	if bytes.Contains(want, []byte("synthetic-anthropic")) || bytes.Contains(want, []byte("synthetic-deepseek")) {
		t.Fatal("discover golden contains a synthetic key")
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
	if len(batch.Cursors) != 1 {
		t.Fatalf("Scan returned %d cursors, want 1", len(batch.Cursors))
	}

	got := marshalGolden(t, batch.Usage)
	want := readGolden(t, "sessions.golden.json")
	if !bytes.Equal(got, want) {
		t.Fatalf("usage events mismatch\n got: %s\nwant: %s", got, want)
	}
	if strings.Contains(string(got), "msg-a3") || strings.Contains(string(got), "msg-a4") {
		t.Fatal("OAuth or unknown-provider messages were reported")
	}

	again, err := harness.Scan(context.Background(), batch.Cursors)
	if err != nil {
		t.Fatalf("second Scan: %v", err)
	}
	if len(again.Usage) != 0 {
		t.Fatalf("second Scan returned %d events, want 0", len(again.Usage))
	}
}
