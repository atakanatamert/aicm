// SPDX-License-Identifier: SUL-1.0

package charge

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/quota"
)

// TopUpHook suggests a top-up when a currency wallet's remaining balance
// jumped up compared with the previous reading. Meters whose key contains
// "granted" or "voucher" are free credit and never produce a top-up.
func (s *Service) TopUpHook() quota.BalanceHook {
	return func(ctx context.Context, orgID, accountID string, def meter.Def, prev *meter.Reading, cur meter.Reading) error {
		if strings.Contains(def.Key, "granted") || strings.Contains(def.Key, "voucher") {
			return nil
		}
		if prev == nil || prev.Remaining == nil || cur.Remaining == nil {
			return nil
		}
		increase := *cur.Remaining - *prev.Remaining
		if increase <= 0 || increase >= float64(math.MaxInt64) {
			return nil
		}
		before := money.Amount{Minor: int64(*prev.Remaining), Currency: def.UnitCurrency}
		after := money.Amount{Minor: int64(*cur.Remaining), Currency: def.UnitCurrency}
		occurred := dayUTC(cur.CapturedAt)

		tx, err := s.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("starting top-up detection: %w", err)
		}
		defer func() { _ = tx.Rollback(ctx) }()
		q := s.q.WithTx(tx)

		created, err := q.InsertCharge(ctx, db.InsertChargeParams{
			OrgID:       orgID,
			AccountID:   accountID,
			Kind:        "topup",
			Status:      "suggested",
			Source:      "detected",
			AmountMinor: int64(increase),
			Currency:    def.UnitCurrency,
			OccurredOn:  occurred,
			Description: fmt.Sprintf("Balance rose from %s to %s", before.Decimal(), after.Decimal()),
		})
		if err != nil {
			return fmt.Errorf("inserting suggested top-up: %w", err)
		}
		p := auth.Principal{OrgID: orgID, Kind: auth.ActorSystem, Label: "top-up detection"}
		if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
			Action:     "charge.create",
			TargetKind: "charge",
			TargetID:   created.ID,
			After:      created,
		}); err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("committing suggested top-up: %w", err)
		}
		return nil
	}
}
