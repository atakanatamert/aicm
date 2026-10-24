// SPDX-License-Identifier: SUL-1.0

package zai_test

import (
	"context"
	"errors"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
	"github.com/atakanatamert/aicm/internal/provider/zai"
)

func TestZaiGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "zai", map[string]string{"region": "global"})
	p := zai.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if usage, quota, charges := provider.Capabilities(p); usage || !quota || charges {
		t.Fatal("zai must implement provider.QuotaFetcher only")
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

	creds.AllowUnofficial = false
	if _, err := p.FetchQuota(ctx, creds); !errors.Is(err, provider.ErrUnofficialNotAllowed) {
		t.Errorf("without the opt-in, FetchQuota error = %v, want ErrUnofficialNotAllowed", err)
	}

	providertest.Golden(t, "zai", readings)
}
