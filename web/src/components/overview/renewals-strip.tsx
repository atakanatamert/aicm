import { Link } from "react-router"
import type { components } from "@/api/schema"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { formatBilledAndConverted, formatDate } from "@/lib/format"

type Renewal = components["schemas"]["Renewal"]

/** Subscriptions renewing within 7 days; details and actions live in Quotas. */
export function RenewalsStrip({ renewals }: { renewals: Renewal[] }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Renews this week</CardTitle>
        <CardDescription>
          Renewing within 7 days.{" "}
          <Link to="/quotas" className="underline underline-offset-2 hover:text-foreground">
            Open Quotas
          </Link>
        </CardDescription>
      </CardHeader>
      <CardContent>
        <ul className="grid gap-2 sm:grid-cols-2 xl:grid-cols-3">
          {renewals.map((renewal) => (
            <li key={renewal.account.id} className="flex items-center justify-between gap-3 rounded-md border px-3 py-2">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium">{renewal.account.name}</p>
                <p className="text-sm text-muted-foreground">{renewal.account.provider_name}</p>
              </div>
              <div className="text-right">
                <p className="text-sm font-medium text-urgent">{formatDate(renewal.renews_on)}</p>
                <p className="text-xs text-muted-foreground tabular-nums">
                  {inDays(renewal.days_left)}
                  {renewal.price ? ` · ${formatBilledAndConverted(renewal.price, renewal.converted)}` : ""}
                  {renewal.auto_renew ? "" : " · auto-renew off"}
                </p>
              </div>
            </li>
          ))}
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
