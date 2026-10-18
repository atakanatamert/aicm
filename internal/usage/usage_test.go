// SPDX-License-Identifier: AGPL-3.0-or-later

package usage_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/dbtest"
	"github.com/atakanatamert/aicm/internal/pricing"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/usage"
)

func TestRollup(t *testing.T) {
	pool := dbtest.New(t)
	ctx := context.Background()
	orgID := dbtest.DefaultOrgID(t, pool)

	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	prices := pricing.NewService(pool, provider.NewHTTPClient(log, nil), log)
	svc := usage.NewService(pool, prices, log)

	var accountID, machineID, userID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency)
		VALUES ($1, 'claude', 'Claude Code', 'none', 'USD')
		RETURNING id`, orgID).Scan(&accountID); err != nil {
		t.Fatalf("inserting account: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO machines (org_id, name, hostname, fingerprint, os, arch, agent_version, token_hash)
		VALUES ($1, 'laptop', 'laptop.local', 'fp-1', 'darwin', 'arm64', 'test', $2)
		RETURNING id`, orgID, []byte{1}).Scan(&machineID); err != nil {
		t.Fatalf("inserting machine: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO users (org_id, email, password_hash, role)
		VALUES ($1, 'viewer@example.com', 'x', 'viewer')
		RETURNING id`, orgID).Scan(&userID); err != nil {
		t.Fatalf("inserting user: %v", err)
	}

	const modelA, modelB = "model-a", "model-b"
	if _, err := pool.Exec(ctx, `
		INSERT INTO model_prices (
			model, provider, currency,
			input_micro_per_mtok, output_micro_per_mtok,
			cache_read_micro_per_mtok, cache_write_micro_per_mtok
		) VALUES ($1, 'anthropic', 'USD', 1000000, 2000000, 100000, 200000)`, modelA); err != nil {
		t.Fatalf("inserting price: %v", err)
	}

	day1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	day2 := day1.AddDate(0, 0, 1)
	ref := agentlink.AccountRef{ProviderID: "claude", ExternalID: "acct-1"}
	accounts := map[agentlink.AccountRef]string{ref: accountID}

	event := func(id, model string, at time.Time, in, out, cacheRead, cacheWrite, reasoning int64, cost *int64) agentlink.UsageEvent {
		e := agentlink.UsageEvent{
			Account:          ref,
			Source:           "claudecode",
			MessageID:        id,
			OccurredAt:       at,
			Model:            model,
			InputTokens:      in,
			OutputTokens:     out,
			CacheReadTokens:  cacheRead,
			CacheWriteTokens: cacheWrite,
			ReasoningTokens:  reasoning,
			CostMicro:        cost,
		}
		if cost != nil {
			e.CostCurrency = "USD"
		}
		return e
	}

	reportedB := int64(1234)
	reportedD2 := int64(999)
	// The second day-2 event has a non-UTC offset but is 2026-03-02 in UTC.
	localDay2 := time.Date(2026, 3, 3, 4, 30, 0, 0, time.FixedZone("+05", 5*60*60))

	batch := []agentlink.UsageEvent{
		event("a1", modelA, day1.Add(time.Hour), 1000, 200, 50, 10, 5, nil),
		event("a2", modelA, day1.Add(2*time.Hour), 2000, 400, 60, 20, 6, nil),
		event("a3", modelA, day1.Add(3*time.Hour), 3000, 600, 70, 30, 7, nil),
		event("a2", modelA, day1.Add(2*time.Hour), 2000, 400, 60, 20, 6, nil), // duplicate
		event("b1", modelB, day1.Add(4*time.Hour), 500, 100, 0, 0, 0, &reportedB),
		event("a4", modelA, day2.Add(23*time.Hour+59*time.Minute), 4000, 800, 80, 40, 9, nil),
		event("a5", modelA, localDay2, 5000, 1000, 90, 50, 11, &reportedD2),
	}

	created, err := svc.RecordAgentEvents(ctx, orgID, machineID, accounts, batch)
	if err != nil {
		t.Fatalf("recording events: %v", err)
	}
	if created != 6 {
		t.Fatalf("created = %d, want 6", created)
	}

	type dailyRow struct {
		day          time.Time
		model        string
		source       string
		requests     int64
		input        int64
		output       int64
		cacheRead    int64
		cacheWrite   int64
		reasoning    int64
		cost         int64
		currency     string
		costReported bool
	}
	loadDaily := func() []dailyRow {
		t.Helper()
		rows, err := pool.Query(ctx, `
			SELECT day, model, source, requests, input_tokens, output_tokens,
			       cache_read_tokens, cache_write_tokens, reasoning_tokens,
			       cost_micro, cost_currency, cost_reported
			FROM usage_daily
			ORDER BY day, model, source`)
		if err != nil {
			t.Fatalf("loading usage_daily: %v", err)
		}
		defer rows.Close()
		var out []dailyRow
		for rows.Next() {
			var r dailyRow
			if err := rows.Scan(&r.day, &r.model, &r.source, &r.requests, &r.input, &r.output,
				&r.cacheRead, &r.cacheWrite, &r.reasoning, &r.cost, &r.currency, &r.costReported); err != nil {
				t.Fatalf("scanning usage_daily: %v", err)
			}
			out = append(out, r)
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("iterating usage_daily: %v", err)
		}
		return out
	}

	priceA := pricing.Price{Model: modelA, Currency: "USD", Input: 1_000_000, Output: 2_000_000, CacheRead: 100_000, CacheWrite: 200_000}
	wantCostA1 := pricing.Estimate(priceA, 6000, 1200, 180, 60)
	wantCostA2 := reportedD2 + pricing.Estimate(priceA, 4000, 800, 80, 40)

	got := loadDaily()
	if len(got) != 3 {
		t.Fatalf("usage_daily rows = %d, want 3: %+v", len(got), got)
	}
	want := []dailyRow{
		{day1, modelA, "agent", 3, 6000, 1200, 180, 60, 18, wantCostA1, "USD", false},
		{day1, modelB, "agent", 1, 500, 100, 0, 0, 0, reportedB, "USD", true},
		{day2, modelA, "agent", 2, 9000, 1800, 170, 90, 20, wantCostA2, "USD", false},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("usage_daily[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The same batch is idempotent.
	again, err := svc.RecordAgentEvents(ctx, orgID, machineID, accounts, batch)
	if err != nil {
		t.Fatalf("recording events again: %v", err)
	}
	if again != 0 {
		t.Errorf("second ingest created %d events, want 0", again)
	}
	if after := loadDaily(); len(after) != 3 || after[0] != want[0] || after[1] != want[1] || after[2] != want[2] {
		t.Errorf("second ingest changed usage_daily: %+v", after)
	}

	// API usage replaces only its own days and source.
	apiCost := int64(777)
	if err := svc.ReplaceAPIUsage(ctx, orgID, accountID, day1, day2, []provider.UsageRecord{
		{Day: day1, Model: modelA, Requests: 10, InputTokens: 100, OutputTokens: 50},
		{Day: day1, Model: "model-c", Requests: 5, CostMicro: &apiCost, CostCurrency: "USD"},
	}); err != nil {
		t.Fatalf("replacing api usage: %v", err)
	}
	if err := svc.ReplaceAPIUsage(ctx, orgID, accountID, day1, day2, []provider.UsageRecord{
		{Day: day1, Model: "model-d", Requests: 1},
	}); err != nil {
		t.Fatalf("replacing api usage again: %v", err)
	}
	var apiRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM usage_daily
		WHERE org_id = $1 AND account_id = $2 AND day = $3 AND source = 'api'`,
		orgID, accountID, day1).Scan(&apiRows); err != nil {
		t.Fatalf("counting api rows: %v", err)
	}
	if apiRows != 1 {
		t.Errorf("api rows for day 1 = %d, want 1", apiRows)
	}
	var agentRows int
	if err := pool.QueryRow(ctx, `
		SELECT count(*) FROM usage_daily
		WHERE org_id = $1 AND account_id = $2 AND source = 'agent'`,
		orgID, accountID).Scan(&agentRows); err != nil {
		t.Fatalf("counting agent rows: %v", err)
	}
	if agentRows != 3 {
		t.Errorf("agent rows = %d, want 3", agentRows)
	}

	// Grants decide visibility.
	query := usage.Query{
		OrgID:   orgID,
		UserID:  userID,
		From:    day1,
		To:      day2,
		GroupBy: "day",
	}
	rows, err := svc.Query(ctx, query)
	if err != nil {
		t.Fatalf("querying usage without grant: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("rows without grant = %d, want 0", len(rows))
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO account_grants (org_id, account_id, user_id) VALUES ($1, $2, $3)`,
		orgID, accountID, userID); err != nil {
		t.Fatalf("inserting grant: %v", err)
	}
	rows, err = svc.Query(ctx, query)
	if err != nil {
		t.Fatalf("querying usage with grant: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows with grant = %d, want 2", len(rows))
	}
	if rows[0].Day == nil || !rows[0].Day.Equal(day1) {
		t.Errorf("first row day = %v, want %s", rows[0].Day, day1)
	}
	// day 1: three agent events plus the one remaining api request.
	if rows[0].Requests != 5 {
		t.Errorf("day 1 requests = %d, want 5", rows[0].Requests)
	}
	if rows[0].CostReported {
		t.Errorf("day 1 cost_reported = true, want false")
	}
	if len(rows[0].Cost) != 1 || rows[0].Cost[0].Currency != "USD" {
		t.Errorf("day 1 cost = %+v, want one USD entry", rows[0].Cost)
	}

	for _, groupBy := range []string{"model", "account", "provider"} {
		grouped, err := svc.Query(ctx, usage.Query{
			OrgID:   orgID,
			UserID:  userID,
			From:    day1,
			To:      day2,
			GroupBy: groupBy,
		})
		if err != nil {
			t.Fatalf("querying usage by %s: %v", groupBy, err)
		}
		if len(grouped) == 0 {
			t.Errorf("usage by %s returned no rows", groupBy)
		}
	}
}
