import type { components } from "@/api/schema"
import { formatDate, formatMicro } from "@/lib/format"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

type UsageRow = components["schemas"]["UsageRow"]

export type UsageGroupBy = "day" | "model" | "account" | "provider" | "project"

const tokens = new Intl.NumberFormat(undefined, { maximumFractionDigits: 0 })

const HEAD_BY_GROUP: Record<UsageGroupBy, string> = {
  day: "Day",
  model: "Model",
  account: "Account",
  provider: "Provider",
  project: "Project",
}

/** The grouping key of a row; the server only fills the keys it grouped by. */
function rowLabel(groupBy: UsageGroupBy, row: UsageRow): string {
  switch (groupBy) {
    case "day":
      return row.day ? formatDate(row.day) : "–"
    case "model":
      return row.model ?? "–"
    case "account":
      return row.account_name ?? row.account_id ?? "–"
    case "provider":
      return row.provider_id ?? "–"
    case "project":
      return row.project || "Unknown"
  }
}

/** One row per group: token counts and the estimated cost (never money out). */
export function UsageTable({ groupBy, rows }: { groupBy: UsageGroupBy; rows: UsageRow[] }) {
  return (
    <div className="overflow-x-auto rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{HEAD_BY_GROUP[groupBy]}</TableHead>
            <TableHead className="text-right">Requests</TableHead>
            <TableHead className="text-right">Input</TableHead>
            <TableHead className="text-right">Output</TableHead>
            <TableHead className="text-right">Cache read</TableHead>
            <TableHead className="text-right">Cache write</TableHead>
            <TableHead className="text-right">Reasoning</TableHead>
            <TableHead className="text-right">Est. cost</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((row, i) => (
            <TableRow key={i}>
              <TableCell className="font-medium">{rowLabel(groupBy, row)}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens.format(row.requests)}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens.format(row.input_tokens)}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens.format(row.output_tokens)}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens.format(row.cache_read_tokens)}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens.format(row.cache_write_tokens)}</TableCell>
              <TableCell className="text-right tabular-nums">{tokens.format(row.reasoning_tokens)}</TableCell>
              <TableCell className="text-right whitespace-nowrap tabular-nums">
                {row.cost.length === 0 ? "–" : row.cost.map((c) => formatMicro(c)).join(" + ")}
                <Badge variant={row.cost_reported ? "secondary" : "outline"} className="ml-2 align-middle text-xs">
                  {row.cost_reported ? "reported" : "estimated"}
                </Badge>
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}
