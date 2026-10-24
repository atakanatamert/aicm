// SPDX-License-Identifier: SUL-1.0

package alert_test

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func subscriptionAccount(t *testing.T, pool *pgxpool.Pool, orgID, name string) string {
	t.Helper()
	var id string
	err := pool.QueryRow(context.Background(), `
		INSERT INTO accounts (org_id, provider_id, name, auth_kind, currency)
		VALUES ($1, 'manual', $2, 'agent_capture', 'USD')
		RETURNING id`, orgID, name).Scan(&id)
	if err != nil {
		t.Fatalf("inserting subscription account: %v", err)
	}
	return id
}

func insertAccountMachine(t *testing.T, pool *pgxpool.Pool, orgID, accountID, machineID, harness, details string) {
	t.Helper()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO account_machines (org_id, account_id, machine_id, harness, details)
		VALUES ($1, $2, $3, $4, $5::jsonb)`,
		orgID, accountID, machineID, harness, details)
	if err != nil {
		t.Fatalf("inserting account machine: %v", err)
	}
}

func TestWrongWallet(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()

	accountID := subscriptionAccount(t, pool, orgID, "Claude Max")
	machineID := insertMachine(t, pool, orgID, "laptop", testNow, nil)
	insertAccountMachine(t, pool, orgID, accountID, machineID, "claude_code",
		`{"billing_mode":"api_key","extra_usage":"on"}`)

	// An account without a subscription is never "billing the wrong wallet".
	plain := plainAccount(t, pool, orgID, "Manual key")
	insertAccountMachine(t, pool, orgID, plain, machineID, "other", `{"billing_mode":"api_key"}`)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("got %d new alerts, want 2 (api-key harness and paid extra usage)", n)
	}
	var walletTitle, extraTitle string
	if err := pool.QueryRow(ctx,
		`SELECT title FROM alerts WHERE org_id = $1 AND event_key LIKE 'wallet:%'`, orgID).
		Scan(&walletTitle); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT title FROM alerts WHERE org_id = $1 AND event_key LIKE 'extra_usage:%'`, orgID).
		Scan(&extraTitle); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(walletTitle, "claude_code on laptop bills an API key, not Claude Max") {
		t.Fatalf("wallet title %q", walletTitle)
	}
	if !strings.Contains(extraTitle, "Paid extra usage is on for Claude Max") {
		t.Fatalf("extra usage title %q", extraTitle)
	}
	var severity string
	if err := pool.QueryRow(ctx,
		`SELECT severity FROM alerts WHERE org_id = $1 AND event_key LIKE 'extra_usage:%'`, orgID).
		Scan(&severity); err != nil {
		t.Fatal(err)
	}
	if severity != "info" {
		t.Fatalf("extra usage severity: got %q, want info", severity)
	}

	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("repeat evaluation: got %d new alerts, want 0", n)
	}
}
