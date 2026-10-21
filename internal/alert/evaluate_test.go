// SPDX-License-Identifier: AGPL-3.0-or-later

package alert_test

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/alert"
	"github.com/atakanatamert/aicm/internal/budget"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/quota"
)

var testNow = time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)

func newService(t *testing.T) (*alert.Service, *pgxpool.Pool, string) {
	t.Helper()
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	fxSvc := fx.NewService(pool, log)
	budgets := budget.NewService(pool, fxSvc)
	return alert.NewService(pool, log, quota.NewService(pool, log, nil, nil), nil, budgets), pool, orgID
}

// plainAccount inserts an active account without a subscription.
func plainAccount(t *testing.T, pool *pgxpool.Pool, orgID, name string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency)
		VALUES ($1, 'manual', $2, 'none', 'USD')
		RETURNING id`, orgID, name).Scan(&id)
	if err != nil {
		t.Fatalf("inserting account: %v", err)
	}
	return id
}

// renewalAccount inserts an active monthly account with a renewal date.
func renewalAccount(t *testing.T, pool *pgxpool.Pool, orgID, name string, renewsOn time.Time, autoRenew bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (
			org_id, provider_id, name, auth_kind, currency,
			billing_cycle, price_minor, renews_on, auto_renew
		) VALUES ($1, 'manual', $2, 'none', 'USD', 'monthly', 20000, $3, $4)
		RETURNING id`, orgID, name, renewsOn, autoRenew).Scan(&id)
	if err != nil {
		t.Fatalf("inserting renewal account: %v", err)
	}
	return id
}

func insertMeter(t *testing.T, pool *pgxpool.Pool, orgID, accountID, key string, limit *float64) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO quota_meters (
			org_id, account_id, key, label, unit, reset_kind, limit_value
		) VALUES ($1, $2, $3, $4, 'percent', 'fixed', $5)
		RETURNING id`, orgID, accountID, key, key, limit).Scan(&id)
	if err != nil {
		t.Fatalf("inserting meter: %v", err)
	}
	return id
}

func insertReading(t *testing.T, pool *pgxpool.Pool, orgID, meterID string, used *float64, resetsAt *time.Time, captured time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO quota_readings (
			org_id, meter_id, used, resets_at, captured_at, source
		) VALUES ($1, $2, $3, $4, $5, 'manual')`,
		orgID, meterID, used, resetsAt, captured)
	if err != nil {
		t.Fatalf("inserting reading: %v", err)
	}
}

func insertMachine(t *testing.T, pool *pgxpool.Pool, orgID, name string, lastSeen time.Time, revokedAt *time.Time) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO machines (
			org_id, name, hostname, fingerprint, os, arch, agent_version,
			token_hash, last_seen_at, revoked_at
		) VALUES (
			$1, $2, $2, $2, 'linux', 'arm64', 'test',
			convert_to(gen_random_uuid()::text, 'UTF8'), $3, $4
		) RETURNING id`, orgID, name, lastSeen, revokedAt).Scan(&id)
	if err != nil {
		t.Fatalf("inserting machine: %v", err)
	}
	return id
}

func countAlerts(t *testing.T, pool *pgxpool.Pool, orgID, kind string) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM alerts WHERE org_id = $1 AND kind = $2`, orgID, kind).Scan(&n)
	if err != nil {
		t.Fatalf("counting alerts: %v", err)
	}
	return n
}

