// SPDX-License-Identifier: AGPL-3.0-or-later

// Package deepseek adapts DeepSeek's balance endpoint. DeepSeek has no usage
// or charge API, so the adapter fetches quota only.
package deepseek

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

const baseURL = "https://api.deepseek.com"

// Meter keys. Kept apart per currency because an account holds one or the
// other (or both), and the provider reports the balance in that currency.
const (
	topUpCNYKey   = "topped_up_cny"
	grantedCNYKey = "granted_cny"
	topUpUSDKey   = "topped_up_usd"
	grantedUSDKey = "granted_usd"
)

// Provider implements provider.Provider and provider.QuotaFetcher.
type Provider struct {
	c *provider.HTTPClient
}

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.QuotaFetcher = (*Provider)(nil)
)

// New returns a DeepSeek adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// balanceResponse is the GET /user/balance response. Amounts are decimal
// strings in major units, never JSON numbers.
type balanceResponse struct {
	IsAvailable  bool          `json:"is_available"`
	BalanceInfos []balanceInfo `json:"balance_infos"`
}

type balanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// Info returns the catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "deepseek",
		Name:      "DeepSeek",
		Currency:  "CNY",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			TopUp: "https://platform.deepseek.com/top_up",
			Usage: "https://platform.deepseek.com/usage",
			Keys:  "https://platform.deepseek.com/api_keys",
			Docs:  "https://api-docs.deepseek.com/api/get-user-balance",
		},
		Meters: []meter.Def{
			{Key: topUpCNYKey, Label: "Topped-up balance (CNY)", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever},
			{Key: grantedCNYKey, Label: "Granted balance (CNY)", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever, Supplemental: true},
			{Key: topUpUSDKey, Label: "Topped-up balance (USD)", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			{Key: grantedUSDKey, Label: "Granted balance (USD)", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever, Supplemental: true},
		},
		MinPollInterval: 15 * time.Minute,
		ModelFamilies:   []string{"deepseek"},
	}
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Secret}}
}

// Validate reads the balance, the cheapest authenticated call DeepSeek has.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	var out balanceResponse
	if err := p.c.GetJSON(ctx, baseURL+"/user/balance", p.auth(c), &out); err != nil {
		return fmt.Errorf("validating deepseek key: %w", err)
	}
	return nil
}

// FetchQuota returns topped-up and granted balances for every currency the
// account reports. Total is deliberately not a meter: top-up detection needs
// the two parts separate from free grants.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	var out balanceResponse
	if err := p.c.GetJSON(ctx, baseURL+"/user/balance", p.auth(c), &out); err != nil {
		return nil, fmt.Errorf("fetching deepseek balance: %w", err)
	}
	now := p.c.Now().UTC()
	var readings []meter.Reading
	for _, b := range out.BalanceInfos {
		keys, ok := currencyMeters(b.Currency)
		if !ok {
			continue // a currency the catalog does not model
		}
		for _, balance := range []struct{ key, value string }{
			{keys.toppedUp, b.ToppedUpBalance},
			{keys.granted, b.GrantedBalance},
		} {
			amt, err := money.ParseDecimal(balance.value, b.Currency)
			if err != nil {
				return nil, fmt.Errorf("parsing deepseek %s: %w", balance.key, err)
			}
			remaining := float64(amt.Minor)
			readings = append(readings, meter.Reading{
				Key:        balance.key,
				Remaining:  &remaining,
				CapturedAt: now,
				Source:     meter.SourceAPI,
			})
		}
	}
	return readings, nil
}

type currencyKeys struct {
	toppedUp string
	granted  string
}

func currencyMeters(currency string) (currencyKeys, bool) {
	switch currency {
	case "CNY":
		return currencyKeys{topUpCNYKey, grantedCNYKey}, true
	case "USD":
		return currencyKeys{topUpUSDKey, grantedUSDKey}, true
	}
	return currencyKeys{}, false
}
