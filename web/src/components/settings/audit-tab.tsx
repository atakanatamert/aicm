import { Fragment, useState } from "react"
import { ChevronDown } from "lucide-react"
import { query } from "@/api/client"
import type { components } from "@/api/schema"
import { EmptyState } from "@/components/empty-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Relay } from "@/components/ui/hairline"
import { JsonBlock, TableSkeleton } from "./fields"
import { messageOf } from "./helpers"

type AuditEntry = components["schemas"]["AuditEntry"]

const PAGE_SIZE = 50

// Audit actions in the user's words; unknown keys fall back to the raw key.
const ACTION_LABEL: Record<string, string> = {
  "account.archive": "Archived account",
  "account.confirm": "Confirmed account",
  "account.create": "Created account",
  "account.discover": "Discovered account",
  "account.dismiss": "Dismissed account",
  "account.renew": "Renewed account",
  "account.sync": "Synced account",
  "account.update": "Updated account",
  "alert.read_all": "Marked alerts read",
  "alert.update": "Updated alert",
  "alert_rule.upsert": "Updated alert rule",
  "charge.confirm": "Confirmed charge",
  "charge.create": "Created charge",
  "charge.dismiss": "Dismissed charge",
  "charge.update": "Updated charge",
  "enroll_token.create": "Created enroll token",
  "enroll_token.revoke": "Revoked enroll token",
  "group.create": "Created group",
  "group.delete": "Deleted group",
  "group.update": "Updated group",
  "machine.enroll": "Enrolled machine",
  "machine.rename": "Renamed machine",
  "machine.revoke": "Revoked machine",
  "meter.upsert": "Updated meter",
  "reading.create": "Recorded reading",
  "secret.set": "Set secret",
  "settings.update": "Updated settings",
  "user.create": "Created user",
  "user.password_change": "Changed password",
  "user.update": "Updated user",
}

/** The target's name from the change itself, when the row carries one. */
function targetName(entry: AuditEntry): string | undefined {
  for (const side of [entry.after, entry.before]) {
    const name = side?.name
    if (typeof name === "string" && name !== "") return name
  }
  return undefined
}

// Local time with seconds: audit entries are about accountability, so the
// exact moment matters and the relative form ("2 days ago") does not.
const dateTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "medium" })

export function AuditTab() {
  // A stack of cursors: undefined is page one, each Next pushes the page's
  // next_cursor, Previous pops. Newest first within every page.
  const [cursors, setCursors] = useState<(string | undefined)[]>([undefined])
  const [openId, setOpenId] = useState<number | null>(null)
  const cursor = cursors[cursors.length - 1]
  const page = query.useQuery("get", "/api/audit", { params: { query: { limit: PAGE_SIZE, cursor } } })
  const next = page.data?.next_cursor ?? null

  return (
    <section className="py-6">
      <div className="mb-4 flex flex-wrap items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          Every change, newest first: who did what to what, with what it looked like before.
        </p>
        <div className="flex gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={cursors.length === 1 || page.isFetching}
            onClick={() => {
              setOpenId(null)
              setCursors((prev) => prev.slice(0, -1))
            }}
          >
            Previous
          </Button>
          <Button
            variant="outline"
            size="sm"
            disabled={next === null || page.isFetching}
            onClick={() => {
              setOpenId(null)
              setCursors((prev) => [...prev, next as string])
            }}
          >
            Next
          </Button>
        </div>
      </div>

      {page.isPending ? (
        <TableSkeleton />
      ) : page.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {messageOf(page.error, "Could not load the audit log.")}
        </p>
      ) : page.data.items.length === 0 ? (
        <EmptyState
          figure={<Relay play />}
          title="Nothing in the audit log yet"
          description="Changes you and your tokens make show up here."
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-44">Time</TableHead>
                <TableHead>Actor</TableHead>
                <TableHead>Action</TableHead>
                <TableHead>Target</TableHead>
                <TableHead className="w-12 text-right">
                  <span className="sr-only">Details</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {page.data.items.map((entry) => (
                <AuditRow
                  key={entry.id}
                  entry={entry}
                  open={openId === entry.id}
                  onToggle={() => setOpenId(openId === entry.id ? null : entry.id)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}
    </section>
  )
}

function AuditRow({ entry, open, onToggle }: { entry: AuditEntry; open: boolean; onToggle: () => void }) {
  const expandable = entry.before !== null || entry.after !== null
  return (
    <Fragment>
      <TableRow className={open ? "bg-muted/30" : undefined}>
        <TableCell className="whitespace-nowrap text-sm">
          {dateTime.format(new Date(entry.occurred_at))}
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-2">
            <span className="max-w-48 truncate text-sm" title={entry.actor_label}>
              {entry.actor_label}
            </span>
            {entry.actor_kind !== "user" && <Badge variant="outline">{entry.actor_kind}</Badge>}
          </div>
        </TableCell>
        <TableCell className="text-sm" title={entry.action}>
          {ACTION_LABEL[entry.action] ?? entry.action}
        </TableCell>
        <TableCell className="text-sm text-muted-foreground">
          <span className="font-mono text-xs">{entry.target_kind}</span>{" "}
          {targetName(entry) ?? (
            <span className="font-mono text-xs" title={entry.target_id}>
              {entry.target_id.length > 14 ? `${entry.target_id.slice(0, 12)}…` : entry.target_id}
            </span>
          )}
        </TableCell>
        <TableCell className="text-right">
          {expandable && (
            <Button
              variant="ghost"
              size="icon-xs"
              aria-expanded={open}
              aria-label={`${open ? "Hide" : "Show"} before and after for ${entry.action}`}
              onClick={onToggle}
            >
              <ChevronDown className={`transition-transform duration-150 ${open ? "rotate-180" : ""}`} />
            </Button>
          )}
        </TableCell>
      </TableRow>
      {open && (
        <TableRow>
          <TableCell colSpan={5} className="bg-muted/30">
            <div className="grid gap-3 py-1 md:grid-cols-2">
              <JsonBlock label="Before" value={entry.before} />
              <JsonBlock label="After" value={entry.after} />
            </div>
          </TableCell>
        </TableRow>
      )}
    </Fragment>
  )
}
