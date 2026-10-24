// SPDX-License-Identifier: SUL-1.0

package pricing

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"github.com/riverqueue/river"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/money"
)

// litellmURL is the public price file. Only the four token costs and the
// provider name are used from it.
const litellmURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// litellmEntry is the subset of one LiteLLM price file entry this package
// reads. Costs are USD per token.
type litellmEntry struct {
	InputCostPerToken           *float64 `json:"input_cost_per_token"`
	OutputCostPerToken          *float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost     *float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost *float64 `json:"cache_creation_input_token_cost"`
	LiteLLMProvider             string   `json:"litellm_provider"`
}

// ParseLiteLLM reads the LiteLLM price file and returns one Price per model,
// sorted by model and provider. The "sample_spec" key and entries with
// neither input nor output cost are skipped.
func ParseLiteLLM(r io.Reader) ([]Price, error) {
	var entries map[string]litellmEntry
	if err := json.NewDecoder(r).Decode(&entries); err != nil {
		return nil, fmt.Errorf("decoding litellm prices: %w", err)
	}

	prices := make([]Price, 0, len(entries))
	for model, e := range entries {
		if model == "sample_spec" {
			continue
		}
		if e.InputCostPerToken == nil && e.OutputCostPerToken == nil {
			continue
		}
		price, err := priceFromEntry(model, e)
		if err != nil {
			return nil, err
		}
		prices = append(prices, price)
	}
	sort.Slice(prices, func(i, j int) bool {
		if prices[i].Model != prices[j].Model {
			return prices[i].Model < prices[j].Model
		}
		return prices[i].Provider < prices[j].Provider
	})
	return prices, nil
}

// priceFromEntry converts per-token USD costs to micro units per million
// tokens.
func priceFromEntry(model string, e litellmEntry) (Price, error) {
	input, err := microPerMtok(e.InputCostPerToken)
	if err != nil {
		return Price{}, fmt.Errorf("input cost for %q: %w", model, err)
	}
	output, err := microPerMtok(e.OutputCostPerToken)
	if err != nil {
		return Price{}, fmt.Errorf("output cost for %q: %w", model, err)
	}
	cacheRead, err := microPerMtok(e.CacheReadInputTokenCost)
	if err != nil {
		return Price{}, fmt.Errorf("cache read cost for %q: %w", model, err)
	}
	cacheWrite, err := microPerMtok(e.CacheCreationInputTokenCost)
	if err != nil {
		return Price{}, fmt.Errorf("cache write cost for %q: %w", model, err)
	}
	return Price{
		Model:      model,
		Provider:   e.LiteLLMProvider,
		Currency:   "USD",
		Input:      input,
		Output:     output,
		CacheRead:  cacheRead,
		CacheWrite: cacheWrite,
	}, nil
}

// microPerMtok converts USD per token to micro units per million tokens.
// A missing cost is zero.
func microPerMtok(perToken *float64) (int64, error) {
	if perToken == nil {
		return 0, nil
	}
	return money.MicroFromFloat(*perToken * 1e6)
}

// Import fetches and upserts the LiteLLM file. Returns the row count.
func (s *Service) Import(ctx context.Context) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, litellmURL, nil)
	if err != nil {
		return 0, fmt.Errorf("building litellm request: %w", err)
	}
	resp, err := s.c.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fetching litellm prices: %w", err)
	}
	defer resp.Body.Close()

	prices, err := ParseLiteLLM(resp.Body)
	if err != nil {
		return 0, err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("starting price import transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	q := s.q.WithTx(tx)
	for _, p := range prices {
		if err := q.UpsertModelPrice(ctx, db.UpsertModelPriceParams{
			Model:                  p.Model,
			Provider:               p.Provider,
			Currency:               p.Currency,
			InputMicroPerMtok:      p.Input,
			OutputMicroPerMtok:     p.Output,
			CacheReadMicroPerMtok:  p.CacheRead,
			CacheWriteMicroPerMtok: p.CacheWrite,
		}); err != nil {
			return 0, fmt.Errorf("upserting price for %q: %w", p.Model, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("committing price import: %w", err)
	}
	return len(prices), nil
}

// ImportArgs is the daily price import job.
type ImportArgs struct{}

// Kind implements river.JobArgs.
func (ImportArgs) Kind() string { return "pricing_import" }

// ImportWorker runs Import for the pricing import job.
type ImportWorker struct {
	river.WorkerDefaults[ImportArgs]
	Service *Service
}

// Work implements river.Worker.
func (w *ImportWorker) Work(ctx context.Context, _ *river.Job[ImportArgs]) error {
	n, err := w.Service.Import(ctx)
	if err != nil {
		return fmt.Errorf("importing litellm prices: %w", err)
	}
	w.Service.log.InfoContext(ctx, "imported model prices", "rows", n)
	return nil
}

// PeriodicJob imports the price file daily at 03:00 UTC and once on start.
func PeriodicJob() *river.PeriodicJob {
	return river.NewPeriodicJob(
		dailyAt{hour: 3},
		func() (river.JobArgs, *river.InsertOpts) {
			return ImportArgs{}, &river.InsertOpts{UniqueOpts: river.UniqueOpts{ByArgs: true}}
		},
		&river.PeriodicJobOpts{ID: "pricing_import", RunOnStart: true},
	)
}

// dailyAt runs once a day at hour:00 UTC.
type dailyAt struct{ hour int }

func (s dailyAt) Next(current time.Time) time.Time {
	current = current.UTC()
	next := time.Date(current.Year(), current.Month(), current.Day(), s.hour, 0, 0, 0, time.UTC)
	if !next.After(current) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
