import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"
import type { components } from "@/api/schema"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { formatMicro } from "@/lib/format"

type UsageRow = components["schemas"]["UsageRow"]

const compact = new Intl.NumberFormat(undefined, { notation: "compact", maximumFractionDigits: 1 })
const axisDay = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", timeZone: "UTC" })

type Point = { day: string; requests: number } & Record<string, string | number>

/** One point per day; one line per currency, in micro units. */
function seriesOf(rows: UsageRow[]): Point[] {
  const byDay = new Map<string, Point>()
  for (const row of rows) {
    const day = row.day
    if (!day) continue
    const point = byDay.get(day) ?? { day, requests: 0 }
    point.requests += row.requests
    for (const c of row.cost) {
      point[c.currency] = (Number(point[c.currency]) || 0) + c.amount_micro
    }
    byDay.set(day, point)
  }
  return [...byDay.values()].sort((a, b) => a.day.localeCompare(b.day))
}

/** Estimated cost per day, one line per currency. */
export function UsageByDayChart({ rows }: { rows: UsageRow[] }) {
  const points = seriesOf(rows)
  const currencies = [...new Set(rows.flatMap((r) => r.cost.map((c) => c.currency)))]
  const config = Object.fromEntries(
    currencies.map((currency, i) => [currency, { label: currency, color: `var(--chart-${(i % 5) + 1})` }]),
  ) as ChartConfig

  return (
    <ChartContainer config={config} className="aspect-auto h-24 w-full" aria-label="Estimated token cost per day, last 30 days">
      <LineChart data={points} margin={{ left: 4, right: 8, top: 4 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="day"
          tickLine={false}
          axisLine={false}
          tickMargin={6}
          minTickGap={40}
          tickFormatter={(day: string) => axisDay.format(new Date(day))}
        />
        <YAxis tickLine={false} axisLine={false} width={44} tickFormatter={(micro: number) => compact.format(micro / 1e6)} />
        <ChartTooltip
          content={
            <ChartTooltipContent
              labelFormatter={(label) => axisDay.format(new Date(String(label)))}
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
  )
}
