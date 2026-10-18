import { query } from "@/api/client"
import { EmptyState } from "@/components/empty-state"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Laptop } from "@/components/ui/hairline"
import { HARNESS_NAMES } from "@/lib/harness-names"
import { formatDate, formatRelative } from "@/lib/format"
import { ErrorLine, SectionCard } from "./section"

const absoluteTime = new Intl.DateTimeFormat(undefined, { dateStyle: "medium", timeStyle: "short" })

/** Machines and harnesses that report this account. */
export function MachinesCard({ accountId }: { accountId: string }) {
  const machines = query.useQuery("get", "/api/accounts/{accountId}/machines", {
    params: { path: { accountId } },
  })

  return (
    <SectionCard title="Machines" description="Where this account has been seen.">
      {machines.isPending ? (
        <div className="space-y-2" aria-hidden>
          <div className="h-8 rounded-md bg-muted" />
          <div className="h-8 rounded-md bg-muted" />
        </div>
      ) : machines.isError ? (
        <ErrorLine error={machines.error} fallback="Could not load machines." />
      ) : machines.data.length === 0 ? (
        <EmptyState
          figure={<Laptop play />}
          title="No machine yet"
          description="Enroll a machine with the agent and it shows up here once it reports this account."
          className="py-6"
        />
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Machine</TableHead>
              <TableHead>Harness</TableHead>
              <TableHead>First seen</TableHead>
              <TableHead>Last seen</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {machines.data.map((m) => (
              <TableRow key={`${m.machine_id}-${m.harness}`}>
                <TableCell className="font-medium">{m.machine_name}</TableCell>
                <TableCell>
                  <Badge variant="secondary">{HARNESS_NAMES[m.harness] ?? m.harness}</Badge>
                </TableCell>
                <TableCell className="whitespace-nowrap text-muted-foreground">{formatDate(m.first_seen_at)}</TableCell>
                <TableCell className="whitespace-nowrap">
                  <span title={absoluteTime.format(new Date(m.last_seen_at))}>{formatRelative(m.last_seen_at)}</span>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </SectionCard>
  )
}
