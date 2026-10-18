// SPDX-License-Identifier: AGPL-3.0-or-later

package registry

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

// TestMarkdownGolden pins the README table. Every provider in the registry
// must get a row; -update rewrites the golden after an intended change.
func TestMarkdownGolden(t *testing.T) {
	ps := All(nil)
	got := Markdown(ps)
	for _, p := range ps {
		if !strings.Contains(got, p.Info().Name) {
			t.Errorf("provider %q is missing from the table", p.Info().ID)
		}
	}

	path := filepath.Join("testdata", "providers.golden.md")
	if *updateGolden {
		if err := os.MkdirAll("testdata", 0o750); err != nil {
			t.Fatalf("creating testdata: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("writing golden: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading golden (create it with -update): %v", err)
	}
	if !bytes.Equal(want, []byte(got)) {
		t.Errorf("table differs from %s (rerun with -update if intended)\n--- got\n%s\n--- want\n%s", path, got, want)
	}
}

// stub is a catalog entry that only carries Info.
type stub struct{ info provider.Info }

func (s stub) Info() provider.Info { return s.info }

func (stub) Validate(context.Context, provider.Credentials) error { return nil }

// TestMarkdownEscapesPipes keeps values from splitting a row.
func TestMarkdownEscapesPipes(t *testing.T) {
	got := Markdown([]provider.Provider{stub{info: provider.Info{
		ID:        "stub",
		Name:      "A | B",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthNone},
		Meters: []meter.Def{
			{Key: "m", Label: "Left | Right", Unit: meter.UnitPercent, ResetKind: meter.ResetNever},
		},
		Links: provider.Links{Billing: "https://example.com/?a=1|2"},
	}}})
	for _, want := range []string{`A \| B`, `Left \| Right`, `a=1\|2`} {
		if !strings.Contains(got, want) {
			t.Errorf("table does not contain %q:\n%s", want, got)
		}
	}
	if !strings.HasSuffix(got, "\n") {
		t.Errorf("table does not end with a newline: %q", got)
	}
}