func TestRenewalAlerts(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()
	today := time.Date(testNow.Year(), testNow.Month(), testNow.Day(), 0, 0, 0, 0, time.UTC)

	renewing := renewalAccount(t, pool, orgID, "Claude Max", today.AddDate(0, 0, 7), true)
	renewalAccount(t, pool, orgID, "No Auto", today.AddDate(0, 0, 7), false)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("first evaluation: got %d new alerts, want 1", n)
	}
	if got := countAlerts(t, pool, orgID, "renewal"); got != 1 {
		t.Fatalf("after first evaluation: got %d renewal alerts, want 1", got)
	}

	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("same-time evaluation: got %d new alerts, want 0", n)
	}

	n, err = svc.Evaluate(ctx, testNow.AddDate(0, 0, 1)) // 6 days before
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("six days before: got %d new alerts, want 0", n)
	}

	n, err = svc.Evaluate(ctx, testNow.AddDate(0, 0, 6)) // 1 day before
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("one day before: got %d new alerts, want 1", n)
	}

	var severity string
	err = pool.QueryRow(ctx,
		`SELECT severity FROM alerts WHERE org_id = $1 AND kind = 'renewal' AND event_key LIKE $2`,
		orgID, "%:1d").Scan(&severity)
	if err != nil {
		t.Fatal(err)
	}
	if severity != "critical" {
		t.Fatalf("one-day renewal severity: got %q, want critical", severity)
	}
	if got := countAlerts(t, pool, orgID, "renewal"); got != 2 {
		t.Fatalf("total renewal alerts: got %d, want 2", got)
	}

	var other int
	err = pool.QueryRow(ctx,
		`SELECT count(*) FROM alerts WHERE org_id = $1 AND account_id <> $2 AND kind = 'renewal'`,
		orgID, renewing).Scan(&other)
	if err != nil {
		t.Fatal(err)
	}
	if other != 0 {
		t.Fatalf("auto_renew=false account produced %d alerts, want 0", other)
	}
}

func TestPerAccountOverride(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()
	today := time.Date(testNow.Year(), testNow.Month(), testNow.Day(), 0, 0, 0, 0, time.UTC)

	silent := renewalAccount(t, pool, orgID, "Silent", today.AddDate(0, 0, 7), true)
	loud := renewalAccount(t, pool, orgID, "Loud", today.AddDate(0, 0, 7), true)

	_, err := pool.Exec(ctx, `
		INSERT INTO alert_rules (org_id, account_id, kind, enabled, params)
		VALUES ($1, $2, 'renewal', false, '{"days_before":[7,1]}')`, orgID, silent)
	if err != nil {
		t.Fatalf("inserting account rule: %v", err)
	}

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d new alerts, want 1", n)
	}
	if got := countAlerts(t, pool, orgID, "renewal"); got != 1 {
		t.Fatalf("got %d renewal alerts, want 1", got)
	}
	var accountID string
	if err := pool.QueryRow(ctx,
		`SELECT account_id FROM alerts WHERE org_id = $1 AND kind = 'renewal'`, orgID).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if accountID != loud {
		t.Fatalf("alert belongs to %s, want the account without an override (%s)", accountID, loud)
	}
}

