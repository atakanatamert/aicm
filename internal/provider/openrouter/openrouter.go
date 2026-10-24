// SPDX-License-Identifier: SUL-1.0

// Package openrouter implements the OpenRouter provider adapter.
package openrouter

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

const baseURL = "https://openrouter.ai/api/v1"

// Provider is the OpenRouter adapter.
type Provider struct {
	c *provider.HTTPClient
}

// New returns an OpenRouter adapter over c.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.QuotaFetcher = (*Provider)(nil)
	_ provider.UsageFetcher = (*Provider)(nil)
)

// Info returns the OpenRouter catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:       "openrouter",
		Name:     "OpenRouter",
		Currency: "USD",
		AuthKinds: []provider.AuthKind{
			provider.AuthAPIKey,
			provider.AuthAdminKey,
		},
		Links: provider.Links{
			TopUp: "https://openrouter.ai/settings/credits",
			Keys:  "https://openrouter.ai/settings/keys",
			Usage: "https://openrouter.ai/activity",
			Docs:  "https://openrouter.ai/docs/api/api-reference/credits/get-remaining-credits",
		},
		Meters: []meter.Def{
			{Key: "credits", Label: "Credits remaining", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			{Key: "key_limit", Label: "Key spend limit", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetFixed},
		},
		MinPollInterval: 15 * time.Minute,
		ModelFamilies:   []string{"*"},
	}
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Secret}}
}

// Validate checks the key with the cheapest authenticated call.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	var out struct{}
	if err := p.c.GetJSON(ctx, baseURL+"/key", p.auth(c), &out); err != nil {
		return fmt.Errorf("validating openrouter key: %w", err)
	}
	return nil
}

// FetchQuota reads the account credit balance and the key's spend limit.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	now := p.c.Now().UTC()
	readings := make([]meter.Reading, 0, 2)

	var credits struct {
		Data struct {
			TotalCredits float64 `json:"total_credits"`
			TotalUsage   float64 `json:"total_usage"`
		} `json:"data"`
	}
	err := p.c.GetJSON(ctx, baseURL+"/credits", p.auth(c), &credits)
	switch {
	case err == nil:
		total, err := money.FromFloat(credits.Data.TotalCredits, "USD")
		if err != nil {
			return nil, fmt.Errorf("parsing openrouter total_credits: %w", err)
		}
		used, err := money.FromFloat(credits.Data.TotalUsage, "USD")
		if err != nil {
			return nil, fmt.Errorf("parsing openrouter total_usage: %w", err)
		}
		remaining := float64(total.Minor - used.Minor)
		readings = append(readings, meter.Reading{
			Key:        "credits",
			Remaining:  &remaining,
			CapturedAt: now,
			Source:     meter.SourceAPI,
		})
	case errors.Is(err, provider.ErrInvalidCredentials):
		// A normal inference key cannot read account credits. Skip the meter;
		// the key endpoint below still runs.
	default:
		return nil, fmt.Errorf("fetching openrouter credits: %w", err)
	}

	var key struct {
		Data struct {
			Limit          *float64 `json:"limit"`
			LimitRemaining *float64 `json:"limit_remaining"`
			LimitReset     *string  `json:"limit_reset"`
		} `json:"data"`
	}
	if err := p.c.GetJSON(ctx, baseURL+"/key", p.auth(c), &key); err != nil {
		return nil, fmt.Errorf("fetching openrouter key: %w", err)
	}
	if key.Data.Limit != nil && key.Data.LimitRemaining != nil {
		limit, err := money.FromFloat(*key.Data.Limit, "USD")
		if err != nil {
			return nil, fmt.Errorf("parsing openrouter key limit: %w", err)
		}
		remaining, err := money.FromFloat(*key.Data.LimitRemaining, "USD")
		if err != nil {
			return nil, fmt.Errorf("parsing openrouter key limit_remaining: %w", err)
		}
		reading := meter.Reading{
			Key:        "key_limit",
			Remaining:  new(float64(remaining.Minor)),
			Limit:      new(float64(limit.Minor)),
			CapturedAt: now,
			Source:     meter.SourceAPI,
		}
		if key.Data.LimitReset != nil {
			if reset, ok := resetAt(now, *key.Data.LimitReset); ok {
				reading.ResetsAt = &reset
			}
		}
		readings = append(readings, reading)
	}
	return readings, nil
}

