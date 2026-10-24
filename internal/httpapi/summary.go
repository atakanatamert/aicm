// SPDX-License-Identifier: SUL-1.0

package httpapi

import (
	"context"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/atakanatamert/aicm/internal/auth"
	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/provider"
)

// requirePrincipal returns the caller. Handlers behind the auth middleware
// always have one; the check guards against wiring mistakes.
func requirePrincipal(ctx context.Context) (auth.Principal, error) {
	p, ok := auth.FromContext(ctx)
	if !ok {
		return p, ErrUnauthorized
	}
	return p, nil
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// toLinks maps catalog links to the API type.
func toLinks(l provider.Links) Links {
	return Links{
		TopUp:              optString(l.TopUp),
		Billing:            optString(l.Billing),
		ManageSubscription: optString(l.ManageSubscription),
		Cancel:             optString(l.Cancel),
		Usage:              optString(l.Usage),
		Keys:               optString(l.Keys),
		Docs:               optString(l.Docs),
	}
}

func toDate(t *time.Time) *openapi_types.Date {
	if t == nil {
		return nil
	}
	return &openapi_types.Date{Time: *t}
}

// toPlan maps an account's subscription columns to the API type.
func toPlan(a db.Account) Plan {
	p := Plan{
		Name:        a.PlanName,
		Cycle:       BillingCycle(a.BillingCycle),
		AutoRenew:   a.AutoRenew,
		RenewsOn:    toDate(a.RenewsOn),
		PromoEndsOn: toDate(a.PromoEndsOn),
	}
	if a.PriceMinor != nil {
		p.Price = &Money{AmountMinor: *a.PriceMinor, Currency: a.Currency}
	}
	if a.PromoKind != nil {
		p.PromoKind = new(PromoKind(*a.PromoKind))
	}
	return p
}

// toAccountSummary is the short account shape used by quotas, renewals and
// recommendations.
func toAccountSummary(a db.Account, info provider.Info) AccountSummary {
	name := info.Name
	if name == "" {
		name = a.ProviderID
	}
	return AccountSummary{
		Id:           a.ID,
		ProviderId:   a.ProviderID,
		ProviderName: name,
		Name:         a.Name,
		Plan:         toPlan(a),
		Links:        toLinks(info.Links),
		LastSyncedAt: a.LastSyncedAt,
	}
}
