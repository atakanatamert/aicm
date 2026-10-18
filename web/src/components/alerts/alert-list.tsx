import { useState } from "react"
import { toast } from "sonner"
import { errorOf, query } from "@/api/client"
import { AlertRow } from "@/components/alerts/alert-row"
import { invalidateAlertQueries } from "@/components/alerts/queries"
import { EmptyState } from "@/components/empty-state"
import { Button } from "@/components/ui/button"
import { Relay } from "@/components/ui/hairline"
import { Skeleton } from "@/components/ui/skeleton"

const PAGE_SIZE = 25

function InboxSkeleton() {
  return (
    <div className="py-4">
      <div className="grid gap-3">
        {Array.from({ length: 5 }, (_, i) => (
          <div key={i} className="flex items-start gap-3">
            <Skeleton className="size-4 rounded-full" />
            <div className="grid w-full gap-1.5">
              <Skeleton className="h-4 w-2/5" />
              <Skeleton className="h-3 w-4/5" />
              <Skeleton className="h-3 w-1/4" />
            </div>
          </div>
        ))}
      </div>
    </div>
  )
}

interface AlertListProps {
  accountNames: Record<string, string>
}

export function AlertList({ accountNames }: AlertListProps) {
  const [unreadOnly, setUnreadOnly] = useState(false)

  const alerts = query.useInfiniteQuery(
    "get",
    "/api/alerts",
    { params: { query: { limit: PAGE_SIZE, unread: unreadOnly || undefined } } },
    {
      // Empty string, not undefined: the generated client merges the page
      // param into the query, and the hub reads cursor="" as "first page".
      initialPageParam: "" as string,
      getNextPageParam: (last) => last.next_cursor ?? undefined,
    },
  )
  const markAllRead = query.useMutation("post", "/api/alerts/read-all", {
    onSuccess: () => invalidateAlertQueries(),
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not mark the alerts read."),
  })

  const items = alerts.data?.pages.flatMap((page) => page.items) ?? []

  return (
    <section aria-label="Alert inbox">
      <div className="flex flex-wrap items-center gap-2 border-b py-3">
        <Button
          variant={unreadOnly ? "default" : "outline"}
          size="sm"
          aria-pressed={unreadOnly}
          onClick={() => setUnreadOnly((v) => !v)}
        >
          Unread only
        </Button>
        <Button
          variant="outline"
          size="sm"
          disabled={markAllRead.isPending}
          onClick={() => markAllRead.mutate()}
        >
          {markAllRead.isPending ? "Marking…" : "Mark all read"}
        </Button>
        {alerts.data && (
          <span className="ml-auto text-xs text-muted-foreground">
            {alerts.data.pages[0]?.unread_count ?? 0} unread
          </span>
        )}
      </div>

      {alerts.isPending ? (
        <InboxSkeleton />
      ) : alerts.isError ? (
        <p role="alert" className="py-6 text-sm text-destructive">
          {errorOf(alerts.error)?.error.message ?? "Could not load the inbox."}
        </p>
      ) : items.length === 0 ? (
        <EmptyState
          figure={<Relay play />}
          title={unreadOnly ? "No unread alerts" : "No alerts yet"}
          description={
            unreadOnly
              ? "Everything in the inbox has been read."
              : "Alerts land here when a renewal comes close, a meter runs low or a machine goes quiet."
          }
        />
      ) : (
        <>
          <div>
            {items.map((alert) => (
              <AlertRow key={alert.id} alert={alert} accountName={alert.account_id ? accountNames[alert.account_id] : undefined} />
            ))}
          </div>
          {alerts.hasNextPage && (
            <div className="flex justify-center py-4">
              <Button
                variant="outline"
                size="sm"
                disabled={alerts.isFetchingNextPage}
                onClick={() => void alerts.fetchNextPage()}
              >
                {alerts.isFetchingNextPage ? "Loading…" : "Load more"}
              </Button>
            </div>
          )}
        </>
      )}
    </section>
  )
}