func TestMeterExhaustedOncePerWindow(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()

	accountID := plainAccount(t, pool, orgID, "Claude Max")
	limit := 100.0
	meterID := insertMeter(t, pool, orgID, accountID, "five_hour", &limit)

	window1 := testNow.Add(5 * time.Hour)
	used92 := 92.0
	insertReading(t, pool, orgID, meterID, &used92, &window1, testNow)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("92%% used: got %d new alerts, want 1", n)
	}
	if got := countAlerts(t, pool, orgID, "meter_exhausted"); got != 1 {
		t.Fatalf("92%% used: got %d meter alerts, want 1", got)
	}

	used95 := 95.0
	insertReading(t, pool, orgID, meterID, &used95, &window1, testNow.Add(time.Hour))
	n, err = svc.Evaluate(ctx, testNow.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("95%% in the same window: got %d new alerts, want 0", n)
	}

	window2 := testNow.Add(10 * time.Hour)
	used91 := 91.0
	insertReading(t, pool, orgID, meterID, &used91, &window2, testNow.Add(2*time.Hour))
	n, err = svc.Evaluate(ctx, testNow.Add(2*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	// The previous window was 95% used, so the new one also raises the
	// meter_reset alert; the exhausted alert is still one per window.
	if n != 2 {
		t.Fatalf("new window at 91%%: got %d new alerts, want 2", n)
	}
	if got := countAlerts(t, pool, orgID, "meter_exhausted"); got != 2 {
		t.Fatalf("total meter alerts: got %d, want 2", got)
	}
}

func TestQuotaWaste(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()

	accountID := plainAccount(t, pool, orgID, "Claude Max")
	limit := 100.0
	resets := testNow.Add(10 * time.Hour)

	waste := insertMeter(t, pool, orgID, accountID, "waste", &limit)
	used40 := 40.0
	insertReading(t, pool, orgID, waste, &used40, &resets, testNow)

	fine := insertMeter(t, pool, orgID, accountID, "fine", &limit)
	used70 := 70.0
	insertReading(t, pool, orgID, fine, &used70, &resets, testNow)

	// An untouched window is a choice, not waste: no alert for it.
	untouchedAccount := plainAccount(t, pool, orgID, "ChatGPT")
	untouched := insertMeter(t, pool, orgID, untouchedAccount, "untouched", &limit)
	used0 := 0.0
	insertReading(t, pool, orgID, untouched, &used0, &resets, testNow)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d new alerts, want 1 (only the started, 60%%-unused window)", n)
	}
	var severity, eventKey, title string
	if err := pool.QueryRow(ctx,
		`SELECT severity, event_key, title FROM alerts WHERE org_id = $1 AND kind = 'quota_waste'`, orgID).
		Scan(&severity, &eventKey, &title); err != nil {
		t.Fatal(err)
	}
	if severity != "info" {
		t.Fatalf("severity: got %q, want info", severity)
	}
	if want := "waste:" + accountID + ":" + testNow.UTC().Format(time.DateOnly); eventKey != want {
		t.Fatalf("event key %q, want one per account per day %q", eventKey, want)
	}
	if !strings.Contains(title, "60%") {
		t.Fatalf("title %q does not name the 60%%-unused window", title)
	}

	// Later the same day, another window qualifies: still one alert.
	later := testNow.Add(time.Hour)
	resetsLater := later.Add(5 * time.Hour)
	other := insertMeter(t, pool, orgID, accountID, "other", &limit)
	used10 := 10.0
	insertReading(t, pool, orgID, other, &used10, &resetsLater, later)
	n, err = svc.Evaluate(ctx, later)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("same day: got %d new alerts, want 0", n)
	}
}

func TestAgentOffline(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()

	lastSeen := testNow.Add(-49 * time.Hour)
	offline := insertMachine(t, pool, orgID, "laptop", lastSeen, nil)
	insertMachine(t, pool, orgID, "retired", lastSeen, new(testNow.Add(-time.Hour)))

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d new alerts, want 1", n)
	}
	var machineID string
	if err := pool.QueryRow(ctx,
		`SELECT machine_id FROM alerts WHERE org_id = $1 AND kind = 'agent_offline'`, orgID).Scan(&machineID); err != nil {
		t.Fatal(err)
	}
	if machineID != offline {
		t.Fatalf("alert machine: got %s, want %s", machineID, offline)
	}
}

func TestHookOnlyForNewAlerts(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()
	today := time.Date(testNow.Year(), testNow.Month(), testNow.Day(), 0, 0, 0, 0, time.UTC)

	renewalAccount(t, pool, orgID, "Claude Max", today.AddDate(0, 0, 7), true)

	calls := 0
	svc.SetHook(func(context.Context, alert.NewAlert) error {
		calls++
		return nil
	})

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || calls != 1 {
		t.Fatalf("first evaluation: %d new alerts, %d hook calls; want 1 and 1", n, calls)
	}

	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 || calls != 1 {
		t.Fatalf("duplicate evaluation: %d new alerts, %d hook calls; want 0 and 1", n, calls)
	}

	n, err = svc.Evaluate(ctx, testNow.AddDate(0, 0, 6))
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || calls != 2 {
		t.Fatalf("one day before: %d new alerts, %d hook calls; want 1 and 2", n, calls)
	}
}
