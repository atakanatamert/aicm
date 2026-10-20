import { useState } from "react"
import { X } from "lucide-react"
import { toast } from "sonner"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { ConfirmDialog } from "@/components/machines/confirm-dialog"
import { StatusDot } from "@/components/status-dot"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatDate, formatRelative } from "@/lib/format"

type EnrollToken = components["schemas"]["EnrollToken"]

/** Past its expiry (and not revoked): it can no longer enroll machines. */
function isExpired(token: EnrollToken): boolean {
  return token.expires_at !== null && token.expires_at !== undefined && new Date(token.expires_at).getTime() <= Date.now()
}

function ExpiresCell({ token }: { token: EnrollToken }) {
  if (token.revoked_at) return <StatusDot tone="idle">Revoked</StatusDot>
  if (isExpired(token)) return <StatusDot tone="bad">Expired</StatusDot>
  if (!token.expires_at) return <StatusDot tone="ok">Permanent</StatusDot>
  return (
    <span className="text-sm text-muted-foreground">
      {formatDate(token.expires_at)}{" "}
      <span className="tabular-nums">({formatRelative(token.expires_at)})</span>
    </span>
  )
}

/** Admin view of the org's enrollment tokens: created, expires, uses, revoke. */
export function EnrollTokens() {
  const tokens = query.useQuery("get", "/api/enroll-tokens")
  const [revoking, setRevoking] = useState<EnrollToken | undefined>(undefined)
  const revoke = query.useMutation("delete", "/api/enroll-tokens/{enrollTokenId}", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/api/enroll-tokens"] })
      setRevoking(undefined)
    },
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not revoke the token."),
  })

  return (
    <section aria-label="Enroll tokens">
      <h2 className="text-base font-semibold">Enroll tokens</h2>
      <p className="mb-3 text-sm text-muted-foreground">
        Tokens from “Add a machine”. A revoked token no longer enrolls new machines; enrolled machines stay enrolled.
      </p>
      {tokens.isPending ? (
        <div className="grid max-w-xl gap-2">
          {Array.from({ length: 2 }, (_, i) => (
            <Skeleton key={i} className="h-8 w-full" />
          ))}
        </div>
      ) : tokens.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {errorOf(tokens.error)?.error.message ?? "Could not load the enrollment tokens."}
        </p>
      ) : tokens.data.length === 0 ? (
        <p className="text-sm text-muted-foreground">No enrollment tokens yet.</p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Created</TableHead>
              <TableHead>Expires</TableHead>
              <TableHead>Uses</TableHead>
              <TableHead className="w-10">
                <span className="sr-only">Actions</span>
              </TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {tokens.data.map((token) => (
              <TableRow key={token.id}>
                <TableCell className="text-muted-foreground" title={formatDate(token.created_at)}>
                  {formatRelative(token.created_at)}
                </TableCell>
                <TableCell>
                  <ExpiresCell token={token} />
                </TableCell>
                <TableCell className="tabular-nums">{token.use_count}</TableCell>
                <TableCell>
                  {!token.revoked_at && !isExpired(token) && (
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      className="group/revoke"
                      aria-label="Revoke token"
                      onClick={() => setRevoking(token)}
                    >
                      <X className="text-muted-foreground group-hover/revoke:text-destructive group-focus-visible/revoke:text-destructive" />
                    </Button>
                  )}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
      <ConfirmDialog
        open={revoking !== undefined}
        onOpenChange={(open) => !open && setRevoking(undefined)}
        title="Revoke this token?"
        description="New machines can no longer enroll with it. Machines already enrolled stay enrolled."
        confirmLabel="Revoke"
        pending={revoke.isPending}
        destructive
        onConfirm={() => revoking && revoke.mutate({ params: { path: { enrollTokenId: revoking.id } } })}
      />
    </section>
  )
}
