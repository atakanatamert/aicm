// SPDX-License-Identifier: AGPL-3.0-or-later

package quota

import (
	"math"
	"sort"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
)

// The scoring rule from Decision 11. Everything here is pure: readings in,
// numbers out. The provider's own figures are never replaced; these numbers
// only rank accounts and explain the ranking.
//
// For each meter:
//
//	b         burn per hour: time-weighted mean of consumption between
//	          consecutive readings in the lookback, skipping pairs that cross
//	          a reset or show a top-up
//	R         remaining now (limit − used, or the reported remaining)
//	T_empty   R ÷ b (∞ when b = 0)
//	T_reset   time to the next reset (30 days for wallets that never reset)
//	headroom  min(1, T_empty ÷ T_reset), 0 when R ≤ 0
//	expiring  max(0, R − b·T_reset) ÷ limit, 0 for wallets
//
// An account's headroom is its weakest meter's; it is infeasible when any
// meter is exhausted.

// NoResetHorizon is T_reset for meters that never reset.
const NoResetHorizon = 30 * 24 * time.Hour

// MeterInput is one meter with its recent readings, oldest first.
type MeterInput struct {
	Def      meter.Def
	Readings []meter.Reading
}

// MeterScore is the derived view of one meter.
type MeterScore struct {
	Key string
	// Remaining and Limit are in the meter's unit; nil when unknown.
	Remaining *float64
	Limit     *float64
	// BurnPerHour is nil when fewer than two usable readings exist.
	BurnPerHour  *float64
	HoursToEmpty *float64 // nil when the burn is zero or unknown
	HoursToReset *float64 // nil when the next reset is unknown
	ResetsAt     *time.Time
	Headroom     float64
	Expiring     float64
	// Stale: the latest reading is past its resets_at, so the window is back to zero.
	Stale     bool
	Exhausted bool
	// NoData: the meter has no readings at all.
	NoData bool
}

// lookback is how far back burn rates look.
func lookback(d meter.Def) time.Duration {
	switch d.ResetKind {
	case meter.ResetNever, meter.ResetCalendarMonth:
		return 7 * 24 * time.Hour
	default:
		return 6 * time.Hour
	}
}

// limitOf returns the limit for a reading: the reading's own, the
// definition's, or 100 for percent meters.
func limitOf(d meter.Def, r meter.Reading) *float64 {
	switch {
	case r.Limit != nil:
		return r.Limit
	case d.Limit != nil:
		return d.Limit
	case d.Unit == meter.UnitPercent:
		return new(100.0)
	}
	return nil
}

// usedOf returns used, derived from remaining and limit when needed.
func usedOf(d meter.Def, r meter.Reading) *float64 {
	if r.Used != nil {
		return r.Used
	}
	if l := limitOf(d, r); l != nil && r.Remaining != nil {
		return new(*l - *r.Remaining)
	}
	return nil
}

// remainingOf returns remaining, derived from used and limit when needed.
func remainingOf(d meter.Def, r meter.Reading) *float64 {
	if r.Remaining != nil {
		return r.Remaining
	}
	if l := limitOf(d, r); l != nil && r.Used != nil {
		return new(*l - *r.Used)
	}
	return nil
}

func sameWindow(a, b meter.Reading) bool {
	if a.ResetsAt == nil || b.ResetsAt == nil {
		return a.ResetsAt == nil && b.ResetsAt == nil
	}
	// Providers jitter reset times by a few seconds between reports.
	d := a.ResetsAt.Sub(*b.ResetsAt)
	return d > -2*time.Minute && d < 2*time.Minute
}

// burn returns consumption per hour, or nil without two usable readings.
func burn(in MeterInput, now time.Time) *float64 {
	since := now.Add(-lookback(in.Def))
	var consumed, hours float64
	pairs := 0
	for i := 1; i < len(in.Readings); i++ {
		a, b := in.Readings[i-1], in.Readings[i]
		if a.CapturedAt.Before(since) || !b.CapturedAt.After(a.CapturedAt) || !sameWindow(a, b) {
			continue
		}
		var delta float64
		ua, ub := usedOf(in.Def, a), usedOf(in.Def, b)
		ra, rb := remainingOf(in.Def, a), remainingOf(in.Def, b)
		switch {
		case ua != nil && ub != nil:
			delta = *ub - *ua
		case ra != nil && rb != nil:
			delta = *ra - *rb
		default:
			continue
		}
		if delta < 0 { // a reset or a top-up, not consumption
			continue
		}
		consumed += delta
		hours += b.CapturedAt.Sub(a.CapturedAt).Hours()
		pairs++
	}
	if pairs == 0 || hours <= 0 {
		return nil
	}
	return new(consumed / hours)
}

