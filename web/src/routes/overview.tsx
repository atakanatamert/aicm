import { Suspense, lazy } from "react"
import { errorOf, query } from "@/api/client"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { ProviderTable } from "@/components/overview/provider-table"
import { RecordChargeDialog } from "@/components/overview/record-charge-dialog"
import { RenewalsStrip } from "@/components/overview/renewals-strip"
import { SpendHeadline } from "@/components/overview/spend-headline"
import { isoDay, unconvertedCount } from "@/components/overview/spend-format"
import { SuggestedCharges } from "@/components/overview/suggested-charges"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Separator } from "@/components/ui/separator"
import { Skeleton } from "@/components/ui/skeleton"
import { Terrain } from "@/components/ui/hairline"
import { useMe } from "@/hooks/use-me"

// Recharts is large; the chart loads after the screen renders.
const SpendChart = lazy(() => import("@/components/overview/spend-chart").then((m) => ({ default: m.SpendChart })))

function InlineError({ error, fallback }: { error: unknown; fallback: string }) {
  return (
    <p role="alert" className="text-sm text-destructive">
      {errorOf(error)?.error.message ?? fallback}
    </p>
  )
}

export default function Overview() {
  const { canEdit, displayCurrency } = useMe()
  const now = new Date()
  const year = now.getUTCFullYear()
  const monthPeriod = `${year}-${String(now.getUTCMonth() + 1).padStart(2, "0")}`
  const yearPeriod = String(year)

  // Month buckets cover Jan 1 → today; year buckets go back five years.
  const monthSpend = query.useQuery("get", "/api/spend", {
    params: { query: { period: "month", from: `${year}-01-01`, to: isoDay(now), group_by: "provider" } },
  })
  const yearSpend = query.useQuery("get", "/api/spend", {
    params: { query: { period: "year", from: `${year - 5}-01-01`, group_by: "provider" } },
  })
  const suggested = query.useQuery("get", "/api/charges", { params: { query: { status: "suggested" } } })
  const renewals = query.useQuery("get", "/api/renewals", { params: { query: { within_days: 7 } } })

  const monthRows = monthSpend.data?.rows ?? []
  const yearRows = yearSpend.data?.rows ?? []
  // The table shows only the current month and year; the chart uses all months.
  const monthRowsNow = monthRows.filter((r) => r.period === monthPeriod)
  const yearRowsNow = yearRows.filter((r) => r.period === yearPeriod)
  const spendLoading = monthSpend.isPending || yearSpend.isPending
  const spendError = monthSpend.error ?? yearSpend.error
  const noSpend = monthRows.length === 0 && yearRows.length === 0

  return (
    <>
      <PageHeader title="Overview" description="Money out this month and year, by provider." />
      <div className="grid gap-6 p-6">
        <section className="flex flex-wrap items-center justify-between gap-4">
          {spendLoading ? (
            <div className="grid flex-1 gap-4 sm:grid-cols-2">
              <Skeleton className="h-32" />
              <Skeleton className="h-32" />
            </div>
          ) : spendError ? (
            <InlineError error={spendError} fallback="Could not load spending." />
          ) : (
            <div className="grid flex-1 gap-4 sm:grid-cols-2">
              <SpendHeadline
                label="This month"
                total={monthSpend.data?.totals.find((t) => t.period === monthPeriod)}
                displayCurrency={displayCurrency}
                unconverted={unconvertedCount(monthRows, monthPeriod)}
              />
              <SpendHeadline
                label="This year"
                total={yearSpend.data?.totals.find((t) => t.period === yearPeriod)}
                displayCurrency={displayCurrency}
                unconverted={unconvertedCount(yearRows, yearPeriod)}
              />
            </div>
          )}
          {canEdit && <RecordChargeDialog />}
        </section>

        {suggested.isError ? (
          <InlineError error={suggested.error} fallback="Could not load suggested top-ups." />
        ) : suggested.data && suggested.data.items.length > 0 ? (
          <SuggestedCharges charges={suggested.data.items} canEdit={canEdit} />
        ) : null}

        {spendLoading ? (
          <Skeleton className="h-96" />
        ) : spendError ? null : noSpend ? (
          <EmptyState
            figure={<Terrain play />}
            title="No money out yet"
            description="Record a charge or connect an account; this month's and this year's payments show up here."
          />
        ) : (
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Money out per month</CardTitle>
              <CardDescription>
                {year}, converted to {displayCurrency}
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-4">
              <Suspense fallback={<Skeleton className="h-64" />}>
                <SpendChart rows={monthRows} displayCurrency={displayCurrency} year={year} />
              </Suspense>
              <Separator />
              <ProviderTable monthRows={monthRowsNow} yearRows={yearRowsNow} displayCurrency={displayCurrency} />
            </CardContent>
          </Card>
        )}

        {renewals.isError ? (
          <InlineError error={renewals.error} fallback="Could not load renewals." />
        ) : renewals.data && renewals.data.length > 0 ? (
          <RenewalsStrip renewals={renewals.data} />
        ) : null}
      </div>
    </>
  )
}
