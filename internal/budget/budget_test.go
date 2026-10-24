// SPDX-License-Identifier: SUL-1.0

package budget_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/budget"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
)

var testNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

// testSource is a rate source that never fetches: the tests insert rates
// directly, as charge tests do.
type testSource struct{ name string }

func (s testSource) Name() string { return s.name }

func (s testSource) FetchRange(context.Context, time.Time, time.Time) ([]fx.Rate, error) {
	return nil, nil
}

func newService(t *testing.T) (*budget.Service, *pgxpool.Pool, string, auth.Principal) {
	t.Helper()
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	p := auth.Principal{OrgID: orgID, Role: auth.RoleAdmin, Kind: auth.ActorSystem, Label: "test"}
	return budget.NewService(pool, fx.NewService(pool, log, testSource{"frankfurter"}, testSource{"ecb"})), pool, orgID, p
}

func insertAccount(t *testing.T, pool *pgxpool.Pool, orgID, name, currency string, price *int64, renewsOn *time.Time) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency, price_minor, billing_cycle, auto_renew, renews_on)
		VALUES ($1, 'manual', $2, 'none', $3, $4, CASE WHEN $4::bigint IS NULL THEN 'none' ELSE 'monthly' END, $5, $6)
		RETURNING id`, orgID, name, currency, price, price != nil, renewsOn).Scan(&id)
	if err != nil {
		t.Fatalf("inserting account: %v", err)
	}
	return id
}

func insertCharge(t *testing.T, pool *pgxpool.Pool, orgID, accountID, kind, currency string, minor int64, on time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO charges (org_id, account_id, kind, status, source, amount_minor, currency, occurred_on)
		VALUES ($1, $2, $3, 'confirmed', 'manual', $4, $5, $6)`,
		orgID, accountID, kind, minor, currency, on)
	if err != nil {
		t.Fatalf("inserting charge: %v", err)
	}
}

func insertRate(t *testing.T, pool *pgxpool.Pool, on time.Time, quote string, rate float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO fx_rates (rate_date, base, quote, rate, source)
		VALUES ($1, 'EUR', $2, $3, 'frankfurter')`, on, quote, rate)
	if err != nil {
		t.Fatalf("inserting rate: %v", err)
	}
}

// TestStatus checks month-to-date conversion per charge date, refunds,
// unconverted charges, the renewal projection and percent_used.
func TestStatus(t *testing.T) {
	svc, pool, orgID, p := newService(t)
	ctx := context.Background()

	renews := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	price := int64(1500)
	accountID := insertAccount(t, pool, orgID, "Claude Max", "USD", &price, &renews)

	// Two EUR charges on different days prove each uses its own date's rate.
	insertRate(t, pool, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC), "USD", 1.1)
	insertRate(t, pool, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC), "USD", 1.2)

	insertCharge(t, pool, orgID, accountID, "subscription", "USD", 3000, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	insertCharge(t, pool, orgID, accountID, "topup", "EUR", 2000, time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC))
	insertCharge(t, pool, orgID, accountID, "refund", "USD", 500, time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))
	insertCharge(t, pool, orgID, accountID, "invoice", "GBP", 1000, time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC))
	insertCharge(t, pool, orgID, accountID, "topup", "EUR", 1000, time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC))

	if _, err := svc.Put(ctx, p, budget.Input{
		AccountID: &accountID,
		Amount:    money.Amount{Minor: 10000, Currency: "USD"},
	}); err != nil {
		t.Fatalf("putting budget: %v", err)
	}

	statuses, err := svc.Status(ctx, orgID, testNow)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(statuses) != 1 {
		t.Fatalf("got %d budgets, want 1", len(statuses))
	}
	st := statuses[0]
	// 3000 USD + 2000 EUR at 1.1 (2200) + 1000 EUR at 1.2 (1200) - 500 refund.
	if want := int64(5900); st.MonthToDate.Minor != want || st.MonthToDate.Currency != "USD" {
		t.Fatalf("month_to_date: got %s, want 5900 minor USD", st.MonthToDate)
	}
	if st.UnconvertedCount != 1 {
		t.Fatalf("unconverted_count: got %d, want 1 (the GBP invoice)", st.UnconvertedCount)
	}
	if want := int64(7400); st.Projected.Minor != want {
		t.Fatalf("projected: got %d minor, want %d (month-to-date plus the Oct 28 renewal)", st.Projected.Minor, want)
	}
	if want := 59.0; st.PercentUsed != want {
		t.Fatalf("percent_used: got %v, want %v", st.PercentUsed, want)
	}
	if st.Period != "2026-10" {
		t.Fatalf("period: got %q, want 2026-10", st.Period)
	}
	if st.AccountName != "Claude Max" {
		t.Fatalf("account name: got %q", st.AccountName)
	}
}

// TestProjectionSkipsNextMonth checks that a renewal after this month does
// not inflate the projection.
func TestProjectionSkipsNextMonth(t *testing.T) {
	svc, pool, orgID, p := newService(t)
	ctx := context.Background()

	nextMonth := time.Date(2026, 11, 3, 0, 0, 0, 0, time.UTC)
	price := int64(1500)
	accountID := insertAccount(t, pool, orgID, "Claude Max", "USD", &price, &nextMonth)
	insertCharge(t, pool, orgID, accountID, "subscription", "USD", 3000, time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))

	if _, err := svc.Put(ctx, p, budget.Input{
		AccountID: &accountID,
		Amount:    money.Amount{Minor: 10000, Currency: "USD"},
	}); err != nil {
		t.Fatalf("putting budget: %v", err)
	}

	statuses, err := svc.Status(ctx, orgID, testNow)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if want := int64(3000); statuses[0].Projected.Minor != want {
		t.Fatalf("projected: got %d, want %d (no renewal due this month)", statuses[0].Projected.Minor, want)
	}
}
