// SPDX-License-Identifier: SUL-1.0

package moonshot_test

import (
	"context"
	"testing"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/moonshot"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestMoonshotGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "moonshot", nil)
	p := moonshot.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if _, quota, _ := provider.Capabilities(p); !quota {
		t.Fatal("moonshot must implement provider.QuotaFetcher")
	}

	check := func(name string, creds provider.Credentials) []meter.Reading {
		t.Helper()
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
				t.Errorf("%s: reading key %q is not a declared meter", name, r.Key)
				continue
			}
			if err := r.Validate(def); err != nil {
				t.Errorf("%s: %v", name, err)
			}
		}
		return readings
	}

	global := check("global", creds)
	china := check("china", provider.Credentials{
		Secret: creds.Secret,
		Config: map[string]string{"region": "china"},
	})
	providertest.Golden(t, "moonshot", map[string]any{"global": global, "china": china})
}
