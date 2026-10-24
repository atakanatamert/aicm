// SPDX-License-Identifier: SUL-1.0

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

// An inference key cannot read /activity (403). The balance must still sync
// and the 403 must not read as a rejected key.
func TestOpenRouterInferenceKey(t *testing.T) {
	c, creds := providertest.Setup(t, "openrouter_inference_key", nil)
	p := New(c)
	ctx := context.Background()

	if err := p.Validate(ctx, creds); err != nil {
		t.Fatal(err)
	}
	quota, err := p.FetchQuota(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	if len(quota) == 0 {
		t.Fatal("no quota readings")
	}
	usage, err := p.FetchUsage(ctx, creds, time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 18, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("FetchUsage with an inference key: %v", err)
	}
	if len(usage) != 0 {
		t.Fatalf("FetchUsage returned %d records, want 0", len(usage))
	}
}
