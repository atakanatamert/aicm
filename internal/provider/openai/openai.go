// SPDX-License-Identifier: AGPL-3.0-or-later

// Package openai reads organization usage and costs from the OpenAI Admin API.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

const baseURL = "https://api.openai.com/v1"

// Provider is the OpenAI Admin API adapter.
type Provider struct {
	c *provider.HTTPClient
}

// New returns an adapter over c.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.UsageFetcher = (*Provider)(nil)
)

// Info declares the OpenAI API catalog entry. OpenAI has no balance API, so
// the adapter has no meters.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "openai",
		Name:      "OpenAI API",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAdminKey},
		Links: provider.Links{
			Billing: "https://platform.openai.com/settings/organization/billing/overview",
			Usage:   "https://platform.openai.com/usage",
			Keys:    "https://platform.openai.com/settings/organization/admin-keys",
			Docs:    "https://platform.openai.com/docs/api-reference/usage",
		},
		MinPollInterval: time.Hour,
		ModelFamilies:   []string{"gpt"},
	}
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Secret}}
}

// Validate makes one costs call for the last day.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	start := p.c.Now().UTC().Truncate(24*time.Hour).AddDate(0, 0, -1)
	var out struct{}
	if err := p.c.GetJSON(ctx, costsQuery(start, ""), p.auth(c), &out); err != nil {
		return fmt.Errorf("validating OpenAI admin key: %w", err)
	}
	return nil
}

// FetchUsage returns token usage per UTC day and model, with costs attached
// when a cost line item names a model seen in that day's usage. Line items
// that name no model are summed into a zero-token "other" record.
func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials, from, to time.Time) ([]provider.UsageRecord, error) {
	records := map[recordKey]*provider.UsageRecord{}
	page := ""
	for {
		var out usagePage
		if err := p.c.GetJSON(ctx, usageQuery(from, to, page), p.auth(c), &out); err != nil {
			return nil, fmt.Errorf("fetching OpenAI usage: %w", err)
		}
		for _, bucket := range out.Data {
			if !inRange(bucket.StartTime, from, to) {
				continue
			}
			for _, r := range bucket.Results {
				model := r.Model
				if model == "" {
					model = "unknown"
				}
				key := recordKey{day: bucket.StartTime, model: model}
				rec, ok := records[key]
				if !ok {
					rec = &provider.UsageRecord{Day: time.Unix(bucket.StartTime, 0).UTC(), Model: model}
					records[key] = rec
				}
				rec.Requests += r.NumModelRequests
				rec.InputTokens += r.InputTokens
				rec.OutputTokens += r.OutputTokens
				rec.CacheReadTokens += r.InputCachedTokens
			}
		}
		if !out.HasMore {
			break
		}
		if out.NextPage == "" {
			return nil, errors.New("fetching OpenAI usage: page has more data but no next_page")
		}
		page = out.NextPage
	}

	models := map[int64]map[string]bool{}
	for _, rec := range records {
		day := rec.Day.Unix()
		if models[day] == nil {
			models[day] = map[string]bool{}
		}
		models[day][rec.Model] = true
	}

	other := map[int64]int64{}
	otherSeen := map[int64]bool{}
	page = ""
	for {
		var out costsPage
		if err := p.c.GetJSON(ctx, costsQuery(from, page), p.auth(c), &out); err != nil {
			return nil, fmt.Errorf("fetching OpenAI costs: %w", err)
		}
		for _, bucket := range out.Data {
			if !inRange(bucket.StartTime, from, to) {
				continue
			}
			for _, r := range bucket.Results {
				if r.Amount.Value == nil {
					continue
				}
				micro, err := money.MicroFromFloat(*r.Amount.Value)
				if err != nil {
					return nil, fmt.Errorf("parsing OpenAI cost for %q: %w", r.LineItem, err)
				}
				model, _, _ := strings.Cut(r.LineItem, ",")
				model = strings.TrimSpace(model)
				key := recordKey{day: bucket.StartTime, model: model}
				if rec, ok := records[key]; ok {
					sum := micro
					if rec.CostMicro != nil {
						sum += *rec.CostMicro
					}
					rec.CostMicro = &sum
					rec.CostCurrency = "USD"
					continue
				}
				other[bucket.StartTime] += micro
				otherSeen[bucket.StartTime] = true
			}
		}
		if !out.HasMore {
			break
		}
		if out.NextPage == "" {
			return nil, errors.New("fetching OpenAI costs: page has more data but no next_page")
		}
		page = out.NextPage
	}

	usage := make([]provider.UsageRecord, 0, len(records)+len(otherSeen))
	for _, rec := range records {
		usage = append(usage, *rec)
	}
	for day := range otherSeen {
		cost := other[day]
		usage = append(usage, provider.UsageRecord{
			Day:          time.Unix(day, 0).UTC(),
			Model:        "other",
			CostMicro:    &cost,
			CostCurrency: "USD",
		})
	}
	sort.Slice(usage, func(i, j int) bool {
		if !usage[i].Day.Equal(usage[j].Day) {
			return usage[i].Day.Before(usage[j].Day)
		}
		return usage[i].Model < usage[j].Model
	})
	return usage, nil
}

type recordKey struct {
	day   int64
	model string
}

func usageQuery(from, to time.Time, page string) string {
	q := url.Values{
		"start_time":   {strconv.FormatInt(from.Unix(), 10)},
		"end_time":     {strconv.FormatInt(to.Unix(), 10)},
		"bucket_width": {"1d"},
		"group_by":     {"model"},
	}
	if page != "" {
		q.Set("page", page)
	}
	return baseURL + "/organization/usage/completions?" + q.Encode()
}

func costsQuery(start time.Time, page string) string {
	q := url.Values{
		"start_time":   {strconv.FormatInt(start.Unix(), 10)},
		"bucket_width": {"1d"},
		"group_by":     {"line_item"},
	}
	if page != "" {
		q.Set("page", page)
	}
	return baseURL + "/organization/costs?" + q.Encode()
}

func inRange(start int64, from, to time.Time) bool {
	return start >= from.Unix() && start < to.Unix()
}

type usagePage struct {
	Data     []usageBucket `json:"data"`
	HasMore  bool          `json:"has_more"`
	NextPage string        `json:"next_page"`
}

type usageBucket struct {
	StartTime int64         `json:"start_time"`
	Results   []usageResult `json:"results"`
}

type usageResult struct {
	InputTokens       int64  `json:"input_tokens"`
	OutputTokens      int64  `json:"output_tokens"`
	InputCachedTokens int64  `json:"input_cached_tokens"`
	NumModelRequests  int64  `json:"num_model_requests"`
	Model             string `json:"model"`
}

type costsPage struct {
	Data     []costsBucket `json:"data"`
	HasMore  bool          `json:"has_more"`
	NextPage string        `json:"next_page"`
}

type costsBucket struct {
	StartTime int64        `json:"start_time"`
	Results   []costResult `json:"results"`
}

type costResult struct {
	Amount struct {
		Value *float64 `json:"value"`
	} `json:"amount"`
	LineItem string `json:"line_item"`
}
