// SPDX-License-Identifier: SUL-1.0

package alert

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/atakanatamert/aicm/internal/money"
)

// evalBudgets emits one alert per budget threshold reached and one when the
// month's projection is on track to exceed the budget.
func (s *Service) evalBudgets(ctx context.Context, now time.Time, rules ruleSet, emit func(candidate) error) error {
	if s.budgets == nil {
		return nil
	}
	orgs, err := s.q.ListAlertOrgs(ctx)
	if err != nil {
		return fmt.Errorf("listing orgs for budgets: %w", err)
	}
	for _, org := range orgs {
		statuses, err := s.budgets.Status(ctx, org.ID, now)
		if err != nil {
			return fmt.Errorf("reading budgets for org %s: %w", org.ID, err)
		}
		for _, st := range statuses {
			accountID := ""
			if st.Budget.AccountID != nil {
				accountID = *st.Budget.AccountID
			}
			rule, ok := rules.get(org.ID, accountID, "budget")
			if !ok || !ruleActive(rule, now) {
				continue
			}
			// A budget's own thresholds customize it; the rule's list is the
			// fallback for a budget stored without any.
			thresholds := make([]int, 0, len(st.Budget.Thresholds))
			for _, t := range st.Budget.Thresholds {
				thresholds = append(thresholds, int(t))
			}
			if len(thresholds) == 0 {
				thresholds = paramsOf(rule).Thresholds
			}
			name := budgetName(st.OrgName, st.AccountName, st.Budget.AccountID)
			amount := money.Amount{Minor: st.Budget.AmountMinor, Currency: st.Budget.Currency}
			for _, t := range thresholds {
				if st.PercentUsed < float64(t) {
					continue
				}
				severity := "warning"
				if t >= 100 {
					severity = "critical"
				}
				if err := emit(candidate{
					orgID:     org.ID,
					accountID: st.Budget.AccountID,
					kind:      "budget",
					eventKey:  fmt.Sprintf("budget:%s:%s:%d", st.Budget.ID, st.Period, t),
					severity:  severity,
					title: fmt.Sprintf("Budget: %s at %d%% of %s",
						name, int(math.Round(st.PercentUsed)), amount.String()),
					body: fmt.Sprintf("Spent %s of %s this month.",
						money.Amount{Minor: st.MonthToDate.Minor, Currency: st.MonthToDate.Currency}.String(),
						amount.String()),
				}); err != nil {
					return err
				}
			}
			if st.Projected.Minor >= st.Budget.AmountMinor && st.PercentUsed < 100 {
				if err := emit(candidate{
					orgID:     org.ID,
					accountID: st.Budget.AccountID,
					kind:      "budget",
					eventKey:  fmt.Sprintf("budget:%s:%s:projected", st.Budget.ID, st.Period),
					severity:  "info",
					title: fmt.Sprintf("Budget: %s is on track to exceed %s at %s",
						name, amount.String(),
						money.Amount{Minor: st.Projected.Minor, Currency: st.Projected.Currency}.String()),
					body: fmt.Sprintf("Projected %s by the end of %s.",
						money.Amount{Minor: st.Projected.Minor, Currency: st.Projected.Currency}.String(), st.Period),
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// budgetName is the account's name, or the org's for the org-wide budget.
func budgetName(orgName, accountName string, accountID *string) string {
	if accountID != nil {
		if accountName != "" {
			return accountName
		}
		return "account"
	}
	if orgName != "" {
		return orgName
	}
	return "organization"
}
