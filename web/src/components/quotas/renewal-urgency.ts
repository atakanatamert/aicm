import type { components } from "@/api/schema"

type Renewal = components["schemas"]["Renewal"]

/**
 * Urgent when renewal is tomorrow or today, or when it renews within 7 days
 * with auto-renew off. Ordinary auto-renewing renewals 2–7 days out stay
 * neutral. Every urgent case names its reason.
 */
export function renewalUrgency(renewal: Renewal): { urgent: boolean; reason: string | undefined } {
  if (renewal.days_left <= 0) return { urgent: true, reason: "Renews today" }
  if (renewal.days_left === 1) return { urgent: true, reason: "Renews tomorrow" }
  if (!renewal.auto_renew && renewal.days_left <= 7) {
    return { urgent: true, reason: `Auto-renew is off: renews in ${renewal.days_left} days` }
  }
  return { urgent: false, reason: undefined }
}
