// SPDX-License-Identifier: AGPL-3.0-or-later

package charge

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

// RecordAPICharges stores charges a provider adapter fetched, as confirmed
// charges with source api, in one transaction. Charges without an external
// id are rejected: without it a repeated sync would double-count. It
// implements syncer.ChargeSink.
func (s *Service) RecordAPICharges(ctx context.Context, orgID, accountID string, cs []provider.Charge) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting api charge import: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := db.New(tx)
	actor := auth.Principal{OrgID: orgID, Kind: auth.ActorSystem, Label: "provider sync"}.Actor()

	for _, c := range cs {
		if c.ExternalID == "" {
			return fmt.Errorf("charge on %s has no external id", c.OccurredOn.Format("2006-01-02"))
		}
		if !money.ValidCurrency(c.Amount.Currency) || c.Amount.Minor < 0 {
			return fmt.Errorf("charge %s has an invalid amount %v", c.ExternalID, c.Amount)
		}
		row, err := q.InsertAPICharge(ctx, db.InsertAPIChargeParams{
			OrgID: orgID, AccountID: accountID, Kind: string(c.Kind),
			AmountMinor: c.Amount.Minor, Currency: c.Amount.Currency,
			OccurredOn: dayUTC(c.OccurredOn), PeriodStart: c.PeriodStart, PeriodEnd: c.PeriodEnd,
			ExternalID: new(c.ExternalID), Description: c.Description,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			continue // already imported
		}
		if err != nil {
			return fmt.Errorf("inserting api charge %s: %w", c.ExternalID, err)
		}
		if err := audit.Write(ctx, q, actor, audit.Entry{
			Action: "charge.create", TargetKind: "charge", TargetID: row.ID,
			After: map[string]any{"kind": row.Kind, "amount_minor": row.AmountMinor, "currency": row.Currency, "external_id": c.ExternalID},
		}); err != nil {
			return err //nolint:wrapcheck // audit.Write wraps
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing api charges: %w", err)
	}
	return nil
}
