import type { components } from "@/api/schema"

type AccountQuota = components["schemas"]["AccountQuota"]

/**
 * Subscriptions have limits that reset (5-hour, weekly, monthly windows) or a
 * recurring plan; everything else is a wallet you top up (API credits).
 * Supplemental meters (Codex credits, DeepSeek granted balance) don't decide.
 */
export function isSubscription(quota: AccountQuota): boolean {
  const meters = quota.meters.filter((m) => !m.def.supplemental)
  if (meters.length > 0) return meters.some((m) => m.def.reset_kind !== "never")
  return quota.account.plan.cycle !== "none"
}
