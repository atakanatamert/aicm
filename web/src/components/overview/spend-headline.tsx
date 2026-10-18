import type { components } from "@/api/schema"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { formatMoney } from "@/lib/format"
import { billedSummary } from "./spend-format"

type SpendTotal = components["schemas"]["SpendTotal"]
type CurrencyCode = components["schemas"]["CurrencyCode"]

interface SpendHeadlineProps {
  label: string
  /** The period's total from a spend report; absent when nothing was spent. */
  total?: SpendTotal
  displayCurrency: CurrencyCode
  /** Charges in this period with no rate for their date. */
  unconverted: number
}

/** Money out headline: converted total, billed per currency underneath. */
export function SpendHeadline({ label, total, displayCurrency, unconverted }: SpendHeadlineProps) {
  const billed = total?.billed ?? []
  const amount = total?.converted
    ? formatMoney(total.converted)
    : formatMoney({ amount_minor: 0, currency: displayCurrency })
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm font-medium text-muted-foreground">{label}</CardTitle>
      </CardHeader>
      <CardContent className="grid gap-1">
        <p className="text-3xl font-semibold tracking-tight tabular-nums">
          {total?.converted ? amount : billed.length > 0 ? "—" : amount}
        </p>
        {billed.length > 0 && <p className="text-sm text-muted-foreground tabular-nums">{billedSummary(billed)}</p>}
        {unconverted > 0 && (
          <p className="text-sm text-muted-foreground">
            {unconverted} {unconverted === 1 ? "charge" : "charges"} not converted
          </p>
        )}
      </CardContent>
    </Card>
  )
}
