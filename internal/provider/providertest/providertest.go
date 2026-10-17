// SPDX-License-Identifier: AGPL-3.0-or-later

// Package providertest runs adapter golden tests: a recorded fixture goes in,
// the adapter's records come out and are compared with a golden JSON file.
//
// Replay (default): requests are answered from testdata/<name>.fixture.json,
// the clock is pinned to the recording time and Credentials.Secret is
// provider.Redacted.
//
// Record: AICM_RECORD=1 AICM_RECORD_SECRET=<real key> go test ./internal/provider/<id>/
// calls the live API and rewrites the fixture with the key redacted. Review
// the fixture for account ids or emails before committing it.
//
// Update goldens after an intended change: go test ./internal/provider/<id>/ -update
package providertest

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/provider"
)

var update = flag.Bool("update", false, "rewrite golden files")

// Setup returns an HTTP client and credentials for the fixture
// testdata/<name>.fixture.json. config is passed through as
// Credentials.Config; keep it free of secrets because it appears in URLs.
func Setup(t *testing.T, name string, config map[string]string) (*provider.HTTPClient, provider.Credentials) {
	t.Helper()
	path := filepath.Join("testdata", name+".fixture.json")
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	creds := provider.Credentials{Config: config, AllowUnofficial: true}

	if os.Getenv("AICM_RECORD") == "1" {
		secret := os.Getenv("AICM_RECORD_SECRET")
		rec := provider.NewRecorder(nil, secret)
		t.Cleanup(func() {
			if err := os.MkdirAll("testdata", 0o750); err != nil {
				t.Errorf("creating testdata: %v", err)
				return
			}
			if err := rec.Save(path); err != nil {
				t.Errorf("saving fixture: %v", err)
			}
		})
		creds.Secret = secret
		return provider.NewHTTPClient(log, rec), creds
	}

	rep, err := provider.LoadReplayer(path)
	if err != nil {
		t.Fatalf("loading fixture (record it with AICM_RECORD=1): %v", err)
	}
	t.Cleanup(func() {
		if n := rep.Unused(); n > 0 && !t.Failed() {
			t.Errorf("fixture %s: %d recorded interactions were never requested", path, n)
		}
	})
	c := provider.NewHTTPClient(log, rep)
	recordedAt := rep.RecordedAt()
	c.Now = func() time.Time { return recordedAt }
	c.Sleep = func(_ context.Context, _ time.Duration) error { return nil }
	creds.Secret = provider.Redacted
	return c, creds
}

// Golden compares got, encoded as indented JSON, with
// testdata/<name>.golden.json. With -update it rewrites the file instead.
func Golden(t *testing.T, name string, got any) {
	t.Helper()
	path := filepath.Join("testdata", name+".golden.json")
	b, err := json.MarshalIndent(got, "", "  ")
	if err != nil {
		t.Fatalf("encoding result: %v", err)
	}
	b = append(b, '\n')
	if *update || os.Getenv("AICM_RECORD") == "1" {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}
		if err := os.WriteFile(path, b, 0o600); err != nil {
			t.Fatalf("writing golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path) //nolint:gosec // path built from testdata and a test-chosen name
	if err != nil {
		t.Fatalf("reading golden (create it with -update): %v", err)
	}
	if !bytes.Equal(want, b) {
		t.Errorf("result differs from %s (rerun with -update if intended)\n--- got\n%s\n--- want\n%s", path, b, want)
	}
}
