// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"context"
	"fmt"
	"time"

	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/usage"
)

// GetUsage returns tokens, requests and estimated cost grouped by one key.
func (s *Server) GetUsage(ctx context.Context, req GetUsageRequestObject) (GetUsageResponseObject, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return nil, ErrUnauthorized
	}

	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	if req.Params.From != nil {
		from = utcDay(req.Params.From.Time)
	}
	if req.Params.To != nil {
		to = utcDay(req.Params.To.Time)
	}
	if from.After(to) {
		return nil, BadRequest("from must not be after to")
	}
	if to.Sub(from) > 400*24*time.Hour {
		return nil, BadRequest("range must be at most 400 days")
	}

	query := usage.Query{
		OrgID:   p.OrgID,
		UserID:  p.UserID,
		IsAdmin: p.IsAdmin(),
		From:    from,
		To:      to,
		GroupBy: string(req.Params.GroupBy),
	}
	if req.Params.AccountId != nil {
		query.AccountID = *req.Params.AccountId
	}
	if req.Params.ProviderId != nil {
		query.ProviderID = *req.Params.ProviderId
	}

	rows, err := s.Usage.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("querying usage: %w", err)
	}

	out := make([]UsageRow, len(rows))
	for i, r := range rows {
		row := UsageRow{
			Requests:         r.Requests,
			InputTokens:      r.Input,
			OutputTokens:     r.Output,
			CacheReadTokens:  r.CacheRead,
			CacheWriteTokens: r.CacheWrite,
			ReasoningTokens:  r.Reasoning,
			Cost:             make([]MicroMoney, len(r.Cost)),
			CostReported:     r.CostReported,
		}
		if r.Day != nil {
			day := FromDate{Time: *r.Day}
			row.Day = &day
		}
		if r.Model != "" {
			model := r.Model
			row.Model = &model
		}
		if r.AccountID != "" {
			accountID := r.AccountID
			row.AccountId = &accountID
		}
		if r.AccountName != "" {
			accountName := r.AccountName
			row.AccountName = &accountName
		}
		if r.ProviderID != "" {
			providerID := r.ProviderID
			row.ProviderId = &providerID
		}
		if req.Params.GroupBy == GetUsageParamsGroupByProject {
			// '' is a real group (unknown or provider-reported); keep it.
			project := r.Project
			row.Project = &project
		}
		for j, c := range r.Cost {
			row.Cost[j] = MicroMoney{AmountMicro: c.Micro, Currency: c.Currency}
		}
		out[i] = row
	}
	return GetUsage200JSONResponse(out), nil
}

// utcDay returns midnight UTC of t's UTC day.
func utcDay(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
