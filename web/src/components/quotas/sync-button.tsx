import { useEffect, useState } from "react"
import { errorOf, query, queryClient } from "@/api/client"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

// How long to watch for the queued sync to finish before giving up.
const WATCH_MS = 60_000

// Editor-only "Sync now", shown only for providers the hub can fetch from:
// subscriptions read by an agent update on their own. After queueing it
// watches the account until last_synced_at moves, then refreshes the page's
// data. 429 too_soon (whose message carries the retry time) shows inline.
export function SyncButton({
  accountId,
  providerId,
  lastSyncedAt,
}: {
  accountId: string
  providerId: string
  /** The account's last_synced_at when the page loaded; a new value means done. */
  lastSyncedAt: string | null | undefined
}) {
  const sync = query.useMutation("post", "/api/accounts/{accountId}/sync")
  const providers = query.useQuery("get", "/api/providers", undefined, { staleTime: Infinity })
  const [note, setNote] = useState<{ text: string; error: boolean } | null>(null)
  const [queuedAt, setQueuedAt] = useState<number | null>(null)
  const account = query.useQuery(
    "get",
    "/api/accounts/{accountId}",
    { params: { path: { accountId } } },
    { enabled: queuedAt !== null, refetchInterval: queuedAt !== null ? 2000 : false },
  )

  const syncedAt = account.data?.sync.last_synced_at
  const failure = account.data?.sync.last_error
  // Changes on every poll, so the time-out check below runs each time.
  const polledAt = account.dataUpdatedAt
  useEffect(() => {
    if (queuedAt === null) return
    // Compare with the value before the click, not the browser clock.
    const done = syncedAt !== undefined && syncedAt !== null && syncedAt !== (lastSyncedAt ?? null)
    const timedOut = Date.now() - queuedAt > WATCH_MS
    if (!done && !timedOut) return
    setQueuedAt(null)
    if (done) {
      setNote(failure ? { text: `Sync failed: ${failure}`, error: true } : { text: "Synced just now.", error: false })
      for (const path of ["/api/quotas", "/api/accounts", "/api/accounts/{accountId}", "/api/usage", "/api/spend"]) {
        void queryClient.invalidateQueries({ queryKey: ["get", path] })
      }
    } else {
      setNote({ text: "Still syncing; the page updates when it finishes.", error: false })
    }
  }, [queuedAt, syncedAt, failure, lastSyncedAt, polledAt])

  const caps = providers.data?.find((p) => p.id === providerId)?.capabilities
  if (!caps || !(caps.usage || caps.quota || caps.charges)) return null

  async function onSync() {
    setNote(null)
    try {
      await sync.mutateAsync({ params: { path: { accountId } } })
      setNote({ text: "Syncing…", error: false })
      setQueuedAt(Date.now())
    } catch (err) {
      setNote({ text: errorOf(err)?.error.message ?? "Sync failed.", error: true })
    }
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <Button variant="outline" size="sm" disabled={sync.isPending || queuedAt !== null} onClick={() => void onSync()}>
        Sync now
      </Button>
      {note && (
        <p role="status" className={cn("text-xs", note.error ? "text-destructive" : "text-muted-foreground")}>
          {note.text}
        </p>
      )}
    </div>
  )
}
