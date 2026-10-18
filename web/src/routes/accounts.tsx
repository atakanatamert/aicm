import { useState } from "react"
import { Plus } from "lucide-react"
import { errorOf, query } from "@/api/client"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { AccountsTable } from "@/components/accounts/accounts-table"
import { AddAccountDialog } from "@/components/accounts/add-account-dialog"
import { ReviewSection } from "@/components/accounts/review"
import { Button } from "@/components/ui/button"
import { Vault } from "@/components/ui/hairline"
import { Skeleton } from "@/components/ui/skeleton"
import { useMe } from "@/hooks/use-me"

export default function Accounts() {
  const { canEdit, isLoading: meLoading } = useMe()
  const [addOpen, setAddOpen] = useState(false)
  const pending = query.useQuery("get", "/api/accounts", { params: { query: { status: "pending" } } })
  const active = query.useQuery("get", "/api/accounts")
  const providers = query.useQuery("get", "/api/providers")

  return (
    <>
      <PageHeader
        title="Accounts"
        description="Provider subscriptions and API accounts."
        actions={
          canEdit && (
            <Button onClick={() => setAddOpen(true)} disabled={providers.isPending}>
              <Plus />
              Add account
            </Button>
          )
        }
      />
      <div className="mx-auto flex w-full max-w-6xl flex-col gap-6 px-4 py-6 sm:px-6">
        {(pending.data?.length ?? 0) > 0 && (
          <ReviewSection accounts={pending.data ?? []} providers={providers.data ?? []} canEdit={canEdit} />
        )}

        {active.isPending || meLoading ? (
          <div className="flex flex-col gap-2">
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
            <Skeleton className="h-10 w-full" />
          </div>
        ) : active.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {errorOf(active.error)?.error.message ?? "Could not load accounts. Check that the hub is running."}
          </p>
        ) : active.data.length === 0 ? (
          <EmptyState
            figure={<Vault play />}
            title="No accounts yet"
            description="Provider accounts appear here once the agent discovers them or you add one."
          />
        ) : (
          <AccountsTable accounts={active.data} providers={providers.data ?? []} />
        )}
      </div>

      <AddAccountDialog open={addOpen} onOpenChange={setAddOpen} providers={providers.data ?? []} />
    </>
  )
}
