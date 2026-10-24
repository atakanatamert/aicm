// SPDX-License-Identifier: SUL-1.0

// Package budget stores the org's monthly budgets and reports their status:
// month-to-date confirmed spend converted per charge to the budget currency,
// the projection to the end of the month and the share of the budget used.
package budget

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/audit"
	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/fx"
	"github.com/atakanatamert/aicm/internal/money"
)

// Errors returned by the service.
var (
	ErrNotFound = errors.New("budget not found")
	ErrInvalid  = errors.New("invalid budget")
)

// DefaultThresholds fire when a budget is 80% and 100% used.
var DefaultThresholds = []int{80, 100}

// Status is one budget with this month's numbers.
type Status struct {
	Budget      db.Budget
	AccountName string
	OrgName     string
	// Period is the current UTC month, YYYY-MM.
	Period           string
	MonthToDate      money.Amount
	Projected        money.Amount
	PercentUsed      float64
	UnconvertedCount int
}

// Input is one upsert. A nil AccountID writes the org budget.
type Input struct {
	AccountID  *string
	Amount     money.Amount
	Thresholds []int
}

// Service reads and writes budgets.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
	fx   *fx.Service
}

// NewService builds the budget service. fxSvc may be nil, in which case
// charges in another currency count as unconverted.
func NewService(pool *pgxpool.Pool, fxSvc *fx.Service) *Service {
	return &Service{pool: pool, q: db.New(pool), fx: fxSvc}
}

// List returns the budgets the principal may see. The org budget's sums
// still cover every account (it is the org's money), even for a viewer who
// cannot see all accounts individually.
func (s *Service) List(ctx context.Context, p auth.Principal) ([]Status, error) {
	ids, err := s.q.ListVisibleBudgetIDs(ctx, db.ListVisibleBudgetIDsParams{
		OrgID: p.OrgID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing visible budgets: %w", err)
	}
	visible := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		visible[id] = struct{}{}
	}
	all, err := s.Status(ctx, p.OrgID, time.Now())
	if err != nil {
		return nil, err
	}
	out := make([]Status, 0, len(visible))
	for _, st := range all {
		if _, ok := visible[st.Budget.ID]; ok {
			out = append(out, st)
		}
	}
	return out, nil
}

// Status reports every budget in the org at now. The month is the current
// UTC month. MonthToDate is confirmed subscription, top-up and invoice money
// out minus refunds, each charge converted to the budget currency at its own
// date. Projected adds the plan price of each auto-renewing account still
// due this month, converted at today's rate.
func (s *Service) Status(ctx context.Context, orgID string, now time.Time) ([]Status, error) {
	now = now.UTC()
	from := monthStart(now)
	to := from.AddDate(0, 1, 0)
	today := dayUTC(now)

	rows, err := s.q.ListBudgetsForOrg(ctx, orgID)
	if err != nil {
		return nil, fmt.Errorf("listing budgets: %w", err)
	}
	charges, err := s.q.ListBudgetCharges(ctx, db.ListBudgetChargesParams{
		OrgID: orgID, FromDay: from, ToDay: to,
	})
	if err != nil {
		return nil, fmt.Errorf("listing budget charges: %w", err)
	}
	renewals, err := s.q.ListBudgetRenewals(ctx, db.ListBudgetRenewalsParams{
		OrgID: orgID, Today: today, MonthEnd: to,
	})
	if err != nil {
		return nil, fmt.Errorf("listing budget renewals: %w", err)
	}
	fxSource := ""
	settings, err := s.q.GetOrgSettings(ctx, orgID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return nil, fmt.Errorf("loading org settings for budgets: %w", err)
	default:
		fxSource = settings.FxSource
	}

	period := from.Format("2006-01")
	out := make([]Status, 0, len(rows))
	for _, row := range rows {
		st := Status{
			Budget: db.Budget{
				ID:          row.ID,
				OrgID:       row.OrgID,
				AccountID:   row.AccountID,
				AmountMinor: row.AmountMinor,
				Currency:    row.Currency,
				Thresholds:  row.Thresholds,
				CreatedAt:   row.CreatedAt,
				UpdatedAt:   row.UpdatedAt,
			},
			AccountName: row.AccountName,
			OrgName:     row.OrgName,
			Period:      period,
			MonthToDate: money.Amount{Currency: row.Currency},
			Projected:   money.Amount{Currency: row.Currency},
		}
		for _, c := range charges {
			if row.AccountID != nil && c.AccountID != *row.AccountID {
				continue
			}
			amount, ok, err := s.convert(ctx, money.Amount{Minor: c.AmountMinor, Currency: c.Currency}, row.Currency, c.OccurredOn, fxSource)
			if err != nil {
				return nil, fmt.Errorf("converting charge %s for budget %s: %w", c.ID, row.ID, err)
			}
			if !ok {
				st.UnconvertedCount++
				continue
			}
			if c.Kind == "refund" {
				amount.Minor = -amount.Minor
			}
			st.MonthToDate, err = st.MonthToDate.Add(amount)
			if err != nil {
				return nil, fmt.Errorf("summing charges for budget %s: %w", row.ID, err)
			}
		}

		st.Projected = st.MonthToDate
		for _, account := range renewals {
			if row.AccountID != nil && account.ID != *row.AccountID {
				continue
			}
			if account.PriceMinor == nil {
				continue
			}
			price, ok, err := s.convert(ctx, money.Amount{Minor: *account.PriceMinor, Currency: account.Currency}, row.Currency, today, fxSource)
			if err != nil {
				return nil, fmt.Errorf("converting renewal for account %s: %w", account.ID, err)
			}
			if !ok {
				continue // a renewal without a rate stays out, like an unconverted charge
			}
			st.Projected, err = st.Projected.Add(price)
			if err != nil {
				return nil, fmt.Errorf("adding renewal for budget %s: %w", row.ID, err)
			}
		}
		if row.AmountMinor > 0 {
			st.PercentUsed = float64(st.MonthToDate.Minor) / float64(row.AmountMinor) * 100
		}
		out = append(out, st)
	}
	return out, nil
}

