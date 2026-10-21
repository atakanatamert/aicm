// SPDX-License-Identifier: AGPL-3.0-or-later

// Package usage stores per-request usage events, rolls them up per UTC day
// and model, replaces provider-reported days, and answers grouped queries.
package usage

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/agentlink"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/pricing"
	"github.com/atakanatamert/aicm/internal/provider"
)

// Service stores and queries usage.
type Service struct {
	pool    *pgxpool.Pool
	q       *db.Queries
	pricing *pricing.Service
	log     *slog.Logger
}

// NewService returns a usage service.
func NewService(pool *pgxpool.Pool, prices *pricing.Service, log *slog.Logger) *Service {
	return &Service{pool: pool, q: db.New(pool), pricing: prices, log: log}
}

// RecordAgentEvents stores events (ON CONFLICT (machine_id, source, message_id)
// DO NOTHING) and recomputes usage_daily rows (source 'agent') for every
// (account, UTC day) the batch touched, in one transaction. accountIDs maps
// each event's AccountRef to the hub account id (the caller resolves them).
// Returns how many events were new.
func (s *Service) RecordAgentEvents(ctx context.Context, orgID, machineID string, accountIDs map[agentlink.AccountRef]string, events []agentlink.UsageEvent) (int, error) {
	if len(events) == 0 {
		return 0, nil
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("starting usage ingest transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	type accountDay struct {
		accountID string
		day       time.Time
	}
	touched := make(map[accountDay]struct{})
	created := 0

	for _, e := range events {
		accountID, ok := accountIDs[e.Account]
		if !ok {
			return 0, fmt.Errorf("storing usage event %q: account %s/%s is not resolved", e.MessageID, e.Account.ProviderID, e.Account.ExternalID)
		}
		costMicro := e.CostMicro
		var costCurrency *string
		if costMicro != nil {
			currency := e.CostCurrency
			if currency == "" {
				currency = "USD"
			}
			costCurrency = &currency
		}
		n, err := q.InsertUsageEvent(ctx, db.InsertUsageEventParams{
			OrgID:            orgID,
			AccountID:        accountID,
			MachineID:        machineID,
			Source:           e.Source,
			MessageID:        e.MessageID,
			OccurredAt:       e.OccurredAt,
			Model:            e.Model,
			Project:          e.Project,
			InputTokens:      e.InputTokens,
			OutputTokens:     e.OutputTokens,
			CacheReadTokens:  e.CacheReadTokens,
			CacheWriteTokens: e.CacheWriteTokens,
			ReasoningTokens:  e.ReasoningTokens,
			CostMicro:        costMicro,
			CostCurrency:     costCurrency,
		})
		if err != nil {
			return 0, fmt.Errorf("storing usage event %q: %w", e.MessageID, err)
		}
		created += int(n)
		touched[accountDay{accountID: accountID, day: dayStart(e.OccurredAt)}] = struct{}{}
	}

	for k := range touched {
		rows, err := q.RollupUsageEvents(ctx, db.RollupUsageEventsParams{
			OrgID:     orgID,
			AccountID: k.accountID,
			FromTime:  k.day,
			ToTime:    k.day.AddDate(0, 0, 1),
		})
		if err != nil {
			return 0, fmt.Errorf("rolling up usage for account %s on %s: %w", k.accountID, k.day.Format(time.DateOnly), err)
		}
		// Replace the day's agent rows, so a row rolled up before projects
		// existed (project '') never survives beside its project rows.
		if err := q.DeleteAgentUsageDaily(ctx, db.DeleteAgentUsageDailyParams{
			OrgID:     orgID,
			AccountID: k.accountID,
			Day:       k.day,
		}); err != nil {
			return 0, fmt.Errorf("clearing usage rollup for account %s on %s: %w", k.accountID, k.day.Format(time.DateOnly), err)
		}
		for _, r := range rows {
			cost, currency, reported, err := s.rollupCost(ctx, r)
			if err != nil {
				return 0, err
			}
			if err := q.UpsertAgentUsageDaily(ctx, db.UpsertAgentUsageDailyParams{
				OrgID:            orgID,
				AccountID:        k.accountID,
				Day:              k.day,
				Model:            r.Model,
				Project:          r.Project,
				Requests:         r.Requests,
				InputTokens:      r.InputTokens,
				OutputTokens:     r.OutputTokens,
				CacheReadTokens:  r.CacheReadTokens,
				CacheWriteTokens: r.CacheWriteTokens,
				ReasoningTokens:  r.ReasoningTokens,
				CostMicro:        cost,
				CostCurrency:     currency,
				CostReported:     reported,
			}); err != nil {
				return 0, fmt.Errorf("storing usage rollup for account %s on %s: %w", k.accountID, k.day.Format(time.DateOnly), err)
			}
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing usage ingest: %w", err)
	}
	return created, nil
}

// rollupCost turns one model's event rollup into cost, currency and whether
// every contributing event reported a cost.
func (s *Service) rollupCost(ctx context.Context, r db.RollupUsageEventsRow) (int64, string, bool, error) {
	price, havePrice, err := s.pricing.Lookup(ctx, r.Model)
	if err != nil {
		return 0, "", false, fmt.Errorf("pricing usage for model %q: %w", r.Model, err)
	}

	reported := r.ReportedEvents > 0 && r.ReportedEvents == r.Requests
	var cost int64
	currency := "USD"

	switch {
	case r.CostCurrencies > 1:
		// Mixed currencies cannot be summed; estimate the whole group.
		if havePrice {
			cost = pricing.Estimate(price, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens)
			currency = price.Currency
		}
	case r.ReportedEvents > 0:
		cost = r.ReportedCostMicro
		currency = r.CostCurrency
		if r.UncostedEvents > 0 && havePrice {
			cost += pricing.Estimate(price, r.UncostedInputTokens, r.UncostedOutputTokens, r.UncostedCacheReadTokens, r.UncostedCacheWriteTokens)
		}
	case havePrice:
		cost = pricing.Estimate(price, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens)
		currency = price.Currency
	}

	if currency == "" {
		currency = "USD"
	}
	return cost, currency, reported, nil
}

// ReplaceAPIUsage replaces usage_daily rows with source 'api' for the account
// and days in [from, to) with recs, in one transaction (delete then insert).
// Records with CostMicro set are cost_reported=true; others are estimated.
func (s *Service) ReplaceAPIUsage(ctx context.Context, orgID, accountID string, from, to time.Time, recs []provider.UsageRecord) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("starting api usage replacement: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)

	fromDay, toDay := dayStart(from), dayStart(to)
	if err := q.DeleteAPIUsageDaily(ctx, db.DeleteAPIUsageDailyParams{
		OrgID:     orgID,
		AccountID: accountID,
		FromDay:   fromDay,
		ToDay:     toDay,
	}); err != nil {
		return fmt.Errorf("deleting api usage for account %s: %w", accountID, err)
	}

	for _, rec := range recs {
		cost := int64(0)
		currency := "USD"
		reported := rec.CostMicro != nil
		if reported {
			cost = *rec.CostMicro
			if rec.CostCurrency != "" {
				currency = rec.CostCurrency
			}
		} else {
			price, ok, err := s.pricing.Lookup(ctx, rec.Model)
			if err != nil {
				return fmt.Errorf("pricing api usage for model %q: %w", rec.Model, err)
			}
			if ok {
				cost = pricing.Estimate(price, rec.InputTokens, rec.OutputTokens, rec.CacheReadTokens, rec.CacheWriteTokens)
				currency = price.Currency
			}
		}
		if err := q.InsertAPIUsageDaily(ctx, db.InsertAPIUsageDailyParams{
			OrgID:            orgID,
			AccountID:        accountID,
			Day:              dayStart(rec.Day),
			Model:            rec.Model,
			Requests:         rec.Requests,
			InputTokens:      rec.InputTokens,
			OutputTokens:     rec.OutputTokens,
			CacheReadTokens:  rec.CacheReadTokens,
			CacheWriteTokens: rec.CacheWriteTokens,
			ReasoningTokens:  rec.ReasoningTokens,
			CostMicro:        cost,
			CostCurrency:     currency,
			CostReported:     reported,
		}); err != nil {
			return fmt.Errorf("storing api usage for model %q: %w", rec.Model, err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing api usage replacement: %w", err)
	}
	return nil
}

// Query selects usage rows. From and To are inclusive UTC days.
type Query struct {
	OrgID, UserID string
	IsAdmin       bool
	From, To      time.Time
	GroupBy       string // day | model | account | provider | project
	AccountID     string // optional
	ProviderID    string // optional
}

// Row is one grouped usage row. Grouping keys not used by GroupBy are zero.
type Row struct {
	Day                                                       *time.Time
	Model, AccountID, AccountName, ProviderID                 string
	Project                                                   string // set only for GroupBy "project"
	Requests, Input, Output, CacheRead, CacheWrite, Reasoning int64
	Cost                                                      []CostTotal // one per currency
	CostReported                                              bool        // true only if every contributing row was reported
}

// CostTotal is one currency's share of a row's cost.
type CostTotal struct {
	Micro    int64
	Currency string
}

// Query returns grouped usage for the accounts q.UserID may see.
func (s *Service) Query(ctx context.Context, q Query) ([]Row, error) {
	from, to := dayStart(q.From), dayStart(q.To)
	orgID, userID := q.OrgID, q.UserID
	isAdmin := q.IsAdmin
	var accountID, providerID *string
	if q.AccountID != "" {
		accountID = &q.AccountID
	}
	if q.ProviderID != "" {
		providerID = &q.ProviderID
	}

	rows := []Row{}
	switch q.GroupBy {
	case "day":
		got, err := s.q.UsageByDay(ctx, db.UsageByDayParams{
			OrgID: orgID, UserID: userID, IsAdmin: isAdmin,
			AccountID: accountID, ProviderID: providerID,
			FromDay: from, ToDay: to,
		})
		if err != nil {
			return nil, fmt.Errorf("querying usage by day: %w", err)
		}
		for _, r := range got {
			day := r.Day
			row, err := makeRow(&day, "", "", "", "",
				r.Requests, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens,
				r.CostReported, r.Cost)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	case "model":
		got, err := s.q.UsageByModel(ctx, db.UsageByModelParams{
			OrgID: orgID, UserID: userID, IsAdmin: isAdmin,
			AccountID: accountID, ProviderID: providerID,
			FromDay: from, ToDay: to,
		})
		if err != nil {
			return nil, fmt.Errorf("querying usage by model: %w", err)
		}
		for _, r := range got {
			row, err := makeRow(nil, r.Model, "", "", "",
				r.Requests, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens,
				r.CostReported, r.Cost)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	case "account":
		got, err := s.q.UsageByAccount(ctx, db.UsageByAccountParams{
			OrgID: orgID, UserID: userID, IsAdmin: isAdmin,
			AccountID: accountID, ProviderID: providerID,
			FromDay: from, ToDay: to,
		})
		if err != nil {
			return nil, fmt.Errorf("querying usage by account: %w", err)
		}
		for _, r := range got {
			row, err := makeRow(nil, "", r.AccountID, r.AccountName, r.ProviderID,
				r.Requests, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens,
				r.CostReported, r.Cost)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	case "provider":
		got, err := s.q.UsageByProvider(ctx, db.UsageByProviderParams{
			OrgID: orgID, UserID: userID, IsAdmin: isAdmin,
			AccountID: accountID, ProviderID: providerID,
			FromDay: from, ToDay: to,
		})
		if err != nil {
			return nil, fmt.Errorf("querying usage by provider: %w", err)
		}
		for _, r := range got {
			row, err := makeRow(nil, "", "", "", r.ProviderID,
				r.Requests, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens,
				r.CostReported, r.Cost)
			if err != nil {
				return nil, err
			}
			rows = append(rows, row)
		}
	case "project":
		got, err := s.q.UsageByProject(ctx, db.UsageByProjectParams{
			OrgID: orgID, UserID: userID, IsAdmin: isAdmin,
			AccountID: accountID, ProviderID: providerID,
			FromDay: from, ToDay: to,
		})
		if err != nil {
			return nil, fmt.Errorf("querying usage by project: %w", err)
		}
		for _, r := range got {
			row, err := makeRow(nil, "", "", "", "",
				r.Requests, r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.ReasoningTokens,
				r.CostReported, r.Cost)
			if err != nil {
				return nil, err
			}
			row.Project = r.Project
			rows = append(rows, row)
		}
	default:
		return nil, fmt.Errorf("unknown usage group_by %q", q.GroupBy)
	}
	return rows, nil
}

// makeRow maps a generated query row into a Row.
func makeRow(day *time.Time, model, accountID, accountName, providerID string, requests, input, output, cacheRead, cacheWrite, reasoning int64, costReported bool, costRaw string) (Row, error) {
	cost, err := parseCost(costRaw)
	if err != nil {
		return Row{}, err
	}
	return Row{
		Day:          day,
		Model:        model,
		AccountID:    accountID,
		AccountName:  accountName,
		ProviderID:   providerID,
		Requests:     requests,
		Input:        input,
		Output:       output,
		CacheRead:    cacheRead,
		CacheWrite:   cacheWrite,
		Reasoning:    reasoning,
		Cost:         cost,
		CostReported: costReported,
	}, nil
}

// parseCost decodes the JSON aggregate the query built for one row.
func parseCost(raw string) ([]CostTotal, error) {
	var items []struct {
		Micro    int64  `json:"micro"`
		Currency string `json:"currency"`
	}
	if err := json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("decoding usage cost: %w", err)
	}
	cost := make([]CostTotal, len(items))
	for i, item := range items {
		cost[i] = CostTotal{Micro: item.Micro, Currency: item.Currency}
	}
	return cost, nil
}

// dayStart returns midnight UTC of t's UTC day.
func dayStart(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
