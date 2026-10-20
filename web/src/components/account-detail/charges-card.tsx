import { query } from "@/api/client"
import { EmptyState } from "@/components/empty-state"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Settle } from "@/components/ui/hairline"
import { formatBilledAndConverted, formatDate } from "@/lib/format"
import { ErrorLine, SectionCard } from "./section"

const KIND_LABEL: Record<string, string> = {
  subscription: "Subscription",
  topup: "Top-up",
  invoice: "Invoice",
  refund: "Refund",
}

/** The account's last 20 confirmed charges, newest first. */
export function ChargesCard({ accountId }: { accountId: string }) {
  const charges = query.useQuery("get", "/api/charges", {
    params: { query: { account_id: accountId, limit: 20 } },
  })

  return (
    <SectionCard title="Charges" description="The last 20 payments on this account.">
      {charges.isPending ? (
        <div className="space-y-2" aria-hidden>
          <div className="h-8 rounded-md bg-muted" />
          <div className="h-8 rounded-md bg-muted" />
          <div className="h-8 rounded-md bg-muted" />
        </div>
      ) : charges.isError ? (
        <ErrorLine error={charges.error} fallback="Could not load charges." />
      ) : charges.data.items.length === 0 ? (
        <EmptyState
          figure={<Settle play />}
          title="No charges yet"
          description="Record a payment on the Overview and it appears here."
          className="py-6"
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Date</TableHead>
              <TableHead>Kind</TableHead>
              <TableHead>Description</TableHead>
              <TableHead className="text-right">Amount</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {charges.data.items.map((charge) => (
              <TableRow key={charge.id}>
                <TableCell className="whitespace-nowrap">{formatDate(charge.occurred_on)}</TableCell>
                <TableCell>
                  <span className="inline-flex gap-1.5">
                    <Badge variant="secondary">{KIND_LABEL[charge.kind] ?? charge.kind}</Badge>
                    {charge.source === "estimated" && <Badge variant="outline">estimated</Badge>}
                  </span>
                </TableCell>
                <TableCell className="max-w-xs truncate text-muted-foreground" title={charge.description}>
                  {charge.description || "–"}
                </TableCell>
                <TableCell className="text-right whitespace-nowrap tabular-nums">
                  {formatBilledAndConverted(charge.amount, charge.converted)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </SectionCard>
  )
}
