// SPDX-License-Identifier: SUL-1.0

package charge

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/quota"
)

type testSource struct{ name string }

func (s testSource) Name() string { return s.name }

func (s testSource) FetchRange(context.Context, time.Time, time.Time) ([]fx.Rate, error) {
	return nil, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(t *testing.T) (*Service, *pgxpool.Pool, string) {
	t.Helper()
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	fxSvc := fx.NewService(pool, testLogger(), testSource{"frankfurter"}, testSource{"ecb"})
	return NewService(pool, fxSvc, testLogger()), pool, orgID
}

func admin(orgID string) auth.Principal {
	return auth.Principal{
		OrgID: orgID, Role: auth.RoleAdmin, Kind: auth.ActorSystem, Label: "test",
	}
}

func insertAccount(t *testing.T, pool *pgxpool.Pool, orgID, name, currency string, price *int64, cycle string, renewsOn *time.Time, anchor *int16) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency, price_minor, billing_cycle, renews_on, renewal_anchor_day)
VALUES ($1, 'anthropic', $2, 'none', $3, $4, $5, $6, $7)
RETURNING id`, orgID, name, currency, price, cycle, renewsOn, anchor).Scan(&id)
	if err != nil {
		t.Fatalf("inserting account: %v", err)
	}
	return id
}

func insertRate(t *testing.T, pool *pgxpool.Pool, day time.Time, quote string, rate float64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
INSERT INTO fx_rates (rate_date, base, quote, rate, source)
VALUES ($1, 'EUR', $2, $3, 'frankfurter')`, day, quote, rate)
	if err != nil {
		t.Fatalf("inserting rate: %v", err)
	}
}

func createCharge(t *testing.T, svc *Service, p auth.Principal, accountID, kind string, minor int64, currency string, on time.Time) db.Charge {
	t.Helper()
	c, err := svc.Create(context.Background(), p, CreateInput{
		AccountID: accountID, Kind: kind,
		Amount:     money.Amount{Minor: minor, Currency: currency},
		OccurredOn: on,
	})
	if err != nil {
		t.Fatalf("creating %s charge: %v", kind, err)
	}
	return c
}

func billedMap(amounts []money.Amount) map[string]int64 {
	out := make(map[string]int64, len(amounts))
	for _, a := range amounts {
		out[a.Currency] = a.Minor
	}
	return out
}

func day(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func spendRequest(orgID string, period string, from, to time.Time) SpendRequest {
	return SpendRequest{
		OrgID: orgID, UserID: "00000000-0000-0000-0000-000000000000", IsAdmin: true,
		Period: period, From: from, To: to, GroupBy: "provider",
		Currency: "USD", FXSource: "frankfurter",
	}
}

func TestSpendConvertsEachChargeAtItsDate(t *testing.T) {
	svc, pool, orgID := newTestService(t)
	accountID := insertAccount(t, pool, orgID, "Main", "CNY", nil, "none", nil, nil)
	p := admin(orgID)

	insertRate(t, pool, day(2026, time.October, 5), "CNY", 8.0)
	insertRate(t, pool, day(2026, time.October, 5), "USD", 1.10)
	insertRate(t, pool, day(2026, time.October, 20), "CNY", 7.5)
	insertRate(t, pool, day(2026, time.October, 20), "USD", 1.20)

	createCharge(t, svc, p, accountID, "subscription", 10000, "CNY", day(2026, time.October, 5))
	createCharge(t, svc, p, accountID, "topup", 5000, "CNY", day(2026, time.October, 20))
	createCharge(t, svc, p, accountID, "invoice", 2000, "USD", day(2026, time.October, 10))

	report, err := svc.Spend(context.Background(), spendRequest(orgID, "month", day(2026, time.October, 1), day(2026, time.October, 31)))
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(report.Rows))
	}
	row := report.Rows[0]
	if row.Period != "2026-10" {
		t.Errorf("period = %q, want 2026-10", row.Period)
	}
	billed := billedMap(row.Billed)
	if billed["CNY"] != 15000 || billed["USD"] != 2000 || len(billed) != 2 {
		t.Errorf("billed = %v, want CNY 15000 and USD 2000", billed)
	}
	// 10000 CNY × 1.10/8.00 = 1375 USD
	//  5000 CNY × 1.20/7.50 =  800 USD
	//                  +2000 USD = 4175 USD
	if row.Converted == nil || row.Converted.Minor != 4175 || row.Converted.Currency != "USD" {
		t.Errorf("converted = %v, want 4175 USD", row.Converted)
	}
	if row.UnconvertedCount != 0 {
		t.Errorf("unconverted = %d, want 0", row.UnconvertedCount)
	}
	if len(report.Totals) != 1 || report.Totals[0].Converted == nil || report.Totals[0].Converted.Minor != 4175 {
		t.Errorf("totals = %+v, want one 4175 USD total", report.Totals)
	}
}

