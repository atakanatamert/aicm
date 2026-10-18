import { Link } from "react-router"
import type { components } from "@/api/schema"
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card"
import { formatBilledAndConverted, formatDate } from "@/lib/format"
import { cn } from "@/lib/utils"
import { renewalUrgency } from "@/components/quotas/renewal-urgency"

type Renewal = components["schemas"]["Renewal"]

/** Subscriptions renewing within 7 days; details and actions live in Quotas. */
export function RenewalsStrip({ renewals }: { renewals: Renewal[] }) {
  return (
    <Card>
      <CardHeader>
        <h3 className="text-base leading-none font-semibold">Renews this week</h3>
        <CardDescription>
          Renewing within 7 days.{" "}
          <Link to="/quotas" className="underline underline-offset-2 hover:text-foreground">
            Open Quotas
          </Link>
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {renewals.map((renewal) => {
            const { urgent, reason } = renewalUrgency(renewal)
            return (
              <li key={renewal.account.id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium">{renewal.account.name}</p>
                  <p className="text-sm text-muted-foreground">{renewal.account.provider_name}</p>
                  {urgent && reason && <p className="text-xs font-medium text-urgent">{reason}</p>}
                </div>
                <div className="text-right">
                  <p className={cn("text-sm font-medium", urgent && "text-urgent")}>{formatDate(renewal.renews_on)}</p>
                  <p className="text-xs text-muted-foreground tabular-nums">
                    {inDays(renewal.days_left)}
                    {renewal.price ? ` · ${formatBilledAndConverted(renewal.price, renewal.converted)}` : ""}
                    {renewal.auto_renew ? "" : " · auto-renew off"}
                  </p>
                </div>
              </li>
            )
          })}
        </ul>
      </CardContent>
    </Card>
  )
}

function inDays(daysLeft: number): string {
  if (daysLeft <= 0) return "today"
  if (daysLeft === 1) return "tomorrow"
  return `in ${daysLeft} days`
}
