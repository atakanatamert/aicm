import { CartesianGrid, Line, LineChart, XAxis, YAxis } from "recharts"
import type { components } from "@/api/schema"
import { ChartContainer, ChartTooltip, ChartTooltipContent, type ChartConfig } from "@/components/ui/chart"
import { formatMeterValue } from "@/lib/format"

type MeterDef = components["schemas"]["MeterDef"]
type MeterReading = components["schemas"]["MeterReading"]

type HistoryPoint = { at: string; value: number }

/** Oldest first; "used" when readings carry it, otherwise "remaining". */
function seriesOf(readings: MeterReading[]): { kind: "used" | "remaining"; points: HistoryPoint[] } {
  const oldestFirst = [...readings].reverse()
  const used = oldestFirst.some((r) => r.used !== null && r.used !== undefined)
  const key = used ? "used" : "remaining"
  const points = oldestFirst
    .map((r) => ({ at: r.captured_at, value: used ? r.used : r.remaining }))
    .filter((p): p is HistoryPoint => p.value !== null && p.value !== undefined)
  return { kind: key, points }
}

const axisTick = new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "numeric" })
const axisLabel = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" })

const config = {
  value: { label: "Value", color: "var(--chart-1)" },
} satisfies ChartConfig

/** Small line chart of one meter's readings, drawn in the meter's unit. */
export function MeterHistoryChart({ def, readings }: { def: MeterDef; readings: MeterReading[] }) {
  const { kind, points } = seriesOf(readings)
  const percent = def.unit === "percent"
  return (
    <ChartContainer config={config} className="aspect-auto h-20 w-full" aria-label={`${def.label}, ${kind} over the last 7 days`}>
      <LineChart data={points} margin={{ left: 4, right: 8, top: 4 }}>
        <CartesianGrid vertical={false} />
        <XAxis
          dataKey="at"
          tickLine={false}
          axisLine={false}
          tickMargin={6}
          minTickGap={40}
          tickFormatter={(at: string) => axisTick.format(new Date(at))}
        />
        <YAxis
          tickLine={false}
          axisLine={false}
          width={44}
          domain={percent ? [0, 100] : ["auto", "auto"]}
          tickFormatter={(v: number) => formatMeterValue(def, v)}
        />
        <ChartTooltip
          content={
            <ChartTooltipContent
              labelFormatter={(label) => axisLabel.format(new Date(String(label)))}
              formatter={(value) => (
                <div className="flex w-full items-center justify-between gap-4">
                  <span className="text-muted-foreground capitalize">{kind}</span>
                  <span className="font-mono font-medium tabular-nums">{formatMeterValue(def, Number(value))}</span>
                </div>
              )}
            />
          }
        />
        <Line dataKey="value" type="monotone" stroke="var(--color-value)" strokeWidth={2} dot={false} isAnimationActive={false} />
      </LineChart>
    </ChartContainer>
  )
}
