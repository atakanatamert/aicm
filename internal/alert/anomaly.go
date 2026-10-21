// SPDX-License-Identifier: AGPL-3.0-or-later

package alert

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
)

// evalSpendAnomalies emits one warning for a day of usage well above the
// account's usual day and one for a burst of top-ups.
func (s *Service) evalSpendAnomalies(ctx context.Context, now time.Time, rules ruleSet,
	accounts []db.Account, emit func(candidate) error) error {
	today := dayUTC(now)
	rows, err := s.q.ListAlertUsageDaily(ctx, db.ListAlertUsageDailyParams{
		Since: today.AddDate(0, 0, -14),
		Today: today,
	})
	if err != nil {
		return fmt.Errorf("listing usage for anomalies: %w", err)
	}

	type usage struct {
		today int64
		days  []int64
	}
	byAccount := map[string]*usage{}
	for _, row := range rows {
		u := byAccount[row.AccountID]
		if u == nil {
			u = &usage{}
			byAccount[row.AccountID] = u
		}
		if row.Day.Equal(today) {
			u.today += row.CostMicro
		} else if row.CostMicro > 0 {
			u.days = append(u.days, row.CostMicro)
		}
	}

	byID := make(map[string]db.Account, len(accounts))
	for _, a := range accounts {
		byID[a.ID] = a
	}
	for accountID, u := range byAccount {
		a, ok := byID[accountID]
		if !ok {
			continue
		}
		rule, ok := rules.get(a.OrgID, a.ID, "spend_anomaly")
		if !ok || !ruleActive(rule, now) {
			continue
		}
		p := paramsOf(rule)
		median := medianOf(u.days)
		if median <= 0 || u.today <= 0 {
			continue
		}
		multiple := floatOr(p.UsageMultiple, 3)
		minDaily := floatOr(p.MinDailyCost, 5)
		if float64(u.today) < multiple*float64(median) || float64(u.today) < minDaily*1_000_000 {
			continue
		}
		ratio := math.Round(float64(u.today)/float64(median)*10) / 10
		name := displayName(a, s.infoFor(a.ProviderID))
		if err := emit(candidate{
			orgID:     a.OrgID,
			accountID: &a.ID,
			kind:      "spend_anomaly",
			eventKey:  fmt.Sprintf("anomaly:usage:%s:%s", a.ID, dateOf(now)),
			severity:  "warning",
			title:     fmt.Sprintf("Usage of %s is %s× its usual day", name, trimFloat(ratio)),
			body: fmt.Sprintf("Today's estimated usage is %s versus a usual %s.",
				usdMicro(u.today).String(), usdMicro(median).String()),
		}); err != nil {
			return err
		}
	}

	topups, err := s.q.ListAlertTopups(ctx, today.AddDate(0, 0, -1))
	if err != nil {
		return fmt.Errorf("listing top-ups for anomalies: %w", err)
	}
	for _, row := range topups {
		a, ok := byID[row.AccountID]
		if !ok {
			continue
		}
		rule, ok := rules.get(a.OrgID, a.ID, "spend_anomaly")
		if !ok || !ruleActive(rule, now) {
			continue
		}
		limit := 2
		if p := paramsOf(rule).TopupsPerDay; p != nil {
			limit = *p
		}
		if row.Topups < int64(limit) {
			continue
		}
		name := displayName(a, s.infoFor(a.ProviderID))
		if err := emit(candidate{
			orgID:     a.OrgID,
			accountID: &a.ID,
			kind:      "spend_anomaly",
			eventKey:  fmt.Sprintf("anomaly:topups:%s:%s", a.ID, dateOf(now)),
			severity:  "warning",
			title:     fmt.Sprintf("%d top-ups on %s in 24 hours", row.Topups, name),
			body:      "Repeated top-ups can mean a runaway job. Check the usage before adding more credit.",
		}); err != nil {
			return err
		}
	}
	return nil
}

// medianOf returns the median of non-empty costs, or 0 with none.
func medianOf(values []int64) int64 {
	if len(values) == 0 {
		return 0
	}
	sorted := append([]int64(nil), values...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// usdMicro renders micro-dollars as a USD amount.
func usdMicro(micro int64) money.Amount {
	return money.Amount{Minor: int64(math.Round(float64(micro) / 10_000)), Currency: "USD"}
}

// trimFloat formats a ratio without a trailing ".0".
func trimFloat(v float64) string {
	if v == math.Trunc(v) {
		return fmt.Sprintf("%.0f", v)
	}
	return fmt.Sprintf("%.1f", v)
}
