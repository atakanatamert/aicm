// SPDX-License-Identifier: SUL-1.0

package providertest_test

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/providertest"
)

// example is a complete adapter for an imaginary wallet provider. Real
// adapters live in internal/provider/<id>/<id>.go and follow the same shape:
// constructor, Info, Validate, then one method per fetcher.
type example struct {
	c *provider.HTTPClient
}

func newExample(c *provider.HTTPClient) *example { return &example{c: c} }

const exampleBase = "https://api.example.com"

func (p *example) Info() provider.Info {
	return provider.Info{
		ID:        "example",
		Name:      "Example AI",
		Currency:  "CNY",
		AuthKinds: []provider.AuthKind{provider.AuthAPIKey},
		Links: provider.Links{
			TopUp: "https://example.com/top_up",
			Usage: "https://example.com/usage",
		},
		Meters: []meter.Def{
			{Key: "balance_topped_up", Label: "Topped-up balance", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever},
			{Key: "balance_granted", Label: "Granted balance", Unit: meter.UnitCurrency, UnitCurrency: "CNY", ResetKind: meter.ResetNever},
		},
		MinPollInterval: 15 * time.Minute,
	}
}

func (p *example) auth(c provider.Credentials) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Secret}}
}

func (p *example) Validate(ctx context.Context, c provider.Credentials) error {
	var out struct{}
	if err := p.c.GetJSON(ctx, exampleBase+"/user/balance", p.auth(c), &out); err != nil {
		return fmt.Errorf("validating example key: %w", err)
	}
	return nil
}

// FetchQuota reads the wallet balance. Decimal strings go through
// money.ParseDecimal; never parse money as float.
func (p *example) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	var out struct {
		Balance struct {
			Currency  string `json:"currency"`
			ToppedUp  string `json:"topped_up"`
			Granted   string `json:"granted"`
			UpdatedAt int64  `json:"updated_at"`
		} `json:"balance"`
	}
	if err := p.c.GetJSON(ctx, exampleBase+"/user/balance", p.auth(c), &out); err != nil {
		return nil, fmt.Errorf("fetching example balance: %w", err)
	}
	now := p.c.Now().UTC()
	balances := []struct{ key, value string }{
		{"balance_topped_up", out.Balance.ToppedUp},
		{"balance_granted", out.Balance.Granted},
	}
	readings := make([]meter.Reading, 0, len(balances))
	for _, b := range balances {
		amt, err := money.ParseDecimal(b.value, out.Balance.Currency)
		if err != nil {
			return nil, fmt.Errorf("parsing %s: %w", b.key, err)
		}
		readings = append(readings, meter.Reading{
			Key:        b.key,
			Remaining:  new(float64(amt.Minor)),
			CapturedAt: now,
			Source:     meter.SourceAPI,
		})
	}
	return readings, nil
}

// FetchUsage follows every page. Days are UTC.
func (p *example) FetchUsage(ctx context.Context, c provider.Credentials, from, to time.Time) ([]provider.UsageRecord, error) {
	var recs []provider.UsageRecord
	page := ""
	for {
		q := url.Values{"start": {from.UTC().Format(time.DateOnly)}, "end": {to.UTC().Format(time.DateOnly)}}
		if page != "" {
			q.Set("page", page)
		}
		var out struct {
			Data []struct {
				Date   string `json:"date"`
				Model  string `json:"model"`
				Reqs   int64  `json:"requests"`
				In     int64  `json:"input_tokens"`
				Out    int64  `json:"output_tokens"`
				Cost   string `json:"cost"`
				CostCC string `json:"currency"`
			} `json:"data"`
			NextPage string `json:"next_page"`
		}
		if err := p.c.GetJSON(ctx, exampleBase+"/usage?"+q.Encode(), p.auth(c), &out); err != nil {
			return nil, fmt.Errorf("fetching example usage: %w", err)
		}
		for _, d := range out.Data {
			day, err := time.Parse(time.DateOnly, d.Date)
			if err != nil {
				return nil, fmt.Errorf("parsing usage date %q: %w", d.Date, err)
			}
			cost, err := money.ParseMicro(d.Cost)
			if err != nil {
				return nil, err
			}
			recs = append(recs, provider.UsageRecord{
				Day: day, Model: d.Model, Requests: d.Reqs,
				InputTokens: d.In, OutputTokens: d.Out,
				CostMicro: new(cost), CostCurrency: d.CostCC,
			})
		}
		if out.NextPage == "" {
			return recs, nil
		}
		page = out.NextPage
	}
}

// The golden test every adapter has. The fixture includes a 429 with
// Retry-After before the usage call succeeds, and a second usage page.
func TestExampleGolden(t *testing.T) {
	c, creds := providertest.Setup(t, "example", nil)
	p := newExample(c)
	ctx := context.Background()

	if err := p.Info().Validate(); err != nil {
		t.Fatal(err)
	}
	if err := p.Validate(ctx, creds); err != nil {
		t.Fatal(err)
	}
	quota, err := p.FetchQuota(ctx, creds)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range quota {
		def, _ := p.Info().Meter(r.Key)
		if err := r.Validate(def); err != nil {
			t.Error(err)
		}
	}
	from := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	usage, err := p.FetchUsage(ctx, creds, from, from.AddDate(0, 0, 2))
	if err != nil {
		t.Fatal(err)
	}
	providertest.Golden(t, "example", map[string]any{"quota": quota, "usage": usage})
}