// nextReset returns the next reset after now, or nil when unknown.
func nextReset(d meter.Def, latest meter.Reading, now time.Time) *time.Time {
	if latest.ResetsAt != nil && latest.ResetsAt.After(now) {
		return latest.ResetsAt
	}
	switch d.ResetKind {
	case meter.ResetCalendarMonth:
		y, m, _ := now.UTC().Date()
		return new(time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC))
	case meter.ResetRolling, meter.ResetFixed:
		if d.WindowMinutes > 0 {
			// Past the announced reset with no newer reading: the next window
			// ends at most one window length from now.
			return new(now.Add(time.Duration(d.WindowMinutes) * time.Minute))
		}
	}
	return nil
}

// ScoreMeter applies the scoring rule to one meter at time now.
func ScoreMeter(in MeterInput, now time.Time) MeterScore {
	s := MeterScore{Key: in.Def.Key}
	if len(in.Readings) == 0 {
		s.NoData = true
		s.Headroom = 1
		return s
	}
	latest := in.Readings[len(in.Readings)-1]
	s.Limit = limitOf(in.Def, latest)
	s.Remaining = remainingOf(in.Def, latest)
	s.Stale = latest.Stale(now)
	if s.Stale && s.Limit != nil {
		// The window reset since the reading: back to the full limit.
		s.Remaining = new(*s.Limit)
	}
	s.BurnPerHour = burn(in, now)
	s.ResetsAt = nextReset(in.Def, latest, now)

	var tReset float64
	switch {
	case in.Def.ResetKind == meter.ResetNever:
		tReset = NoResetHorizon.Hours()
	case s.ResetsAt != nil:
		tReset = math.Max(s.ResetsAt.Sub(now).Hours(), 0)
		s.HoursToReset = new(tReset)
	default:
		tReset = NoResetHorizon.Hours()
	}

	b := 0.0
	if s.BurnPerHour != nil {
		b = *s.BurnPerHour
	}
	if s.Remaining == nil {
		// Nothing to score against; don't let an unknown meter block the account.
		s.Headroom = 1
		return s
	}
	r := *s.Remaining
	if r <= 0 {
		s.Exhausted = true
		s.Headroom = 0
		return s
	}
	if b > 0 {
		s.HoursToEmpty = new(r / b)
		if tReset > 0 {
			s.Headroom = math.Min(1, r/b/tReset)
		} else {
			s.Headroom = 1
		}
	} else {
		s.Headroom = 1
	}
	if in.Def.ResetKind != meter.ResetNever && s.Limit != nil && *s.Limit > 0 {
		s.Expiring = clamp01(math.Max(0, r-b*tReset) / *s.Limit)
	}
	return s
}

// AccountScore combines an account's meters.
type AccountScore struct {
	Feasible bool
	// Headroom is the weakest meter's headroom (1 with no meters).
	Headroom float64
	// Expiring is the largest expiring share among the meters.
	Expiring float64
	// Binding is the key of the meter that sets Headroom, "" with no meters.
	Binding string
}

// ScoreAccount combines meter scores: weakest headroom, largest expiring
// share, infeasible when any meter is exhausted.
func ScoreAccount(ms []MeterScore) AccountScore {
	a := AccountScore{Feasible: true, Headroom: 1}
	for _, m := range ms {
		if m.Exhausted {
			a.Feasible = false
		}
		if m.Headroom < a.Headroom || (a.Binding == "" && m.Headroom == a.Headroom && !m.NoData) {
			a.Headroom = m.Headroom
			a.Binding = m.Key
		}
		a.Expiring = math.Max(a.Expiring, m.Expiring)
	}
	return a
}

