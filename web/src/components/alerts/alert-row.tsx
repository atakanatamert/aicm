import { useState } from "react"
import { AlarmClock, CircleAlert, Info, TriangleAlert } from "lucide-react"
import { toast } from "sonner"
import { errorOf, query } from "@/api/client"
import type { components } from "@/api/schema"
import { invalidateAlertQueries } from "@/components/alerts/queries"
import { ProviderLinks } from "@/components/quotas/provider-links"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

type Alert = components["schemas"]["Alert"]
type AlertKind = components["schemas"]["AlertKind"]
type Links = NonNullable<Alert["links"]>

// The one link that leads, by alert kind: money that is running out gets its
// action button; every other kind keeps its links in the menu.
const PRIMARY_BY_KIND: Partial<Record<AlertKind, keyof Links>> = {
  low_balance: "top_up",
  renewal: "manage_subscription",
  promo_ending: "manage_subscription",
}

function SeverityIcon({ alert }: { alert: Alert }) {
  if (alert.severity === "critical") {
    return <CircleAlert aria-label="Critical" className="mt-0.5 size-4 shrink-0 text-urgent" />
  }
  if (alert.severity === "warning") {
    return <TriangleAlert aria-label="Warning" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
  }
  return <Info aria-label="Info" className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
}

function SnoozeMenu({ onSnooze }: { onSnooze: (iso: string | null) => void }) {
  const [untilOpen, setUntilOpen] = useState(false)
  const [until, setUntil] = useState("")
  const inOneDay = new Date(Date.now() + 24 * 3600 * 1000).toISOString()
  const inOneWeek = new Date(Date.now() + 7 * 24 * 3600 * 1000).toISOString()

  const saveUntil = () => {
    if (!until) return
    // Local midnight: the alert reappears the moment the chosen day starts.
    onSnooze(new Date(`${until}T00:00:00`).toISOString())
    setUntilOpen(false)
  }

  return (
    <>
      <DropdownMenu>
        <DropdownMenuTrigger asChild>
          <Button variant="ghost" size="sm">
            <AlarmClock />
            Snooze
          </Button>
        </DropdownMenuTrigger>
        <DropdownMenuContent align="start">
          <DropdownMenuItem onSelect={() => onSnooze(inOneDay)}>For 1 day</DropdownMenuItem>
          <DropdownMenuItem onSelect={() => onSnooze(inOneWeek)}>For 1 week</DropdownMenuItem>
          <DropdownMenuSeparator />
          <DropdownMenuItem onSelect={() => setUntilOpen(true)}>Until date…</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>
      <Dialog open={untilOpen} onOpenChange={setUntilOpen}>
        <DialogContent className="sm:max-w-sm" showCloseButton={false}>
          <DialogHeader>
            <DialogTitle>Snooze until a date</DialogTitle>
            <DialogDescription>The alert reappears on the morning of that day.</DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="snooze-until">Until date</Label>
            <Input
              id="snooze-until"
              type="date"
              value={until}
              onChange={(e) => setUntil(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button variant="outline" size="sm" onClick={() => setUntilOpen(false)}>
              Cancel
            </Button>
            <Button size="sm" disabled={!until} onClick={saveUntil}>
              Snooze
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  )
}

export function AlertRow({ alert, accountName }: { alert: Alert; accountName?: string }) {
  const patch = query.useMutation("patch", "/api/alerts/{alertId}", {
    onSuccess: () => invalidateAlertQueries(),
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not update the alert."),
  })
  const alertPath = { params: { path: { alertId: alert.id } } }

  return (
    <article className="flex gap-3 border-b py-3">
      <SeverityIcon alert={alert} />
      <div className="min-w-0 flex-1">
        <div className="flex items-baseline gap-2">
          <h3 className={cn("min-w-0 truncate text-sm", alert.read ? "font-normal text-muted-foreground" : "font-medium")}>
            {alert.title}
          </h3>
          <time dateTime={alert.created_at} className="ml-auto shrink-0 text-xs text-muted-foreground">
            {formatRelative(alert.created_at)}
          </time>
        </div>
        {alert.body && <p className="mt-0.5 text-sm text-muted-foreground">{alert.body}</p>}
        <div className="mt-1 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
          {accountName && <span className="truncate">{accountName}</span>}
          {alert.snoozed_until && (
            <Badge variant="outline">
              Snoozed · reappears {formatRelative(alert.snoozed_until)}
            </Badge>
          )}
        </div>
        <div className="mt-2 flex flex-wrap items-center gap-1.5">
          {/* Mapped kinds pin their link; the rest have no primary at all. */}
          <ProviderLinks links={alert.links} primaryKey={PRIMARY_BY_KIND[alert.kind] ?? null} />
          {!alert.read && (
            <Button
              variant="ghost"
              size="sm"
              disabled={patch.isPending}
              onClick={() => patch.mutate({ ...alertPath, body: { read: true } })}
            >
              Mark read
            </Button>
          )}
          <SnoozeMenu
            onSnooze={(iso) => patch.mutate({ ...alertPath, body: { snoozed_until: iso } })}
          />
          {alert.snoozed_until && (
            <Button
              variant="ghost"
              size="sm"
              disabled={patch.isPending}
              onClick={() => patch.mutate({ ...alertPath, body: { snoozed_until: null } })}
            >
              Unsnooze
            </Button>
          )}
        </div>
      </div>
      {!alert.read && (
        <span
          role="img"
          aria-label="Unread"
          className={cn("mt-1.5 size-2 shrink-0 rounded-full", alert.severity === "critical" ? "bg-urgent" : "bg-foreground")}
        />
      )}
    </article>
  )
}
