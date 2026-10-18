// SPDX-License-Identifier: AGPL-3.0-or-later

// Package registry lists every provider compiled into the hub. Adding a
// provider is one line in All.
package registry

import (
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/anthropic"
	"github.com/atakanatamert/aicm/internal/provider/deepseek"
	"github.com/atakanatamert/aicm/internal/provider/manual"
	"github.com/atakanatamert/aicm/internal/provider/moonshot"
	"github.com/atakanatamert/aicm/internal/provider/openai"
	"github.com/atakanatamert/aicm/internal/provider/openrouter"
	"github.com/atakanatamert/aicm/internal/provider/xai"
	"github.com/atakanatamert/aicm/internal/provider/zai"
)

// All returns every provider, in catalog display order: API adapters first,
// then catalog-only entries.
func All(c *provider.HTTPClient) []provider.Provider {
	ps := []provider.Provider{
		openrouter.New(c),
		anthropic.New(c),
		openai.New(c),
		xai.New(c),
		deepseek.New(c),
		moonshot.New(c),
		zai.New(c),
	}
	return append(ps, manual.All()...)
}

// ByID finds a provider by its Info().ID.
func ByID(ps []provider.Provider, id string) (provider.Provider, bool) {
	for _, p := range ps {
		if p.Info().ID == id {
			return p, true
		}
	}
	return nil, false
}
