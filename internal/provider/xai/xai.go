// SPDX-License-Identifier: AGPL-3.0-or-later

// Package xai adapts xAI's Management API. It reads the team's prepaid
// credit balance, daily usage grouped by billing line description, and the
// invoices and prepaid top-ups that took money out.
package xai

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

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
	"github.com/atakanatamert/aicm/internal/provider"
)

const baseURL = "https://management-api.x.ai"

// balanceKey is the only meter xAI's billing API reports.
const balanceKey = "prepaid_balance"

// Provider implements provider.Provider, provider.QuotaFetcher,
// provider.UsageFetcher and provider.ChargeFetcher.
type Provider struct {
	c *provider.HTTPClient
}

var (
	_ provider.Provider      = (*Provider)(nil)
	_ provider.QuotaFetcher  = (*Provider)(nil)
	_ provider.UsageFetcher  = (*Provider)(nil)
	_ provider.ChargeFetcher = (*Provider)(nil)
)

// New returns an xAI Management API adapter using c for HTTP.
func New(c *provider.HTTPClient) *Provider { return &Provider{c: c} }

// Info returns the catalog entry. Every xAI billing amount is in USD cents.
func (p *Provider) Info() provider.Info {
	return provider.Info{
		ID:        "xai",
		Name:      "xAI API",
		Currency:  "USD",
		AuthKinds: []provider.AuthKind{provider.AuthAdminKey},
		Links: provider.Links{
			Billing: "https://console.x.ai",
			Docs:    "https://docs.x.ai/developers/rest-api-reference/management/billing",
		},
		Meters: []meter.Def{
			{Key: balanceKey, Label: "Prepaid credits", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
		},
		ConfigFields: []provider.ConfigField{
			{Key: "team_id", Label: "Team ID", Required: true},
		},
		MinPollInterval: time.Hour,
		ModelFamilies:   []string{"grok"},
	}
}

func (p *Provider) auth(c provider.Credentials) http.Header {
	return http.Header{"Authorization": {"Bearer " + c.Secret}}
}

// Validate checks the key with the Management API's dedicated validation
// endpoint, which needs no team id.
func (p *Provider) Validate(ctx context.Context, c provider.Credentials) error {
	var out struct{}
	if err := p.c.GetJSON(ctx, baseURL+"/auth/management-keys/validation", p.auth(c), &out); err != nil {
		return fmt.Errorf("validating xAI management key: %w", err)
	}
	return nil
}

// FetchQuota reads the posted prepaid credit balance. xAI reports the ledger
// as USD cents with purchases negative, so the available balance is its
// negation.
func (p *Provider) FetchQuota(ctx context.Context, c provider.Credentials) ([]meter.Reading, error) {
	team, err := teamID(c)
	if err != nil {
		return nil, err
	}
	bal, err := p.fetchPrepaidBalance(ctx, c, team)
	if err != nil {
		return nil, err
	}
	cents, err := parseCents(bal.Total.Val)
	if err != nil {
		return nil, fmt.Errorf("parsing xAI prepaid balance: %w", err)
	}
	remaining := float64(-cents)
	return []meter.Reading{{
		Key:        balanceKey,
		Remaining:  &remaining,
		CapturedAt: p.c.Now().UTC(),
		Source:     meter.SourceAPI,
	}}, nil
}

// FetchUsage reads daily cost per billing description. The documented
// analytics fields are usd and description; the description is the
// provider's model-and-endpoint label, for example "Chat grok-4-0709".
func (p *Provider) FetchUsage(ctx context.Context, c provider.Credentials, from, to time.Time) ([]provider.UsageRecord, error) {
	team, err := teamID(c)
	if err != nil {
		return nil, err
	}
	req := usageRequest{
		AnalyticsRequest: analyticsRequest{
			TimeRange: timeRange{
				StartTime: from.UTC().Format("2006-01-02 15:04:05"),
				EndTime:   to.UTC().Format("2006-01-02 15:04:05"),
				Timezone:  "Etc/GMT",
			},
			TimeUnit: "TIME_UNIT_DAY",
			Values: []analyticsValue{
				{Name: "usd", Aggregation: "AGGREGATION_SUM"},
			},
			GroupBy: []string{"description"},
			Filters: []string{},
		},
	}
	var out usageResponse
	if err := p.c.SendJSON(ctx, http.MethodPost, teamURL(team, "/usage"), p.auth(c), req, &out); err != nil {
		return nil, fmt.Errorf("fetching xAI usage: %w", err)
	}
	if out.LimitReached {
		return nil, errors.New("fetching xAI usage: the analytics query reached its cardinality limit; narrow the range")
	}

	records := map[usageKey]*provider.UsageRecord{}
	for _, series := range out.TimeSeries {
		model := "unknown"
		if len(series.GroupLabels) > 0 {
			model = series.GroupLabels[0]
		} else if len(series.Group) > 0 {
			model = series.Group[0]
		}
		for _, point := range series.DataPoints {
			ts, err := time.Parse(time.RFC3339Nano, point.Timestamp)
			if err != nil {
				return nil, fmt.Errorf("parsing xAI usage timestamp %q: %w", point.Timestamp, err)
			}
			day := dayUTC(ts)
			if day.Before(from.UTC()) || !day.Before(to.UTC()) {
				continue
			}
			if len(point.Values) == 0 {
				continue
			}
			micro, err := money.MicroFromFloat(point.Values[0])
			if err != nil {
				return nil, fmt.Errorf("parsing xAI usage cost for %q: %w", model, err)
			}
			key := usageKey{day: day, model: model}
			rec, ok := records[key]
			if !ok {
				rec = &provider.UsageRecord{Day: day, Model: model}
				records[key] = rec
			}
			sum := micro
			if rec.CostMicro != nil {
				sum += *rec.CostMicro
			}
			rec.CostMicro = &sum
			rec.CostCurrency = "USD"
		}
	}

	usage := make([]provider.UsageRecord, 0, len(records))
	for _, rec := range records {
		usage = append(usage, *rec)
	}
	sort.Slice(usage, func(i, j int) bool {
		if !usage[i].Day.Equal(usage[j].Day) {
			return usage[i].Day.Before(usage[j].Day)
		}
		return usage[i].Model < usage[j].Model
	})
	return usage, nil
}

// FetchCharges returns invoices and succeeded prepaid top-ups since the given
// time. Both endpoints report money in USD cents.
func (p *Provider) FetchCharges(ctx context.Context, c provider.Credentials, since time.Time) ([]provider.Charge, error) {
	team, err := teamID(c)
	if err != nil {
		return nil, err
	}
	invoices, err := p.fetchInvoices(ctx, c, team, since)
	if err != nil {
		return nil, err
	}
	bal, err := p.fetchPrepaidBalance(ctx, c, team)
	if err != nil {
		return nil, err
	}
	charges := invoices
	for _, change := range bal.Changes {
		if change.ChangeOrigin != "PURCHASE" && change.ChangeOrigin != "AUTO_PURCHASE" {
			continue
		}
		if change.TopUpStatus != "" && change.TopUpStatus != "SUCCEEDED" {
			continue
		}
		occurred, err := parseTime(firstNonEmpty(change.CreateTime, change.CreateTS))
		if err != nil {
			return nil, fmt.Errorf("parsing xAI top-up time: %w", err)
		}
		if occurred.Before(since.UTC()) {
			continue
		}
		cents, err := parseCents(change.Amount.Val)
		if err != nil {
			return nil, fmt.Errorf("parsing xAI top-up amount: %w", err)
		}
		externalID := firstNonEmpty(change.InvoiceID, change.CreateTS)
		if externalID == "" {
			continue // no stable id, so a retry could not be deduplicated
		}
		charges = append(charges, provider.Charge{
			Kind:        provider.ChargeTopUp,
			Amount:      usdCents(-cents),
			OccurredOn:  dayUTC(occurred),
			ExternalID:  externalID,
			Description: firstNonEmpty(change.InvoiceNumber, "Prepaid top-up"),
		})
	}
	sort.Slice(charges, func(i, j int) bool {
		if !charges[i].OccurredOn.Equal(charges[j].OccurredOn) {
			return charges[i].OccurredOn.Before(charges[j].OccurredOn)
		}
		if charges[i].Kind != charges[j].Kind {
			return charges[i].Kind < charges[j].Kind
		}
		return charges[i].ExternalID < charges[j].ExternalID
	})
	return charges, nil
}

func (p *Provider) fetchInvoices(ctx context.Context, c provider.Credentials, team string, since time.Time) ([]provider.Charge, error) {
	q := url.Values{
		"since.year":  {strconv.Itoa(since.UTC().Year())},
		"since.month": {strconv.Itoa(int(since.UTC().Month()))},
	}
	var out invoicesResponse
	if err := p.c.GetJSON(ctx, teamURL(team, "/invoices")+"?"+q.Encode(), p.auth(c), &out); err != nil {
		return nil, fmt.Errorf("fetching xAI invoices: %w", err)
	}
	var charges []provider.Charge
	for _, inv := range out.Invoices {
		if inv.InvoiceID == "" {
			continue // ExternalID is required for idempotent syncs
		}
		created, err := parseTime(inv.CreateTime)
		if err != nil {
			return nil, fmt.Errorf("parsing xAI invoice %q time: %w", inv.InvoiceID, err)
		}
		if created.Before(since.UTC()) {
			continue
		}
		cents, err := parseCents(inv.Total)
		if err != nil {
			return nil, fmt.Errorf("parsing xAI invoice %q total: %w", inv.InvoiceID, err)
		}
		charges = append(charges, provider.Charge{
			Kind:        provider.ChargeInvoice,
			Amount:      usdCents(cents),
			OccurredOn:  dayUTC(created),
			ExternalID:  inv.InvoiceID,
			Description: inv.InvoiceNumber,
		})
	}
	return charges, nil
}

func (p *Provider) fetchPrepaidBalance(ctx context.Context, c provider.Credentials, team string) (prepaidBalance, error) {
	var out prepaidBalance
	if err := p.c.GetJSON(ctx, teamURL(team, "/prepaid/balance"), p.auth(c), &out); err != nil {
		return prepaidBalance{}, fmt.Errorf("fetching xAI prepaid balance: %w", err)
	}
	return out, nil
}

func teamID(c provider.Credentials) (string, error) {
	id := strings.TrimSpace(c.Config["team_id"])
	if id == "" {
		return "", errors.New("xAI team_id is required")
	}
	return id, nil
}

func teamURL(team, suffix string) string {
	return baseURL + "/v1/billing/teams/" + url.PathEscape(team) + suffix
}

// usdCents returns an Amount whose minor units are already USD cents.
func usdCents(cents int64) money.Amount {
	return money.Amount{Minor: cents, Currency: "USD"}
}

// parseCents parses an xAI "USD Cents" string. These values are already
// whole minor units, so they need no scaling.
func parseCents(s string) (int64, error) {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parsing USD cents %q: %w", s, err)
	}
	return v, nil
}

