import type { components } from "@/api/schema"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { useProviderNames } from "@/hooks/use-provider-names"
import { formatBilledAndConverted, formatMoney } from "@/lib/format"
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
  const yearTotals: Record<string, number> = {}
  for (const row of yearRows) {
    yearTotals[row.provider_id] = (yearTotals[row.provider_id] ?? 0) + (row.converted?.amount_minor ?? 0)
  }
  const providers = [...new Set([...monthRows, ...yearRows].map((r) => r.provider_id))].sort(
    (a, b) => (yearTotals[b] ?? 0) - (yearTotals[a] ?? 0),
  )
  return (
    <>
      {/* Desktop: one row per provider. */}
      <div className="hidden overflow-x-auto sm:block">
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
      {/* Mobile: stacked entries, same order and numbers. */}
      <ul className="grid gap-3 sm:hidden">
        {providers.map((provider) => (
          <li key={provider} className="flex items-center justify-between gap-3 border-b pb-3 last:border-b-0 last:pb-0">
            <span className="font-medium">{providerName(provider)}</span>
            <div className="text-right tabular-nums">
              <PeriodText row={monthRows.find((r) => r.provider_id === provider)} label="month" />
              <PeriodText row={yearRows.find((r) => r.provider_id === provider)} label="year" />
            </div>
          </li>
        ))}
      </ul>
    </>
  )
}

/** Billed first, converted second — "≈" only when a conversion actually happened. */
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
      {row.billed.length === 1 && row.converted ? (
        <div>{formatBilledAndConverted(row.billed[0], row.converted)}</div>
      ) : (
        <>
          <div>{billedSummary(row.billed)}</div>
          {row.converted && (
            <div className="text-xs text-muted-foreground">≈ {formatMoney(row.converted)}</div>
          )}
        </>
      )}
      {unconverted > 0 && (
        <div className="text-xs text-muted-foreground">{unconverted} not converted</div>
      )}
    </TableCell>
  )
}

function PeriodText({ row, label }: { row?: SpendRow; label: string }) {
  if (!row) {
    return <p className="text-sm text-muted-foreground">{label} —</p>
  }
  const line =
    row.billed.length === 1 && row.converted
      ? formatBilledAndConverted(row.billed[0], row.converted)
      : row.converted
        ? `${billedSummary(row.billed)} (≈ ${formatMoney(row.converted)})`
        : billedSummary(row.billed)
  return (
    <p className="text-sm">
      <span className="text-muted-foreground">{label} </span>
      {line}
    </p>
  )
}
