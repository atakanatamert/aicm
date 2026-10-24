// SPDX-License-Identifier: SUL-1.0

package typesafe

import (
	"context"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestTypeSafeValidate(t *testing.T) {
	c, creds := providertest.Setup(t, "typesafe", nil)
	p := New(c)
	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if usage, quota, charges := provider.Capabilities(p); usage || quota || charges {
		t.Fatalf("capabilities: usage=%v quota=%v charges=%v, want none", usage, quota, charges)
	}
	if err := p.Validate(context.Background(), creds); err != nil {
		t.Fatal(err)
	}
}
