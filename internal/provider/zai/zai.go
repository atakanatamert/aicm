// SPDX-License-Identifier: SUL-1.0

// Package zai adapts the Z.ai GLM Coding Plan's quota endpoint. The endpoint
// is not part of the public API reference, so the adapter is marked
// unofficial and needs the account's opt-in (Decision 9).
package zai

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

// Hosts a region selects. Both serve the same monitoring API.
const (
	globalBaseURL = "https://api.z.ai"
	chinaBaseURL  = "https://open.bigmodel.cn"
	quotaPath     = "/api/monitor/usage/quota/limit"
)

// Meter keys.
const (
	tokensLimitKey = "tokens_limit"
	timeLimitKey   = "time_limit"
	creditLimitKey = "credit_limit"
)

// Provider implements provider.Provider and provider.QuotaFetcher.
type Provider struct {
	c *provider.HTTPClient
}

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.QuotaFetcher = (*Provider)(nil)
)

// New returns a Z.ai adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// quotaResponse is the GET /api/monitor/usage/quota/limit response. Limits
// carry their own type and window; a plan reports one or more of them.
type quotaResponse struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
	Data    struct {
		Limits []quotaLimit `json:"limits"`
	} `json:"data"`
}

type quotaLimit struct {
	Type string `json:"type"`
	// Unit and Number describe the window: unit 3 is hours, 5 months and
	// 6 weeks, so unit 3 with number 5 is the five-hour window.
	Unit int `json:"unit"`
	// Number is the window length, not a quota amount.
	Number int `json:"number"`
	// Percentage is the used share of the window, 0–100.
	Percentage *float64 `json:"percentage"`
	// CurrentValue, Remaining and Usage/Total are counts or credits.
	CurrentValue *int64 `json:"currentValue"`
	Remaining    *int64 `json:"remaining"`
	Usage        *int64 `json:"usage"`
	Total        *int64 `json:"total"`
	// NextResetTime is Unix epoch milliseconds.
	NextResetTime *int64 `json:"nextResetTime"`
}

// Info returns the catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "zai",
		Name:      "Z.ai GLM Coding Plan",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			Billing:            "https://z.ai/manage-apikey/billing",
			ManageSubscription: "https://z.ai/subscribe",
		},
		Meters: []meter.Def{
			{Key: tokensLimitKey, Label: "Prompt window (5 h)", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 300},
			{Key: timeLimitKey, Label: "Monthly tool calls", Unit: meter.UnitCount, ResetKind: meter.ResetCalendarMonth},
			{Key: creditLimitKey, Label: "Credits", Unit: meter.UnitCredits, ResetKind: meter.ResetNever},
		},
		ConfigFields: []provider.ConfigField{
			{
				Key:      "region",
				Label:    "Region",
				Help:     "global uses https://api.z.ai, china uses https://open.bigmodel.cn",
				Required: true,
				Options:  []string{"global", "china"},
			},
		},
		Unofficial:      true,
		MinPollInterval: 10 * time.Minute,
		ModelFamilies:   []string{"glm"},
	}
}

func (p *Provider) baseURL(c provider.Credentials) string {
	if c.Config["region"] == "china" {
		return chinaBaseURL
	}
	return globalBaseURL
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	// The monitoring API takes the raw key, without a Bearer prefix.
	return http.Header{"Authorization": {c.Secret}}
}

// Validate reads the quota limits, the cheapest authenticated call the
// coding plan exposes.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	if _, err := p.fetch(ctx, c); err != nil {
		return fmt.Errorf("validating zai key: %w", err)
	}
	return nil
}

// FetchQuota returns the 5-hour prompt window, the monthly tool-call count
// and the credit quota, for whichever limits the account reports.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	out, err := p.fetch(ctx, c)
	if err != nil {
		return nil, err
	}
	now := p.c.Now().UTC()
	tokens, tools, credits := pickLimits(out.Data.Limits)

	var readings []meter.Reading
	if tokens != nil && tokens.Percentage != nil {
		used := *tokens.Percentage
		readings = append(readings, meter.Reading{
			Key:        tokensLimitKey,
			Used:       &used,
			ResetsAt:   resetAt(tokens.NextResetTime),
			CapturedAt: now,
			Source:     meter.SourceAPI,
		})
	}
	for _, m := range []struct {
		key   string
		limit *quotaLimit
	}{
		{timeLimitKey, tools},
		{creditLimitKey, credits},
	} {
		if m.limit == nil {
			continue
		}
		used, remaining, limit := countValues(m.limit)
		if used == nil && remaining == nil {
			continue
		}
		readings = append(readings, meter.Reading{
			Key:        m.key,
			Used:       used,
			Remaining:  remaining,
			Limit:      limit,
			ResetsAt:   resetAt(m.limit.NextResetTime),
			CapturedAt: now,
			Source:     meter.SourceAPI,
		})
	}
	return readings, nil
}

// fetch calls the quota endpoint and checks the vendor envelope.
func (p *Provider) fetch(ctx context.Context, c provider.Credentials) (quotaResponse, error) {
	var out quotaResponse
	if !c.AllowUnofficial {
		return out, provider.ErrUnofficialNotAllowed
	}
	if err := p.c.GetJSON(ctx, p.baseURL(c)+quotaPath, p.auth(c), &out); err != nil {
		return out, fmt.Errorf("fetching zai quota: %w", err)
	}
	if !out.Success {
		msg := out.Msg
		if msg == "" {
			msg = "request failed"
		}
		return out, fmt.Errorf("fetching zai quota: %s (code %d)", msg, out.Code)
	}
	return out, nil
}

// pickLimits chooses one limit per declared meter. The catalog has a single
// token meter, so it takes the five-hour window (unit 3, number 5) when the
// plan also reports a weekly one. Tool calls prefer the monthly window.
func pickLimits(limits []quotaLimit) (tokens, tools, credits *quotaLimit) {
	for i := range limits {
		l := &limits[i]
		switch l.Type {
		case "TOKENS_LIMIT":
			if tokens == nil || (l.Unit == 3 && l.Number == 5 && !isFiveHour(*tokens)) {
				tokens = l
			}
		case "TIME_LIMIT":
			if tools == nil || (l.Unit == 5 && l.Number == 1 && !isMonthly(*tools)) {
				tools = l
			}
		case "CREDIT_LIMIT":
			if credits == nil {
				credits = l
			}
		}
	}
	return tokens, tools, credits
}

func isFiveHour(l quotaLimit) bool { return l.Unit == 3 && l.Number == 5 }

func isMonthly(l quotaLimit) bool { return l.Unit == 5 && l.Number == 1 }

// countValues returns currentValue and remaining as readings, with the
// provider's quota ceiling (usage, or total on older token limits) as Limit.
func countValues(l *quotaLimit) (used, remaining, limit *float64) {
	used = floatValue(l.CurrentValue)
	remaining = floatValue(l.Remaining)
	if l.Usage != nil {
		limit = floatValue(l.Usage)
	} else {
		limit = floatValue(l.Total)
	}
	return used, remaining, limit
}

func floatValue(v *int64) *float64 {
	if v == nil {
		return nil
	}
	f := float64(*v)
	return &f
}

func resetAt(ms *int64) *time.Time {
	if ms == nil || *ms <= 0 {
		return nil
	}
	t := time.UnixMilli(*ms).UTC()
	return &t
}
