// SPDX-License-Identifier: SUL-1.0

package fx_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
)

func TestConvert(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()

	insertRate(t, pool, "frankfurter", "USD", "2026-10-16", 1.1253)
	insertRate(t, pool, "frankfurter", "CNY", "2026-10-16", 7.524)
	insertRate(t, pool, "frankfurter", "GBP", "2026-10-01", 0.85)
	insertRate(t, pool, "ecb", "USD", "2026-10-16", 1.1253)
	insertRate(t, pool, "ecb", "JPY", "2026-10-16", 177.82)

	// Convert only needs the sources' names; it never fetches.
	svc := fx.NewService(pool, slog.New(slog.NewTextHandler(io.Discard, nil)),
		fx.NewFrankfurter(nil), fx.NewECB(nil))
	day := func(day int) time.Time {
		return time.Date(2026, 10, day, 0, 0, 0, 0, time.UTC)
	}

	t.Run("CNY to USD on Friday", func(t *testing.T) {
		got, rateDate, err := svc.Convert(ctx, money.Amount{Minor: 10000, Currency: "CNY"}, "USD", day(16), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != (money.Amount{Minor: 1496, Currency: "USD"}) {
			t.Errorf("amount = %v, want 1496 USD", got)
		}
		if !rateDate.Equal(day(16)) {
			t.Errorf("rate date = %s, want 2026-10-16", rateDate)
		}
	})

	t.Run("JPY to USD uses minor-unit exponents", func(t *testing.T) {
		got, rateDate, err := svc.Convert(ctx, money.Amount{Minor: 1000, Currency: "JPY"}, "USD", day(16), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != (money.Amount{Minor: 633, Currency: "USD"}) {
			t.Errorf("amount = %v, want 633 USD", got)
		}
		if !rateDate.Equal(day(16)) {
			t.Errorf("rate date = %s, want 2026-10-16", rateDate)
		}
	})

	t.Run("Saturday uses Friday's rate", func(t *testing.T) {
		got, rateDate, err := svc.Convert(ctx, money.Amount{Minor: 10000, Currency: "CNY"}, "USD", day(17), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != (money.Amount{Minor: 1496, Currency: "USD"}) {
			t.Errorf("amount = %v, want 1496 USD", got)
		}
		if !rateDate.Equal(day(16)) {
			t.Errorf("rate date = %s, want 2026-10-16", rateDate)
		}
	})

	t.Run("no rate within seven days", func(t *testing.T) {
		_, _, err := svc.Convert(ctx, money.Amount{Minor: 10000, Currency: "GBP"}, "USD", day(16), "frankfurter")
		if !errors.Is(err, fx.ErrNoRate) {
			t.Errorf("err = %v, want ErrNoRate", err)
		}
	})

	t.Run("preferred source falls back to the other", func(t *testing.T) {
		got, rateDate, err := svc.Convert(ctx, money.Amount{Minor: 1000, Currency: "JPY"}, "USD", day(16), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got.Minor != 633 || got.Currency != "USD" {
			t.Errorf("amount = %v, want 633 USD", got)
		}
		if !rateDate.Equal(day(16)) {
			t.Errorf("rate date = %s, want 2026-10-16", rateDate)
		}
	})

	t.Run("EUR to USD", func(t *testing.T) {
		got, rateDate, err := svc.Convert(ctx, money.Amount{Minor: 10000, Currency: "EUR"}, "USD", day(16), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != (money.Amount{Minor: 11253, Currency: "USD"}) {
			t.Errorf("amount = %v, want 11253 USD", got)
		}
		if !rateDate.Equal(day(16)) {
			t.Errorf("rate date = %s, want 2026-10-16", rateDate)
		}
	})

	t.Run("USD to EUR", func(t *testing.T) {
		got, rateDate, err := svc.Convert(ctx, money.Amount{Minor: 11253, Currency: "USD"}, "EUR", day(16), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != (money.Amount{Minor: 10000, Currency: "EUR"}) {
			t.Errorf("amount = %v, want 10000 EUR", got)
		}
		if !rateDate.Equal(day(16)) {
			t.Errorf("rate date = %s, want 2026-10-16", rateDate)
		}
	})

	t.Run("same currency is unchanged", func(t *testing.T) {
		in := money.Amount{Minor: 1234, Currency: "USD"}
		got, rateDate, err := svc.Convert(ctx, in, "USD", day(18), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != in {
			t.Errorf("amount = %v, want %v", got, in)
		}
		if !rateDate.Equal(day(18)) {
			t.Errorf("rate date = %s, want 2026-10-18", rateDate)
		}
	})

	t.Run("negative amounts round away from zero", func(t *testing.T) {
		got, _, err := svc.Convert(ctx, money.Amount{Minor: -10000, Currency: "CNY"}, "USD", day(16), "frankfurter")
		if err != nil {
			t.Fatalf("Convert: %v", err)
		}
		if got != (money.Amount{Minor: -1496, Currency: "USD"}) {
			t.Errorf("amount = %v, want -1496 USD", got)
		}
	})
}

func insertRate(t *testing.T, pool *pgxpool.Pool, source, quote, date string, rate float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO fx_rates (rate_date, base, quote, rate, source)
		 VALUES ($1::date, 'EUR', $2, $3, $4)`,
		date, quote, rate, source)
	if err != nil {
		t.Fatalf("inserting %s rate for %s on %s: %v", source, quote, date, err)
	}
}
