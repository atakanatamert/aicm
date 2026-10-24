// SPDX-License-Identifier: SUL-1.0

// Package googleaistudio adapts Gemini API keys from Google AI Studio. Google
// offers no balance, spend or usage API for these keys outside Google Cloud
// billing, so the adapter only checks keys: usage comes from agents reading
// coding-tool logs, and prepaid credits and the monthly spend are manual
// meters with links to the AI Studio pages.
package googleaistudio

import (
	"context"
	"fmt"
	"net/http"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

const modelsURL = "https://generativelanguage.googleapis.com/v1beta/models?pageSize=1"

// Provider implements provider.Provider.
type Provider struct {
	c *provider.HTTPClient
}

var _ provider.Provider = (*Provider)(nil)

// New returns the AI Studio adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// Info returns the catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "google-ai-studio",
		Name:      "Google AI Studio (Gemini API)",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey, provider.AuthNone},
		Links: provider.Links{
			TopUp:   "https://aistudio.google.com/billing",
			Billing: "https://aistudio.google.com/billing",
			Usage:   "https://aistudio.google.com/usage",
			Keys:    "https://aistudio.google.com/apikey",
			Docs:    "https://ai.google.dev/gemini-api/docs/billing",
		},
		Meters: []meter.Def{
			{Key: "prepaid_credits", Label: "Prepaid credits", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			{Key: "monthly_spend", Label: "Spend this month", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetCalendarMonth},
		},
		ModelFamilies: []string{"gemini"},
	}
}

// Validate lists one model, the cheapest authenticated call.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	if c.Secret == "" {
		return nil // an account tracked by hand, without a key
	}
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	auth := http.Header{"x-goog-api-key": {c.Secret}}
	if err := p.c.GetJSON(ctx, modelsURL, auth, &out); err != nil {
		return fmt.Errorf("validating gemini api key: %w", err)
	}
	return nil
}
