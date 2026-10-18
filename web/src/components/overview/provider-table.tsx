import type { components } from "@/api/schema"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useProviderNames } from "@/hooks/use-provider-names"
import { formatMoney } from "@/lib/format"
import { billedSummary } from "./spend-format"

type SpendRow = components["schemas"]["SpendRow"]
type CurrencyCode = components["schemas"]["CurrencyCode"]

interface ProviderTableProps {
  /** Rows from the month spend report (current year, grouped by month). */
  monthRows: SpendRow[]
  /** Rows from the year spend report. */
  yearRows: SpendRow[]
  displayCurrency: CurrencyCode
}

/** Per provider: this month and this year, billed per currency first, converted second. */
export function ProviderTable({ monthRows, yearRows, displayCurrency }: ProviderTableProps) {
  const providerName = useProviderNames()
  const yearTotals = new Map<string, number>()
  for (const row of yearRows) {
    yearTotals.set(row.provider_id, (yearTotals.get(row.provider_id) ?? 0) + (row.converted?.amount_minor ?? 0))
  }
  const providers = [...new Set([...monthRows, ...yearRows].map((r) => r.provider_id))].sort(
    (a, b) => (yearTotals.get(b) ?? 0) - (yearTotals.get(a) ?? 0),
  )
  return (
    <div className="overflow-x-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Provider</TableHead>
            <TableHead className="text-right">This month</TableHead>
            <TableHead className="text-right">This year</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {providers.map((provider) => (
            <TableRow key={provider}>
              <TableCell className="font-medium">{providerName(provider)}</TableCell>
              <PeriodCell row={monthRows.find((r) => r.provider_id === provider)} displayCurrency={displayCurrency} />
              <PeriodCell row={yearRows.find((r) => r.provider_id === provider)} displayCurrency={displayCurrency} />
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )
}

function PeriodCell({ row, displayCurrency }: { row?: SpendRow; displayCurrency: CurrencyCode }) {
  if (!row) {
    return (
      <TableCell className="text-right tabular-nums text-muted-foreground">
        {formatMoney({ amount_minor: 0, currency: displayCurrency })}
      </TableCell>
    )
  }
  const unconverted = row.unconverted_count ?? 0
  return (
    <TableCell className="text-right align-top tabular-nums">
      <div>{billedSummary(row.billed)}</div>
      <div className="text-xs text-muted-foreground">
        {row.converted ? `≈ ${formatMoney(row.converted)}` : unconverted > 0 ? `${unconverted} not converted` : ""}
      </div>
    </TableCell>
  )
}
