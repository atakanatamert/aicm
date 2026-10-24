// SPDX-License-Identifier: SUL-1.0

package account

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/provider"
)

var plusPrice = provider.PlanPrice{Tier: "plus", Name: "Plus", PriceMinor: 2000, Currency: "USD"}

func insertUnpricedAccount(t *testing.T, pool *pgxpool.Pool, orgID string) string {
	t.Helper()
	var id string
	if err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency)
		VALUES ($1, 'chatgpt', 'ChatGPT', 'agent_capture', 'USD') RETURNING id`, orgID).Scan(&id); err != nil {
		t.Fatalf("inserting account: %v", err)
	}
	return id
}

type estimatedRow struct {
	on, end string
	amount  int64
}

func estimatedCharges(t *testing.T, pool *pgxpool.Pool, accountID string) []estimatedRow {
	t.Helper()
	rows, err := pool.Query(context.Background(), `
		SELECT occurred_on::text, period_end::text, amount_minor FROM charges
		WHERE account_id = $1 AND source = 'estimated' ORDER BY occurred_on`, accountID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []estimatedRow
	for rows.Next() {
		var r estimatedRow
		if err := rows.Scan(&r.on, &r.end, &r.amount); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// A plan detected mid-month books one estimated fee per past billing day
// (never today's, which the renewal job books) and renews on the next one.
func TestFillDetectedBackfillsAndPrices(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	q := db.New(pool)
	actor := audit.Actor{OrgID: orgID, Kind: "system", Label: "test"}
	id := insertUnpricedAccount(t, pool, orgID)

	plan := DetectedPlan{Price: plusPrice, Started: time.Date(2026, time.June, 9, 8, 48, 55, 0, time.UTC)}
	filled, err := FillDetected(ctx, q, actor, orgID, id, plan, date(2026, time.October, 9))
	if err != nil || !filled {
		t.Fatalf("FillDetected = %v, %v; want true", filled, err)
	}

	got := estimatedCharges(t, pool, id)
	want := []estimatedRow{
		{"2026-06-09", "2026-07-08", 2000},
		{"2026-07-09", "2026-08-08", 2000},
		{"2026-08-09", "2026-09-08", 2000},
		{"2026-09-09", "2026-10-08", 2000},
	}
	if len(got) != len(want) {
		t.Fatalf("estimated charges = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("charge %d = %v, want %v", i, got[i], want[i])
		}
	}

	var renewsOn time.Time
	var price int64
	var planName string
	if err := pool.QueryRow(ctx, `SELECT renews_on, price_minor, plan_name FROM accounts WHERE id = $1`, id).
		Scan(&renewsOn, &price, &planName); err != nil {
		t.Fatal(err)
	}
	if !renewsOn.Equal(date(2026, time.October, 9)) || price != 2000 || planName != "Plus" {
		t.Fatalf("plan = %s %d renews %s; want Plus 2000 renews 2026-10-09", planName, price, renewsOn.Format(time.DateOnly))
	}

	// The next report changes nothing: the plan is set.
	again, err := FillDetected(ctx, q, actor, orgID, id, plan, date(2026, time.October, 20))
	if err != nil || again {
		t.Fatalf("second FillDetected = %v, %v; want false", again, err)
	}
	if n := len(estimatedCharges(t, pool, id)); n != 4 {
		t.Fatalf("estimated charges after second call = %d, want 4", n)
	}
}

// A plan the user priced is never overwritten, and an account that already
// has subscription fees gets no estimated history.
func TestFillDetectedRespectsTheUser(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.New(t)
	orgID := dbtest.DefaultOrgID(t, pool)
	q := db.New(pool)
	actor := audit.Actor{OrgID: orgID, Kind: "system", Label: "test"}
	plan := DetectedPlan{Price: plusPrice, Started: date(2026, time.January, 31)}

	priced := insertAccount(t, pool, orgID, "priced", date(2026, time.November, 1), 1, true)
	if filled, err := FillDetected(ctx, q, actor, orgID, priced, plan, date(2026, time.March, 15)); err != nil || filled {
		t.Fatalf("priced account: FillDetected = %v, %v; want false", filled, err)
	}

	paid := insertUnpricedAccount(t, pool, orgID)
	if _, err := pool.Exec(ctx, `
		INSERT INTO charges (org_id, account_id, kind, source, amount_minor, currency, occurred_on)
		VALUES ($1, $2, 'subscription', 'manual', 2000, 'USD', '2026-02-01')`, orgID, paid); err != nil {
		t.Fatal(err)
	}
	filled, err := FillDetected(ctx, q, actor, orgID, paid, plan, date(2026, time.March, 15))
	if err != nil || !filled {
		t.Fatalf("account with fees: FillDetected = %v, %v; want true", filled, err)
	}
	if n := len(estimatedCharges(t, pool, paid)); n != 0 {
		t.Fatalf("account with fees got %d estimated charges, want 0", n)
	}
	var renewsOn time.Time
	if err := pool.QueryRow(ctx, `SELECT renews_on FROM accounts WHERE id = $1`, paid).Scan(&renewsOn); err != nil {
		t.Fatal(err)
	}
	// Anchor 31: January 31, February 28, then back to March 31.
	if !renewsOn.Equal(date(2026, time.March, 31)) {
		t.Fatalf("renews_on = %s, want 2026-03-31", renewsOn.Format(time.DateOnly))
	}
}
