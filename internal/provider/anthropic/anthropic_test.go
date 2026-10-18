// SPDX-License-Identifier: AGPL-3.0-or-later

package anthropic_test

import (
	"context"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/provider/anthropic"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

// TestAnthropicGolden replays a recorded Admin API conversation: Validate,
// then the usage report (two pages) and the cost report (two pages).
func TestAnthropicGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "anthropic", nil)
	p := anthropic.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(ctx, creds); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 9, 20, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	usage, err := p.FetchUsage(ctx, creds, from, to)
	if err != nil {
		t.Fatal(err)
	}
	providertest.Golden(t, "anthropic", map[string]any{"usage": usage})
}
