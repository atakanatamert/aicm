// SPDX-License-Identifier: SUL-1.0

package openai_test

import (
	"context"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/provider/openai"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestOpenAIGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "openai", nil)
	p := openai.New(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(ctx, creds); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 15, 0, 0, 0, 0, time.UTC)
	usage, err := p.FetchUsage(ctx, creds, from, from.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	providertest.Golden(t, "openai", map[string]any{"usage": usage})
}
