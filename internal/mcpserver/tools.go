// SPDX-License-Identifier: Apache-2.0

package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/atakanatamert/aicm/internal/apiclient"
)

// toolset holds the generated hub client every tool calls.
type toolset struct {
	hub *apiclient.ClientWithResponses
}

// registerTools adds the PLAN.md tool surface to s.
func registerTools(s *mcp.Server, t *toolset) {
	mcp.AddTool(s, &mcp.Tool{
		Name:         "list_accounts",
		Description:  "List accounts with provider, kind, plan, billed currency and last sync; numbers are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.listAccounts)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "get_quotas",
		Description:  "Return every meter in the provider's own terms with remaining, resets_at, headroom and expiring share; numbers are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.getQuotas)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "recommend_account",
		Description:  "Rank accounts for a job by model family, size and horizon, with reasons to read; numbers are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.recommendAccount)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "forecast_capacity",
		Description:  "Return each account's projected score hour by hour; numbers are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.forecastCapacity)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "list_renewals",
		Description:  "List subscriptions renewing within N days with price, manage and cancel links; amounts are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.listRenewals)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "get_spend",
		Description:  "Report money out per month or year by provider or account, in billed currency and optionally converted; amounts are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.getSpend)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "get_usage",
		Description:  "Return tokens, requests and estimated cost grouped by provider, account, model or day; numbers are in the provider's own units.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.getUsage)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "get_links",
		Description:  "Return an account's top-up, billing, manage, cancel, usage, keys and docs URLs.",
		Annotations:  &mcp.ToolAnnotations{ReadOnlyHint: true},
		OutputSchema: objectSchema(),
	}, t.getLinks)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "add_account",
		Description:  "Add a manual account with provider, plan, price, cycle and renewal date and return it; amounts are in the account currency and the tool never accepts a secret.",
		OutputSchema: objectSchema(),
	}, t.addAccount)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "update_account",
		Description:  "Update an account's name, plan, price, cycle, renewal date or notes and return it; amounts are in the account currency.",
		OutputSchema: objectSchema(),
	}, t.updateAccount)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "record_charge",
		Description:  "Record a top-up, subscription fee, invoice or refund and return it; the amount is a decimal string in the given currency's own units.",
		OutputSchema: objectSchema(),
	}, t.recordCharge)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "sync_account",
		Description:  "Queue a fetch now, respecting the provider's minimum poll interval, and report whether it was queued.",
		OutputSchema: objectSchema(),
	}, t.syncAccount)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "snooze_alert",
		Description:  "Silence one alert until an RFC 3339 date-time and return the alert.",
		OutputSchema: objectSchema(),
	}, t.snoozeAlert)
	mcp.AddTool(s, &mcp.Tool{
		Name:         "archive_account",
		Description:  "Hide an account, keep its history and return it; requires confirm=true and deletes nothing.",
		Annotations:  &mcp.ToolAnnotations{DestructiveHint: new(false)},
		OutputSchema: objectSchema(),
	}, t.archiveAccount)
}

// objectSchema keeps structured output an object, as MCP requires. The hub's
// generated types carry date wrappers whose inferred schema would be wrong.
func objectSchema() map[string]any {
	return map[string]any{"type": "object"}
}

// ---------------------------------------------------------------- inputs

type listAccountsInput struct {
	Status     *string `json:"status,omitempty" jsonschema:"active, pending or dismissed; defaults to active"`
	ProviderID *string `json:"provider_id,omitempty" jsonschema:"provider id, for example openrouter"`
}

type getQuotasInput struct {
	AccountID *string `json:"account_id,omitempty" jsonschema:"limit the result to one account id"`
}

type recommendAccountInput struct {
	ModelFamily  *string  `json:"model_family,omitempty" jsonschema:"model family the job needs, for example claude, gpt, gemini or deepseek"`
	Size         *string  `json:"size,omitempty" jsonschema:"small, medium or large"`
	HorizonHours *float64 `json:"horizon_hours,omitempty" jsonschema:"how long the job will run, in hours"`
	Limit        *int     `json:"limit,omitempty" jsonschema:"maximum number of ranked accounts"`
}

type forecastCapacityInput struct {
	AccountID *string `json:"account_id,omitempty" jsonschema:"limit the forecast to one account id"`
	Hours     *int    `json:"hours,omitempty" jsonschema:"hours to forecast; defaults to 24"`
}

