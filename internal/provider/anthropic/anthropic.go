// SPDX-License-Identifier: AGPL-3.0-or-later

// Package anthropic reads organization usage and cost from the Anthropic
// Admin API. It needs an Admin API key (sk-ant-admin…); the key only travels
// in the x-api-key header and is never stored or logged here.
package anthropic

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

const (
	apiBaseURL = "https://api.anthropic.com"
	apiVersion = "2023-06-01"
	usagePath  = "/v1/organizations/usage_report/messages"
	costPath   = "/v1/organizations/cost_report"
)

// Provider is the Anthropic Admin API adapter.
type Provider struct {
	c *provider.HTTPClient
}

// New returns the adapter over c.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.UsageFetcher = (*Provider)(nil)
)

// Info returns the catalog entry. The Admin API has no wallet or quota
// meter, so the adapter declares no meters and implements only usage.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "anthropic",
		Name:      "Anthropic API",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAdminKey},
		Links: provider.Links{
			Billing: "https://platform.claude.com/settings/billing",
			Usage:   "https://platform.claude.com/usage",
			Keys:    "https://platform.claude.com/settings/admin-keys",
			Docs:    "https://platform.claude.com/docs/en/build-with-claude/usage-cost-api",
		},
		MinPollInterval: time.Hour,
		ModelFamilies:   []string{"claude"},
	}
}

func auth(c provider.Credentials) http.Header {
	return http.Header{
		"X-Api-Key":         {c.Secret},
		"Anthropic-Version": {apiVersion},
	}
}

// Validate reads the usage report for the last day.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	now := p.c.Now().UTC()
	q := rangeQuery(now.Add(-24*time.Hour), now)
	var out usageReport
	if err := p.c.GetJSON(ctx, apiBaseURL+usagePath+"?"+q.Encode(), auth(c), &out); err != nil {
		return fmt.Errorf("validating anthropic admin key: %w", err)
	}
	return nil
}

// FetchUsage returns token usage per UTC day and model, with the cost the
// cost report attributes to that day and model. Both reports are paged and
// followed to the end.
func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials, from, to time.Time) ([]provider.UsageRecord, error) {
	records, index, err := p.fetchUsage(ctx, c, from, to)
	if err != nil {
		return nil, err
	}
	costs, err := p.fetchCosts(ctx, c, from, to)
	if err != nil {
		return nil, err
	}
	for key, cost := range costs {
		i, ok := index[key]
		if !ok {
			continue
		}
		micro := cost.micro
		records[i].CostMicro = &micro
		records[i].CostCurrency = cost.currency
	}
	return records, nil
}

// dayModel identifies one usage record: one UTC day and one model.
type dayModel struct {
	day   time.Time
	model string
}

func (p *Provider) fetchUsage(ctx context.Context, c provider.Credentials, from, to time.Time) ([]provider.UsageRecord, map[dayModel]int, error) {
	q := rangeQuery(from, to)
	q.Add("group_by[]", "model")

	var records []provider.UsageRecord
	index := make(map[dayModel]int)
	for {
		var out usageReport
		if err := p.c.GetJSON(ctx, apiBaseURL+usagePath+"?"+q.Encode(), auth(c), &out); err != nil {
			return nil, nil, fmt.Errorf("fetching anthropic usage: %w", err)
		}
		for _, bucket := range out.Data {
			day := utcDay(bucket.StartingAt)
			for _, r := range bucket.Results {
				if r.Model == "" {
					continue
				}
				key := dayModel{day: day, model: r.Model}
				if i, ok := index[key]; ok {
					records[i].InputTokens += r.UncachedInputTokens
					records[i].CacheReadTokens += r.CacheReadInputTokens
					records[i].CacheWriteTokens += r.CacheCreation.Ephemeral1hInputTokens + r.CacheCreation.Ephemeral5mInputTokens
					records[i].OutputTokens += r.OutputTokens
					continue
				}
				index[key] = len(records)
				records = append(records, provider.UsageRecord{
					Day:              day,
					Model:            r.Model,
					InputTokens:      r.UncachedInputTokens,
					OutputTokens:     r.OutputTokens,
					CacheReadTokens:  r.CacheReadInputTokens,
					CacheWriteTokens: r.CacheCreation.Ephemeral1hInputTokens + r.CacheCreation.Ephemeral5mInputTokens,
				})
			}
		}
		if !out.HasMore || out.NextPage == "" {
			return records, index, nil
		}
		q.Set("page", out.NextPage)
	}
}

