// Formatting helpers local to the Overview screen. Types come from the
// generated schema; no response type is hand-written.

import type { components } from "@/api/schema"
import { formatMoney } from "@/lib/format"

type Money = components["schemas"]["Money"]
type SpendRow = components["schemas"]["SpendRow"]

/** Today as YYYY-MM-DD in UTC, matching the hub's day boundaries. */
export function isoDay(d: Date = new Date()): string {
  return d.toISOString().slice(0, 10)
}

/** "anthropic" → "Anthropic", "zai-glm" → "Zai Glm". */
export function providerLabel(providerId: string): string {
  return providerId
    .split(/[-_.]/)
    .filter(Boolean)
    .map((word) => word[0]!.toUpperCase() + word.slice(1))
    .join(" ")
}

/** "¥300.00 · $212.00" — billed amounts stay per currency, never summed. */
export function billedSummary(billed: Money[]): string {
  return billed.map((m) => formatMoney(m)).join(" · ")
}

/** Charges in `period` ("2026-10" or "2026") that had no rate for their date. */
export function unconvertedCount(rows: SpendRow[], period: string): number {
  return rows
    .filter((r) => r.period === period)
    .reduce((n, r) => n + (r.unconverted_count ?? 0), 0)
}
