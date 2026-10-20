// SPDX-License-Identifier: AGPL-3.0-or-later

package googleaistudio

import (
	"context"
	"testing"

	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestGoogleAIStudioValidate(t *testing.T) {
	c, creds := providertest.Setup(t, "googleaistudio", nil)
	p := New(c)
	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if usage, quota, charges := provider.Capabilities(p); usage || quota || charges {
		t.Fatalf("capabilities: usage=%v quota=%v charges=%v, want none", usage, quota, charges)
	}
	if creds.Secret == "" {
		creds.Secret = "synthetic-key"
	}
	if err := p.Validate(context.Background(), creds); err != nil {
		t.Fatal(err)
	}
}
