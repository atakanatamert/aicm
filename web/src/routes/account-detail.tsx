import { Link, useParams } from "react-router"
import type { components } from "@/api/schema"
import { query } from "@/api/client"
import { AccessCard } from "@/components/account-detail/access-card"
import { ArchiveCard } from "@/components/account-detail/archive-card"
import { BudgetCard } from "@/components/account-detail/budget-card"
import { ChargesCard } from "@/components/account-detail/charges-card"
import { KeyCard } from "@/components/account-detail/key-card"
import { MachinesCard } from "@/components/account-detail/machines-card"
import { MetersCard } from "@/components/account-detail/meters-card"
import { PlanCard } from "@/components/account-detail/plan-card"
import { ErrorLine, SectionCard } from "@/components/account-detail/section"
import { UsageCard } from "@/components/account-detail/usage-card"
import { ProviderLinks } from "@/components/quotas/provider-links"
import { hasWalletMeter } from "@/components/quotas/wallet-meter"
import { SyncButton } from "@/components/quotas/sync-button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { useMe } from "@/hooks/use-me"
import { useProviderNames } from "@/hooks/use-provider-names"
import { formatDate } from "@/lib/format"

type Account = components["schemas"]["Account"]
type AccountQuota = components["schemas"]["AccountQuota"]

function DetailSkeleton() {
  return (
    <div aria-hidden>
      <div className="space-y-3 border-b px-6 py-5">
        <Skeleton className="h-3 w-24" />
        <Skeleton className="h-6 w-64" />
        <Skeleton className="h-8 w-full max-w-md" />
      </div>
      <div className="mx-auto w-full max-w-6xl space-y-6 px-4 py-6 sm:px-6">
        <div className="grid gap-6 md:grid-cols-2">
          <Skeleton className="h-48" />
          <Skeleton className="h-48" />
        </div>
        <Skeleton className="h-72" />
      </div>
    </div>
  )
}

function AccountHeader({ account, wallet }: { account: Account; wallet: boolean }) {
  const { canEdit } = useMe()
  const providerName = useProviderNames()(account.provider_id)
  return (
    <div className="border-b px-6 py-5">
      <Link to="/accounts" className="text-sm text-muted-foreground underline-offset-2 hover:text-foreground hover:underline">
        Accounts
      </Link>
      <div className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1.5">
        <h1 className="text-lg font-semibold tracking-tight">{account.name}</h1>
        <Badge variant="secondary">{providerName}</Badge>
        <Badge variant="outline">{account.archived_at ? "Archived" : account.status}</Badge>
        {account.plan.renews_on && <span className="text-sm text-muted-foreground">renews {formatDate(account.plan.renews_on)}</span>}
      </div>
      <div className="mt-3 flex flex-wrap items-start gap-2">
        <ProviderLinks
          links={account.links}
          wallet={wallet}
          renewal={account.plan.renews_on != null}
        />
        {canEdit && <SyncButton accountId={account.id} providerId={account.provider_id} lastSyncedAt={account.sync.last_synced_at} />}
      </div>
    </div>
  )
}

export default function AccountDetail() {
  const { id } = useParams()
  const { canEdit, isAdmin } = useMe()
  const accountId = id ?? ""

  const account = query.useQuery("get", "/api/accounts/{accountId}", {
    params: { path: { accountId } },
  })
  const quota = query.useQuery("get", "/api/quotas", {
    params: { query: { account_id: accountId } },
  })

  if (account.isPending) return <DetailSkeleton />
  if (account.isError) {
    return (
      <div className="px-6 py-6">
        <ErrorLine error={account.error} fallback="Could not load this account." />
        <p className="mt-2 text-sm">
          <Link to="/accounts" className="underline underline-offset-2">
            Back to accounts
          </Link>
        </p>
      </div>
    )
  }

  const data = account.data
  const accountQuota: AccountQuota | undefined = quota.data?.find((q) => q.account.id === data.id)

  return (
    <>
      <AccountHeader account={data} wallet={hasWalletMeter(accountQuota?.meters)} />
      <div className="mx-auto w-full max-w-6xl space-y-6 px-4 py-6 sm:px-6">
        <div className="grid gap-6 md:grid-cols-2">
          <SectionCard title="Plan & renewal" description="What you pay and when it renews.">
            <PlanCard account={data} canEdit={canEdit} />
          </SectionCard>
          <SectionCard title="Key" description="The credential the hub syncs with.">
            <KeyCard account={data} canEdit={canEdit} />
          </SectionCard>
        </div>

        <BudgetCard account={data} />

        <MetersCard account={data} quota={accountQuota} canEdit={canEdit} />
        <MachinesCard accountId={data.id} />

        <div className="grid gap-6 lg:grid-cols-2">
          <ChargesCard accountId={data.id} />
          <UsageCard accountId={data.id} />
        </div>

        {isAdmin && (
          <div className="grid gap-6 lg:grid-cols-2">
            <AccessCard accountId={data.id} />
            <ArchiveCard accountId={data.id} accountName={data.name} />
          </div>
        )}
      </div>
    </>
  )
}
