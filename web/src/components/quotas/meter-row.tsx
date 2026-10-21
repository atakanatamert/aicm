import type { components } from "@/api/schema"
import { formatMeterValue, formatRelative } from "@/lib/format"
import { MeterBar } from "./meter-bar"

type MeterView = components["schemas"]["MeterView"]
type ReadingSource = NonNullable<MeterView["latest"]>["source"]

const absoluteTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" })

/** Where a reading came from, in the user's words: "read 12 min ago · agent". */
const SOURCE_LABEL: Record<ReadingSource, string> = {
  api: "API",
  rollout: "agent",
  app_server: "agent",
  statusline: "agent",
  stream_json: "agent",
  manual: "entered manually",
}

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

/**
 * "Runs out around November 3 at this pace" for a wallet that never resets.
 * Needs a currency meter, a known burn and more than 48 h of headroom; the
 * caller says "in about N hours" for anything closer.
 */
function runwayLine(meter: MeterView, now: Date): string | undefined {
  if (meter.def.reset_kind !== "never" || meter.def.unit !== "currency") return undefined
  const hours = meter.score.hours_to_empty
  if (hours === null || hours === undefined || hours <= 0) return undefined
  const when = new Intl.DateTimeFormat(undefined, { dateStyle: "long" }).format(
    new Date(now.getTime() + hours * 3600 * 1000),
  )
  return `Runs out around ${when} at this pace`
}

// One provider-native meter: the provider's label and numbers, reset time,
// when the reading was captured and where it came from, and a bar only when a
// limit is known.
export function MeterRow({ meter }: { meter: MeterView }) {
  const reading = meter.latest
  const bar = barOf(meter)
  const now = new Date()
  const hoursToEmpty = meter.score.hours_to_empty
  const runway =
    hoursToEmpty !== null && hoursToEmpty !== undefined && hoursToEmpty > 0 && hoursToEmpty < 48
      ? `Runs out in about ${Math.round(hoursToEmpty)} ${Math.round(hoursToEmpty) === 1 ? "hour" : "hours"} at this pace`
      : runwayLine(meter, now)

  const parts: string[] = []
  if (reading?.used !== null && reading?.used !== undefined) {
    parts.push(`${formatMeterValue(meter.def, reading.used)} used`)
  }
  if (reading?.remaining !== null && reading?.remaining !== undefined) {
    parts.push(`${formatMeterValue(meter.def, reading.remaining)} remaining`)
  }
  // Percent meters drop "limit 100%": it says nothing the bar doesn't.
  if (reading?.limit !== null && reading?.limit !== undefined && meter.def.unit !== "percent") {
    parts.push(`limit ${formatMeterValue(meter.def, reading.limit)}`)
  }

  return (
    <li className="space-y-1.5">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{meter.def.label}</span>
        {reading && (
          <span className="text-xs text-muted-foreground">
            read {formatRelative(reading.captured_at)} · {SOURCE_LABEL[reading.source]}
          </span>
        )}
      </div>
      {bar && (
        <MeterBar
          value={bar.value}
          max={bar.max}
          label={`${meter.def.label} used`}
        />
      )}
      {runway && <p className="text-xs font-medium">{runway}</p>}
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