// Put creates or replaces the budget for the account (or the org when
// AccountID is nil) and audits it.
func (s *Service) Put(ctx context.Context, p auth.Principal, in Input) (Status, error) {
	if in.Amount.Minor <= 0 {
		return Status{}, fmt.Errorf("%w: amount must be greater than zero", ErrInvalid)
	}
	if !money.ValidCurrency(in.Amount.Currency) {
		return Status{}, fmt.Errorf("%w: invalid currency %q", ErrInvalid, in.Amount.Currency)
	}
	thresholds, err := normalizeThresholds(in.Thresholds)
	if err != nil {
		return Status{}, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return Status{}, fmt.Errorf("starting budget put: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	if in.AccountID != nil {
		_, err := q.GetVisibleAccount(ctx, db.GetVisibleAccountParams{
			OrgID: p.OrgID, AccountID: *in.AccountID, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return Status{}, ErrNotFound
		}
		if err != nil {
			return Status{}, fmt.Errorf("loading budget account: %w", err)
		}
	}

	before, err := q.GetBudgetForAccount(ctx, db.GetBudgetForAccountParams{
		OrgID: p.OrgID, AccountID: in.AccountID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		before = db.Budget{}
	} else if err != nil {
		return Status{}, fmt.Errorf("loading existing budget: %w", err)
	}
	hadBefore := before.ID != ""

	row, err := q.UpsertBudget(ctx, db.UpsertBudgetParams{
		OrgID:       p.OrgID,
		AccountID:   in.AccountID,
		AmountMinor: in.Amount.Minor,
		Currency:    in.Amount.Currency,
		Thresholds:  int32s(thresholds),
	})
	if err != nil {
		return Status{}, fmt.Errorf("upserting budget: %w", err)
	}

	var beforeValue any
	if hadBefore {
		beforeValue = budgetSnapshot(before)
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "budget.put",
		TargetKind: "budget",
		TargetID:   row.ID,
		Before:     beforeValue,
		After:      budgetSnapshot(row),
	}); err != nil {
		return Status{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return Status{}, fmt.Errorf("committing budget put: %w", err)
	}
	return s.get(ctx, p.OrgID, row.ID)
}

// Delete removes a visible budget and audits the deletion.
func (s *Service) Delete(ctx context.Context, p auth.Principal, id string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting budget delete: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	before, err := q.GetVisibleBudget(ctx, db.GetVisibleBudgetParams{
		OrgID: p.OrgID, ID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("loading budget: %w", err)
	}
	n, err := q.DeleteVisibleBudget(ctx, db.DeleteVisibleBudgetParams{
		OrgID: p.OrgID, ID: id, UserID: userIDOf(p), IsAdmin: p.IsAdmin(),
	})
	if err != nil {
		return fmt.Errorf("deleting budget: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	if err := audit.Write(ctx, q, p.Actor(), audit.Entry{
		Action:     "budget.delete",
		TargetKind: "budget",
		TargetID:   id,
		Before:     budgetSnapshot(before),
	}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing budget delete: %w", err)
	}
	return nil
}

// get returns one budget with its status after a write.
func (s *Service) get(ctx context.Context, orgID, id string) (Status, error) {
	all, err := s.Status(ctx, orgID, time.Now())
	if err != nil {
		return Status{}, err
	}
	for _, st := range all {
		if st.Budget.ID == id {
			return st, nil
		}
	}
	return Status{}, ErrNotFound
}

// convert changes a to the budget currency at on. ok is false when no rate
// exists; that amount is left out of the total instead of failing the read.
func (s *Service) convert(ctx context.Context, a money.Amount, to string, on time.Time, fxSource string) (money.Amount, bool, error) {
	if a.Currency == to {
		return a, true, nil
	}
	if s.fx == nil {
		return money.Amount{}, false, nil
	}
	out, _, err := s.fx.Convert(ctx, a, to, on, fxSource)
	if errors.Is(err, fx.ErrNoRate) {
		return money.Amount{}, false, nil
	}
	if err != nil {
		return money.Amount{}, false, err
	}
	return out, true, nil
}

// normalizeThresholds defaults, sorts and deduplicates the thresholds.
func normalizeThresholds(in []int) ([]int, error) {
	if len(in) == 0 {
		return append([]int(nil), DefaultThresholds...), nil
	}
	seen := make(map[int]struct{}, len(in))
	out := make([]int, 0, len(in))
	for _, v := range in {
		if v < 1 || v > 1000 {
			return nil, fmt.Errorf("%w: thresholds must be between 1 and 1000", ErrInvalid)
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sort.Ints(out)
	return out, nil
}

func budgetSnapshot(b db.Budget) map[string]any {
	return map[string]any{
		"account_id":   b.AccountID,
		"amount_minor": b.AmountMinor,
		"currency":     b.Currency,
		"thresholds":   b.Thresholds,
	}
}

func int32s(in []int) []int32 {
	out := make([]int32, len(in))
	for i, v := range in {
		out[i] = int32(v) //nolint:gosec // normalizeThresholds keeps 1..1000
	}
	return out
}

// userIDOf returns a uuid for account_visible. Principals without a user id
// see only what an admin sees.
func userIDOf(p auth.Principal) string {
	if p.UserID == "" {
		return "00000000-0000-0000-0000-000000000000"
	}
	return p.UserID
}

func dayUTC(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func monthStart(t time.Time) time.Time {
	y, m, _ := t.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}
