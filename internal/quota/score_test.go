// SPDX-License-Identifier: AGPL-3.0-or-later

package quota

import (
	"math"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
)

var now = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func pct(key string, window int) meter.Def {
	return meter.Def{Key: key, Label: key, Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: window}
}

func wallet() meter.Def {
	return meter.Def{Key: "balance", Label: "Balance", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever}
}

func used(ago time.Duration, v float64, resets time.Time) meter.Reading {
	return meter.Reading{Used: new(v), ResetsAt: new(resets), CapturedAt: now.Add(-ago), Source: meter.SourceStatusline}
}

func left(ago time.Duration, v float64) meter.Reading {
	return meter.Reading{Remaining: new(v), CapturedAt: now.Add(-ago), Source: meter.SourceAPI}
}

func approx(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("%s = %v, want %v", name, got, want)
	}
}

func ptrVal(p *float64) float64 {
	if p == nil {
		return math.NaN()
	}
	return *p
}

// 5-hour window: 20 → 30 → 40 % over two hours, resets in 3 h.
// b = 10 %/h, R = 60, T_empty = 6 h, T_reset = 3 h
// headroom = min(1, 6/3) = 1; expiring = (60 − 10·3) / 100 = 0.30.
func TestScoreExpiringWindow(t *testing.T) {
	reset := now.Add(3 * time.Hour)
	s := ScoreMeter(MeterInput{Def: pct("five_hour", 300), Readings: []meter.Reading{
		used(2*time.Hour, 20, reset), used(time.Hour, 30, reset), used(0, 40, reset),
	}}, now)
	approx(t, "burn", ptrVal(s.BurnPerHour), 10)
	approx(t, "remaining", ptrVal(s.Remaining), 60)
	approx(t, "hours to empty", ptrVal(s.HoursToEmpty), 6)
	approx(t, "hours to reset", ptrVal(s.HoursToReset), 3)
	approx(t, "headroom", s.Headroom, 1)
	approx(t, "expiring", s.Expiring, 0.30)
	if s.Exhausted || s.Stale {
		t.Errorf("exhausted=%v stale=%v, want both false", s.Exhausted, s.Stale)
	}
}

// Weekly window: 50 → 70 % over two hours (b = 10), resets in 10 h.
// R = 30, T_empty = 3 h → headroom 0.3; expiring = max(0, 30 − 100) = 0.
func TestScoreRunsDryBeforeReset(t *testing.T) {
	reset := now.Add(10 * time.Hour)
	s := ScoreMeter(MeterInput{Def: pct("seven_day", 10080), Readings: []meter.Reading{
		used(2*time.Hour, 50, reset), used(0, 70, reset),
	}}, now)
	approx(t, "headroom", s.Headroom, 0.3)
	approx(t, "expiring", s.Expiring, 0)
}

func TestScoreExhausted(t *testing.T) {
	s := ScoreMeter(MeterInput{Def: pct("seven_day", 10080), Readings: []meter.Reading{used(0, 100, now.Add(time.Hour))}}, now)
	if !s.Exhausted || s.Headroom != 0 {
		t.Errorf("exhausted=%v headroom=%v, want true, 0", s.Exhausted, s.Headroom)
	}
	a := ScoreAccount([]MeterScore{s, {Key: "five_hour", Headroom: 1}})
	if a.Feasible || a.Binding != "seven_day" {
		t.Errorf("account = %+v, want infeasible bound by seven_day", a)
	}
}

// A reading past its resets_at describes a finished window: back to the full
// limit, and the next reset is one window from now.
func TestScoreStaleReading(t *testing.T) {
	s := ScoreMeter(MeterInput{Def: pct("five_hour", 300), Readings: []meter.Reading{used(2*time.Hour, 95, now.Add(-time.Hour))}}, now)
	if !s.Stale || s.Exhausted {
		t.Fatalf("stale=%v exhausted=%v, want true, false", s.Stale, s.Exhausted)
	}
	approx(t, "remaining", ptrVal(s.Remaining), 100)
	approx(t, "hours to reset", ptrVal(s.HoursToReset), 5)
}

// Wallet: 10000 → 9000 over 10 h is b = 100/h; a top-up in between is not
// consumption. T_reset = 30 days = 720 h, T_empty = 90 h → headroom 0.125.
// Wallets never expire.
func TestScoreWalletIgnoresTopUps(t *testing.T) {
	s := ScoreMeter(MeterInput{Def: wallet(), Readings: []meter.Reading{
		left(20*time.Hour, 10000), left(10*time.Hour, 9000), // −1000 in 10 h
		left(5*time.Hour, 50000), // top-up: skipped
		left(0, 50000),
	}}, now)
	// pairs: (−20h,−10h) 1000 over 10h; (−10h,−5h) top-up skipped; (−5h,0) 0 over 5h
	// b = 1000 / 15 h
	b := 1000.0 / 15
	approx(t, "burn", ptrVal(s.BurnPerHour), b)
	approx(t, "headroom", s.Headroom, math.Min(1, 50000/b/720))
	approx(t, "expiring", s.Expiring, 0)
}

