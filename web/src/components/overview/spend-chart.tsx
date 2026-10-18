import { Link } from "react-router"
import { Bar, BarChart, CartesianGrid, XAxis, YAxis } from "recharts"
import type { components } from "@/api/schema"
import { ChartContainer, ChartLegend, ChartLegendContent, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { currencyExponent, formatMoney } from "@/lib/format"
import { providerLabel } from "./spend-format"

type SpendRow = components["schemas"]["SpendRow"]
type CurrencyCode = components["schemas"]["CurrencyCode"]

const MONTHS = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"]
const compactNumber = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 })

interface SpendChartProps {
  /** Rows from the month spend report for the current year. */
  rows: SpendRow[]
  displayCurrency: CurrencyCode
  year: number
}

/** Money out per month this year, stacked by provider, in the display currency. */
export function SpendChart({ rows, displayCurrency, year }: SpendChartProps) {
  const exp = currencyExponent(displayCurrency)
  const totals = new Map<string, number>()
  for (const row of rows) {
    totals.set(row.provider_id, (totals.get(row.provider_id) ?? 0) + (row.converted?.amount_minor ?? 0))
  }
  const providers = [...totals.keys()].sort((a, b) => (totals.get(b) ?? 0) - (totals.get(a) ?? 0))

  const data = []
  for (let m = 0; m <= new Date().getUTCMonth(); m++) {
    const period = `${year}-${String(m + 1).padStart(2, "0")}`
    const point: Record<string, number | string> = { month: MONTHS[m] }
    for (const provider of providers) {
      const row = rows.find((r) => r.period === period && r.provider_id === provider)
      point[provider] = row?.converted ? row.converted.amount_minor / 10 ** exp : 0
    }
    data.push(point)
  }

  const config: ChartConfig = Object.fromEntries(
    providers.map((provider, i) => [provider, { label: providerLabel(provider), color: `var(--chart-${(i % 5) + 1})` }]),
  )

  return (
    <div>
      <ChartContainer config={config} className="aspect-auto h-64 w-full">
        <BarChart data={data}>
          <CartesianGrid vertical={false} />
          <XAxis dataKey="month" tickLine={false} axisLine={false} tickMargin={8} />
          <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(v) => compactNumber.format(Number(v))} />
          <ChartTooltip
            content={
              <ChartTooltipContent
                formatter={(value, name) => (
                  <div className="flex w-full items-center justify-between gap-3 tabular-nums">
                    <span className="text-muted-foreground">{name}</span>
                    <span className="font-medium">{formatMoney({ amount_minor: Math.round(Number(value) * 10 ** exp), currency: displayCurrency })}</span>
                  </div>
                )}
              />
            }
          />
          <ChartLegend content={<ChartLegendContent />} />
          {providers.map((provider) => (
            <Bar key={provider} dataKey={provider} stackId="money" fill={`var(--color-${provider})`} />
          ))}
        </BarChart>
      </ChartContainer>
      <p className="mt-3 text-xs text-muted-foreground">
        Token-cost estimates are on the{" "}
        <Link to="/usage" className="underline underline-offset-2 hover:text-foreground">
          Usage page
        </Link>
        ; this page shows money that left your account.
      </p>
    </div>
  )
}
