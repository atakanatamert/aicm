// SPDX-License-Identifier: AGPL-3.0-or-later

// Package typesafe adapts TypeSafe (the Jev System One model). Its API has no
// balance, usage or billing endpoint, so the adapter only checks keys; usage
// comes from agents reading coding-tool logs, and the prepaid balance is a
// manual meter.
package typesafe

import (
	"context"
	"fmt"
	"net/http"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

const baseURL = "https://api.typesafe.ai/v1"

// Provider implements provider.Provider.
type Provider struct {
	c *provider.HTTPClient
}

var _ provider.Provider = (*Provider)(nil)

// New returns a TypeSafe adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// Info returns the catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "typesafe",
		Name:      "TypeSafe (Jev)",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			TopUp:   "https://console.typesafe.ai/",
			Billing: "https://console.typesafe.ai/",
			Keys:    "https://console.typesafe.ai/",
			Docs:    "https://docs.typesafe.ai/",
		},
		Meters: []meter.Def{
			{Key: "prepaid_balance", Label: "Prepaid balance", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
		},
		ModelFamilies: []string{"jev"},
	}
}

// Validate lists the account's models, the only cheap authenticated call.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	var out struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	auth := http.Header{"Authorization": {"Bearer " + c.Secret}}
	if err := p.c.GetJSON(ctx, baseURL+"/models", auth, &out); err != nil {
		return fmt.Errorf("validating typesafe key: %w", err)
	}
	return nil
}
