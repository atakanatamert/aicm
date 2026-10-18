import { useState } from "react"
import { errorOf, query, queryClient } from "@/api/client"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"

// Editor-only "Sync now". 409 nothing_to_sync and 429 too_soon (whose message
// carries the retry time) show inline under the button.
export function SyncButton({ accountId }: { accountId: string }) {
  const sync = query.useMutation("post", "/api/accounts/{accountId}/sync")
  const [note, setNote] = useState<{ text: string; error: boolean } | null>(null)

  async function onSync() {
    setNote(null)
    try {
      await sync.mutateAsync({ params: { path: { accountId } } })
      setNote({ text: "Sync queued.", error: false })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/quotas"] })
    } catch (err) {
      setNote({ text: errorOf(err)?.error.message ?? "Sync failed.", error: true })
    }
  }

  return (
    <div className="flex flex-col items-start gap-1">
      <Button variant="outline" size="xs" disabled={sync.isPending} onClick={() => void onSync()}>
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
