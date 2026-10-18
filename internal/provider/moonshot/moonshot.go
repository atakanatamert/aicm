// SPDX-License-Identifier: AGPL-3.0-or-later

// Package moonshot adapts the Moonshot AI (Kimi) Open Platform balance
// endpoint. Moonshot has no usage or charge API, so the adapter fetches quota
// only. The region config switches between the global and China hosts and
// their currencies.
package moonshot

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

// Base URLs. A region switch picks one; the key is only valid on the host
// that issued it.
const (
	globalBaseURL = "https://api.moonshot.ai"
	chinaBaseURL  = "https://api.moonshot.cn"
)

// Meter keys. The provider reports the region's currency, so both currencies
// are declared and FetchQuota returns the pair for the configured region.
const (
	cashUSDKey    = "cash_usd"
	voucherUSDKey = "voucher_usd"
	cashCNYKey    = "cash_cny"
	voucherCNYKey = "voucher_cny"
)

// Provider implements provider.Provider and provider.QuotaFetcher.
type Provider struct {
	c *provider.HTTPClient
}

var (
	_ provider.Provider     = (*Provider)(nil)
	_ provider.QuotaFetcher = (*Provider)(nil)
)

// New returns a Moonshot adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// balanceResponse is the GET /v1/users/me/balance response. Amounts are JSON
// numbers in the region's currency: USD globally and CNY in China.
type balanceResponse struct {
	Data struct {
		AvailableBalance float64 `json:"available_balance"`
		VoucherBalance   float64 `json:"voucher_balance"`
		CashBalance      float64 `json:"cash_balance"`
	} `json:"data"`
}

// Info returns the catalog entry.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "moonshot",
		Name:      "Moonshot (Kimi)",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			TopUp: "https://platform.moonshot.ai/console/pay",
			Docs:  "https://platform.kimi.ai/docs/api/balance",
		},
		Meters: []meter.Def{
			{Key: cashUSDKey, Label: "Cash balance (USD)", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			{Key: voucherUSDKey, Label: "Voucher balance (USD)", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			{Key: cashCNYKey, Label: "Cash balance (CNY)", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever},
			{Key: voucherCNYKey, Label: "Voucher balance (CNY)", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever},
		},
		ConfigFields: []provider.ConfigField{
			{Key: "region", Label: "Region", Options: []string{"global", "china"}},
		},
		MinPollInterval: 15 * time.Minute,
		ModelFamilies:   []string{"kimi"},
	}
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Secret}}
}

// region describes the host and currency selected by Credentials.Config.
type region struct {
	baseURL    string
	currency   string
	cashKey    string
	voucherKey string
}

// regionFor returns the configured region, defaulting to global when the
// field is absent or unrecognised.
func regionFor(c provider.Credentials) region {
	if c.Config["region"] == "china" {
		return region{baseURL: chinaBaseURL, currency: "CNY", cashKey: cashCNYKey, voucherKey: voucherCNYKey}
	}
	return region{baseURL: globalBaseURL, currency: "USD", cashKey: cashUSDKey, voucherKey: voucherUSDKey}
}

// Validate reads the balance, the cheapest authenticated call Moonshot has.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	var out balanceResponse
	if err := p.c.GetJSON(ctx, regionFor(c).baseURL+"/v1/users/me/balance", p.auth(c), &out); err != nil {
		return fmt.Errorf("validating moonshot key: %w", err)
	}
	return nil
}

// FetchQuota returns cash and voucher balances for the configured region.
// available_balance is their sum, so it is not stored.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	r := regionFor(c)
	var out balanceResponse
	if err := p.c.GetJSON(ctx, r.baseURL+"/v1/users/me/balance", p.auth(c), &out); err != nil {
		return nil, fmt.Errorf("fetching moonshot balance: %w", err)
	}
	now := p.c.Now().UTC()
	balances := []struct {
		key   string
		value float64
	}{
		{r.cashKey, out.Data.CashBalance},
		{r.voucherKey, out.Data.VoucherBalance},
	}
	readings := make([]meter.Reading, 0, len(balances))
	for _, b := range balances {
		amt, err := money.FromFloat(b.value, r.currency)
		if err != nil {
			return nil, fmt.Errorf("parsing moonshot %s: %w", b.key, err)
		}
		remaining := float64(amt.Minor)
		readings = append(readings, meter.Reading{
			Key:        b.key,
			Remaining:  &remaining,
			CapturedAt: now,
			Source:     meter.SourceAPI,
		})
	}
	return readings, nil
}
