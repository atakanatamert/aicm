import { Link, useNavigate, useSearchParams } from "react-router"
import { ArrowDown, ArrowUp, ArrowUpDown, Search } from "lucide-react"
import type { components } from "@/api/schema"
import { formatDate, formatMoney, formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"
import { renewalUrgency } from "@/components/quotas/renewal-urgency"
import { StatusDot } from "@/components/status-dot"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"

type Account = components["schemas"]["Account"]
type ProviderInfo = components["schemas"]["ProviderInfo"]

const DAY_MS = 24 * 60 * 60 * 1000

const SORT_KEYS = ["name", "provider", "renews_on"] as const
type SortKey = (typeof SORT_KEYS)[number]

const collator = new Intl.Collator(undefined, { sensitivity: "base", numeric: true })

/** What to show for the credential: last 4, or where the key lives. */
function keyText(a: Account): string {
  if (a.auth_kind === "api_key" || a.auth_kind === "admin_key") {
    return a.secret.last4 ? `•••• ${a.secret.last4}` : "not stored"
  }
  // agent_capture: the key never left the machine. none: nothing to store.
  return a.auth_kind === "agent_capture" ? "agent" : "manual"
}

function providerName(a: Account, providers: ProviderInfo[]): string {
  return providers.find((p) => p.id === a.provider_id)?.name ?? a.provider_id
}

/**
 * `?sort=` holds the column, with a `-` prefix for descending. Anything
 * unknown sorts by name ascending — the default.
 */
function parseSort(raw: string | null): { key: SortKey; desc: boolean } {
  const key = SORT_KEYS.find((k) => k === raw?.replace(/^-/, ""))
  if (key === undefined) return { key: "name", desc: false }
  return { key, desc: raw?.startsWith("-") ?? false }
}

function SortableHead({
  label,
  sortKey,
  sort,
  onSort,
}: {
  label: string
  sortKey: SortKey
  sort: { key: SortKey; desc: boolean }
  onSort: (key: SortKey) => void
}) {
  const active = sort.key === sortKey
  return (
    <TableHead aria-sort={active ? (sort.desc ? "descending" : "ascending") : undefined}>
      <button
        type="button"
        className="inline-flex items-center gap-1 font-medium hover:text-foreground"
        onClick={() => onSort(sortKey)}
      >
        {label}
        {active ? (
          sort.desc ? (
            <ArrowDown aria-hidden className="size-3.5" />
          ) : (
            <ArrowUp aria-hidden className="size-3.5" />
          )
        ) : (
          <ArrowUpDown aria-hidden className="size-3.5 text-muted-foreground/60" />
        )}
      </button>
    </TableHead>
  )
}

/**
 * The tracked accounts, one row each; rows open the account detail page.
 * Search (?q=) filters by name or provider, the Name/Provider/Renews on
 * headers sort (?sort=), both in the URL.
 */
export function AccountsTable({ accounts, providers }: { accounts: Account[]; providers: ProviderInfo[] }) {
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const qRaw = searchParams.get("q") ?? ""
  const q = qRaw.toLowerCase()
  const sort = parseSort(searchParams.get("sort"))

  const setParam = (key: "q" | "sort", value: string | null) => {
    const next = new URLSearchParams(searchParams)
    if (value === null) next.delete(key)
    else next.set(key, value)
    setSearchParams(next, { replace: true })
  }

  const onSort = (key: SortKey) => setParam("sort", sort.key === key && !sort.desc ? `-${key}` : key)

  const rows = accounts
    .filter(
      (a) =>
        q === "" ||
        a.name.toLowerCase().includes(q) ||
        providerName(a, providers).toLowerCase().includes(q),
    )
    .sort((a, b) => {
      let cmp = 0
      if (sort.key === "provider") {
        cmp = collator.compare(providerName(a, providers), providerName(b, providers))
      } else if (sort.key === "renews_on") {
        // Accounts without a renewal date keep sitting at the bottom.
        const ar = a.plan.renews_on
        const br = b.plan.renews_on
        if (!ar && !br) cmp = 0
        else if (!ar) cmp = 1
        else if (!br) cmp = -1
        else cmp = ar < br ? (sort.desc ? 1 : -1) : ar > br ? (sort.desc ? -1 : 1) : 0
      } else {
        cmp = collator.compare(a.name, b.name) * (sort.desc ? -1 : 1)
      }
      return cmp !== 0 ? cmp : collator.compare(a.name, b.name)
    })

  return (
    <div className="flex flex-col gap-3">
      <div>
        <div className="relative max-w-xs">
          <Search aria-hidden className="absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={qRaw}
            onChange={(e) => setParam("q", e.target.value || null)}
            placeholder="Search name or provider"
            aria-label="Search accounts by name or provider"
            className="pl-8"
          />
        </div>
      </div>
      <div className="overflow-x-auto rounded-xl border">
        <Table>
          <TableHeader>
            <TableRow>
              <SortableHead label="Name" sortKey="name" sort={sort} onSort={onSort} />
              <SortableHead label="Provider" sortKey="provider" sort={sort} onSort={onSort} />
              <TableHead>Plan</TableHead>
              <TableHead className="text-right">Price</TableHead>
              <SortableHead label="Renews on" sortKey="renews_on" sort={sort} onSort={onSort} />
              <TableHead>Key</TableHead>
              <TableHead>Last sync</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.length === 0 ? (
              <TableRow>
                <TableCell colSpan={7} className="text-sm text-muted-foreground">
                  No accounts match the search.
                </TableCell>
              </TableRow>
            ) : (
              rows.map((a) => {
                const renewal = a.plan.renews_on
                  ? renewalUrgency({
                      days_left: Math.ceil((new Date(a.plan.renews_on).getTime() - Date.now()) / DAY_MS),
                      auto_renew: a.plan.auto_renew,
                    })
                  : undefined
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
                    <TableCell>{providerName(a, providers)}</TableCell>
                    <TableCell>{a.plan.name || "–"}</TableCell>
                    <TableCell className="text-right whitespace-nowrap tabular-nums">
                      {a.plan.price ? formatMoney(a.plan.price) : "–"}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">
                      <span className={cn(renewal?.urgent && "font-medium text-urgent")}>
                        {a.plan.renews_on ? formatDate(a.plan.renews_on) : "–"}
                      </span>
                      {renewal?.reason && <span className="block text-xs text-muted-foreground">{renewal.reason}</span>}
                    </TableCell>
                    <TableCell className="whitespace-nowrap">{keyText(a)}</TableCell>
                    <TableCell className="whitespace-nowrap">
                      {failing ? (
                        <StatusDot tone="bad" title={a.sync.last_error ?? undefined}>
                          Sync failing
                        </StatusDot>
                      ) : a.sync.last_synced_at ? (
                        <StatusDot tone="ok">Synced {formatRelative(a.sync.last_synced_at)}</StatusDot>
                      ) : a.auth_kind === "api_key" || a.auth_kind === "admin_key" ? (
                        <StatusDot tone="idle">Not synced yet</StatusDot>
                      ) : (
                        <StatusDot tone="ok">Via agent</StatusDot>
                      )}
                    </TableCell>
                  </TableRow>
                )
              })
            )}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}
