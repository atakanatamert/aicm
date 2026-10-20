// SPDX-License-Identifier: AGPL-3.0-or-later

package zai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

// accountReportPath is the console's finance summary. Like the quota
// endpoint it is undocumented; it accepts any API key of the account.
const accountReportPath = "/api/biz/account/query-customer-account-report"

// Balance meter keys, one per region currency.
const (
	balanceUSDKey = "balance_usd"
	balanceCNYKey = "balance_cny"
)

// APIProvider reads the pay-as-you-go API balance, which Z.ai bills
// separately from the GLM Coding Plan.
type APIProvider struct {
	c *provider.HTTPClient
}

var (
	_ provider.Provider     = (*APIProvider)(nil)
	_ provider.QuotaFetcher = (*APIProvider)(nil)
)

// NewAPI returns the Z.ai API balance adapter using c for HTTP.
func NewAPI(c *provider.HTTPClient) *APIProvider { return &APIProvider{c: c} }

type accountReport struct {
	Code    int    `json:"code"`
	Msg     string `json:"msg"`
	Success bool   `json:"success"`
	Data    struct {
		AvailableBalance *json.Number `json:"availableBalance"`
	} `json:"data"`
}

// Info returns the catalog entry.
func (p *APIProvider) Info() provider.Info {
	return provider.Info{
		ID:        "zai-api",
		Name:      "Z.ai API (pay-as-you-go)",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			TopUp:   "https://z.ai/manage-apikey/billing",
			Billing: "https://z.ai/manage-apikey/billing",
		},
		Meters: []meter.Def{
			{Key: balanceUSDKey, Label: "API balance (USD)", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			{Key: balanceCNYKey, Label: "API balance (CNY)", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever},
		},
		ConfigFields: []provider.ConfigField{
			{
				Key:      "region",
				Label:    "Region",
				Help:     "global uses https://api.z.ai (USD), china uses https://open.bigmodel.cn (CNY)",
				Required: true,
				Options:  []string{"global", "china"},
			},
		},
		Unofficial:      true,
		MinPollInterval: 15 * time.Minute,
		ModelFamilies:   []string{"glm"},
	}
}

// Validate reads the balance, the only call this adapter makes.
func (p *APIProvider) Validate(ctx context.Context, c provider.Credentials) error {
	if _, err := p.fetch(ctx, c); err != nil {
		return fmt.Errorf("validating zai api key: %w", err)
	}
	return nil
}

// FetchQuota returns the available API balance in the region's currency.
func (p *APIProvider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	report, err := p.fetch(ctx, c)
	if err != nil {
		return nil, err
	}
	if report.Data.AvailableBalance == nil {
		return nil, nil
	}
	key, currency := balanceUSDKey, "USD"
	if c.Config["region"] == "china" {
		key, currency = balanceCNYKey, "CNY"
	}
	amt, err := money.ParseDecimal(report.Data.AvailableBalance.String(), currency)
	if err != nil {
		return nil, fmt.Errorf("parsing zai api balance: %w", err)
	}
	remaining := float64(amt.Minor)
	return []meter.Reading{{
		Key:        key,
		Remaining:  &remaining,
		CapturedAt: p.c.Now().UTC(),
		Source:     meter.SourceAPI,
	}}, nil
}

func (p *APIProvider) fetch(ctx context.Context, c provider.Credentials) (accountReport, error) {
	var out accountReport
	if !c.AllowUnofficial {
		return out, provider.ErrUnofficialNotAllowed
	}
	base := globalBaseURL
	if c.Config["region"] == "china" {
		base = chinaBaseURL
	}
	auth := http.Header{"Authorization": {"Bearer " + c.Secret}}
	if err := p.c.GetJSON(ctx, base+accountReportPath, auth, &out); err != nil {
		return out, fmt.Errorf("fetching zai account report: %w", err)
	}
	if !out.Success {
		return out, fmt.Errorf("zai account report: code %d: %s", out.Code, out.Msg)
	}
	return out, nil
}
