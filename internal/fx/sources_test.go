// SPDX-License-Identifier: SUL-1.0

package fx_test

import (
	"context"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

func TestFrankfurterGolden(t *testing.T) {
	c, _ := providertest.Setup(t, "frankfurter", nil)
	src := fx.NewFrankfurter(c)
	rates, err := src.FetchRange(context.Background(), date(2026, 10, 12), date(2026, 10, 16))
	if err != nil {
		t.Fatalf("FetchRange: %v", err)
	}
	providertest.Golden(t, "frankfurter", rates)
}

func TestECBGolden(t *testing.T) {
	c, _ := providertest.Setup(t, "ecb", nil)
	src := fx.NewECB(c)
	rates, err := src.FetchRange(context.Background(), date(2026, 10, 14), date(2026, 10, 16))
	if err != nil {
		t.Fatalf("FetchRange: %v", err)
	}
	providertest.Golden(t, "ecb", rates)
}

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
