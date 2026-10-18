// SPDX-License-Identifier: AGPL-3.0-or-later

package account

import "time"

// NextRenewal returns the renewal after prev for the cycle, on anchorDay
// clamped to the month's length. Weekly adds 7 days. Yearly keeps month and
// anchor day (Feb 29 anchor → Feb 28 in non-leap years, Feb 29 again in leap years).
func NextRenewal(prev time.Time, cycle string, anchorDay int) time.Time {
	prev = dayUTC(prev)
	switch cycle {
	case "weekly":
		return prev.AddDate(0, 0, 7)
	case "monthly":
		year, month, _ := prev.Date()
		return clampDay(year, month+1, anchorDay)
	case "yearly":
		return clampDay(prev.Year()+1, prev.Month(), anchorDay)
	default:
		return prev
	}
}

// RollForward returns every renewal date d with prev <= d < today in order,
// and the first date >= today. For cycle "none" it returns (nil, prev).
func RollForward(prev, today time.Time, cycle string, anchorDay int) (passed []time.Time, next time.Time) {
	prev = dayUTC(prev)
	today = dayUTC(today)
	if cycle == "none" {
		return nil, prev
	}
	current := prev
	for current.Before(today) {
		passed = append(passed, current)
		following := NextRenewal(current, cycle, anchorDay)
		if !following.After(current) {
			break
		}
		current = following
	}
	return passed, current
}

// clampDay returns midnight UTC on day in the given month, clamped to the
// month's last day.
func clampDay(year int, month time.Month, day int) time.Time {
	if day < 1 {
		day = 1
	}
	last := time.Date(year, month+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

// dayUTC truncates t to midnight UTC.
func dayUTC(t time.Time) time.Time {
	year, month, day := t.UTC().Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}