func TestSpendMissingRate(t *testing.T) {
	svc, pool, orgID := newTestService(t)
	accountID := insertAccount(t, pool, orgID, "Main", "CNY", nil, "none", nil, nil)
	p := admin(orgID)

	// The USD rate exists, but no CNY rate is within 7 days of the charge.
	insertRate(t, pool, day(2026, time.June, 15), "USD", 1.10)
	createCharge(t, svc, p, accountID, "topup", 10000, "CNY", day(2026, time.June, 15))

	report, err := svc.Spend(context.Background(), spendRequest(orgID, "month", day(2026, time.June, 1), day(2026, time.June, 30)))
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(report.Rows))
	}
	row := report.Rows[0]
	if row.Converted != nil {
		t.Errorf("converted = %v, want absent", row.Converted)
	}
	if row.UnconvertedCount != 1 {
		t.Errorf("unconverted = %d, want 1", row.UnconvertedCount)
	}
	if billed := billedMap(row.Billed); billed["CNY"] != 10000 {
		t.Errorf("billed = %v, want CNY 10000", billed)
	}
}

func TestSpendRefund(t *testing.T) {
	svc, pool, orgID := newTestService(t)
	accountID := insertAccount(t, pool, orgID, "Main", "CNY", nil, "none", nil, nil)
	p := admin(orgID)

	insertRate(t, pool, day(2026, time.October, 5), "CNY", 8.0)
	insertRate(t, pool, day(2026, time.October, 5), "USD", 1.10)
	createCharge(t, svc, p, accountID, "subscription", 10000, "CNY", day(2026, time.October, 5))
	createCharge(t, svc, p, accountID, "refund", 4000, "CNY", day(2026, time.October, 5))

	report, err := svc.Spend(context.Background(), spendRequest(orgID, "month", day(2026, time.October, 1), day(2026, time.October, 31)))
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if len(report.Rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(report.Rows))
	}
	row := report.Rows[0]
	if billed := billedMap(row.Billed); billed["CNY"] != 6000 {
		t.Errorf("billed = %v, want CNY 6000", billed)
	}
	// 10000 CNY × 1.10/8.00 = 1375; 4000 CNY × 1.10/8.00 = 550; 1375−550 = 825.
	if row.Converted == nil || row.Converted.Minor != 825 {
		t.Errorf("converted = %v, want 825 USD", row.Converted)
	}
}

func TestSpendYearBuckets(t *testing.T) {
	svc, pool, orgID := newTestService(t)
	accountID := insertAccount(t, pool, orgID, "Main", "USD", nil, "none", nil, nil)
	p := admin(orgID)

	createCharge(t, svc, p, accountID, "topup", 1000, "USD", day(2025, time.May, 1))
	createCharge(t, svc, p, accountID, "topup", 2000, "USD", day(2026, time.May, 1))

	report, err := svc.Spend(context.Background(), spendRequest(orgID, "year", day(2025, time.January, 1), day(2026, time.December, 31)))
	if err != nil {
		t.Fatalf("spend: %v", err)
	}
	if len(report.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(report.Rows))
	}
	if len(report.Totals) != 2 {
		t.Fatalf("got %d totals, want 2", len(report.Totals))
	}
	if report.Rows[0].Period != "2025" || report.Rows[1].Period != "2026" {
		t.Errorf("periods = %q, %q, want 2025, 2026", report.Rows[0].Period, report.Rows[1].Period)
	}
	if billed := billedMap(report.Rows[0].Billed); billed["USD"] != 1000 {
		t.Errorf("2025 billed = %v, want USD 1000", billed)
	}
	if billed := billedMap(report.Rows[1].Billed); billed["USD"] != 2000 {
		t.Errorf("2026 billed = %v, want USD 2000", billed)
	}
}

