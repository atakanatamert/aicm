import { lazy, Suspense, useState } from "react"
import { Download } from "lucide-react"
import { errorOf, query } from "@/api/client"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Phosphor } from "@/components/ui/hairline"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Skeleton } from "@/components/ui/skeleton"
import { UsageTable, type UsageGroupBy } from "@/components/usage/usage-table"

// Recharts is heavy and only needed for the day grouping; keep it out of the
// main bundle.
const UsageChart = lazy(() => import("@/components/usage/usage-chart").then((m) => ({ default: m.UsageChart })))

type UsageRange = "month" | "last30" | "year"

const RANGE_LABEL: Record<UsageRange, string> = {
  month: "This month",
  last30: "Last 30 days",
  year: "This year",
}

const GROUPS: UsageGroupBy[] = ["day", "model", "account", "provider"]

/** Inclusive UTC day range for a picker choice. */
function rangeOf(range: UsageRange): { from: string; to: string } {
  const to = new Date().toISOString().slice(0, 10)
  switch (range) {
    case "month":
      return { from: `${to.slice(0, 7)}-01`, to }
    case "last30":
      return { from: new Date(Date.now() - 29 * 86_400_000).toISOString().slice(0, 10), to }
    case "year":
      return { from: `${to.slice(0, 4)}-01-01`, to }
  }
}

export default function Usage() {
  const [range, setRange] = useState<UsageRange>("last30")
  const [groupBy, setGroupBy] = useState<UsageGroupBy>("day")
  const { from, to } = rangeOf(range)
  const usage = query.useQuery("get", "/api/usage", { params: { query: { group_by: groupBy, from, to } } })

  return (
    <>
      <PageHeader
        title="Usage"
        description="Token usage and cost estimates. Estimates from token counts × list prices. Money out is on the Overview."
      />
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-4 px-4 py-6 sm:px-6">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="flex flex-wrap items-end gap-3">
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="usage-range">Range</Label>
              <Select value={range} onValueChange={(v) => setRange(v as UsageRange)}>
                <SelectTrigger id="usage-range" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {(Object.keys(RANGE_LABEL) as UsageRange[]).map((r) => (
                    <SelectItem key={r} value={r}>
                      {RANGE_LABEL[r]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="usage-group">Group by</Label>
              <Select value={groupBy} onValueChange={(v) => setGroupBy(v as UsageGroupBy)}>
                <SelectTrigger id="usage-group" className="w-40">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {GROUPS.map((g) => (
                    <SelectItem key={g} value={g}>
                      {g[0].toUpperCase() + g.slice(1)}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <Button asChild variant="outline">
            <a href={`/api/export/usage.csv?from=${from}&to=${to}`} download>
              <Download />
              Export CSV
            </a>
          </Button>
        </div>

        {usage.isPending ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-64 w-full" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : usage.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {errorOf(usage.error)?.error.message ?? "Could not load usage. Check that the hub is running."}
          </p>
        ) : usage.data.length === 0 ? (
          <EmptyState
            figure={<Phosphor play />}
            title="No usage in this range"
            description="Usage appears once an agent reads Claude Code, Codex or omp logs on a machine, or an API account syncs."
            action={
              range !== "year" && (
                <Button variant="outline" onClick={() => setRange("year")}>
                  Show this year
                </Button>
              )
            }
          />
        ) : (
          <>
            {groupBy === "day" && (
              <Suspense fallback={<Skeleton className="h-64 w-full" />}>
                <UsageChart rows={usage.data} />
              </Suspense>
            )}
            <UsageTable groupBy={groupBy} rows={usage.data} />
          </>
        )}
      </div>
    </>
  )
}
