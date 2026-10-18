import { lazy, Suspense, useMemo } from "react"
import type { components } from "@/api/schema"
import { query } from "@/api/client"

type MeterDef = components["schemas"]["MeterDef"]

const WEEK_MS = 7 * 86_400_000
const READING_LIMIT = 500

// Recharts is heavy; keep it out of the main bundle like the Usage page does.
const MeterHistoryChart = lazy(() => import("./meter-history-chart").then((m) => ({ default: m.MeterHistoryChart })))

/** The meter's readings over the last 7 days, in the meter's own unit. */
export function MeterHistory({ accountId, def }: { accountId: string; def: MeterDef }) {
  // Stable across renders: a fresh Date each render would change the query key
  // and retrigger the fetch forever.
  const from = useMemo(() => new Date(Date.now() - WEEK_MS).toISOString(), [])
  const readings = query.useQuery("get", "/api/accounts/{accountId}/meters/{meterKey}/readings", {
    params: { path: { accountId, meterKey: def.key }, query: { from, limit: READING_LIMIT } },
  })

  if (readings.isPending) return <div className="h-16 rounded-md bg-muted" aria-hidden />
  if (readings.isError || readings.data.length < 2) {
    return <p className="text-xs text-muted-foreground">No readings in the last 7 days.</p>
  }

  return (
    <Suspense fallback={<div className="h-20 rounded-md bg-muted" aria-hidden />}>
      <MeterHistoryChart def={def} readings={readings.data} />
    </Suspense>
  )
}
