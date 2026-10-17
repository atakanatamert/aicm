// SPDX-License-Identifier: Apache-2.0

// Package meter defines provider-native quota meters and their readings.
// Both the hub (provider adapters) and the agent (harness capture) produce
// these, so the package is Apache-2.0 and imports nothing from the hub.
//
// A meter keeps the provider's own key, label, unit and reset rule. Readings
// carry the provider's numbers untouched; the only derived figure anywhere is
// the ranking score in internal/quota.
package meter

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"time"
)

// Unit is what a meter's values count.
type Unit string

// Units. For UnitCurrency, values are minor units of Def.UnitCurrency
// (cents, fen), always whole numbers.
const (
	UnitPercent  Unit = "percent" // 0–100 used
	UnitCount    Unit = "count"
	UnitTokens   Unit = "tokens"
	UnitRequests Unit = "requests"
	UnitCredits  Unit = "credits"
	UnitCurrency Unit = "currency"
)

// ResetKind is how a meter's window resets.
type ResetKind string

// Reset kinds.
const (
	ResetNever         ResetKind = "never"          // a wallet; no reset
	ResetRolling       ResetKind = "rolling"        // restarts WindowMinutes after first use
	ResetFixed         ResetKind = "fixed"          // provider announces ResetsAt
	ResetCalendarMonth ResetKind = "calendar_month" // 1st of the month, UTC
)

// Source says where a reading came from. The UI shows it next to the number.
type Source string

// Sources.
const (
	SourceStatusline Source = "statusline"  // Claude Code statusline wrapper
	SourceStreamJSON Source = "stream_json" // claude -p rate_limit_event
	SourceRollout    Source = "rollout"     // Codex rollout log token_count
	SourceAppServer  Source = "app_server"  // codex app-server account/rateLimits/read
	SourceAPI        Source = "api"         // provider API (hub adapter or agent opt-in poll)
	SourceManual     Source = "manual"      // typed in by a user
)

var keyRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)

// Def declares a meter.
type Def struct {
	// Key is stable per provider, for example "five_hour", "seven_day", "balance".
	Key string `json:"key"`
	// Label is the provider's own name, for example "5-hour limit".
	Label string `json:"label"`
	Unit  Unit   `json:"unit"`
	// UnitCurrency is the ISO 4217 code when Unit is UnitCurrency.
	UnitCurrency string    `json:"unit_currency,omitempty"`
	ResetKind    ResetKind `json:"reset_kind"`
	// WindowMinutes is the window length for rolling or fixed windows, if known.
	WindowMinutes int `json:"window_minutes,omitempty"`
	// Limit is the known limit in Unit; nil when the provider doesn't say.
	Limit *float64 `json:"limit,omitempty"`
}

// Validate checks that the definition is well-formed.
func (d Def) Validate() error {
	var errs []error
	if !keyRe.MatchString(d.Key) {
		errs = append(errs, fmt.Errorf("key %q must match %s", d.Key, keyRe))
	}
	if d.Label == "" {
		errs = append(errs, errors.New("label is required"))
	}
	switch d.Unit {
	case UnitPercent, UnitCount, UnitTokens, UnitRequests, UnitCredits:
		if d.UnitCurrency != "" {
			errs = append(errs, errors.New("unit_currency is only allowed with unit currency"))
		}
	case UnitCurrency:
		if len(d.UnitCurrency) != 3 {
			errs = append(errs, errors.New("unit currency needs a 3-letter unit_currency"))
		}
	default:
		errs = append(errs, fmt.Errorf("unknown unit %q", d.Unit))
	}
	switch d.ResetKind {
	case ResetNever, ResetRolling, ResetFixed, ResetCalendarMonth:
	default:
		errs = append(errs, fmt.Errorf("unknown reset_kind %q", d.ResetKind))
	}
	if d.WindowMinutes < 0 {
		errs = append(errs, errors.New("window_minutes must not be negative"))
	}
	if d.Limit != nil && (*d.Limit < 0 || !finite(*d.Limit)) {
		errs = append(errs, errors.New("limit must be a finite non-negative number"))
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("meter %q: %w", d.Key, err)
	}
	return nil
}

// Reading is one observation of a meter, exactly as reported.
type Reading struct {
	// Key names the meter (Def.Key).
	Key       string   `json:"key"`
	Used      *float64 `json:"used,omitempty"`
	Remaining *float64 `json:"remaining,omitempty"`
	// Limit overrides Def.Limit for this reading when the provider reports it.
	Limit *float64 `json:"limit,omitempty"`
	// ResetsAt is the absolute time the window resets, if known.
	ResetsAt   *time.Time `json:"resets_at,omitempty"`
	CapturedAt time.Time  `json:"captured_at"`
	Source     Source     `json:"source"`
}

// Validate checks the reading against its meter definition. It rejects values
// a buggy source could produce, such as an epoch in a percent field.
func (r Reading) Validate(d Def) error {
	var errs []error
	if r.Key != d.Key {
		errs = append(errs, fmt.Errorf("reading key %q does not match meter %q", r.Key, d.Key))
	}
	if r.Used == nil && r.Remaining == nil {
		errs = append(errs, errors.New("used or remaining is required"))
	}
	if r.CapturedAt.IsZero() {
		errs = append(errs, errors.New("captured_at is required"))
	}
	switch r.Source {
	case SourceStatusline, SourceStreamJSON, SourceRollout, SourceAppServer, SourceAPI, SourceManual:
	default:
		errs = append(errs, fmt.Errorf("unknown source %q", r.Source))
	}
	for name, v := range map[string]*float64{"used": r.Used, "remaining": r.Remaining, "limit": r.Limit} {
		if v == nil {
			continue
		}
		switch {
		case !finite(*v):
			errs = append(errs, fmt.Errorf("%s is not finite", name))
		case d.Unit == UnitPercent && (*v < 0 || *v > 100):
			errs = append(errs, fmt.Errorf("%s %v is outside 0–100 percent", name, *v))
		case d.Unit == UnitCurrency && *v != math.Trunc(*v):
			errs = append(errs, fmt.Errorf("%s %v must be whole minor units", name, *v))
		case name == "limit" && *v < 0:
			errs = append(errs, errors.New("limit must not be negative"))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("reading for meter %q: %w", d.Key, err)
	}
	return nil
}

// Stale reports whether the window has reset since the reading was taken, so
// the reading no longer describes the current window (which is back at zero).
func (r Reading) Stale(now time.Time) bool {
	return r.ResetsAt != nil && !now.Before(*r.ResetsAt)
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