func TestRenewalHookIdempotent(t *testing.T) {
	svc, pool, orgID := newTestService(t)
	price := int64(2000)
	renewsOn := day(2026, time.September, 15)
	anchor := int16(15)
	accountID := insertAccount(t, pool, orgID, "Main", "USD", &price, "monthly", &renewsOn, &anchor)

	q := db.New(pool)
	accountRow, err := q.GetVisibleAccount(context.Background(), db.GetVisibleAccountParams{
		OrgID: orgID, AccountID: accountID,
		UserID: "00000000-0000-0000-0000-000000000000", IsAdmin: true,
	})
	if err != nil {
		t.Fatalf("loading account: %v", err)
	}
	hook := svc.RenewalHook()
	if err := hook(context.Background(), q, accountRow, renewsOn); err != nil {
		t.Fatalf("first renewal: %v", err)
	}
	if err := hook(context.Background(), q, accountRow, renewsOn); err != nil {
		t.Fatalf("second renewal: %v", err)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `
SELECT count(*) FROM charges WHERE account_id = $1 AND source = 'renewal'`, accountID).Scan(&count); err != nil {
		t.Fatalf("counting charges: %v", err)
	}
	if count != 1 {
		t.Fatalf("got %d renewal charges, want 1", count)
	}
	var amount int64
	var currency, kind, status string
	var start, end time.Time
	if err := pool.QueryRow(context.Background(), `
SELECT amount_minor, currency, kind, status, period_start, period_end
FROM charges WHERE account_id = $1 AND source = 'renewal'`, accountID).Scan(&amount, &currency, &kind, &status, &start, &end); err != nil {
		t.Fatalf("loading renewal charge: %v", err)
	}
	if amount != price || currency != "USD" || kind != "subscription" || status != "confirmed" {
		t.Errorf("charge = %d %s %s %s, want %d USD subscription confirmed", amount, currency, kind, status, price)
	}
	if !start.Equal(renewsOn) || !end.Equal(day(2026, time.October, 15)) {
		t.Errorf("period = %s..%s, want 2026-09-15..2026-10-15", start.Format(time.DateOnly), end.Format(time.DateOnly))
	}
}

func TestTopUpDetection(t *testing.T) {
	svc, pool, orgID := newTestService(t)
	accountID := insertAccount(t, pool, orgID, "Wallet", "CNY", nil, "none", nil, nil)
	grantedAccountID := insertAccount(t, pool, orgID, "Granted", "CNY", nil, "none", nil, nil)

	quotaSvc := quota.NewService(pool, testLogger(), nil, nil)
	quotaSvc.SetBalanceHook(svc.TopUpHook())

	base := day(2026, time.October, 1)
	readings := balanceReadings("balance", base, 5000, 4800, 9800, 9700)
	wallet := meter.Def{Key: "balance", Label: "Balance", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever}
	if _, err := quotaSvc.RecordReadings(context.Background(), orgID, accountID, wallet, readings, nil); err != nil {
		t.Fatalf("recording balance readings: %v", err)
	}

	granted := meter.Def{Key: "granted_cny", Label: "Granted", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever}
	if _, err := quotaSvc.RecordReadings(context.Background(), orgID, grantedAccountID, granted, balanceReadings("granted_cny", base, 5000, 4800, 9800, 9700), nil); err != nil {
		t.Fatalf("recording granted readings: %v", err)
	}

	rows, err := pool.Query(context.Background(), `
SELECT account_id, amount_minor, currency, kind, status, source, occurred_on, description
FROM charges WHERE org_id = $1 AND kind = 'topup'`, orgID)
	if err != nil {
		t.Fatalf("listing charges: %v", err)
	}
	defer rows.Close()
	var got int
	for rows.Next() {
		var gotAccount, currency, kind, status, source, description string
		var amount int64
		var occurred time.Time
		if err := rows.Scan(&gotAccount, &amount, &currency, &kind, &status, &source, &occurred, &description); err != nil {
			t.Fatalf("scanning charge: %v", err)
		}
		got++
		if gotAccount != accountID || amount != 5000 || currency != "CNY" || kind != "topup" || status != "suggested" || source != "detected" {
			t.Errorf("unexpected top-up: account=%s amount=%d %s %s %s %s", gotAccount, amount, currency, kind, status, source)
		}
		if !occurred.Equal(day(2026, time.October, 1)) {
			t.Errorf("occurred_on = %s, want 2026-10-01", occurred.Format(time.DateOnly))
		}
		if description != "Balance rose from 48.00 to 98.00" {
			t.Errorf("description = %q", description)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating charges: %v", err)
	}
	if got != 1 {
		t.Fatalf("got %d top-up charges, want 1", got)
	}
}

func balanceReadings(key string, base time.Time, remaining ...float64) []meter.Reading {
	out := make([]meter.Reading, len(remaining))
	for i, r := range remaining {
		out[i] = meter.Reading{
			Key: key, Remaining: new(r), CapturedAt: base.Add(time.Duration(i) * time.Hour),
			Source: meter.SourceManual,
		}
	}
	return out
}
