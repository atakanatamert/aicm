// SPDX-License-Identifier: AGPL-3.0-or-later

// Package registry lists every provider compiled into the hub. Adding a
// provider is one line in All.
package registry

import (
	"github.com/atakanatamert/aicm/internal/provider"
	"github.com/atakanatamert/aicm/internal/provider/deepseek"
	"github.com/atakanatamert/aicm/internal/provider/manual"
	"github.com/atakanatamert/aicm/internal/provider/openrouter"
)

// All returns every provider, in catalog display order.
func All(c *provider.HTTPClient) []provider.Provider {
	ps := []provider.Provider{
		openrouter.New(c),
		deepseek.New(c),
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
