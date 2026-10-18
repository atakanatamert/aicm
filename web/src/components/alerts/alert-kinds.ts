import type { components } from "@/api/schema"

type AlertKind = components["schemas"]["AlertKind"]
type AlertParams = components["schemas"]["AlertParams"]

/** One editable parameter of a rule kind, with the spec's default as the placeholder. */
export interface KindField {
  key: keyof AlertParams
  label: string
  placeholder: string
  /** days_before arrives as a list and is edited comma-separated. */
  list?: boolean
}

export interface KindMeta {
  kind: AlertKind
  label: string
  fields: KindField[]
}

// Placeholders are the org-wide defaults from docs/PLAN.md "Alerts", which the
// migration seeds for every new org.
export const ALERT_KINDS: KindMeta[] = [
  {
    kind: "renewal",
    label: "Renewal coming",
    fields: [{ key: "days_before", label: "Days before", placeholder: "7, 1", list: true }],
  },
  {
    kind: "promo_ending",
    label: "Trial or promo ending",
    fields: [{ key: "days_before", label: "Days before", placeholder: "3", list: true }],
  },
  {
    kind: "low_balance",
    label: "Low balance",
    fields: [
      { key: "below_percent", label: "Below %", placeholder: "20" },
      { key: "days_left", label: "Days left", placeholder: "7" },
    ],
  },
  {
    kind: "meter_exhausted",
    label: "Meter nearly exhausted",
    fields: [{ key: "used_percent", label: "Used %", placeholder: "90" }],
  },
  {
    kind: "quota_waste",
    label: "Quota going to waste",
    fields: [
      { key: "unused_percent", label: "Unused %", placeholder: "50" },
      { key: "hours_before_reset", label: "Hours before reset", placeholder: "12" },
    ],
  },
  {
    kind: "sync_failing",
    label: "Sync failing",
    fields: [{ key: "after_hours", label: "After hours", placeholder: "24" }],
  },
  {
    kind: "agent_offline",
    label: "Agent offline",
    fields: [{ key: "after_hours", label: "After hours", placeholder: "48" }],
  },
]

/** One comma-separated or numeric draft value turned into a number or list. */
function fieldValue(raw: string, list: boolean): number[] | number | null | undefined {
  const text = raw.trim()
  if (!text) return null
  if (list) {
    const nums = text.split(",").map((part) => Number(part.trim()))
    if (nums.some((n) => !Number.isInteger(n) || n < 0)) return undefined
    return nums
  }
  const n = Number(text)
  if (!Number.isFinite(n) || n < 0) return undefined
  return n
}

/**
 * Draft text fields -> AlertParams for one kind. Returns valid=false when a
 * value does not parse, so the caller can keep the save button off.
 */
export function buildParams(meta: KindMeta, draft: Record<string, string>): { params: AlertParams; valid: boolean } {
  const params: AlertParams = {}
  let valid = true
  for (const field of meta.fields) {
    const value = fieldValue(draft[field.key] ?? "", field.list ?? false)
    if (value === undefined) {
      valid = false
      continue
    }
    if (value === null) continue
    switch (field.key) {
      case "days_before":
        params.days_before = value as number[]
        break
      case "below_percent":
        params.below_percent = value as number
        break
      case "days_left":
        params.days_left = value as number
        break
      case "used_percent":
        params.used_percent = value as number
        break
      case "unused_percent":
        params.unused_percent = value as number
        break
      case "hours_before_reset":
        params.hours_before_reset = value as number
        break
      case "after_hours":
        params.after_hours = value as number
        break
    }
  }
  return { params, valid }
}

/** A rule's params -> draft text fields for the inputs. */
export function draftOf(meta: KindMeta, params: AlertParams | undefined): Record<string, string> {
  const draft: Record<string, string> = {}
  for (const field of meta.fields) {
    const value = params?.[field.key]
    if (value === undefined || value === null) continue
    draft[field.key] = Array.isArray(value) ? value.join(", ") : String(value)
  }
  return draft
}