// FetchUsage reads the last 30 completed UTC days of activity and groups it
// per UTC day and model. The activity endpoint is not paged.
func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials, from, to time.Time) ([]provider.UsageRecord, error) {
	var out struct {
		Data []struct {
			Date             string  `json:"date"`
			Model            string  `json:"model"`
			Requests         int64   `json:"requests"`
			PromptTokens     int64   `json:"prompt_tokens"`
			CompletionTokens int64   `json:"completion_tokens"`
			ReasoningTokens  int64   `json:"reasoning_tokens"`
			CachedTokens     int64   `json:"cached_tokens"`
			Usage            float64 `json:"usage"`
		} `json:"data"`
	}
	if err := p.c.GetJSON(ctx, baseURL+"/activity", p.auth(c), &out); err != nil {
		// Only management keys may read activity. With an inference key the
		// balance still syncs; usage comes from agents reading local logs.
		var he *provider.HTTPError
		if errors.As(err, &he) && he.Status == http.StatusForbidden {
			return nil, nil
		}
		return nil, fmt.Errorf("fetching openrouter activity: %w", err)
	}

	fromDay := dayUTC(from)
	toDay := dayUTC(to)
	type usageKey struct {
		day   time.Time
		model string
	}
	grouped := make(map[usageKey]*provider.UsageRecord, len(out.Data))
	for _, item := range out.Data {
		day, err := time.Parse(time.DateOnly, item.Date)
		if err != nil {
			return nil, fmt.Errorf("parsing openrouter activity date %q: %w", item.Date, err)
		}
		if day.Before(fromDay) || !day.Before(toDay) {
			continue
		}
		cost, err := money.MicroFromFloat(item.Usage)
		if err != nil {
			return nil, fmt.Errorf("parsing openrouter activity usage for %s on %s: %w", item.Model, item.Date, err)
		}
		k := usageKey{day: day, model: item.Model}
		rec := grouped[k]
		if rec == nil {
			rec = &provider.UsageRecord{
				Day:          day,
				Model:        item.Model,
				CostMicro:    new(cost),
				CostCurrency: "USD",
			}
			grouped[k] = rec
		} else {
			*rec.CostMicro += cost
		}
		rec.Requests += item.Requests
		rec.InputTokens += item.PromptTokens
		rec.OutputTokens += item.CompletionTokens
		rec.CacheReadTokens += item.CachedTokens
		rec.ReasoningTokens += item.ReasoningTokens
	}

	records := make([]provider.UsageRecord, 0, len(grouped))
	for _, rec := range grouped {
		records = append(records, *rec)
	}
	sort.Slice(records, func(i, j int) bool {
		if !records[i].Day.Equal(records[j].Day) {
			return records[i].Day.Before(records[j].Day)
		}
		return records[i].Model < records[j].Model
	})
	return records, nil
}

// dayUTC returns midnight UTC of t's calendar day.
func dayUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// resetAt computes the next window boundary for a key limit reset interval.
func resetAt(now time.Time, interval string) (time.Time, bool) {
	day := dayUTC(now)
	switch interval {
	case "daily":
		return day.AddDate(0, 0, 1), true
	case "weekly":
		days := (8 - int(day.Weekday())) % 7
		if days == 0 {
			days = 7
		}
		return day.AddDate(0, 0, days), true
	case "monthly":
		return time.Date(day.Year(), day.Month()+1, 1, 0, 0, 0, 0, time.UTC), true
	default:
		return time.Time{}, false
	}
}
