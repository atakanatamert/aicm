import { lazy, Suspense, useMemo } from "react"
import { query } from "@/api/client"
import { ErrorLine, SectionCard } from "./section"

const DAY_MS = 86_400_000
const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 })

// Recharts is heavy; keep it out of the main bundle like the Usage page does.
const UsageByDayChart = lazy(() => import("./usage-by-day-chart").then((m) => ({ default: m.UsageByDayChart })))

/** Estimated token cost per day for this account, last 30 days. */
export function UsageCard({ accountId }: { accountId: string }) {
  // Stable across renders: a fresh Date each render would change the query key
  // and retrigger the fetch forever.
  const { from, to } = useMemo(() => {
    const to = new Date().toISOString().slice(0, 10)
    const from = new Date(Date.now() - 29 * DAY_MS).toISOString().slice(0, 10)
    return { from, to }
  }, [])
  const usage = query.useQuery("get", "/api/usage", {
    params: { query: { group_by: "day", account_id: accountId, from, to } },
  })

  const rows = usage.data ?? []
  const requests = rows.reduce((n, r) => n + r.requests, 0)
  const tokens = rows.reduce((n, r) => n + r.input_tokens + r.output_tokens, 0)
  const hasData = rows.some((r) => r.day !== undefined)

  return (
    <SectionCard title="Usage" description="Token estimates for the last 30 days — secondary to money out.">
      {usage.isPending ? (
        <div className="h-24 rounded-md bg-muted" aria-hidden />
      ) : usage.isError ? (
        <ErrorLine error={usage.error} fallback="Could not load usage." />
      ) : !hasData ? (
        <p className="text-sm text-muted-foreground">No usage in the last 30 days.</p>
      ) : (
        <div className="space-y-2">
          <p className="text-sm text-muted-foreground">
            {compact.format(requests)} requests · {compact.format(tokens)} input + output tokens
          </p>
          <Suspense fallback={<div className="h-24 rounded-md bg-muted" aria-hidden />}>
            <UsageByDayChart rows={rows} />
          </Suspense>
        </div>
      )}
    </SectionCard>
  )
}
