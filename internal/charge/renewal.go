// SPDX-License-Identifier: AGPL-3.0-or-later

package charge

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/atakanatamert/aicm/internal/account"
	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
)

// RenewalHook records the subscription fee for a passed renewal date.
//
// The charge uses the account's plan price and currency, kind subscription,
// source renewal, status confirmed and the period [renewedOn, next renewal).
// It inserts with ON CONFLICT DO NOTHING on charges_renewal, so running the
// roll-forward twice records one charge. Accounts without a price are skipped.
func (s *Service) RenewalHook() account.OnRenewal {
	return func(ctx context.Context, q *db.Queries, a db.Account, renewedOn time.Time) error {
		if a.PriceMinor == nil {
			return nil
		}
		start := dayUTC(renewedOn)
		anchor := 0
		if a.RenewalAnchorDay != nil {
			anchor = int(*a.RenewalAnchorDay)
		}
		end := account.NextRenewal(start, a.BillingCycle, anchor)
		description := a.PlanName
		if description == "" {
			description = "Subscription renewal"
		}

		created, err := q.InsertRenewalCharge(ctx, db.InsertRenewalChargeParams{
			OrgID:       a.OrgID,
			AccountID:   a.ID,
			AmountMinor: *a.PriceMinor,
			Currency:    a.Currency,
			OccurredOn:  start,
			PeriodStart: start,
			PeriodEnd:   end,
			Description: description,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil // already recorded for this date
		}
		if err != nil {
			return fmt.Errorf("inserting renewal charge: %w", err)
		}
		p := auth.Principal{OrgID: a.OrgID, Kind: auth.ActorSystem, Label: "renewal job"}
		if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
			Action:     "charge.create",
			TargetKind: "charge",
			TargetID:   created.ID,
			After:      created,
		}); err != nil {
			return err
		}
		return nil
	}
}
