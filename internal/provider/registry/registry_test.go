// SPDX-License-Identifier: SUL-1.0

package registry

import (
	"io"
	"log/slog"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
)

// TestCatalog checks every catalog entry: valid Info, unique ids, and that
// an adapter that fetches quota declares at least one meter.
func TestCatalog(t *testing.T) {
	ps := All(provider.NewHTTPClient(slog.New(slog.NewTextHandler(io.Discard, nil)), nil))
	seen := map[string]bool{}
	for _, p := range ps {
		info := p.Info()
		if err := info.Validate(); err != nil {
			t.Error(err)
		}
		if seen[info.ID] {
			t.Errorf("duplicate provider id %q", info.ID)
		}
		seen[info.ID] = true
		if _, quota, _ := provider.Capabilities(p); quota && len(info.Meters) == 0 {
			t.Errorf("provider %q fetches quota but declares no meters", info.ID)
		}
	}
}
