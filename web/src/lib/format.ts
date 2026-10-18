// Formatting shared by every screen. Money arrives as integer minor units (or
// micro units for token-cost estimates) with an ISO 4217 code; meters arrive
// in the provider's own unit. Nothing here converts between currencies.

import type { components } from "@/api/schema"

type Money = components["schemas"]["Money"]
type MicroMoney = components["schemas"]["MicroMoney"]
type MeterDef = components["schemas"]["MeterDef"]

// ISO 4217 minor-unit digits that are not 2 (mirrors internal/money).
const EXPONENT: Record<string, number> = {
  BIF: 0, CLP: 0, DJF: 0, GNF: 0, ISK: 0, JPY: 0, KMF: 0, KRW: 0, PYG: 0,
  RWF: 0, UGX: 0, UYI: 0, VND: 0, VUV: 0, XAF: 0, XOF: 0, XPF: 0,
  BHD: 3, IQD: 3, JOD: 3, KWD: 3, LYD: 3, OMR: 3, TND: 3,
}

export function currencyExponent(currency: string): number {
  return EXPONENT[currency] ?? 2
}

/** "$200.00", "¥1,500", "CN¥110.00" — billed currency, exact minor units. */
export function formatMoney(m: Money): string {
  const exp = currencyExponent(m.currency)
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: m.currency,
    minimumFractionDigits: exp,
    maximumFractionDigits: exp,
  }).format(m.amount_minor / 10 ** exp)
}

/** Billed first, converted second: "¥100.00 (≈ $13.90)". */
export function formatBilledAndConverted(billed: Money, converted?: Money | null): string {
  if (!converted || converted.currency === billed.currency) return formatMoney(billed)
  return `${formatMoney(billed)} (≈ ${formatMoney(converted)})`
}

/** Token-cost estimates: micro units, shown with up to 4 decimals. */
export function formatMicro(m: MicroMoney): string {
  return new Intl.NumberFormat(undefined, {
    style: "currency",
    currency: m.currency,
    minimumFractionDigits: 2,
    maximumFractionDigits: 4,
  }).format(m.amount_micro / 1e6)
}

/** A meter value in the meter's own unit: "62%", "1,240 requests", "$12.40". */
export function formatMeterValue(def: MeterDef, value: number | null | undefined): string {
  if (value === null || value === undefined) return "–"
  switch (def.unit) {
    case "percent":
      return `${new Intl.NumberFormat(undefined, { maximumFractionDigits: 1 }).format(value)}%`
    case "currency":
      return formatMoney({ amount_minor: Math.round(value), currency: def.unit_currency ?? "USD" })
    default: {
      const n = new Intl.NumberFormat(undefined, { maximumFractionDigits: 2 }).format(value)
      return def.unit === "count" ? n : `${n} ${def.unit}`
    }
  }
}

const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: "auto" })

/** "in 3 hours", "2 days ago". */
export function formatRelative(iso: string | Date, now: Date = new Date()): string {
  const t = typeof iso === "string" ? new Date(iso) : iso
  const diffSec = (t.getTime() - now.getTime()) / 1000
  const abs = Math.abs(diffSec)
  if (abs < 60) return rtf.format(Math.round(diffSec), "second")
  if (abs < 3600) return rtf.format(Math.round(diffSec / 60), "minute")
  if (abs < 86400 * 2) return rtf.format(Math.round(diffSec / 3600), "hour")
  return rtf.format(Math.round(diffSec / 86400), "day")
}

/** Calendar date "Oct 27, 2026" for an ISO date (YYYY-MM-DD) or timestamp, in UTC. */
export function formatDate(iso: string): string {
  return new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeZone: "UTC" }).format(new Date(iso))
}
