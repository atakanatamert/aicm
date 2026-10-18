import type { components } from "@/api/schema"
import { renewalUrgency } from "@/components/quotas/renewal-urgency"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader } from "@/components/ui/card"
import { ExternalLink } from "lucide-react"
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
          {account.links.manage_subscription && (
            <Button asChild variant="outline" size="sm">
              <a href={account.links.manage_subscription} target="_blank" rel="noreferrer">
                Manage subscription
                <ExternalLink aria-hidden className="size-3" />
              </a>
            </Button>
          )}
          {account.links.cancel && (
            <Button asChild variant="outline" size="sm">
              <a href={account.links.cancel} target="_blank" rel="noreferrer">
                Cancel subscription
                <ExternalLink aria-hidden className="size-3" />
              </a>
            </Button>
          )}
        </CardFooter>
      )}
    </Card>
  )
}
