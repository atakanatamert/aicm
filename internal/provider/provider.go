// SPDX-License-Identifier: AGPL-3.0-or-later

// Package provider defines what a provider adapter is. Each provider is one
// package under internal/provider/<id>/ that declares its catalog entry
// (Info) and implements only the fetchers its API supports. The sync engine
// calls only what is declared.
//
// Adding a provider:
//  1. Create internal/provider/<id>/<id>.go with a New(c *provider.HTTPClient) constructor.
//  2. Return a complete Info (links and meters even if nothing is fetched).
//  3. Implement UsageFetcher, QuotaFetcher or ChargeFetcher as the API allows.
//  4. Record fixtures and write one golden test (see providertest).
//  5. Add one line to internal/provider/registry.
package provider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/money"
)

// AuthKind is how the hub authenticates to the provider for an account.
type AuthKind string

// Auth kinds.
const (
	AuthAPIKey       AuthKind = "api_key"       // normal provider key, stored by the hub
	AuthAdminKey     AuthKind = "admin_key"     // org-wide admin or management key
	AuthAgentCapture AuthKind = "agent_capture" // data comes from the agent; the hub holds nothing
	AuthNone         AuthKind = "none"          // manual entry only
)

// Links are provider pages shown as one-click buttons. Empty means none.
type Links struct {
	TopUp              string `json:"top_up,omitempty"`
	Billing            string `json:"billing,omitempty"`
	ManageSubscription string `json:"manage_subscription,omitempty"`
	Cancel             string `json:"cancel,omitempty"`
	Usage              string `json:"usage,omitempty"`
	Keys               string `json:"keys,omitempty"`
	Docs               string `json:"docs,omitempty"`
}

// ConfigField is a non-secret option the account form asks for, such as
// xAI's team id or a China-region host. Values arrive in Credentials.Config.
type ConfigField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Help     string   `json:"help,omitempty"`
	Required bool     `json:"required"`
	Options  []string `json:"options,omitempty"`
}

// Info is a provider's catalog entry. It ships in the binary and feeds the
// UI, MCP and the README providers table.
type Info struct {
	ID       string // stable lower-case id, also the package name: "openrouter"
	Name     string // display name: "OpenRouter"
	Currency string // ISO 4217 currency the provider bills in
	// AuthKinds lists supported auth kinds, preferred first.
	AuthKinds    []AuthKind
	Links        Links
	Meters       []meter.Def
	ConfigFields []ConfigField
	// Unofficial marks adapters that call undocumented endpoints. The sync
	// engine skips them unless the account opted in (Decision 9).
	Unofficial bool
	// MinPollInterval is the shortest gap between two syncs of one account.
	// The sync engine enforces it from accounts.last_synced_at.
	MinPollInterval time.Duration
	// ModelFamilies lists the model families the account can run, for
	// recommend_account: "claude", "gpt", "gemini", "grok", "deepseek",
	// "kimi", "glm". "*" means any (routers such as OpenRouter).
	ModelFamilies []string
	// Plans are the monthly list prices of the subscription tiers an agent
	// can detect, used to fill in a plan the user has not entered.
	Plans []PlanPrice
}

// PlanPrice is one subscription tier's public monthly list price before tax.
type PlanPrice struct {
	// Tier is the id agents report in agentlink.DetailPlanTier.
	Tier       string
	Name       string
	PriceMinor int64
	Currency   string
}

// Plan finds the tier's list price.
func (i Info) Plan(tier string) (PlanPrice, bool) {
	for _, p := range i.Plans {
		if p.Tier == tier {
			return p, true
		}
	}
	return PlanPrice{}, false
}

// Validate checks the catalog entry. The registry test runs it for every
// provider.
func (i Info) Validate() error {
	var errs []error
	if i.ID == "" || i.Name == "" {
		errs = append(errs, errors.New("id and name are required"))
	}
	if !money.ValidCurrency(i.Currency) {
		errs = append(errs, fmt.Errorf("invalid currency %q", i.Currency))
	}
	if len(i.AuthKinds) == 0 {
		errs = append(errs, errors.New("at least one auth kind is required"))
	}
	seen := map[string]bool{}
	for _, m := range i.Meters {
		if err := m.Validate(); err != nil {
			errs = append(errs, err)
		}
		if seen[m.Key] {
			errs = append(errs, fmt.Errorf("duplicate meter key %q", m.Key))
		}
		seen[m.Key] = true
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("provider %q: %w", i.ID, err)
	}
	return nil
}