// ForecastPoint is the projected account score at one hour.
type ForecastPoint struct {
	At       time.Time
	Headroom float64
	Feasible bool
}

// Forecast projects the account score hourly for the next hours, starting
// at now. Each meter keeps burning at b; when its reset passes, remaining
// returns to the limit and, for windows with a known length, the next reset
// moves one window later.
func Forecast(meters []MeterInput, now time.Time, hours int) []ForecastPoint {
	type state struct {
		def       meter.Def
		remaining *float64
		limit     *float64
		burn      float64
		reset     *time.Time
		window    time.Duration
	}
	states := make([]state, 0, len(meters))
	for _, in := range meters {
		s := ScoreMeter(in, now)
		st := state{def: in.Def, remaining: s.Remaining, limit: s.Limit, reset: s.ResetsAt, window: time.Duration(in.Def.WindowMinutes) * time.Minute}
		if s.BurnPerHour != nil {
			st.burn = *s.BurnPerHour
		}
		if in.Def.ResetKind == meter.ResetCalendarMonth {
			st.window = 0 // handled by nextMonth below
		}
		states = append(states, st)
	}

	out := make([]ForecastPoint, 0, hours+1)
	for h := 0; h <= hours; h++ {
		at := now.Add(time.Duration(h) * time.Hour)
		scores := make([]MeterScore, 0, len(states))
		for i := range states {
			st := &states[i]
			if st.remaining == nil {
				scores = append(scores, MeterScore{Key: st.def.Key, Headroom: 1})
				continue
			}
			// Roll resets that have passed by this hour.
			for st.reset != nil && !at.Before(*st.reset) && st.limit != nil {
				st.remaining = new(*st.limit)
				switch {
				case st.def.ResetKind == meter.ResetCalendarMonth:
					y, m, _ := st.reset.UTC().Date()
					st.reset = new(time.Date(y, m+1, 1, 0, 0, 0, 0, time.UTC))
				case st.window > 0:
					st.reset = new(st.reset.Add(st.window))
				default:
					st.reset = nil
				}
			}
			r := *st.remaining
			if h > 0 {
				r = math.Max(0, r-st.burn)
				st.remaining = new(r)
			}
			ms := MeterScore{Key: st.def.Key, Remaining: new(r)}
			tReset := NoResetHorizon.Hours()
			if st.def.ResetKind != meter.ResetNever && st.reset != nil {
				tReset = math.Max(st.reset.Sub(at).Hours(), 0)
			}
			switch {
			case r <= 0:
				ms.Exhausted = true
			case st.burn > 0 && tReset > 0:
				ms.Headroom = math.Min(1, r/st.burn/tReset)
			default:
				ms.Headroom = 1
			}
			scores = append(scores, ms)
		}
		a := ScoreAccount(scores)
		out = append(out, ForecastPoint{At: at, Headroom: a.Headroom, Feasible: a.Feasible})
	}
	return out
}

// Candidate is an account being ranked.
type Candidate struct {
	AccountID string
	Score     AccountScore
	// MarginalCostMicro is the cost per million tokens in micro units; 0
	// for flat-rate subscriptions.
	MarginalCostMicro int64
}

// Rank orders candidates: feasible first, then most expiring quota, then
// lowest marginal cost, then highest headroom, then account id for a stable
// order.
func Rank(cs []Candidate) {
	sort.SliceStable(cs, func(i, j int) bool {
		a, b := cs[i], cs[j]
		if a.Score.Feasible != b.Score.Feasible {
			return a.Score.Feasible
		}
		if !nearlyEqual(a.Score.Expiring, b.Score.Expiring) {
			return a.Score.Expiring > b.Score.Expiring
		}
		if a.MarginalCostMicro != b.MarginalCostMicro {
			return a.MarginalCostMicro < b.MarginalCostMicro
		}
		if !nearlyEqual(a.Score.Headroom, b.Score.Headroom) {
			return a.Score.Headroom > b.Score.Headroom
		}
		return a.AccountID < b.AccountID
	})
}

// nearlyEqual treats shares within half a percentage point as a tie, so
// noise in readings doesn't reorder accounts that are effectively equal.
func nearlyEqual(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func clamp01(f float64) float64 { return math.Max(0, math.Min(1, f)) }
