import { Link, useNavigate } from "react-router"
import type { components } from "@/api/schema"
import { formatDate, formatMoney, formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

type Account = components["schemas"]["Account"]
type ProviderInfo = components["schemas"]["ProviderInfo"]

const RENEW_SOON_MS = 7 * 24 * 60 * 60 * 1000

/** What to show for the credential: last 4, or where the key lives. */
function keyText(a: Account): string {
  if (a.auth_kind === "api_key" || a.auth_kind === "admin_key") {
    return a.secret.last4 ? `•••• ${a.secret.last4}` : "manual"
  }
  // agent_capture: the key never left the machine. none: nothing to store.
  return a.auth_kind === "agent_capture" ? "agent" : "manual"
}

/** The tracked accounts, one row each; rows open the account detail page. */
export function AccountsTable({ accounts, providers }: { accounts: Account[]; providers: ProviderInfo[] }) {
  const navigate = useNavigate()
  return (
    <div className="overflow-x-auto rounded-xl border">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Provider</TableHead>
            <TableHead>Plan</TableHead>
            <TableHead className="text-right">Price</TableHead>
            <TableHead>Renews on</TableHead>
            <TableHead>Key</TableHead>
            <TableHead>Last sync</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {accounts.map((a) => {
            const provider = providers.find((p) => p.id === a.provider_id)
            const renewsSoon =
              a.plan.renews_on != null && new Date(a.plan.renews_on).getTime() - Date.now() <= RENEW_SOON_MS
            const failing = a.sync.failing_since != null
            return (
              <TableRow
                key={a.id}
                className="cursor-pointer"
                onClick={() => navigate(`/accounts/${a.id}`)}
              >
                <TableCell>
                  <Link
                    to={`/accounts/${a.id}`}
                    className="font-medium underline-offset-2 hover:underline"
                    onClick={(e) => e.stopPropagation()}
                  >
                    {a.name}
                  </Link>
                </TableCell>
                <TableCell>{provider?.name ?? a.provider_id}</TableCell>
                <TableCell>{a.plan.name || "–"}</TableCell>
                <TableCell className="text-right whitespace-nowrap tabular-nums">
                  {a.plan.price ? formatMoney(a.plan.price) : "–"}
                </TableCell>
                <TableCell className={cn("whitespace-nowrap", renewsSoon && "font-medium text-urgent")}>
                  {a.plan.renews_on ? formatDate(a.plan.renews_on) : "–"}
                </TableCell>
                <TableCell className="whitespace-nowrap">{keyText(a)}</TableCell>
                <TableCell className="whitespace-nowrap">
                  <span
                    className="inline-flex items-center gap-1.5"
                    title={a.sync.last_error ?? undefined}
                  >
                    <span
                      aria-hidden
                      className={cn("size-1.5 shrink-0 rounded-full", failing ? "bg-urgent" : "bg-muted-foreground/40")}
                    />
                    {a.sync.last_synced_at ? `Synced ${formatRelative(a.sync.last_synced_at)}` : "Never synced"}
                  </span>
                </TableCell>
              </TableRow>
            )
          })}
        </TableBody>
      </Table>
    </div>
  )
}