type costValue struct {
	micro    int64
	currency string
}

// fetchCosts sums the daily cost report by day and model. Results without a
// model (web search, code execution) cannot be attributed to a usage record
// and are left out; the hub estimates those.
func (p *Provider) fetchCosts(ctx context.Context, c provider.Credentials, from, to time.Time) (map[dayModel]costValue, error) {
	q := rangeQuery(from, to)
	q.Add("group_by[]", "description")

	costs := make(map[dayModel]costValue)
	for {
		var out costReport
		if err := p.c.GetJSON(ctx, apiBaseURL+costPath+"?"+q.Encode(), auth(c), &out); err != nil {
			return nil, fmt.Errorf("fetching anthropic costs: %w", err)
		}
		for _, bucket := range out.Data {
			day := utcDay(bucket.StartingAt)
			for _, r := range bucket.Results {
				if r.Model == "" {
					continue
				}
				micro, err := microFromCents(r.Amount)
				if err != nil {
					return nil, fmt.Errorf("anthropic cost on %s for %s: %w", day.Format(time.DateOnly), r.Model, err)
				}
				key := dayModel{day: day, model: r.Model}
				v := costs[key]
				v.micro += micro
				if v.currency == "" {
					v.currency = r.Currency
					if v.currency == "" {
						v.currency = "USD"
					}
				}
				costs[key] = v
			}
		}
		if !out.HasMore || out.NextPage == "" {
			return costs, nil
		}
		q.Set("page", out.NextPage)
	}
}

// microFromCents converts a cost report amount to micro dollars. The report
// states amounts as decimal strings in the lowest currency unit, cents
// ("123.78912" is $1.2378912), so one cent is 10,000 micro. ParseMicro keeps
// the value exact to a millionth of a cent; the division rounds half away
// from zero to the nearest micro dollar.
func microFromCents(amount string) (int64, error) {
	microCents, err := money.ParseMicro(amount)
	if err != nil {
		return 0, fmt.Errorf("parsing amount %q: %w", amount, err)
	}
	if microCents < 0 {
		return -((-microCents + 50) / 100), nil
	}
	return (microCents + 50) / 100, nil
}

func rangeQuery(from, to time.Time) url.Values {
	return url.Values{
		"starting_at":  {from.UTC().Format(time.RFC3339)},
		"ending_at":    {to.UTC().Format(time.RFC3339)},
		"bucket_width": {"1d"},
	}
}

func utcDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

type usageReport struct {
	Data []struct {
		StartingAt time.Time `json:"starting_at"`
		Results    []struct {
			Model                string `json:"model"`
			UncachedInputTokens  int64  `json:"uncached_input_tokens"`
			CacheReadInputTokens int64  `json:"cache_read_input_tokens"`
			OutputTokens         int64  `json:"output_tokens"`
			CacheCreation        struct {
				Ephemeral1hInputTokens int64 `json:"ephemeral_1h_input_tokens"`
				Ephemeral5mInputTokens int64 `json:"ephemeral_5m_input_tokens"`
			} `json:"cache_creation"`
		} `json:"results"`
	} `json:"data"`
	HasMore  bool   `json:"has_more"`
	NextPage string `json:"next_page"`
}

type costReport struct {
	Data []struct {
		StartingAt time.Time `json:"starting_at"`
		Results    []struct {
			Amount   string `json:"amount"`
			Currency string `json:"currency"`
			Model    string `json:"model"`
		} `json:"results"`
	} `json:"data"`
	HasMore  bool   `json:"has_more"`
	NextPage string `json:"next_page"`
}
