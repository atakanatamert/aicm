// SPDX-License-Identifier: SUL-1.0

package xai_test

import (
	"context"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
	"github.com/atakanatamert/aicm/internal/provider/xai"
)

const testTeamID = "65c1e471-205f-4566-9c5a-07198bcdf4ce"

func TestXAIGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "xai", map[string]string{"team_id": testTeamID})
	p := xai.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	usageOK, quotaOK, chargesOK := provider.Capabilities(p)
	if !usageOK || !quotaOK || !chargesOK {
		t.Fatalf("xAI must implement usage, quota and charge fetchers, got usage=%v quota=%v charges=%v", usageOK, quotaOK, chargesOK)
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
			t.Errorf("reading key %q is not a declared meter", r.Key)
			continue
		}
		if err := r.Validate(def); err != nil {
			t.Error(err)
		}
	}

	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	usage, err := p.FetchUsage(ctx, creds, from, from.AddDate(0, 0, 3))
	if err != nil {
		t.Fatal(err)
	}

	charges, err := p.FetchCharges(ctx, creds, from)
	if err != nil {
		t.Fatal(err)
	}

	providertest.Golden(t, "xai", map[string]any{
		"quota":   quota,
		"usage":   usage,
		"charges": charges,
	})
}