func parseTime(s string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parsing time %q: %w", s, err)
	}
	return t.UTC(), nil
}

func dayUTC(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

type usageKey struct {
	day   time.Time
	model string
}

type usageRequest struct {
	AnalyticsRequest analyticsRequest `json:"analyticsRequest"`
}

type analyticsRequest struct {
	TimeRange timeRange        `json:"timeRange"`
	TimeUnit  string           `json:"timeUnit"`
	Values    []analyticsValue `json:"values"`
	GroupBy   []string         `json:"groupBy"`
	Filters   []string         `json:"filters"`
}

type timeRange struct {
	StartTime string `json:"startTime"`
	EndTime   string `json:"endTime"`
	Timezone  string `json:"timezone"`
}

type analyticsValue struct {
	Name        string `json:"name"`
	Aggregation string `json:"aggregation"`
}

type usageResponse struct {
	TimeSeries   []usageSeries `json:"timeSeries"`
	LimitReached bool          `json:"limitReached"`
}

type usageSeries struct {
	Group       []string    `json:"group"`
	GroupLabels []string    `json:"groupLabels"`
	DataPoints  []dataPoint `json:"dataPoints"`
}

type dataPoint struct {
	Timestamp string    `json:"timestamp"`
	Values    []float64 `json:"values"`
}

type prepaidBalance struct {
	Changes []prepaidChange `json:"changes"`
	Total   cent            `json:"total"`
}

type cent struct {
	Val string `json:"val"`
}

type prepaidChange struct {
	ChangeOrigin  string `json:"changeOrigin"`
	TopUpStatus   string `json:"topupStatus"`
	Amount        cent   `json:"amount"`
	InvoiceID     string `json:"invoiceId"`
	InvoiceNumber string `json:"invoiceNumber"`
	CreateTime    string `json:"createTime"`
	CreateTS      string `json:"createTs"`
}

type invoicesResponse struct {
	Invoices []invoice `json:"invoices"`
}

type invoice struct {
	InvoiceID     string `json:"invoiceId"`
	InvoiceNumber string `json:"invoiceNumber"`
	CreateTime    string `json:"createTime"`
	Total         string `json:"total"`
}