type listRenewalsInput struct {
	WithinDays *int `json:"within_days,omitempty" jsonschema:"look this many days ahead; defaults to 30"`
}

type getSpendInput struct {
	Period   string  `json:"period" jsonschema:"month or year"`
	From     *string `json:"from,omitempty" jsonschema:"inclusive start day, YYYY-MM-DD"`
	To       *string `json:"to,omitempty" jsonschema:"inclusive end day, YYYY-MM-DD"`
	GroupBy  *string `json:"group_by,omitempty" jsonschema:"provider or account; defaults to provider"`
	Currency *string `json:"currency,omitempty" jsonschema:"conversion target; defaults to the org display currency"`
}

type getUsageInput struct {
	GroupBy    string  `json:"group_by" jsonschema:"day, model, account or provider"`
	From       *string `json:"from,omitempty" jsonschema:"inclusive start day, YYYY-MM-DD"`
	To         *string `json:"to,omitempty" jsonschema:"inclusive end day, YYYY-MM-DD"`
	AccountID  *string `json:"account_id,omitempty" jsonschema:"limit to one account id"`
	ProviderID *string `json:"provider_id,omitempty" jsonschema:"limit to one provider id"`
}

type getLinksInput struct {
	AccountID string `json:"account_id" jsonschema:"account id"`
}

type addAccountInput struct {
	ProviderID  string  `json:"provider_id" jsonschema:"provider id, for example openrouter"`
	Name        string  `json:"name" jsonschema:"display name"`
	PlanName    *string `json:"plan_name,omitempty" jsonschema:"plan name as the provider shows it"`
	PriceAmount *string `json:"price_amount,omitempty" jsonschema:"price as a decimal string in major units"`
	Currency    *string `json:"currency,omitempty" jsonschema:"ISO 4217 code; defaults to the provider currency"`
	Cycle       *string `json:"cycle,omitempty" jsonschema:"none, weekly, monthly or yearly; defaults to monthly when a plan is given"`
	RenewsOn    *string `json:"renews_on,omitempty" jsonschema:"next renewal day, YYYY-MM-DD"`
	Notes       *string `json:"notes,omitempty" jsonschema:"free-form notes"`
}

type updateAccountInput struct {
	AccountID   string  `json:"account_id" jsonschema:"account id"`
	Name        *string `json:"name,omitempty" jsonschema:"display name"`
	PlanName    *string `json:"plan_name,omitempty" jsonschema:"plan name as the provider shows it"`
	PriceAmount *string `json:"price_amount,omitempty" jsonschema:"price as a decimal string in major units"`
	Currency    *string `json:"currency,omitempty" jsonschema:"ISO 4217 code for the account and price"`
	Cycle       *string `json:"cycle,omitempty" jsonschema:"none, weekly, monthly or yearly"`
	RenewsOn    *string `json:"renews_on,omitempty" jsonschema:"next renewal day, YYYY-MM-DD"`
	Notes       *string `json:"notes,omitempty" jsonschema:"free-form notes"`
}

type recordChargeInput struct {
	AccountID   string  `json:"account_id" jsonschema:"account id"`
	Kind        string  `json:"kind" jsonschema:"subscription, topup, invoice or refund"`
	Amount      string  `json:"amount" jsonschema:"decimal string in major units"`
	Currency    string  `json:"currency" jsonschema:"ISO 4217 code"`
	OccurredOn  string  `json:"occurred_on" jsonschema:"day of the payment, YYYY-MM-DD"`
	Description *string `json:"description,omitempty" jsonschema:"free-form description"`
}

type syncAccountInput struct {
	AccountID string `json:"account_id" jsonschema:"account id"`
}

type snoozeAlertInput struct {
	AlertID string `json:"alert_id" jsonschema:"alert id"`
	Until   string `json:"until" jsonschema:"RFC 3339 date-time, for example 2026-10-20T09:00:00Z"`
}

type archiveAccountInput struct {
	AccountID string `json:"account_id" jsonschema:"account id"`
	Confirm   bool   `json:"confirm" jsonschema:"must be true; the archive does not delete anything"`
}

// ---------------------------------------------------------------- outputs

type accountsOutput struct {
	Accounts []apiclient.Account `json:"accounts"`
}

type quotasOutput struct {
	Quotas []apiclient.AccountQuota `json:"quotas"`
}

type forecastOutput struct {
	Forecast []apiclient.CapacityForecast `json:"forecast"`
}

