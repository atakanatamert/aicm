import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"
import type { components } from "@/api/schema"
import { formatDate, formatMicro } from "@/lib/format"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"

type UsageRow = components["schemas"]["UsageRow"]

const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 })
const axisDay = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" })

/**
 * Estimated cost per day, one line per currency in the estimates. Estimates
 * only: money out lives on the Overview.
 */
export function UsageChart({ rows }: { rows: UsageRow[] }) {
  const currencies = [...new Set(rows.flatMap((row) => row.cost.map((c) => c.currency)))]
  const data = rows.map((row) => {
    const point: Record<string, string | number> = { day: row.day ?? "" }
    for (const c of row.cost) {
      point[c.currency] = (Number(point[c.currency]) || 0) + c.amount_micro
    }
    return point
  })
  const config = Object.fromEntries(
    currencies.map((currency, i) => [currency, { label: currency, color: `var(--chart-${(i % 5) + 1})` }]),
  ) as ChartConfig

  return (
    <Card>
      <CardHeader>
        <CardTitle>Estimated cost per day</CardTitle>
        <CardDescription>Token counts × list prices, before payments.</CardDescription>
      </CardHeader>
      <CardContent>
        <ChartContainer config={config} className="aspect-auto h-64 w-full">
          <LineChart data={data} margin={{ left: 8, right: 8 }}>
            <CartesianGrid vertical={false} />
            <XAxis
              dataKey="day"
              tickLine={false}
              axisLine={false}
              tickMargin={8}
              minTickGap={24}
              tickFormatter={(day: string) => axisDay.format(new Date(day))}
            />
            <YAxis
              tickLine={false}
              axisLine={false}
              width={48}
              tickFormatter={(micro: number) => compact.format(micro / 1e6)}
            />
            <ChartTooltip
              content={
                <ChartTooltipContent
                  labelFormatter={(label) => formatDate(String(label))}
                  formatter={(value, name) => (
                    <div className="flex w-full items-center justify-between gap-4">
                      <span className="text-muted-foreground">{name}</span>
                      <span className="font-mono font-medium tabular-nums">
                        {formatMicro({ amount_micro: Number(value), currency: String(name) })}
                      </span>
                    </div>
                  )}
                />
              }
            />
            {currencies.map((currency) => (
              <Line
                key={currency}
                dataKey={currency}
                type="monotone"
                stroke={`var(--color-${currency})`}
                strokeWidth={2}
                dot={false}
                isAnimationActive={false}
              />
            ))}
          </LineChart>
        </ChartContainer>
      </CardContent>
    </Card>
  )
}
