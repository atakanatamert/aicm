import type { components } from "@/api/schema"
import { ProviderLinks } from "./provider-links"
import { hasWalletMeter } from "./wallet-meter"
import { MeterRow } from "./meter-row"
import { SyncButton } from "./sync-button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader } from "@/components/ui/card"
import { formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

type AccountQuota = components["schemas"]["AccountQuota"]
type MeterView = components["schemas"]["MeterView"]

/** The meter that sets the account's headroom, falling back to the most expiring one. */
function bindingMeter(quota: AccountQuota): MeterView | undefined {
  const byKey = quota.score.binding_meter
    ? quota.meters.find((m) => m.def.key === quota.score.binding_meter)
    : undefined
  return (
    byKey ??
    [...quota.meters].sort((a, b) => a.score.headroom - b.score.headroom)[0]
  )
}

// "Unused before reset: Weekly limit, 31%" — only when a meaningful share of
// the window would expire unused.
function expiringLine(quota: AccountQuota): string | undefined {
  if (quota.meters.length === 0) return undefined
  const share = quota.score.expiring_share
  if (share < 0.1) return undefined
  const expiring = [...quota.meters].sort(
    (a, b) => b.score.expiring_share - a.score.expiring_share,
  )[0]
  return `Unused before reset: ${expiring.def.label}, ${Math.round(share * 100)}%`
}

/** One line of text saying why the card is urgent; never colour alone. */
function urgentReason(quota: AccountQuota): string | undefined {
  if (!quota.score.feasible) return "Runs out before it resets at current use"
  if (quota.score.headroom < 0.3) {
    const hours = bindingMeter(quota)?.score.hours_to_empty
    return hours !== null && hours !== undefined
      ? hours >= 48
        ? `Runs out in about ${Math.round(hours / 24)} days`
        : `Runs out in about ${Math.round(hours)} h`
      : "Less than 30% left before reset"
  }
  return undefined
}

// "synced 3 h ago"; a never-synced account with readings says when they were
// captured instead.
function syncText(quota: AccountQuota): string | undefined {
  if (quota.account.last_synced_at) return `synced ${formatRelative(quota.account.last_synced_at)}`
  const captured = quota.meters
    .map((m) => m.latest?.captured_at)
    .filter((at): at is string => at !== undefined)
    .sort()
  return captured.length > 0 ? `updated ${formatRelative(captured[captured.length - 1])}` : "not synced yet"
}

// One account: provider-native meters, provider links, and for editors a sync.
// The urgent border marks infeasible accounts or headroom below 0.3, and the
// reason line next to it says which.
export function QuotaCard({ quota, canEdit }: { quota: AccountQuota; canEdit: boolean }) {
  const { account } = quota
  const reason = urgentReason(quota)
  const subtitle = [
    account.provider_name,
    account.plan.name,
    syncText(quota),
  ]
    .filter(Boolean)
    .join(" · ")
  const binding = bindingMeter(quota)
  const expiring = expiringLine(quota)
  return (
    <Card className={cn("gap-4 py-5", reason && "border-urgent")}>
      <CardHeader>
        <h3 className="text-base leading-none font-semibold">{account.name}</h3>
        <CardDescription>{subtitle}</CardDescription>
        {reason && <p className="text-xs font-medium text-urgent">{reason}</p>}
        {binding && quota.meters.length > 0 && (
          <p className="text-xs text-muted-foreground">Tightest: {binding.def.label}</p>
        )}
        {expiring && <p className="text-xs text-muted-foreground">{expiring}</p>}
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
        <ProviderLinks
          links={account.links}
          wallet={hasWalletMeter(quota.meters)}
          renewal={account.plan.renews_on != null}
        />
        {canEdit && <SyncButton accountId={account.id} />}
      </CardFooter>
    </Card>
  )
}
