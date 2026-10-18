// SPDX-License-Identifier: AGPL-3.0-or-later

// Package copilot adapts GitHub's billing AI credit usage report for an
// individual Copilot plan. GitHub bills Copilot in AI credits; the endpoint
// reports usage for the current month only, so the adapter returns one
// calendar-month meter.
package copilot

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

// baseURL is the GitHub REST API host.
const baseURL = "https://api.github.com"

// apiVersion is the pinned REST API version the docs describe.
const apiVersion = "2026-03-10"

// meterKey is the single meter the AI credit report feeds.
const meterKey = "ai_credits"

// Provider implements provider.Provider and provider.QuotaFetcher.
type Provider struct {
	c *provider.HTTPClient
}

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.QuotaFetcher = (*Provider)(nil)
)

// New returns a GitHub Copilot adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// usageResponse is one page of GET /users/{username}/settings/billing/ai_credit/usage.
// The docs also return top-level user, product and model filters; the
// adapter only needs the usage items.
type usageResponse struct {
	UsageItems []usageItem `json:"usageItems"`
}

// usageItem is one row of the report. Net quantity is what the account is
// billed after discounts.
type usageItem struct {
	Product       string  `json:"product"`
	SKU           string  `json:"sku"`
	Model         string  `json:"model"`
	UnitType      string  `json:"unitType"`
	NetQuantity   float64 `json:"netQuantity"`
	NetAmount     float64 `json:"netAmount"`
	GrossQuantity float64 `json:"grossQuantity"`
}

// Info returns the catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "copilot",
		Name:      "GitHub Copilot",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			Billing:            "https://github.com/settings/billing",
			ManageSubscription: "https://github.com/settings/copilot",
			Usage:              "https://github.com/settings/billing/usage",
			Docs:               "https://docs.github.com/en/rest/billing/usage",
		},
		Meters: []meter.Def{
			{Key: meterKey, Label: "AI credits", Unit: meter.UnitCredits, ResetKind: meter.ResetCalendarMonth},
		},
		ConfigFields: []provider.ConfigField{
			{Key: "username", Label: "Username", Help: "GitHub user whose Copilot usage to read.", Required: true},
			{Key: "plan", Label: "Plan", Options: []string{"free", "pro", "pro_plus", "max"}, Required: true},
		},
		MinPollInterval: time.Hour,
		ModelFamilies:   []string{"claude", "gpt", "gemini"},
	}
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	return http.Header{
		"Authorization":        {"Bearer " + c.Secret},
		"Accept":               {"application/vnd.github+json"},
		"X-GitHub-Api-Version": {apiVersion},
	}
}

// usageURL is the report for the current month. The API pages with Link.
func usageURL(username string) string {
	return baseURL + "/users/" + url.PathEscape(username) + "/settings/billing/ai_credit/usage"
}

// Validate reads the first page of the usage report, the cheapest
// authenticated call the billing API exposes.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	if _, _, err := p.fetchPage(ctx, c, usageURL(c.Config["username"])); err != nil {
		return fmt.Errorf("validating copilot key: %w", err)
	}
	return nil
}

// FetchQuota sums net AI credit usage over every page and compares it with
// the plan allowance.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	next := usageURL(c.Config["username"])
	used := 0.0
	for next != "" {
		page, link, err := p.fetchPage(ctx, c, next)
		if err != nil {
			return nil, fmt.Errorf("fetching copilot AI credit usage: %w", err)
		}
		for _, item := range page.UsageItems {
			used += item.NetQuantity
		}
		next = nextPage(link)
	}

	now := p.c.Now().UTC()
	reset := time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, time.UTC)
	reading := meter.Reading{
		Key:        meterKey,
		Used:       &used,
		ResetsAt:   &reset,
		CapturedAt: now,
		Source:     meter.SourceAPI,
	}
	if limit := planLimit(c.Config["plan"]); limit != nil {
		reading.Limit = limit
		remaining := *limit - used
		if remaining < 0 {
			remaining = 0
		}
		reading.Remaining = &remaining
	}
	return []meter.Reading{reading}, nil
}

// fetchPage reads one report page and returns its Link header.
func (p *Provider) fetchPage(ctx context.Context, c provider.Credentials, pageURL string) (usageResponse, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, pageURL, nil)
	if err != nil {
		return usageResponse{}, "", fmt.Errorf("building request: %w", err)
	}
	req.Header = p.auth(c)
	resp, err := p.c.Do(req)
	if err != nil {
		return usageResponse{}, "", err
	}
	defer resp.Body.Close()

	var out usageResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return usageResponse{}, "", fmt.Errorf("decoding copilot usage response: %w", err)
	}
	return out, resp.Header.Get("Link"), nil
}

// planLimit is the monthly AI credit allowance of an individual plan. Free
// has an allowance the docs do not quantify, so it stays unknown.
func planLimit(plan string) *float64 {
	var limit float64
	switch plan {
	case "pro":
		limit = 1500
	case "pro_plus":
		limit = 7000
	case "max":
		limit = 20000
	default:
		return nil
	}
	return &limit
}

// nextPage extracts the rel="next" target from a Link header. An absent or
// malformed header means there is no next page.
func nextPage(link string) string {
	for _, part := range strings.Split(link, ",") {
		segments := strings.Split(part, ";")
		target := strings.TrimSpace(segments[0])
		if !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		for _, param := range segments[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(param), "=")
			if ok && strings.TrimSpace(key) == "rel" && strings.Trim(strings.TrimSpace(value), `"`) == "next" {
				return strings.TrimSuffix(strings.TrimPrefix(target, "<"), ">")
			}
		}
	}
	return ""
}
