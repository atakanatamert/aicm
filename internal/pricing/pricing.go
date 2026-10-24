// SPDX-License-Identifier: SUL-1.0

// Package pricing looks up model token prices and estimates request cost in
// micro units. Prices come from the LiteLLM price file (see litellm.go) and
// are stored per million tokens.
package pricing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/atakanatamert/aicm/internal/db"
	"github.com/atakanatamert/aicm/internal/provider"
)

// Price is per million tokens in micro units of Currency ($3.00/Mtok = 3_000_000).
type Price struct {
	Model, Provider, Currency            string
	Input, Output, CacheRead, CacheWrite int64
}

// Service looks up prices and imports the LiteLLM price file.
type Service struct {
	pool *pgxpool.Pool
	q    *db.Queries
	c    *provider.HTTPClient
	log  *slog.Logger
}

// NewService returns a pricing service.
func NewService(pool *pgxpool.Pool, c *provider.HTTPClient, log *slog.Logger) *Service {
	return &Service{pool: pool, q: db.New(pool), c: c, log: log}
}

// Lookup finds a price by exact model name; if none, by the model name with
// a "provider/" prefix stripped (e.g. "anthropic/claude-sonnet-4-5"), then by
// the name with a trailing date suffix "-YYYYMMDD" stripped. ok is false when
// nothing matches.
func (s *Service) Lookup(ctx context.Context, model string) (Price, bool, error) {
	for _, candidate := range priceCandidates(model) {
		row, err := s.q.GetModelPrice(ctx, candidate)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return Price{}, false, fmt.Errorf("looking up price for %q: %w", candidate, err)
		}
		return Price{
			Model:      row.Model,
			Provider:   row.Provider,
			Currency:   row.Currency,
			Input:      row.InputMicroPerMtok,
			Output:     row.OutputMicroPerMtok,
			CacheRead:  row.CacheReadMicroPerMtok,
			CacheWrite: row.CacheWriteMicroPerMtok,
		}, true, nil
	}
	return Price{}, false, nil
}

// priceCandidates returns the lookup names in contract order, without
// duplicates.
func priceCandidates(model string) []string {
	base := []string{model}
	if stripped := stripProviderPrefix(model); stripped != model {
		base = append(base, stripped)
	}
	seen := make(map[string]struct{}, len(base)*2)
	out := make([]string, 0, len(base)*2)
	add := func(m string) {
		if m == "" {
			return
		}
		if _, ok := seen[m]; ok {
			return
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	for _, m := range base {
		add(m)
		add(stripDateSuffix(m))
	}
	return out
}

// stripProviderPrefix removes one leading "provider/" segment.
func stripProviderPrefix(model string) string {
	if i := strings.IndexByte(model, '/'); i > 0 && i+1 < len(model) {
		return model[i+1:]
	}
	return model
}

// stripDateSuffix removes a trailing "-YYYYMMDD".
func stripDateSuffix(model string) string {
	if len(model) < 9 {
		return model
	}
	tail := model[len(model)-9:]
	if tail[0] != '-' {
		return model
	}
	for _, c := range tail[1:] {
		if c < '0' || c > '9' {
			return model
		}
	}
	return model[:len(model)-9]
}

// Estimate returns cost_micro for token counts: Σ tokens × price / 1_000_000,
// rounded half away from zero. It stays in int64 and falls back to math/big
// only when a product could overflow.
func Estimate(p Price, input, output, cacheRead, cacheWrite int64) int64 {
	terms := [4]tokenTerm{
		{input, p.Input},
		{output, p.Output},
		{cacheRead, p.CacheRead},
		{cacheWrite, p.CacheWrite},
	}

	var sum int64
	for _, t := range terms {
		if t.tokens == 0 || t.price == 0 {
			continue
		}
		product, ok := multiply(t.tokens, t.price)
		if !ok {
			return estimateBig(terms)
		}
		next, ok := add(sum, product)
		if !ok {
			return estimateBig(terms)
		}
		sum = next
	}
	return divRound(sum)
}

// multiply returns a*b and whether the product fits in int64.
func multiply(a, b int64) (int64, bool) {
	if a == 0 || b == 0 {
		return 0, true
	}
	if a == math.MinInt64 || b == math.MinInt64 {
		return 0, false
	}
	product := a * b
	if product/b != a {
		return 0, false
	}
	return product, true
}

// add returns a+b and whether the sum fits in int64.
func add(a, b int64) (int64, bool) {
	sum := a + b
	if (b > 0 && sum < a) || (b < 0 && sum > a) {
		return 0, false
	}
	return sum, true
}

// divRound divides by 1_000_000, rounding half away from zero.
func divRound(v int64) int64 {
	q, r := v/1_000_000, v%1_000_000
	switch {
	case r >= 500_000:
		return q + 1
	case r <= -500_000:
		return q - 1
	default:
		return q
	}
}

// tokenTerm is one tokens × price term of Estimate.
type tokenTerm struct {
	tokens int64
	price  int64
}

// estimateBig is the overflow-safe path for Estimate.
func estimateBig(terms [4]tokenTerm) int64 {
	var total big.Int
	for _, t := range terms {
		if t.tokens == 0 || t.price == 0 {
			continue
		}
		var product big.Int
		product.Mul(big.NewInt(t.tokens), big.NewInt(t.price))
		total.Add(&total, &product)
	}

	var quotient, remainder big.Int
	quotient.QuoRem(&total, big.NewInt(1_000_000), &remainder)
	twice := new(big.Int).Abs(&remainder)
	twice.Lsh(twice, 1)
	if twice.Cmp(big.NewInt(1_000_000)) >= 0 {
		if total.Sign() < 0 {
			quotient.Sub(&quotient, big.NewInt(1))
		} else {
			quotient.Add(&quotient, big.NewInt(1))
		}
	}
	return quotient.Int64()
}
