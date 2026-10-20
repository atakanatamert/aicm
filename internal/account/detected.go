// SPDX-License-Identifier: AGPL-3.0-or-later

package account

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/provider"
)

// DetectedPlan is a subscription an agent read from a harness: the catalog
// tier's list price and when the paid plan began.
type DetectedPlan struct {
	Price   provider.PlanPrice
	Started time.Time
}

// FillDetected sets a detected plan on an active account whose price and
// renewal date are both empty: monthly at the list price, renewing on the
// start date's day of month. When the account has no subscription charges
// yet it also books one estimated fee per past billing day since the start,
// so spend history covers the months before aicm saw the account. It
// returns false when the account already has a plan or is not active.
func FillDetected(ctx context.Context, q *db.Queries, actor audit.Actor, orgID, accountID string, plan DetectedPlan, today time.Time) (bool, error) {
	anchor := plan.Started.UTC().Day()
	passed, next := RollForward(plan.Started, today, "monthly", anchor)

	updated, err := q.FillDetectedPlan(ctx, db.FillDetectedPlanParams{
		PlanName:         plan.Price.Name,
		PriceMinor:       new(plan.Price.PriceMinor),
		Currency:         plan.Price.Currency,
		RenewsOn:         next,
		RenewalAnchorDay: new(int16(anchor)), //nolint:gosec // a day of month
		OrgID:            orgID,
		ID:               accountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("filling detected plan: %w", err)
	}

	existing, err := q.CountSubscriptionCharges(ctx, db.CountSubscriptionChargesParams{OrgID: orgID, AccountID: accountID})
	if err != nil {
		return false, fmt.Errorf("counting subscription charges: %w", err)
	}
	estimated := 0
	if existing == 0 {
		description := "Estimated from the " + plan.Price.Name + " list price"
		for _, day := range passed {
			n, err := q.InsertEstimatedCharge(ctx, db.InsertEstimatedChargeParams{
				OrgID:       orgID,
				AccountID:   accountID,
				AmountMinor: plan.Price.PriceMinor,
				Currency:    plan.Price.Currency,
				OccurredOn:  day,
				PeriodEnd:   NextRenewal(day, "monthly", anchor).AddDate(0, 0, -1),
				Description: description,
			})
			if err != nil {
				return false, fmt.Errorf("booking estimated fee for %s: %w", day.Format(time.DateOnly), err)
			}
			estimated += int(n)
		}
	}

	if err := audit.Write(ctx, q, actor, audit.Entry{
		Action:     "account.plan_detect",
		TargetKind: "account",
		TargetID:   updated.ID,
		Before:     map[string]any{"price_minor": nil, "renews_on": nil},
		After: map[string]any{
			"plan_name":         updated.PlanName,
			"price_minor":       plan.Price.PriceMinor,
			"currency":          plan.Price.Currency,
			"renews_on":         next.Format(time.DateOnly),
			"estimated_charges": estimated,
		},
	}); err != nil {
		return false, err
	}
	return true, nil
}