type renewalsOutput struct {
	Renewals []apiclient.Renewal `json:"renewals"`
}

type usageOutput struct {
	Usage []apiclient.UsageRow `json:"usage"`
}

type linksOutput struct {
	AccountID string          `json:"account_id"`
	Links     apiclient.Links `json:"links"`
}

type syncOutput struct {
	Queued bool `json:"queued"`
}

// ---------------------------------------------------------------- handlers

func (t *toolset) listAccounts(ctx context.Context, _ *mcp.CallToolRequest, in listAccountsInput) (*mcp.CallToolResult, accountsOutput, error) {
	params := &apiclient.ListAccountsParams{ProviderId: in.ProviderID}
	if in.Status != nil {
		status := apiclient.AccountStatus(*in.Status)
		params.Status = &status
	}
	resp, err := t.hub.ListAccountsWithResponse(ctx, params)
	if err != nil {
		return nil, accountsOutput{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, accountsOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	out := accountsOutput{Accounts: *resp.JSON200}
	return text(fmt.Sprintf("%s.", itemCount(len(out.Accounts), "account", "accounts"))), out, nil
}

func (t *toolset) getQuotas(ctx context.Context, _ *mcp.CallToolRequest, in getQuotasInput) (*mcp.CallToolResult, quotasOutput, error) {
	resp, err := t.hub.ListQuotasWithResponse(ctx, &apiclient.ListQuotasParams{AccountId: in.AccountID})
	if err != nil {
		return nil, quotasOutput{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, quotasOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	out := quotasOutput{Quotas: *resp.JSON200}
	meters := 0
	for _, q := range out.Quotas {
		meters += len(q.Meters)
	}
	return text(fmt.Sprintf("%s, %s.", itemCount(len(out.Quotas), "account", "accounts"), itemCount(meters, "meter", "meters"))), out, nil
}

func (t *toolset) recommendAccount(ctx context.Context, _ *mcp.CallToolRequest, in recommendAccountInput) (*mcp.CallToolResult, apiclient.Recommendation, error) {
	params := &apiclient.RecommendAccountParams{
		ModelFamily:  in.ModelFamily,
		HorizonHours: in.HorizonHours,
		Limit:        in.Limit,
	}
	if in.Size != nil {
		size := apiclient.JobSize(*in.Size)
		params.Size = &size
	}
	resp, err := t.hub.RecommendAccountWithResponse(ctx, params)
	if err != nil {
		return nil, apiclient.Recommendation{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, apiclient.Recommendation{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	rec := *resp.JSON200
	summary := fmt.Sprintf("%s ranked.", itemCount(len(rec.Ranked), "account", "accounts"))
	if len(rec.Ranked) > 0 {
		best := rec.Ranked[0]
		if len(best.Reasons) > 0 {
			summary = fmt.Sprintf("%s ranked; best: %s (%s).", itemCount(len(rec.Ranked), "account", "accounts"), best.Account.Name, best.Reasons[0])
		} else {
			summary = fmt.Sprintf("%s ranked; best: %s.", itemCount(len(rec.Ranked), "account", "accounts"), best.Account.Name)
		}
	}
	return text(summary), rec, nil
}

func (t *toolset) forecastCapacity(ctx context.Context, _ *mcp.CallToolRequest, in forecastCapacityInput) (*mcp.CallToolResult, forecastOutput, error) {
	hours := 24
	if in.Hours != nil {
		hours = *in.Hours
	}
	resp, err := t.hub.ForecastCapacityWithResponse(ctx, &apiclient.ForecastCapacityParams{
		AccountId: in.AccountID,
		Hours:     &hours,
	})
	if err != nil {
		return nil, forecastOutput{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, forecastOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	out := forecastOutput{Forecast: *resp.JSON200}
	return text(fmt.Sprintf("%s forecast for %d hours.", itemCount(len(out.Forecast), "account", "accounts"), hours)), out, nil
}

func (t *toolset) listRenewals(ctx context.Context, _ *mcp.CallToolRequest, in listRenewalsInput) (*mcp.CallToolResult, renewalsOutput, error) {
	days := 30
	if in.WithinDays != nil {
		days = *in.WithinDays
	}
	resp, err := t.hub.ListRenewalsWithResponse(ctx, &apiclient.ListRenewalsParams{WithinDays: &days})
	if err != nil {
		return nil, renewalsOutput{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, renewalsOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	out := renewalsOutput{Renewals: *resp.JSON200}
	summary := fmt.Sprintf("No renewals within %d days.", days)
	if len(out.Renewals) > 0 {
		next := out.Renewals[0]
		summary = fmt.Sprintf("%s within %d days; next: %s on %s.",
			itemCount(len(out.Renewals), "renewal", "renewals"), days, next.Account.Name, next.RenewsOn.Format("2006-01-02"))
	}
	return text(summary), out, nil
}

func (t *toolset) getSpend(ctx context.Context, _ *mcp.CallToolRequest, in getSpendInput) (*mcp.CallToolResult, apiclient.SpendReport, error) {
	from, err := optionalDay(in.From)
	if err != nil {
		return nil, apiclient.SpendReport{}, err
	}
	to, err := optionalDay(in.To)
	if err != nil {
		return nil, apiclient.SpendReport{}, err
	}
	params := &apiclient.GetSpendParams{
		Period: apiclient.GetSpendParamsPeriod(in.Period),
		From:   from,
		To:     to,
	}
	if in.GroupBy != nil {
		group := apiclient.GetSpendParamsGroupBy(*in.GroupBy)
		params.GroupBy = &group
	}
	if in.Currency != nil {
		currency := *in.Currency
		params.Currency = &currency
	}
	resp, err := t.hub.GetSpendWithResponse(ctx, params)
	if err != nil {
		return nil, apiclient.SpendReport{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, apiclient.SpendReport{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	report := *resp.JSON200
	return text(fmt.Sprintf("%s across %s in %s.", itemCount(len(report.Rows), "row", "rows"), itemCount(len(report.Totals), "period", "periods"), report.DisplayCurrency)), report, nil
}

func (t *toolset) getUsage(ctx context.Context, _ *mcp.CallToolRequest, in getUsageInput) (*mcp.CallToolResult, usageOutput, error) {
	from, err := optionalDay(in.From)
	if err != nil {
		return nil, usageOutput{}, err
	}
	to, err := optionalDay(in.To)
	if err != nil {
		return nil, usageOutput{}, err
	}
	resp, err := t.hub.GetUsageWithResponse(ctx, &apiclient.GetUsageParams{
		GroupBy:    apiclient.GetUsageParamsGroupBy(in.GroupBy),
		From:       from,
		To:         to,
		AccountId:  in.AccountID,
		ProviderId: in.ProviderID,
	})
	if err != nil {
		return nil, usageOutput{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, usageOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	out := usageOutput{Usage: *resp.JSON200}
	var requests, tokens int64
	for _, row := range out.Usage {
		requests += row.Requests
		tokens += row.InputTokens + row.OutputTokens
	}
	return text(fmt.Sprintf("%s; %d requests and %d tokens.", itemCount(len(out.Usage), "row", "rows"), requests, tokens)), out, nil
}

func (t *toolset) getLinks(ctx context.Context, _ *mcp.CallToolRequest, in getLinksInput) (*mcp.CallToolResult, linksOutput, error) {
	resp, err := t.hub.GetAccountWithResponse(ctx, in.AccountID)
	if err != nil {
		return nil, linksOutput{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, linksOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	account := *resp.JSON200
	out := linksOutput{AccountID: account.Id, Links: account.Links}
	return text(fmt.Sprintf("%s for %s.", itemCount(countLinks(account.Links), "link", "links"), account.Name)), out, nil
}

func (t *toolset) addAccount(ctx context.Context, _ *mcp.CallToolRequest, in addAccountInput) (*mcp.CallToolResult, apiclient.Account, error) {
	currency := ""
	if in.Currency != nil {
		currency = *in.Currency
	}
	if in.PriceAmount != nil && currency == "" {
		var err error
		currency, err = t.providerCurrency(ctx, in.ProviderID)
		if err != nil {
			return nil, apiclient.Account{}, err
		}
	}
	plan, err := newPlan(in.PlanName, in.PriceAmount, currency, in.Cycle, in.RenewsOn)
	if err != nil {
		return nil, apiclient.Account{}, err
	}
	body := apiclient.AccountCreate{
		ProviderId: in.ProviderID,
		Name:       in.Name,
		Notes:      in.Notes,
		Plan:       plan,
	}
	if in.Currency != nil {
		c := *in.Currency
		body.Currency = &c
	}
	resp, err := t.hub.CreateAccountWithResponse(ctx, body)
	if err != nil {
		return nil, apiclient.Account{}, hubCallErr(err)
	}
	if resp.JSON201 == nil {
		return nil, apiclient.Account{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	account := *resp.JSON201
	return text(fmt.Sprintf("Added account %s (%s).", account.Name, account.Id)), account, nil
}

func (t *toolset) updateAccount(ctx context.Context, _ *mcp.CallToolRequest, in updateAccountInput) (*mcp.CallToolResult, apiclient.Account, error) {
	current, err := t.getAccount(ctx, in.AccountID)
	if err != nil {
		return nil, apiclient.Account{}, err
	}
	currency := current.Currency
	if in.Currency != nil {
		currency = *in.Currency
	}
	body := apiclient.AccountUpdate{Name: in.Name, Notes: in.Notes}
	if in.Currency != nil {
		c := *in.Currency
		body.Currency = &c
	}
	plan, err := patchPlan(in, current.Plan, currency)
	if err != nil {
		return nil, apiclient.Account{}, err
	}
	body.Plan = plan
	resp, err := t.hub.UpdateAccountWithResponse(ctx, in.AccountID, body)
	if err != nil {
		return nil, apiclient.Account{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, apiclient.Account{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	account := *resp.JSON200
	return text(fmt.Sprintf("Updated account %s.", account.Name)), account, nil
}

func (t *toolset) recordCharge(ctx context.Context, _ *mcp.CallToolRequest, in recordChargeInput) (*mcp.CallToolResult, apiclient.Charge, error) {
	minor, err := parseMinor(in.Amount, in.Currency)
	if err != nil {
		return nil, apiclient.Charge{}, err
	}
	occurred, err := parseDay(in.OccurredOn)
	if err != nil {
		return nil, apiclient.Charge{}, err
	}
	resp, err := t.hub.CreateChargeWithResponse(ctx, apiclient.ChargeCreate{
		AccountId:   in.AccountID,
		Kind:        apiclient.ChargeKind(in.Kind),
		Amount:      apiclient.Money{AmountMinor: minor, Currency: in.Currency},
		OccurredOn:  occurred,
		Description: in.Description,
	})
	if err != nil {
		return nil, apiclient.Charge{}, hubCallErr(err)
	}
	if resp.JSON201 == nil {
		return nil, apiclient.Charge{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	charge := *resp.JSON201
	return text(fmt.Sprintf("Recorded %s %s %s on %s.",
		charge.Kind, formatMinor(charge.Amount.AmountMinor, charge.Amount.Currency),
		charge.Amount.Currency, charge.OccurredOn.Format("2006-01-02"))), charge, nil
}

func (t *toolset) syncAccount(ctx context.Context, _ *mcp.CallToolRequest, in syncAccountInput) (*mcp.CallToolResult, syncOutput, error) {
	resp, err := t.hub.SyncAccountWithResponse(ctx, in.AccountID)
	if err != nil {
		return nil, syncOutput{}, hubCallErr(err)
	}
	if resp.JSON202 == nil {
		return nil, syncOutput{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	out := syncOutput{Queued: resp.JSON202.Queued}
	if out.Queued {
		return text("Sync queued."), out, nil
	}
	return text("Sync was not queued."), out, nil
}

func (t *toolset) snoozeAlert(ctx context.Context, _ *mcp.CallToolRequest, in snoozeAlertInput) (*mcp.CallToolResult, apiclient.Alert, error) {
	until, err := time.Parse(time.RFC3339, in.Until)
	if err != nil {
		return nil, apiclient.Alert{}, fmt.Errorf("parsing until %q: want an RFC 3339 date-time: %w", in.Until, err)
	}
	resp, err := t.hub.UpdateAlertWithResponse(ctx, in.AlertID, apiclient.UpdateAlertJSONRequestBody{SnoozedUntil: &until})
	if err != nil {
		return nil, apiclient.Alert{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, apiclient.Alert{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	alert := *resp.JSON200
	return text(fmt.Sprintf("Snoozed alert %s until %s.", alert.Id, until.UTC().Format(time.RFC3339))), alert, nil
}

func (t *toolset) archiveAccount(ctx context.Context, _ *mcp.CallToolRequest, in archiveAccountInput) (*mcp.CallToolResult, apiclient.Account, error) {
	if !in.Confirm {
		return nil, apiclient.Account{}, errors.New("archive_account requires confirm=true")
	}
	resp, err := t.hub.ArchiveAccountWithResponse(ctx, in.AccountID, apiclient.ArchiveAccountJSONRequestBody{Confirm: true})
	if err != nil {
		return nil, apiclient.Account{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return nil, apiclient.Account{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	account := *resp.JSON200
	return text(fmt.Sprintf("Archived account %s; its history is kept.", account.Name)), account, nil
}

// ---------------------------------------------------------------- helpers

func (t *toolset) getAccount(ctx context.Context, id string) (apiclient.Account, error) {
	resp, err := t.hub.GetAccountWithResponse(ctx, id)
	if err != nil {
		return apiclient.Account{}, hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return apiclient.Account{}, hubFailure(resp.HTTPResponse, resp.Body)
	}
	return *resp.JSON200, nil
}

func (t *toolset) providerCurrency(ctx context.Context, id string) (string, error) {
	resp, err := t.hub.ListProvidersWithResponse(ctx)
	if err != nil {
		return "", hubCallErr(err)
	}
	if resp.JSON200 == nil {
		return "", hubFailure(resp.HTTPResponse, resp.Body)
	}
	for _, p := range *resp.JSON200 {
		if p.Id == id {
			return p.Currency, nil
		}
	}
	return "", fmt.Errorf("unknown provider %q", id)
}

// newPlan builds the plan for add_account. A plan is sent only when the
// caller supplied at least one plan field.
func newPlan(name, price *string, currency string, cycle *string, renewsOn *string) (*apiclient.PlanWrite, error) {
	if name == nil && price == nil && cycle == nil && renewsOn == nil {
		return nil, nil
	}
	p := &apiclient.PlanWrite{Name: name}
	switch {
	case cycle != nil:
		p.Cycle = apiclient.BillingCycle(*cycle)
	case price != nil:
		p.Cycle = apiclient.BillingCycleMonthly
	default:
		p.Cycle = apiclient.BillingCycleNone
	}
	if p.Cycle == apiclient.BillingCycleNone {
		if price != nil {
			return nil, errors.New("price_amount requires a billing cycle other than none")
		}
	} else if price != nil {
		minor, err := parseMinor(*price, currency)
		if err != nil {
			return nil, err
		}
		p.Price = &apiclient.Money{AmountMinor: minor, Currency: currency}
	}
	if renewsOn != nil {
		day, err := parseDay(*renewsOn)
		if err != nil {
			return nil, err
		}
		p.RenewsOn = &day
	}
	return p, nil
}

// patchPlan fills the update plan from the current one so omitted fields keep
// their value. PATCH replaces the plan object, not its individual fields.
func patchPlan(in updateAccountInput, current apiclient.Plan, currency string) (*apiclient.PlanWrite, error) {
	if in.PlanName == nil && in.PriceAmount == nil && in.Cycle == nil && in.RenewsOn == nil {
		return nil, nil
	}
	name := current.Name
	if in.PlanName != nil {
		name = *in.PlanName
	}
	cycle := current.Cycle
	if cycle == "" {
		cycle = apiclient.BillingCycleNone
	}
	if in.Cycle != nil {
		cycle = apiclient.BillingCycle(*in.Cycle)
	}
	price := current.Price
	if in.PriceAmount != nil {
		minor, err := parseMinor(*in.PriceAmount, currency)
		if err != nil {
			return nil, err
		}
		price = &apiclient.Money{AmountMinor: minor, Currency: currency}
	}
	renewsOn := current.RenewsOn
	if in.RenewsOn != nil {
		day, err := parseDay(*in.RenewsOn)
		if err != nil {
			return nil, err
		}
		renewsOn = &day
	}
	p := &apiclient.PlanWrite{Cycle: cycle, Name: &name, Price: price, RenewsOn: renewsOn}
	if cycle == apiclient.BillingCycleNone {
		p.Price = nil
		p.RenewsOn = nil
	}
	return p, nil
}

// hubFailure turns the hub's error envelope into a tool error with its code
// and message. It never includes a stack trace.
func hubFailure(resp *http.Response, body []byte) error {
	var e apiclient.Error
	if err := json.Unmarshal(body, &e); err == nil && e.Error.Code != "" {
		return fmt.Errorf("%s: %s", e.Error.Code, e.Error.Message)
	}
	if resp != nil {
		return fmt.Errorf("hub returned %s", resp.Status)
	}
	return errors.New("hub returned an error")
}

func hubCallErr(err error) error {
	return fmt.Errorf("calling hub: %w", err)
}

func text(summary string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: summary}}}
}

func itemCount(n int, singular, plural string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, singular)
	}
	return fmt.Sprintf("%d %s", n, plural)
}

func optionalDay(s *string) (*apiclient.FromDate, error) {
	if s == nil {
		return nil, nil
	}
	day, err := parseDay(*s)
	if err != nil {
		return nil, err
	}
	return &day, nil
}

func parseDay(s string) (apiclient.FromDate, error) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return apiclient.FromDate{}, fmt.Errorf("parsing date %q: want YYYY-MM-DD: %w", s, err)
	}
	return apiclient.FromDate{Time: t}, nil
}

func countLinks(l apiclient.Links) int {
	n := 0
	for _, v := range []*string{l.TopUp, l.Billing, l.ManageSubscription, l.Cancel, l.Usage, l.Keys, l.Docs} {
		if v != nil && *v != "" {
			n++
		}
	}
	return n
}

// ---------------------------------------------------------------- money

var (
	zeroDecimal = map[string]struct{}{
		"BIF": {}, "CLP": {}, "DJF": {}, "GNF": {}, "ISK": {}, "JPY": {},
		"KMF": {}, "KRW": {}, "PYG": {}, "RWF": {}, "UGX": {}, "UYI": {},
		"VND": {}, "VUV": {}, "XAF": {}, "XOF": {}, "XPF": {},
	}
	threeDecimal = map[string]struct{}{
		"BHD": {}, "IQD": {}, "JOD": {}, "KWD": {}, "LYD": {}, "OMR": {}, "TND": {},
	}
)

// currencyExponent is internal/money.Exponent, copied because that package is
// AGPL and mcpserver is Apache-2.0.
func currencyExponent(currency string) (int, error) {
	if len(currency) != 3 {
		return 0, fmt.Errorf("invalid currency code %q", currency)
	}
	for i := range 3 {
		if currency[i] < 'A' || currency[i] > 'Z' {
			return 0, fmt.Errorf("invalid currency code %q", currency)
		}
	}
	if _, ok := zeroDecimal[currency]; ok {
		return 0, nil
	}
	if _, ok := threeDecimal[currency]; ok {
		return 3, nil
	}
	return 2, nil
}

// parseMinor parses a decimal string in major units into minor units without
// strconv, rounding half away from zero like internal/money.ParseDecimal.
func parseMinor(amount, currency string) (int64, error) {
	exp, err := currencyExponent(currency)
	if err != nil {
		return 0, err
	}
	s := strings.TrimSpace(amount)
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg, s = true, s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	intPart, frac, _ := strings.Cut(s, ".")
	if intPart == "" && frac == "" {
		return 0, fmt.Errorf("parsing %s amount %q: not a plain decimal number", currency, amount)
	}
	if !digits(intPart) || !digits(frac) {
		return 0, fmt.Errorf("parsing %s amount %q: not a plain decimal number", currency, amount)
	}

	const maxInt64 = int64(^uint64(0) >> 1)
	var v int64
	add := func(c byte) error {
		d := int64(c - '0')
		if v > (maxInt64-d)/10 {
			return fmt.Errorf("parsing %s amount %q: out of range", currency, amount)
		}
		v = v*10 + d
		return nil
	}
	for i := range len(intPart) {
		if err := add(intPart[i]); err != nil {
			return 0, err
		}
	}
	roundUp := false
	for i := range exp {
		c := byte('0')
		if i < len(frac) {
			c = frac[i]
		}
		if err := add(c); err != nil {
			return 0, err
		}
	}
	if len(frac) > exp {
		roundUp = frac[exp] >= '5'
	}
	if roundUp {
		if v == maxInt64 {
			return 0, fmt.Errorf("parsing %s amount %q: out of range", currency, amount)
		}
		v++
	}
	if neg {
		v = -v
	}
	return v, nil
}

func digits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// formatMinor renders minor units for summaries.
func formatMinor(v int64, currency string) string {
	exp, err := currencyExponent(currency)
	if err != nil {
		exp = 2
	}
	neg := v < 0
	if neg {
		v = -v
	}
	s := fmt.Sprint(v)
	if exp > 0 {
		for len(s) <= exp {
			s = "0" + s
		}
		s = s[:len(s)-exp] + "." + s[len(s)-exp:]
	}
	if neg {
		return "-" + s
	}
	return s
}
