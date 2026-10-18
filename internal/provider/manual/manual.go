// SPDX-License-Identifier: AGPL-3.0-or-later

// Package manual declares catalog-only providers. They have no API the hub
// calls: the user or the agent fills in their meters, and the links take the
// user to the provider's own pages.
package manual

import (
	"context"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

// entry is a catalog-only provider: it has no API the hub calls.
type entry struct{ info provider.Info }

func (e entry) Info() provider.Info { return e.info }

// Validate accepts anything: there is no credential to check.
func (e entry) Validate(context.Context, provider.Credentials) error { return nil }

// All returns every manual and agent-captured provider, in display order.
func All() []provider.Provider {
	infos := []provider.Info{
		{
			ID:       "claude",
			Name:     "Claude (Pro / Max)",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthAgentCapture,
				provider.AuthNone,
			},
			Links: provider.Links{
				Billing:            "https://claude.ai/settings/billing",
				ManageSubscription: "https://claude.ai/settings/billing",
				Cancel:             "https://claude.ai/settings/billing",
				Usage:              "https://claude.ai/settings/usage",
			},
			Meters: []meter.Def{
				{Key: "five_hour", Label: "5-hour limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 300},
				{Key: "seven_day", Label: "Weekly limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080},
				{Key: "seven_day_opus", Label: "Weekly limit (Opus)", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080},
				{Key: "seven_day_sonnet", Label: "Weekly limit (Sonnet)", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080},
			},
			ModelFamilies: []string{"claude"},
		},
		{
			ID:       "chatgpt",
			Name:     "ChatGPT (Plus / Pro, Codex)",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthAgentCapture,
				provider.AuthNone,
			},
			Links: provider.Links{
				Billing:            "https://chatgpt.com/#settings/Account",
				ManageSubscription: "https://chatgpt.com/#settings/Account",
				Cancel:             "https://chatgpt.com/#settings/Account",
				Usage:              "https://chatgpt.com/codex/settings/usage",
			},
			Meters: []meter.Def{
				{Key: "primary", Label: "5-hour limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 300},
				{Key: "secondary", Label: "Weekly limit", Unit: meter.UnitPercent, ResetKind: meter.ResetFixed, WindowMinutes: 10080},
				{Key: "credits", Label: "Codex credits", Unit: meter.UnitCredits, ResetKind: meter.ResetNever},
			},
			ModelFamilies: []string{"gpt"},
		},
		{
			ID:       "google-ai-studio",
			Name:     "Google AI Studio (Gemini API)",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
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
		},
		{
			ID:       "google-one",
			Name:     "Google AI Pro / Ultra",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				Billing:            "https://one.google.com/settings",
				ManageSubscription: "https://one.google.com/settings",
				Cancel:             "https://one.google.com/settings",
			},
			ModelFamilies: []string{"gemini"},
		},
		{
			ID:       "supergrok",
			Name:     "SuperGrok",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				Billing:            "https://grok.com/plans",
				ManageSubscription: "https://grok.com/plans",
				Cancel:             "https://grok.com/plans",
			},
			ModelFamilies: []string{"grok"},
		},
		{
			ID:       "cursor",
			Name:     "Cursor",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				Billing:            "https://cursor.com/dashboard",
				ManageSubscription: "https://cursor.com/dashboard",
				Cancel:             "https://cursor.com/dashboard",
				Usage:              "https://cursor.com/dashboard",
			},
			ModelFamilies: []string{"claude", "gpt", "gemini"},
		},
		{
			ID:       "mistral",
			Name:     "Mistral",
			Currency: "EUR",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				TopUp:   "https://admin.mistral.ai/organization/billing",
				Billing: "https://admin.mistral.ai/organization/billing",
				Usage:   "https://console.mistral.ai/usage",
			},
			Meters: []meter.Def{
				{Key: "balance", Label: "Balance", Unit: meter.UnitCurrency, UnitCurrency: "EUR", ResetKind: meter.ResetNever},
			},
			ModelFamilies: []string{"mistral"},
		},
		{
			ID:       "together",
			Name:     "Together AI",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				TopUp:   "https://api.together.ai/settings/organization/~current/billing",
				Billing: "https://api.together.ai/settings/organization/~current/billing",
			},
			Meters: []meter.Def{
				{Key: "balance", Label: "Balance", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			},
			ModelFamilies: []string{"*"},
		},
		{
			ID:       "perplexity",
			Name:     "Perplexity (API and Pro)",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				TopUp:              "https://console.perplexity.ai/project/billing",
				Billing:            "https://console.perplexity.ai/project/billing",
				ManageSubscription: "https://www.perplexity.ai/settings/account",
				Cancel:             "https://www.perplexity.ai/settings/account",
				Usage:              "https://console.perplexity.ai/project/billing",
			},
			Meters: []meter.Def{
				{Key: "balance", Label: "API balance", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			},
			ModelFamilies: []string{"perplexity"},
		},
		{
			ID:       "cohere",
			Name:     "Cohere",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				TopUp:   "https://dashboard.cohere.com/billing",
				Billing: "https://dashboard.cohere.com/billing",
				Usage:   "https://dashboard.cohere.com/billing",
			},
			ModelFamilies: []string{"cohere"},
		},
		{
			ID:       "groq",
			Name:     "Groq",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				TopUp:   "https://console.groq.com/settings/billing",
				Billing: "https://console.groq.com/settings/billing",
				Usage:   "https://console.groq.com/settings/billing",
			},
			Meters: []meter.Def{
				{Key: "balance", Label: "Balance", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			},
			ModelFamilies: []string{"*"},
		},
		{
			ID:       "fireworks",
			Name:     "Fireworks AI",
			Currency: "USD",
			AuthKinds: []provider.AuthKind{
				provider.AuthNone,
			},
			Links: provider.Links{
				TopUp:   "https://app.fireworks.ai/account/billing",
				Billing: "https://app.fireworks.ai/account/billing",
				Usage:   "https://app.fireworks.ai/account/billing",
			},
			Meters: []meter.Def{
				{Key: "balance", Label: "Balance", Unit: meter.UnitCurrency, UnitCurrency: "USD", ResetKind: meter.ResetNever},
			},
			ModelFamilies: []string{"*"},
		},
	}

	ps := make([]provider.Provider, len(infos))
	for i, info := range infos {
		ps[i] = entry{info: info}
	}
	return ps
}
