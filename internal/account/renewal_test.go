// SPDX-License-Identifier: AGPL-3.0-or-later

package account

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/dbtest"
)

func date(year int, month time.Month, day int) time.Time {
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func TestNextRenewal(t *testing.T) {
	tests := []struct {
		name   string
		prev   time.Time
		cycle  string
		anchor int
		want   time.Time
	}{
		{"monthly anchor 31 into February", date(2027, time.January, 31), "monthly", 31, date(2027, time.February, 28)},
		{"monthly anchor 31 back to the 31st", date(2027, time.February, 28), "monthly", 31, date(2027, time.March, 31)},
		{"monthly anchor 31 into April", date(2027, time.March, 31), "monthly", 31, date(2027, time.April, 30)},
		{"monthly anchor 31 in a leap year", date(2028, time.January, 31), "monthly", 31, date(2028, time.February, 29)},
		{"monthly anchor 15", date(2027, time.January, 15), "monthly", 15, date(2027, time.February, 15)},
		{"weekly across a month end", date(2027, time.January, 28), "weekly", 1, date(2027, time.February, 4)},
		{"yearly keeps a leap day", date(2028, time.February, 29), "yearly", 29, date(2029, time.February, 28)},
		{"yearly returns to February 29", date(2031, time.February, 28), "yearly", 29, date(2032, time.February, 29)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NextRenewal(tt.prev, tt.cycle, tt.anchor); !got.Equal(tt.want) {
				t.Fatalf("NextRenewal(%s, %s, %d) = %s, want %s", tt.prev.Format(time.DateOnly), tt.cycle, tt.anchor, got.Format(time.DateOnly), tt.want.Format(time.DateOnly))
			}
		})
	}

	t.Run("yearly leap day chain", func(t *testing.T) {
		got := date(2028, time.February, 29)
		want := []time.Time{
			date(2029, time.February, 28),
			date(2030, time.February, 28),
			date(2031, time.February, 28),
			date(2032, time.February, 29),
		}
		for _, next := range want {
			got = NextRenewal(got, "yearly", 29)
			if !got.Equal(next) {
				t.Fatalf("NextRenewal chain reached %s, want %s", got.Format(time.DateOnly), next.Format(time.DateOnly))
			}
		}
	})
}

func TestRollForward(t *testing.T) {
	today := date(2026, time.October, 18)

	t.Run("three passed months", func(t *testing.T) {
		passed, next := RollForward(date(2026, time.July, 18), today, "monthly", 18)
		want := []time.Time{date(2026, time.July, 18), date(2026, time.August, 18), date(2026, time.September, 18)}
		if len(passed) != len(want) {
			t.Fatalf("passed = %v, want %v", passed, want)
		}
		for i := range want {
			if !passed[i].Equal(want[i]) {
				t.Fatalf("passed[%d] = %s, want %s", i, passed[i].Format(time.DateOnly), want[i].Format(time.DateOnly))
			}
		}
		if !next.Equal(today) {
			t.Fatalf("next = %s, want %s", next.Format(time.DateOnly), today.Format(time.DateOnly))
		}
	})

	t.Run("prev equals today", func(t *testing.T) {
		passed, next := RollForward(today, today, "monthly", 18)
		if len(passed) != 0 {
			t.Fatalf("passed = %v, want none", passed)
		}
		if !next.Equal(today) {
			t.Fatalf("next = %s, want %s", next.Format(time.DateOnly), today.Format(time.DateOnly))
		}
	})

	t.Run("cycle none", func(t *testing.T) {
		prev := date(2026, time.July, 18)
		passed, next := RollForward(prev, today, "none", 18)
		if len(passed) != 0 {
			t.Fatalf("passed = %v, want none", passed)
		}
		if !next.Equal(prev) {
			t.Fatalf("next = %s, want %s", next.Format(time.DateOnly), prev.Format(time.DateOnly))
		}
	})
}

func TestRollForwardDue(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	orgID := dbtest.DefaultOrgID(t, pool)
	today := date(2026, time.October, 18)

	overdue := insertAccount(t, pool, orgID, "overdue", date(2026, time.August, 18), 18, true)
	future := insertAccount(t, pool, orgID, "future", date(2026, time.October, 28), 28, true)
	manual := insertAccount(t, pool, orgID, "manual", date(2026, time.July, 1), 1, false)

	svc := NewService(pool, nil, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	var calls []struct {
		accountID string
		on        time.Time
	}
	hook := func(_ context.Context, _ *db.Queries, a db.Account, renewedOn time.Time) error {
		calls = append(calls, struct {
			accountID string
			on        time.Time
		}{a.ID, renewedOn})
		return nil
	}

	advanced, err := svc.RollForwardDue(ctx, today, hook)
	if err != nil {
		t.Fatalf("RollForwardDue: %v", err)
	}
	if advanced != 1 {
		t.Fatalf("advanced = %d, want 1", advanced)
	}

	if len(calls) != 2 {
		t.Fatalf("hook calls = %d, want 2", len(calls))
	}
	if calls[0].accountID != overdue || !calls[0].on.Equal(date(2026, time.August, 18)) {
		t.Fatalf("first hook call = %+v, want overdue on 2026-08-18", calls[0])
	}
	if calls[1].accountID != overdue || !calls[1].on.Equal(date(2026, time.September, 18)) {
		t.Fatalf("second hook call = %+v, want overdue on 2026-09-18", calls[1])
	}

	var renewsOn time.Time
	var anchor int16
	if err := pool.QueryRow(ctx, `SELECT renews_on, renewal_anchor_day FROM accounts WHERE id = $1`, overdue).Scan(&renewsOn, &anchor); err != nil {
		t.Fatalf("loading overdue account: %v", err)
	}
	if !renewsOn.Equal(today) {
		t.Fatalf("overdue renews_on = %s, want %s", renewsOn.Format(time.DateOnly), today.Format(time.DateOnly))
	}
	if anchor != 18 {
		t.Fatalf("renewal_anchor_day = %d, want 18", anchor)
	}

	for _, id := range []string{future, manual} {
		var got time.Time
		if err := pool.QueryRow(ctx, `SELECT renews_on FROM accounts WHERE id = $1`, id).Scan(&got); err != nil {
			t.Fatalf("loading account %s: %v", id, err)
		}
		if id == future && !got.Equal(date(2026, time.October, 28)) {
			t.Fatalf("future renews_on = %s, want 2026-10-28", got.Format(time.DateOnly))
		}
		if id == manual && !got.Equal(date(2026, time.July, 1)) {
			t.Fatalf("manual renews_on = %s, want 2026-07-01", got.Format(time.DateOnly))
		}
	}

	var audits int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM audit_log WHERE org_id = $1 AND action = 'account.renew'`, orgID).Scan(&audits); err != nil {
		t.Fatalf("counting renew audits: %v", err)
	}
	if audits != 1 {
		t.Fatalf("account.renew audit rows = %d, want 1", audits)
	}
}

func insertAccount(t *testing.T, pool *pgxpool.Pool, orgID, name string, renewsOn time.Time, anchor int, autoRenew bool) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency, billing_cycle,
			price_minor, renews_on, renewal_anchor_day, auto_renew)
		VALUES ($1, 'test', $2, 'none', 'USD', 'monthly', 1000, $3, $4, $5)
		RETURNING id`, orgID, name, renewsOn, anchor, autoRenew).Scan(&id)
	if err != nil {
		t.Fatalf("inserting test account: %v", err)
	}
	return id
}
