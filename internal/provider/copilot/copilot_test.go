// SPDX-License-Identifier: AGPL-3.0-or-later

package copilot_test

import (
	"context"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/copilot"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestCopilotGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "copilot", map[string]string{"username": "octocat", "plan": "pro_plus"})
	p := copilot.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if usage, quota, charges := provider.Capabilities(p); usage || !quota || charges {
		t.Fatal("copilot must implement provider.QuotaFetcher only")
	}
	if err := p.Validate(ctx, creds); err != nil {
		t.Fatal(err)
	}
	readings, err := p.FetchQuota(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range readings {
		def, ok := p.Info().Meter(r.Key)
		if !ok {
			t.Errorf("reading key %q is not a declared meter", r.Key)
			continue
		}
		if err := r.Validate(def); err != nil {
			t.Error(err)
		}
	}

	providertest.Golden(t, "copilot", readings)
}
