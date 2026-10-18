import type { components } from "@/api/schema"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBilledAndConverted, formatDate } from "@/lib/format"
import { cn } from "@/lib/utils"

type Renewal = components["schemas"]["Renewal"]

// One upcoming renewal. The urgent border marks renewals within 7 days.
export function RenewalCard({ renewal }: { renewal: Renewal }) {
  const { account } = renewal
  const urgent = renewal.days_left <= 7
  return (
    <Card className={cn("gap-4 py-5", urgent && "border-urgent")}>
      <CardHeader>
        <CardTitle className="text-base">{account.name}</CardTitle>
        <CardDescription>
          {account.provider_name} · {account.plan.name}
        </CardDescription>
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
        <CardFooter className="gap-2">
          {account.links.manage_subscription && (
            <Button asChild variant="outline" size="xs">
              <a href={account.links.manage_subscription} target="_blank" rel="noreferrer">
                Manage
              </a>
            </Button>
          )}
          {account.links.cancel && (
            <Button asChild variant="outline" size="xs">
              <a href={account.links.cancel} target="_blank" rel="noreferrer">
                Cancel
              </a>
            </Button>
          )}
        </CardFooter>
      )}
    </Card>
  )
}
