// SPDX-License-Identifier: SUL-1.0

package alert

import (
	"fmt"
	"math"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/quota"
)

// resetEarly is how much before the announced reset a new window may start
// and still count as an early reset.
const resetEarly = 30 * time.Minute

// resetCandidate reports the "limits are back" event for one meter: the
// previous window was at least usedThreshold percent used and the newest
// reading belongs to a later window.
func resetCandidate(now time.Time, a db.Account, name string, def meter.Def, rows []db.QuotaReading,
	usedThreshold float64, links *provider.Links) (candidate, bool) {
	if def.Supplemental || def.ResetKind == meter.ResetNever || len(rows) < 2 {
		return candidate{}, false
	}
	latest := rows[len(rows)-1]
	if latest.ResetsAt == nil {
		return candidate{}, false
	}
	// The previous window's last reading is the newest one whose reset time
	// differs from the newest reading's. When every reading carries the same
	// announced reset (the provider has not refreshed it), the reading before
	// the newest is the closest thing to it; the stale check below decides
	// whether the window actually ended.
	idx := -1
	for i := len(rows) - 2; i >= 0; i-- {
		if !sameWindow(rows[i].ResetsAt, latest.ResetsAt) {
			idx = i
			break
		}
	}
	if idx < 0 {
		idx = len(rows) - 2
	}
	prev := rows[idx]

	newWindow := prev.ResetsAt != nil && latest.ResetsAt.After(*prev.ResetsAt)
	if !newWindow {
		// The announced reset passed without a fresh resets_at: the
		// previous reading is stale, which also means a new window began.
		prevReadings := readingsOf(def, rows[:idx+1])
		newWindow = quota.ScoreMeter(quota.MeterInput{Def: def, Readings: prevReadings}, now).Stale
	}
	if !newWindow {
		return candidate{}, false
	}

	prevReadings := readingsOf(def, rows[:idx+1])
	pct, ok := usedPercent(def, prevReadings,
		quota.ScoreMeter(quota.MeterInput{Def: def, Readings: prevReadings}, prev.CapturedAt))
	if !ok || pct < usedThreshold {
		return candidate{}, false
	}

	body := fmt.Sprintf("The previous window was %d%% used. Limits are back.", int(math.Round(pct)))
	if prev.ResetsAt != nil && latest.CapturedAt.Add(resetEarly).Before(*prev.ResetsAt) {
		body = fmt.Sprintf("The previous window was %d%% used. Limits reset early.", int(math.Round(pct)))
	}
	return candidate{
		orgID:     a.OrgID,
		accountID: &a.ID,
		kind:      "meter_reset",
		eventKey: fmt.Sprintf("reset:%s:%s:%s", a.ID, def.Key,
			latest.ResetsAt.UTC().Format(time.RFC3339)),
		severity: "info",
		title:    fmt.Sprintf("%s %s is available again", name, def.Label),
		body:     body,
		data:     alertData{Links: links},
	}, true
}

// readingsOf converts stored readings to meter readings for scoring.
func readingsOf(def meter.Def, rows []db.QuotaReading) []meter.Reading {
	out := make([]meter.Reading, len(rows))
	for i, row := range rows {
		out[i] = readingOf(def.Key, row)
	}
	return out
}

// sameWindow reports whether two readings belong to the same reset window,
// allowing the few seconds of jitter providers show between reports.
func sameWindow(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	d := a.Sub(*b)
	return d > -2*time.Minute && d < 2*time.Minute
}