// Meter returns the meter definition with the given key.
func (i Info) Meter(key string) (meter.Def, bool) {
	for _, m := range i.Meters {
		if m.Key == key {
			return m, true
		}
	}
	return meter.Def{}, false
}

// Credentials is what an adapter gets for one account. The secret is
// decrypted by the sync worker just before the call and must never be
// logged, returned or stored by an adapter.
type Credentials struct {
	Secret string
	// Config holds the values of Info.ConfigFields.
	Config map[string]string
	// AllowUnofficial is the account's opt-in for undocumented endpoints.
	AllowUnofficial bool
}

// String hides the secret from fmt.
func (c Credentials) String() string { return "provider.Credentials{redacted}" }

// GoString hides the secret from %#v.
func (c Credentials) GoString() string { return c.String() }

// LogValue hides the secret from slog.
func (c Credentials) LogValue() slog.Value { return slog.StringValue(c.String()) }

// Provider is implemented by every adapter.
type Provider interface {
	Info() Info
	// Validate makes the cheapest authenticated call to check the key.
	// Return ErrInvalidCredentials (wrapped) when the provider rejects it.
	// Providers with AuthNone or AuthAgentCapture only return nil.
	Validate(ctx context.Context, c Credentials) error
}

// UsageFetcher returns token usage per UTC day and model.
type UsageFetcher interface {
	// FetchUsage returns every record with Day in [from, to). The sync
	// engine replaces stored api usage for those days with the result, so
	// return complete days and follow every page.
	FetchUsage(ctx context.Context, c Credentials, from, to time.Time) ([]UsageRecord, error)
}

// QuotaFetcher returns current meter readings.
type QuotaFetcher interface {
	// FetchQuota returns one reading per meter it can see. Each Reading.Key
	// must match a meter in Info.Meters, and Source must be meter.SourceAPI.
	FetchQuota(ctx context.Context, c Credentials) ([]meter.Reading, error)
}

// ChargeFetcher returns money out the API exposes: invoices, top-ups.
type ChargeFetcher interface {
	// FetchCharges returns charges that occurred on or after since. Each
	// must carry the provider's ExternalID so repeated syncs are idempotent.
	FetchCharges(ctx context.Context, c Credentials, since time.Time) ([]Charge, error)
}

// Capabilities reports which fetchers p implements.
func Capabilities(p Provider) (usage, quota, charges bool) {
	_, usage = p.(UsageFetcher)
	_, quota = p.(QuotaFetcher)
	_, charges = p.(ChargeFetcher)
	return usage, quota, charges
}

// UsageRecord is token usage for one account, UTC day and model.
type UsageRecord struct {
	// Day is midnight UTC of the day.
	Day              time.Time `json:"day"`
	Model            string    `json:"model"`
	Requests         int64     `json:"requests"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	ReasoningTokens  int64     `json:"reasoning_tokens"`
	// CostMicro is the provider-reported cost in micro units of
	// CostCurrency (see money.ParseMicro), or nil if the API gives none.
	CostMicro    *int64 `json:"cost_micro,omitempty"`
	CostCurrency string `json:"cost_currency,omitempty"`
}

// ChargeKind classifies money out.
type ChargeKind string

// Charge kinds.
const (
	ChargeSubscription ChargeKind = "subscription"
	ChargeTopUp        ChargeKind = "topup"
	ChargeInvoice      ChargeKind = "invoice"
	ChargeRefund       ChargeKind = "refund"
)

// Charge is one payment the provider's API reports.
type Charge struct {
	Kind   ChargeKind   `json:"kind"`
	Amount money.Amount `json:"amount"`
	// OccurredOn is midnight UTC of the day the money left.
	OccurredOn  time.Time  `json:"occurred_on"`
	PeriodStart *time.Time `json:"period_start,omitempty"`
	PeriodEnd   *time.Time `json:"period_end,omitempty"`
	ExternalID  string     `json:"external_id"`
	Description string     `json:"description,omitempty"`
}

// Errors adapters return (wrapped) so the sync engine can react.
var (
	// ErrInvalidCredentials means the provider rejected the key (401/403).
	ErrInvalidCredentials = errors.New("provider rejected the credentials")
	// ErrUnofficialNotAllowed means the call needs the account's opt-in.
	ErrUnofficialNotAllowed = errors.New("unofficial endpoint not enabled for this account")
)

// RateLimitError means the provider is throttling; retry after RetryAfter.
type RateLimitError struct {
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("provider rate limit, retry after %s", e.RetryAfter)
}
