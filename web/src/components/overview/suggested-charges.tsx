import type { components } from "@/api/schema"
import { errorOf, query, queryClient } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card"
import { formatBilledAndConverted, formatDate } from "@/lib/format"

type Charge = components["schemas"]["Charge"]

const KIND_LABEL: Record<Charge["kind"], string> = {
  topup: "Top-up",
  subscription: "Subscription",
  invoice: "Invoice",
  refund: "Refund",
}

/** Detected balance jumps awaiting review; Confirm/Dismiss for editors. */
export function SuggestedCharges({ charges, canEdit }: { charges: Charge[]; canEdit: boolean }) {
  return (
    <Card>
      <CardHeader>
        <h3 className="text-base leading-none font-semibold">Suggested top-ups</h3>
        <CardDescription>Balance jumps we detected. Confirming counts them as money out.</CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="divide-y">
          {charges.map((charge) => (
            <li key={charge.id} className="first:pt-0 last:pb-0">
              <ChargeRow charge={charge} canEdit={canEdit} />
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  )
}

function ChargeRow({ charge, canEdit }: { charge: Charge; canEdit: boolean }) {
  const update = query.useMutation("patch", "/api/charges/{chargeId}", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/api/charges"] })
      queryClient.invalidateQueries({ queryKey: ["get", "/api/spend"] })
    },
  })
  const errorMessage = errorOf(update.error)?.error.message
  return (
    <div className="flex flex-wrap items-center justify-between gap-x-3 gap-y-2 py-3">
      <div className="min-w-0">
        <p className="truncate text-sm font-medium">{charge.description || KIND_LABEL[charge.kind]}</p>
        <p className="text-sm text-muted-foreground">
          {charge.account_name ?? "Account"} · {formatDate(charge.occurred_on)}
        </p>
        {errorMessage && (
          <p role="alert" className="text-sm text-destructive">
            {errorMessage}
          </p>
        )}
      </div>
      <div className="flex items-center gap-3">
        <span className="text-sm font-medium tabular-nums">{formatBilledAndConverted(charge.amount, charge.converted)}</span>
        {canEdit && (
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={update.isPending}
              onClick={() => update.mutate({ params: { path: { chargeId: charge.id } }, body: { status: "confirmed" } })}
            >
              Confirm
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={update.isPending}
              onClick={() => update.mutate({ params: { path: { chargeId: charge.id } }, body: { status: "dismissed" } })}
            >
              Dismiss
            </Button>
          </div>
        )}
      </div>
    </div>
  )
}
