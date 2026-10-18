import { Link } from "react-router"
import { errorOf, query } from "@/api/client"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { QuotaCard } from "@/components/quotas/quota-card"
import { RenewalCard } from "@/components/quotas/renewal-card"
import { AGENT_READS_TEXT } from "@/components/machines/enroll-commands"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Terrain } from "@/components/ui/hairline"
import { useMe } from "@/hooks/use-me"

const CONTAINER = "mx-auto w-full max-w-6xl px-4 sm:px-6"

function QuotasSkeleton() {
  return (
    <div className={`${CONTAINER} space-y-10 py-6`} aria-hidden>
      <section className="space-y-3">
        <Skeleton className="h-4 w-44" />
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-64" />
          ))}
        </div>
      </section>
      <section className="space-y-3">
        <Skeleton className="h-4 w-32" />
        <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} className="h-64" />
          ))}
        </div>
      </section>
    </div>
  )
}

function ErrorLine({ error }: { error: unknown }) {
  return (
    <p role="alert" className="text-sm text-destructive">
      {errorOf(error)?.error.message ?? "Could not load this section."}
    </p>
  )
}

// One card per account in the API's urgency order, then renewals for the next
// 30 days (the Overview already leads with this week's renewals).
export default function Quotas() {
  const { canEdit } = useMe()
  const quotas = query.useQuery("get", "/api/quotas")
  const renewals = query.useQuery("get", "/api/renewals", { params: { query: { within_days: 30 } } })
  const machines = query.useQuery("get", "/api/machines")
  const noMachines = machines.isSuccess && machines.data.length === 0

  if (quotas.isPending || renewals.isPending) return <QuotasSkeleton />

  const noAccounts = (quotas.data?.length ?? 0) === 0 && (renewals.data?.length ?? 0) === 0

  return (
    <>
      <PageHeader title="Quotas" description="Provider meters and renewals, sorted by urgency." />
      <div className={`${CONTAINER} space-y-10 py-6`}>
        {noAccounts ? (
          <div>
            <EmptyState
              figure={<Terrain play />}
              title="No accounts yet"
              description={
                noMachines
                  ? AGENT_READS_TEXT
                  : "Add a provider account and its meters and renewals appear here."
              }
            />
            <div className="flex justify-center">
              {noMachines && canEdit ? (
                <Button asChild variant="outline" size="sm">
                  <Link to="/">Import from your computer</Link>
                </Button>
              ) : (
                <Button asChild>
                  <Link to="/accounts">Go to accounts</Link>
                </Button>
              )}
            </div>
          </div>
        ) : (
          <>
            <section aria-labelledby="quotas-heading" className="space-y-3">
              <h2 id="quotas-heading" className="text-sm font-medium text-muted-foreground">
                Quotas
              </h2>
              {quotas.isError ? (
                <ErrorLine error={quotas.error} />
              ) : quotas.data.length === 0 ? (
                <p className="text-sm text-muted-foreground">No accounts to show.</p>
              ) : (
                <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                  {quotas.data.map((quota) => (
                    <QuotaCard key={quota.account.id} quota={quota} canEdit={canEdit} />
                  ))}
                </div>
              )}
            </section>
            <section aria-labelledby="renewals-heading" className="space-y-3">
              <h2 id="renewals-heading" className="text-sm font-medium text-muted-foreground">
                Renewals · next 30 days
              </h2>
              {renewals.isError ? (
                <ErrorLine error={renewals.error} />
              ) : renewals.data.length === 0 ? (
                <p className="text-sm text-muted-foreground">Nothing renews in the next 30 days.</p>
              ) : (
                <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
                  {renewals.data.map((renewal) => (
                    <RenewalCard key={renewal.account.id} renewal={renewal} />
                  ))}
                </div>
              )}
            </section>
          </>
        )}
      </div>
    </>
  )
}
