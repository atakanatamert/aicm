// SPDX-License-Identifier: AGPL-3.0-or-later

package alert_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func insertUsageDaily(t *testing.T, pool *pgxpool.Pool, orgID, accountID string, day time.Time, costMicro int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO usage_daily (org_id, account_id, day, model, source, project, cost_micro, cost_currency)
		VALUES ($1, $2, $3, 'claude-sonnet', 'api', '', $4, 'USD')
		ON CONFLICT (account_id, day, model, source, project) DO UPDATE
		SET cost_micro = excluded.cost_micro`,
		orgID, accountID, day, costMicro)
	if err != nil {
		t.Fatalf("inserting usage: %v", err)
	}
}

func TestSpendAnomaly(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()
	today := time.Date(testNow.Year(), testNow.Month(), testNow.Day(), 0, 0, 0, 0, time.UTC)

	accountID := plainAccount(t, pool, orgID, "Claude Max")
	for i := 1; i <= 3; i++ {
		insertUsageDaily(t, pool, orgID, accountID, today.AddDate(0, 0, -i), 2_000_000) // $2 a day
	}
	insertUsageDaily(t, pool, orgID, accountID, today, 10_000_000) // $10 today: 5× the median

	// A different account whose spike is real but below min_daily_cost ($5).
	cheap := plainAccount(t, pool, orgID, "ChatGPT")
	for i := 1; i <= 3; i++ {
		insertUsageDaily(t, pool, orgID, cheap, today.AddDate(0, 0, -i), 1_000_000) // $1 a day
	}
	insertUsageDaily(t, pool, orgID, cheap, today, 4_000_000) // $4 today

	insertAlertCharge(t, pool, orgID, accountID, "topup", "USD", 2000, today)
	insertAlertCharge(t, pool, orgID, accountID, "topup", "USD", 2000, today)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("got %d new alerts, want 2 (usage spike and top-up burst)", n)
	}
	var usageTitle, topupTitle string
	if err := pool.QueryRow(ctx,
		`SELECT title FROM alerts WHERE org_id = $1 AND event_key LIKE 'anomaly:usage:%'`, orgID).
		Scan(&usageTitle); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT title FROM alerts WHERE org_id = $1 AND event_key LIKE 'anomaly:topups:%'`, orgID).
		Scan(&topupTitle); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(usageTitle, "5×") || !strings.Contains(usageTitle, "Claude Max") {
		t.Fatalf("usage title %q does not describe a 5× day", usageTitle)
	}
	if !strings.Contains(topupTitle, "2 top-ups") {
		t.Fatalf("top-up title %q does not count the burst", topupTitle)
	}

	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("repeat evaluation: got %d new alerts, want 0", n)
	}
}
