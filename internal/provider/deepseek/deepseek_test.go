// SPDX-License-Identifier: SUL-1.0

package deepseek_test

import (
	"context"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/deepseek"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestDeepSeekGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "deepseek", nil)
	p := deepseek.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if _, quota, _ := provider.Capabilities(p); !quota {
		t.Fatal("deepseek must implement provider.QuotaFetcher")
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
	providertest.Golden(t, "deepseek", readings)
}
