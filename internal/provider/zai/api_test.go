// SPDX-License-Identifier: AGPL-3.0-or-later

package zai_test

import (
	"context"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
	"github.com/atakanatamert/aicm/internal/provider/zai"
)

func TestZaiAPIGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "zai_api", map[string]string{"region": "global"})
	p := zai.NewAPI(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if usage, quota, charges := provider.Capabilities(p); usage || !quota || charges {
		t.Fatalf("capabilities: usage=%v quota=%v charges=%v", usage, quota, charges)
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
	providertest.Golden(t, "zai_api", map[string]any{"quota": quota})
}
