// SPDX-License-Identifier: AGPL-3.0-or-later

package alert_test

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestMeterReset(t *testing.T) {
	svc, pool, orgID := newService(t)
	ctx := context.Background()

	accountID := plainAccount(t, pool, orgID, "Claude Max")
	limit := 100.0

	// Previous window 95% used, then a new window began.
	meterID := insertMeter(t, pool, orgID, accountID, "five_hour", &limit)
	prevReset := testNow.Add(2 * time.Hour)
	used95 := 95.0
	insertReading(t, pool, orgID, meterID, &used95, &prevReset, testNow.Add(-time.Hour))
	nextReset := testNow.Add(20 * time.Hour)
	used5 := 5.0
	insertReading(t, pool, orgID, meterID, &used5, &nextReset, testNow)

	// Previous window only 50% used: no "limits are back" alert.
	otherMeter := insertMeter(t, pool, orgID, accountID, "daily", &limit)
	used50 := 50.0
	insertReading(t, pool, orgID, otherMeter, &used50, &prevReset, testNow.Add(-time.Hour))
	insertReading(t, pool, orgID, otherMeter, &used5, &nextReset, testNow)

	n, err := svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("got %d new alerts, want 1", n)
	}
	var severity, eventKey, title, body string
	if err := pool.QueryRow(ctx,
		`SELECT severity, event_key, title, body FROM alerts WHERE org_id = $1 AND kind = 'meter_reset'`, orgID).
		Scan(&severity, &eventKey, &title, &body); err != nil {
		t.Fatal(err)
	}
	if severity != "info" {
		t.Fatalf("severity: got %q, want info", severity)
	}
	if !strings.Contains(title, "available again") {
		t.Fatalf("title %q does not say the limits are back", title)
	}
	if want := nextReset.UTC().Format(time.RFC3339); !strings.HasSuffix(eventKey, want) {
		t.Fatalf("event key %q does not carry the new window %q", eventKey, want)
	}
	// The new reading arrived more than 30 minutes before the announced reset.
	if !strings.Contains(body, "reset early") {
		t.Fatalf("body %q does not mention the early reset", body)
	}

	n, err = svc.Evaluate(ctx, testNow)
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("repeat evaluation: got %d new alerts, want 0", n)
	}
}
