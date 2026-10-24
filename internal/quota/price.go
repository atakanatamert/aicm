// SPDX-License-Identifier: SUL-1.0

package quota

import (
	"context"

	"github.com/atakanatamert/aicm/internal/pricing"
	"github.com/atakanatamert/aicm/internal/provider"
)

// FamilyModels is the representative model priced for each model family
// when ranking pay-per-use accounts. Names match the LiteLLM price file.
var FamilyModels = map[string]string{
	"claude":   "claude-sonnet-4-5",
	"gpt":      "gpt-5",
	"gemini":   "gemini-2.5-pro",
	"grok":     "grok-4",
	"deepseek": "deepseek-chat",
	"kimi":     "moonshot/kimi-k2-0905-preview",
	"glm":      "zai/glm-4.6",
}

// PriceFromTable prices an account's marginal cost from the model price
// table: a 3:1 input:output blend per million tokens of the family's
// representative model, in micro USD.
func PriceFromTable(prices *pricing.Service) PriceFunc {
	return func(ctx context.Context, _ string, family string) (int64, bool) {
		model, ok := FamilyModels[family]
		if !ok {
			return 0, false
		}
		p, ok, err := prices.Lookup(ctx, model)
		if err != nil || !ok {
			return 0, false
		}
		return (3*p.Input + p.Output) / 4, true
	}
}

// priceFamily is the family to price an account at: the one asked for, else
// the provider's own first family. Routers ("*") with no family asked stay
// unpriced.
func priceFamily(asked string, info provider.Info) string {
	if asked != "" || len(info.ModelFamilies) == 0 || info.ModelFamilies[0] == "*" {
		return asked
	}
	return info.ModelFamilies[0]
}
