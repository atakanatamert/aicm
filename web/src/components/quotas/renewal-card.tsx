import type { components } from "@/api/schema"
import { ProviderLinks } from "@/components/quotas/provider-links"
import { renewalUrgency } from "@/components/quotas/renewal-urgency"
import { Badge } from "@/components/ui/badge"
import { Card, CardContent, CardDescription, CardFooter, CardHeader } from "@/components/ui/card"
import { formatBilledAndConverted, formatDate } from "@/lib/format"
import { cn } from "@/lib/utils"

type Renewal = components["schemas"]["Renewal"]

// One upcoming renewal. The urgent border comes with a reason line, never
// colour alone.
export function RenewalCard({ renewal }: { renewal: Renewal }) {
  const { account } = renewal
  const { urgent, reason } = renewalUrgency(renewal)
  return (
    <Card className={cn("gap-4 py-5", urgent && "border-urgent")}>
      <CardHeader>
        <h3 className="text-base leading-none font-semibold">{account.name}</h3>
        <CardDescription>
          {account.provider_name} · {account.plan.name}
        </CardDescription>
        {reason && <p className="text-xs font-medium text-urgent">{reason}</p>}
      </CardHeader>
      <CardContent className="space-y-1">
        {renewal.price && (
          <p className="text-sm font-medium">{formatBilledAndConverted(renewal.price, renewal.converted)}</p>
        )}
        <p className="flex items-center gap-2 text-sm text-muted-foreground">
          {formatDate(renewal.renews_on)} · in {renewal.days_left} {renewal.days_left === 1 ? "day" : "days"}
          {!renewal.auto_renew && <Badge variant="outline">Auto-renew off</Badge>}
        </p>
      </CardContent>
      {(account.links.manage_subscription || account.links.cancel) && (
        <CardFooter className="flex-wrap gap-2">
          <ProviderLinks links={account.links} renewal />
        </CardFooter>
      )}
    </Card>
  )
}
