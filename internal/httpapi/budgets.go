// SPDX-License-Identifier: AGPL-3.0-or-later

package httpapi

import (
	"context"
	"errors"

	"github.com/atakanatamert/aicm/internal/budget"
	"github.com/atakanatamert/aicm/internal/money"
)

// budgetError maps the budget service's sentinel errors to HTTP responses.
func budgetError(err error) error {
	switch {
	case errors.Is(err, budget.ErrNotFound):
		return NotFound("account or budget not found")
	case errors.Is(err, budget.ErrInvalid):
		return BadRequest(err.Error())
	}
	return err
}

// toBudget maps one budget status to the API shape.
func toBudget(st budget.Status) Budget {
	out := Budget{
		Id:               st.Budget.ID,
		Amount:           Money{AmountMinor: st.Budget.AmountMinor, Currency: st.Budget.Currency},
		Thresholds:       intsOf(st.Budget.Thresholds),
		Period:           st.Period,
		MonthToDate:      Money{AmountMinor: st.MonthToDate.Minor, Currency: st.MonthToDate.Currency},
		Projected:        Money{AmountMinor: st.Projected.Minor, Currency: st.Projected.Currency},
		PercentUsed:      st.PercentUsed,
		UnconvertedCount: st.UnconvertedCount,
	}
	if st.Budget.AccountID != nil {
		out.AccountId = st.Budget.AccountID
		if st.AccountName != "" {
			out.AccountName = &st.AccountName
		}
	}
	return out
}

// intsOf widens the stored integer[] thresholds to the API's []int.
func intsOf(in []int32) []int {
	out := make([]int, len(in))
	for i, v := range in {
		out[i] = int(v)
	}
	return out
}

// ListBudgets returns the org budget and the budgets of visible accounts.
func (s *Server) ListBudgets(ctx context.Context, _ ListBudgetsRequestObject) (ListBudgetsResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.Budgets.List(ctx, p)
	if err != nil {
		return nil, budgetError(err)
	}
	out := make([]Budget, 0, len(rows))
	for _, st := range rows {
		out = append(out, toBudget(st))
	}
	return ListBudgets200JSONResponse(out), nil
}

// PutBudget creates or replaces the org budget or one account's budget.
func (s *Server) PutBudget(ctx context.Context, req PutBudgetRequestObject) (PutBudgetResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if req.Body == nil {
		return nil, BadRequest("body is required")
	}
	in := budget.Input{
		AccountID: req.Body.AccountId,
		Amount: money.Amount{
			Minor:    req.Body.Amount.AmountMinor,
			Currency: req.Body.Amount.Currency,
		},
	}
	if req.Body.Thresholds != nil {
		in.Thresholds = *req.Body.Thresholds
	}
	st, err := s.Budgets.Put(ctx, p, in)
	if err != nil {
		return nil, budgetError(err)
	}
	return PutBudget200JSONResponse(toBudget(st)), nil
}

// DeleteBudget removes a budget.
func (s *Server) DeleteBudget(ctx context.Context, req DeleteBudgetRequestObject) (DeleteBudgetResponseObject, error) {
	p, err := requirePrincipal(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.Budgets.Delete(ctx, p, req.BudgetId); err != nil {
		return nil, budgetError(err)
	}
	return DeleteBudget204Response{}, nil
}
