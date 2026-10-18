// SPDX-License-Identifier: AGPL-3.0-or-later

package openrouter

import (
	"context"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestOpenRouterGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "openrouter", nil)
	p := New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(ctx, creds); err != nil {
		t.Fatal(err)
	}

	quota, err := p.FetchQuota(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range quota {
		def, ok := p.Info().Meter(r.Key)
		if !ok {
			t.Fatalf("reading for unknown meter %q", r.Key)
		}
		if err := r.Validate(def); err != nil {
			t.Error(err)
		}
	}

	from := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC)
	usage, err := p.FetchUsage(ctx, creds, from, to)
	if err != nil {
		t.Fatal(err)
	}

	providertest.Golden(t, "openrouter", map[string]any{"quota": quota, "usage": usage})
}
