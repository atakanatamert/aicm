import type { components } from "@/api/schema"

type MeterView = components["schemas"]["MeterView"]

/**
 * A wallet is a never-resetting meter, a currency-unit meter (a balance), or
 * the balance key itself — the thing a "Top up" link acts on.
 */
export function hasWalletMeter(meters: MeterView[] | undefined): boolean {
  return (
    meters?.some(
      (m) => m.def.reset_kind === "never" || m.def.unit === "currency" || m.def.key === "balance",
    ) ?? false
  )
}
