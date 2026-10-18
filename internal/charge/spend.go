// SPDX-License-Identifier: AGPL-3.0-or-later

package charge

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
)

// SpendRequest is one spend report query.
type SpendRequest struct {
	OrgID, UserID string
	IsAdmin       bool
	Period        string    // month | year
	From, To      time.Time // inclusive days
	GroupBy       string    // provider | account
	Currency      string    // conversion target
	FXSource      string    // org preference
}

// Report is the spend per period and group.
type Report struct {
	DisplayCurrency string
	Rows            []SpendRow
	Totals          []SpendTotal
}

// SpendRow is one period and group.
type SpendRow struct {
	Period           string
	ProviderID       string
	AccountID        *string
	AccountName      *string
	Billed           []money.Amount
	Converted        *money.Amount
	UnconvertedCount int
}

// SpendTotal is one period across all groups.
type SpendTotal struct {
	Period    string
	Billed    []money.Amount
	Converted *money.Amount
}

// bucket accumulates one period/group (or one period for totals). Billed is
// kept per currency so two currencies are never added. Converted is the sum
// of each charge converted at its own date; it is absent when any charge had
// no rate.
type bucket struct {
	period      string
	group       string
	providerID  string
	accountID   *string
	accountName *string
	billed      map[string]money.Amount
	converted   money.Amount
	convertedOK bool
	unconverted int
}

func (b *bucket) add(ctx context.Context, fxSvc *fx.Service, row db.ListChargesRow, r SpendRequest) error {
	sign := int64(1)
	if row.Kind == "refund" {
		sign = -1
	}
	amount := money.Amount{Minor: row.AmountMinor, Currency: row.Currency}
	if err := addTo(b.billed, money.Amount{Minor: sign * amount.Minor, Currency: amount.Currency}); err != nil {
		return err
	}

	converted := amount
	if fxSvc != nil {
		var err error
		converted, _, err = fxSvc.Convert(ctx, amount, r.Currency, row.OccurredOn, r.FXSource)
		switch {
		case errors.Is(err, fx.ErrNoRate):
			b.convertedOK = false
			b.unconverted++
			return nil
		case err != nil:
			return fmt.Errorf("converting charge %s: %w", row.ID, err)
		}
	} else if amount.Currency != r.Currency {
		b.convertedOK = false
		b.unconverted++
		return nil
	}
	if err := addToAmount(&b.converted, money.Amount{Minor: sign * converted.Minor, Currency: r.Currency}); err != nil {
		return err
	}
	return nil
}

// Spend sums confirmed charges visible to the user in [From, To], per
// period and provider (or account). Billed stays per currency; converted
// converts every charge at its own date and is absent from a bucket when
// any of its charges has no rate.
func (s *Service) Spend(ctx context.Context, r SpendRequest) (Report, error) {
	if r.Period != "month" && r.Period != "year" {
		return Report{}, fmt.Errorf("%w: period must be month or year", ErrInvalid)
	}
	if r.GroupBy == "" {
		r.GroupBy = "provider"
	}
	if r.GroupBy != "provider" && r.GroupBy != "account" {
		return Report{}, fmt.Errorf("%w: group_by must be provider or account", ErrInvalid)
	}
	if !money.ValidCurrency(r.Currency) {
		return Report{}, fmt.Errorf("%w: invalid currency %q", ErrInvalid, r.Currency)
	}
	if r.To.Before(r.From) {
		return Report{}, fmt.Errorf("%w: to is before from", ErrInvalid)
	}
	from, to := dayUTC(r.From), dayUTC(r.To)
	uid := r.UserID
	if uid == "" {
		uid = "00000000-0000-0000-0000-000000000000"
	}
	rows, err := s.q.ListCharges(ctx, db.ListChargesParams{
		OrgID:   r.OrgID,
		UserID:  uid,
		IsAdmin: r.IsAdmin,
		FromDay: &from,
		ToDay:   &to,
		Status:  "confirmed",
	})
	if err != nil {
		return Report{}, fmt.Errorf("listing charges for spend: %w", err)
	}

	buckets := map[string]*bucket{}
	totals := map[string]*bucket{}
	for _, row := range rows {
		period := periodKey(row.OccurredOn, r.Period)
		group := row.ProviderID
		if r.GroupBy == "account" {
			group = row.AccountID
		}
		key := period + "\x00" + group
		b := buckets[key]
		if b == nil {
			b = &bucket{
				period: period, group: group, providerID: row.ProviderID,
				billed: map[string]money.Amount{}, convertedOK: true,
			}
			if r.GroupBy == "account" {
				b.accountID = new(row.AccountID)
				b.accountName = new(row.AccountName)
			}
			buckets[key] = b
		}
		if err := b.add(ctx, s.fx, row, r); err != nil {
			return Report{}, err
		}

		t := totals[period]
		if t == nil {
			t = &bucket{period: period, billed: map[string]money.Amount{}, convertedOK: true}
			totals[period] = t
		}
		if err := t.add(ctx, s.fx, row, r); err != nil {
			return Report{}, err
		}
	}

	out := Report{DisplayCurrency: r.Currency, Rows: make([]SpendRow, 0, len(buckets)), Totals: make([]SpendTotal, 0, len(totals))}
	for _, b := range buckets {
		row := SpendRow{
			Period: b.period, ProviderID: b.providerID,
			AccountID: b.accountID, AccountName: b.accountName,
			Billed: sortedAmounts(b.billed), UnconvertedCount: b.unconverted,
		}
		if b.convertedOK {
			c := b.converted
			row.Converted = &c
		}
		out.Rows = append(out.Rows, row)
	}
	sort.Slice(out.Rows, func(i, j int) bool {
		if out.Rows[i].Period != out.Rows[j].Period {
			return out.Rows[i].Period < out.Rows[j].Period
		}
		return groupKey(out.Rows[i]) < groupKey(out.Rows[j])
	})

	for _, b := range totals {
		total := SpendTotal{Period: b.period, Billed: sortedAmounts(b.billed)}
		if b.convertedOK {
			c := b.converted
			total.Converted = &c
		}
		out.Totals = append(out.Totals, total)
	}
	sort.Slice(out.Totals, func(i, j int) bool { return out.Totals[i].Period < out.Totals[j].Period })
	return out, nil
}

func groupKey(r SpendRow) string {
	if r.AccountID != nil {
		return *r.AccountID
	}
	return r.ProviderID
}

func periodKey(occurred time.Time, period string) string {
	if period == "year" {
		return occurred.UTC().Format("2006")
	}
	return occurred.UTC().Format("2006-01")
}

func addTo(billed map[string]money.Amount, a money.Amount) error {
	cur, ok := billed[a.Currency]
	if !ok {
		billed[a.Currency] = a
		return nil
	}
	sum, err := cur.Add(a)
	if err != nil {
		return fmt.Errorf("summing billed amounts: %w", err)
	}
	billed[a.Currency] = sum
	return nil
}

func addToAmount(dst *money.Amount, a money.Amount) error {
	if dst.Minor == 0 && dst.Currency == "" {
		*dst = a
		return nil
	}
	sum, err := dst.Add(a)
	if err != nil {
		return fmt.Errorf("summing converted amounts: %w", err)
	}
	*dst = sum
	return nil
}

func sortedAmounts(m map[string]money.Amount) []money.Amount {
	out := make([]money.Amount, 0, len(m))
	for _, a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Currency < out[j].Currency })
	return out
}
