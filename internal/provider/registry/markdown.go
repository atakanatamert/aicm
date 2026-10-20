// SPDX-License-Identifier: AGPL-3.0-or-later

package registry

import (
	"fmt"
	"strings"

	"github.com/atakanatamert/aicm/internal/meter"
	"github.com/atakanatamert/aicm/internal/provider"
)

// dash marks an empty table cell.
const dash = "–"

// Markdown renders the providers table for the README, one row per catalog
// entry in registry order.
func Markdown(ps []provider.Provider) string {
	var b strings.Builder
	b.WriteString("| Provider | Usage | Money out | Quota meters | Captured by | Links |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- |\n")
	for _, p := range ps {
		info := p.Info()
		usage, _, charges := provider.Capabilities(p)

		name := info.Name
		if info.Unofficial {
			name += " (unofficial endpoint)"
		}

		usageCell := dash
		switch {
		case usage:
			usageCell = "API"
		case hasAuthKind(info.AuthKinds, provider.AuthAgentCapture):
			usageCell = "Agent"
		}

		moneyCell := "Manual"
		if charges {
			moneyCell = "API"
		}

		fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s |\n",
			escape(name), usageCell, moneyCell, meterCell(info.Meters),
			capturedBy(info.AuthKinds), linksCell(info.Links))
	}
	return b.String()
}

// hasAuthKind reports whether kinds contains kind.
func hasAuthKind(kinds []provider.AuthKind, kind provider.AuthKind) bool {
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}

// capturedBy names the first auth kind, which is the preferred one.
func capturedBy(kinds []provider.AuthKind) string {
	if len(kinds) == 0 {
		return "Manual"
	}
	switch kinds[0] {
	case provider.AuthAPIKey:
		return "API key"
	case provider.AuthAdminKey:
		return "Admin key"
	case provider.AuthAgentCapture:
		return "Agent"
	default:
		return "Manual"
	}
}

// meterCell joins the meter labels the provider reports.
func meterCell(meters []meter.Def) string {
	if len(meters) == 0 {
		return dash
	}
	labels := make([]string, len(meters))
	for i, m := range meters {
		labels[i] = m.Label
	}
	return escape(strings.Join(labels, ", "))
}

// linksCell renders the provider page links in README order.
func linksCell(l provider.Links) string {
	var links []string
	add := func(label, url string) {
		if url != "" {
			links = append(links, "["+label+"]("+url+")")
		}
	}
	add("Billing", l.Billing)
	add("Top up", l.TopUp)
	add("Usage", l.Usage)
	add("Manage", l.ManageSubscription)
	add("Docs", l.Docs)
	if len(links) == 0 {
		return dash
	}
	return escape(strings.Join(links, " · "))
}

// escape keeps a value from breaking the table.
func escape(s string) string {
	return strings.ReplaceAll(s, "|", `\|`)
}
