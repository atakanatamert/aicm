import type { components } from "@/api/schema"
import { MeterRow } from "./meter-row"
import { SyncButton } from "./sync-button"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

type AccountQuota = components["schemas"]["AccountQuota"]

const LINKS: { key: keyof AccountQuota["account"]["links"]; label: string }[] = [
  { key: "top_up", label: "Top up" },
  { key: "billing", label: "Billing" },
  { key: "manage_subscription", label: "Manage subscription" },
  { key: "usage", label: "Usage" },
]

// "Weakest: Weekly limit · 30% of the 5-hour window expires unused" — the
// headroom-setting meter plus the meter expiring the biggest share.
function scoreLine(quota: AccountQuota): string | undefined {
  if (quota.meters.length === 0) return undefined
  const binding = quota.score.binding_meter
    ? quota.meters.find((m) => m.def.key === quota.score.binding_meter)
    : undefined
  const expiring = [...quota.meters].sort(
    (a, b) => (b.score.expiring_share ?? 0) - (a.score.expiring_share ?? 0),
  )[0]
  const weakest = binding?.def.label ?? expiring?.def.label
  if (!weakest) return undefined
  const share = quota.score.expiring_share
  if (expiring && share > 0) {
    return `Weakest: ${weakest} · ${Math.round(share * 100)}% of the ${expiring.def.label} expires unused`
  }
  return `Weakest: ${weakest}`
}

// One account: provider-native meters, provider links, and for editors a sync.
// The urgent border marks infeasible accounts or headroom below 0.3.
export function QuotaCard({ quota, canEdit }: { quota: AccountQuota; canEdit: boolean }) {
  const { account, score: accountScore } = quota
  const urgent = !accountScore.feasible || accountScore.headroom < 0.3
  const score = scoreLine(quota)
  const subtitle = [
    account.provider_name,
    account.plan.name,
    account.last_synced_at ? `synced ${formatRelative(account.last_synced_at)}` : "never",
  ]
    .filter(Boolean)
    .join(" · ")
  return (
    <Card className={cn("gap-4 py-5", urgent && "border-urgent")}>
      <CardHeader>
        <CardTitle className="text-base">{account.name}</CardTitle>
        <CardDescription>{subtitle}</CardDescription>
        {score && <p className="text-xs text-muted-foreground">{score}</p>}
      </CardHeader>
      <CardContent>
        {quota.meters.length > 0 ? (
          <ul className="space-y-4">
            {quota.meters.map((meter) => (
              <MeterRow key={meter.def.key} meter={meter} />
            ))}
          </ul>
        ) : (
          <p className="text-sm text-muted-foreground">No meters on this account.</p>
        )}
      </CardContent>
      <CardFooter className="flex-wrap gap-2">
        {LINKS.map(({ key, label }) =>
          account.links[key] ? (
            <Button key={key} asChild variant="outline" size="xs">
              <a href={account.links[key]!} target="_blank" rel="noreferrer">
                {label}
              </a>
            </Button>
          ) : null,
        )}
        {canEdit && <SyncButton accountId={account.id} />}
      </CardFooter>
    </Card>
  )
}
