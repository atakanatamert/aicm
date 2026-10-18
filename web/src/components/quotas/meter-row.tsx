import type { components } from "@/api/schema"
import { Badge } from "@/components/ui/badge"
import { formatMeterValue, formatRelative } from "@/lib/format"
import { MeterBar } from "./meter-bar"

type MeterView = components["schemas"]["MeterView"]

const absoluteTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" })

/** Used, remaining and limit as a filled fraction, or null when no bar applies. */
function barOf(meter: MeterView): { value: number; max: number } | null {
  const used = meter.latest?.used
  const remaining = meter.latest?.remaining
  const limit = meter.latest?.limit
  if (meter.def.unit === "percent") {
    if (used !== null && used !== undefined) return { value: used, max: 100 }
    if (remaining !== null && remaining !== undefined) return { value: 100 - remaining, max: 100 }
    return null
  }
  const knownLimit = meter.def.limit ?? limit
  if (knownLimit === null || knownLimit === undefined || knownLimit <= 0) return null
  if (used !== null && used !== undefined) return { value: used, max: knownLimit }
  if (remaining !== null && remaining !== undefined && remaining <= knownLimit) {
    return { value: knownLimit - remaining, max: knownLimit }
  }
  return null
}

// One provider-native meter: the provider's label and numbers, reset time,
// where the reading came from, and a bar only when a limit is known.
export function MeterRow({ meter }: { meter: MeterView }) {
  const reading = meter.latest
  const bar = barOf(meter)

  const parts: string[] = []
  if (reading?.used !== null && reading?.used !== undefined) {
    parts.push(`${formatMeterValue(meter.def, reading.used)} used`)
  }
  if (reading?.remaining !== null && reading?.remaining !== undefined) {
    parts.push(`${formatMeterValue(meter.def, reading.remaining)} remaining`)
  }
  if (reading?.limit !== null && reading?.limit !== undefined) {
    parts.push(`limit ${formatMeterValue(meter.def, reading.limit)}`)
  }

  return (
    <li className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{meter.def.label}</span>
        {reading && <Badge variant="secondary">{reading.source}</Badge>}
      </div>
      {bar && (
        <MeterBar
          value={bar.value}
          max={bar.max}
          label={`${meter.def.label} used`}
        />
      )}
      <p className="text-xs text-muted-foreground">
        {parts.length > 0 ? parts.join(" · ") : "No reading yet"}
        {reading?.resets_at && (
          <>
            {" · "}
            <span title={absoluteTime.format(new Date(reading.resets_at))}>
              resets {formatRelative(reading.resets_at)}
            </span>
          </>
        )}
        {meter.score.stale && " · window has reset since this reading"}
      </p>
    </li>
  )
}
