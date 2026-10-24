// SPDX-License-Identifier: SUL-1.0

package alert_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func insertBudget(t *testing.T, pool *pgxpool.Pool, orgID, accountID string, amountMinor int64, currency, thresholds string) string {
	t.Helper()
	ctx := context.Background()
	var account *string
	if accountID != "" {
		account = &accountID
	}
	var id string
	err := pool.QueryRow(ctx, `
		INSERT INTO budgets (org_id, account_id, amount_minor, currency, thresholds)
		VALUES ($1, $2, $3, $4, $5::integer[]) RETURNING id`,
		orgID, account, amountMinor, currency, thresholds).Scan(&id)
	if err != nil {
		t.Fatalf("inserting budget: %v", err)
	}
	return id
}

func insertAlertCharge(t *testing.T, pool *pgxpool.Pool, orgID, accountID, kind, currency string, minor int64, on time.Time) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO charges (org_id, account_id, kind, status, source, amount_minor, currency, occurred_on)
		VALUES ($1, $2, $3, 'confirmed', 'manual', $4, $5, $6)`,
		orgID, accountID, kind, minor, currency, on)
	if err != nil {
		t.Fatalf("inserting charge: %v", err)
	}
}

func TestBudgetAlerts(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()
	today := time.Date(testNow.Year(), testNow.Month(), testNow.Day(), 0, 0, 0, 0, time.UTC)

	accountID := plainAccount(t, pool, orgID, "Claude Max")
	insertBudget(t, pool, orgID, accountID, 10000, "USD", "{80,100}")

	// 40% used: below every threshold, no alert.
	insertAlertCharge(t, pool, orgID, accountID, "subscription", "USD", 4000, today.AddDate(0, 0, -2))
	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("40%% used: got %d new alerts, want 0", n)
	}

	// 90% used: the 80 threshold fires once, warning.
	insertAlertCharge(t, pool, orgID, accountID, "topup", "USD", 5000, today.AddDate(0, 0, -1))
	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("90%% used: got %d new alerts, want 1", n)
	}
	var severity, title string
	if err := pool.QueryRow(ctx,
		`SELECT severity, title FROM alerts WHERE org_id = $1 AND kind = 'budget'`, orgID).
		Scan(&severity, &title); err != nil {
		t.Fatal(err)
	}
	if severity != "warning" || !strings.Contains(title, "90%") || !strings.Contains(title, "Claude Max") {
		t.Fatalf("90%% alert: severity %q title %q", severity, title)
	}

	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("repeat evaluation: got %d new alerts, want 0", n)
	}

	// 100% used: the 100 threshold fires once, critical.
	insertAlertCharge(t, pool, orgID, accountID, "invoice", "USD", 1000, today)
	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		rows, _ := pool.Query(ctx, `SELECT event_key, title FROM alerts WHERE org_id = $1 AND kind = 'budget'`, orgID)
		var keys []string
		for rows.Next() {
			var k, title string
			_ = rows.Scan(&k, &title)
			keys = append(keys, k+" | "+title)
		}
		rows.Close()
		t.Fatalf("100%% used: got %d new alerts, want 1: %v", n, keys)
	}
	var critical int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM alerts WHERE org_id = $1 AND kind = 'budget' AND severity = 'critical'`, orgID).
		Scan(&critical); err != nil {
		t.Fatal(err)
	}
	if critical != 1 {
		t.Fatalf("critical budget alerts: got %d, want 1", critical)
	}
}

func TestBudgetProjection(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()
	today := time.Date(testNow.Year(), testNow.Month(), testNow.Day(), 0, 0, 0, 0, time.UTC)

	renews := today.AddDate(0, 0, 8) // still this month
	accountID := renewalAccount(t, pool, orgID, "Claude Max", renews, true)
	insertBudget(t, pool, orgID, accountID, 10000, "USD", "{80,100}")
	insertAlertCharge(t, pool, orgID, accountID, "subscription", "USD", 3000, today)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d new alerts, want 1 projected alert", n)
	}
	var severity, eventKey, title string
	if err := pool.QueryRow(ctx,
		`SELECT severity, event_key, title FROM alerts WHERE org_id = $1 AND kind = 'budget'`, orgID).
		Scan(&severity, &eventKey, &title); err != nil {
		t.Fatal(err)
	}
	if severity != "info" || !strings.Contains(eventKey, ":projected") || !strings.Contains(title, "on track to exceed") {
		t.Fatalf("projection alert: severity %q key %q title %q", severity, eventKey, title)
	}
}