func TestScoreWindowChangeIsNotConsumption(t *testing.T) {
	// The first pair spans a reset (different resets_at): skipped. Only the
	// second pair counts: 5 → 15 over one hour.
	s := ScoreMeter(MeterInput{Def: pct("five_hour", 300), Readings: []meter.Reading{
		used(2*time.Hour, 90, now.Add(-90*time.Minute)),
		used(time.Hour, 5, now.Add(4*time.Hour)),
		used(0, 15, now.Add(4*time.Hour)),
	}}, now)
	approx(t, "burn", ptrVal(s.BurnPerHour), 10)
}

func TestScoreSingleReading(t *testing.T) {
	s := ScoreMeter(MeterInput{Def: pct("five_hour", 300), Readings: []meter.Reading{used(0, 25, now.Add(2*time.Hour))}}, now)
	if s.BurnPerHour != nil {
		t.Errorf("burn = %v, want unknown", *s.BurnPerHour)
	}
	approx(t, "headroom", s.Headroom, 1)
	approx(t, "expiring", s.Expiring, 0.75) // nothing burning, so all of it resets unused
}

func TestScoreNoData(t *testing.T) {
	s := ScoreMeter(MeterInput{Def: pct("five_hour", 300)}, now)
	if !s.NoData || s.Headroom != 1 || s.Exhausted {
		t.Errorf("score = %+v, want no data, headroom 1", s)
	}
}

func TestScoreAccountWeakestMeter(t *testing.T) {
	a := ScoreAccount([]MeterScore{
		{Key: "five_hour", Headroom: 1, Expiring: 0.3},
		{Key: "seven_day", Headroom: 0.4, Expiring: 0.1},
	})
	if !a.Feasible || a.Binding != "seven_day" {
		t.Errorf("account = %+v", a)
	}
	approx(t, "headroom", a.Headroom, 0.4)
	approx(t, "expiring", a.Expiring, 0.3)
}

// An empty Codex credits wallet must not make a plan with free limits look
// exhausted or tight.
func TestScoreAccountSkipsSupplemental(t *testing.T) {
	a := ScoreAccount([]MeterScore{
		{Key: "primary", Headroom: 0.9},
		{Key: "secondary", Headroom: 0.7},
		{Key: "credits", Headroom: 0, Exhausted: true, Supplemental: true},
	})
	if !a.Feasible || a.Binding != "secondary" {
		t.Errorf("account = %+v", a)
	}
	approx(t, "headroom", a.Headroom, 0.7)
}

func TestRank(t *testing.T) {
	cs := []Candidate{
		{AccountID: "exhausted-sub", Score: AccountScore{Feasible: false, Expiring: 0.9, Headroom: 0}},
		{AccountID: "api-cheap", Score: AccountScore{Feasible: true, Expiring: 0, Headroom: 1}, MarginalCostMicro: 1_000_000},
		{AccountID: "api-pricey", Score: AccountScore{Feasible: true, Expiring: 0, Headroom: 1}, MarginalCostMicro: 15_000_000},
		{AccountID: "sub-expiring", Score: AccountScore{Feasible: true, Expiring: 0.6, Headroom: 0.9}},
		{AccountID: "sub-tight", Score: AccountScore{Feasible: true, Expiring: 0, Headroom: 0.2}},
		{AccountID: "sub-roomy", Score: AccountScore{Feasible: true, Expiring: 0.002, Headroom: 0.9}}, // expiring within tie band of 0
	}
	Rank(cs)
	want := []string{"sub-expiring", "sub-roomy", "sub-tight", "api-cheap", "api-pricey", "exhausted-sub"}
	for i, id := range want {
		if cs[i].AccountID != id {
			got := make([]string, len(cs))
			for j, c := range cs {
				got[j] = c.AccountID
			}
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// A wallet with 300 left burning 100/h is empty at hour 3. A 5-hour window
// at 90 % burning 10 %/h hits 100 % at hour 1 and resets at hour 2.
func TestForecast(t *testing.T) {
	walletIn := MeterInput{Def: wallet(), Readings: []meter.Reading{left(time.Hour, 400), left(0, 300)}}
	pts := Forecast([]MeterInput{walletIn}, now, 4)
	if len(pts) != 5 {
		t.Fatalf("got %d points, want 5", len(pts))
	}
	for h, wantFeasible := range []bool{true, true, true, false, false} {
		if pts[h].Feasible != wantFeasible {
			t.Errorf("wallet hour %d feasible = %v, want %v", h, pts[h].Feasible, wantFeasible)
		}
	}

	reset := now.Add(2 * time.Hour)
	win := MeterInput{Def: pct("five_hour", 300), Readings: []meter.Reading{used(time.Hour, 80, reset), used(0, 90, reset)}}
	pts = Forecast([]MeterInput{win}, now, 3)
	for h, wantFeasible := range []bool{true, false, true, true} {
		if pts[h].Feasible != wantFeasible {
			t.Errorf("window hour %d feasible = %v, want %v", h, pts[h].Feasible, wantFeasible)
		}
	}
	// After the reset at hour 2: remaining 100 − 10 = 90 at hour 2... the
	// reset happens first, then that hour's burn: 90 left, 5 h to the next
	// reset, T_empty 9 h → headroom 1.
	approx(t, "headroom after reset", pts[2].Headroom, 1)
}
