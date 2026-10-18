// SPDX-License-Identifier: AGPL-3.0-or-later

package pricing_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/atakanatamert/aicm/internal/pricing"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestParseLiteLLMGolden(t *testing.T) {
	f, err := os.Open(filepath.Join("testdata", "litellm.fixture.json"))
	if err != nil {
		t.Fatalf("opening fixture: %v", err)
	}
	defer f.Close()

	prices, err := pricing.ParseLiteLLM(f)
	if err != nil {
		t.Fatalf("parsing fixture: %v", err)
	}
	providertest.Golden(t, "litellm", prices)
}
