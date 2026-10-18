import { useState } from "react"
import { Pencil, X } from "lucide-react"
import { toast } from "sonner"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { ConfirmDialog } from "@/components/machines/confirm-dialog"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { formatRelative } from "@/lib/format"
import { cn } from "@/lib/utils"

type Machine = components["schemas"]["Machine"]

function OnlineCell({ machine }: { machine: Machine }) {
  if (machine.revoked_at) {
    return <Badge variant="outline">Revoked</Badge>
  }
  return (
    <span className="inline-flex items-center gap-1.5 text-xs text-muted-foreground">
      <span
        aria-hidden
        className={cn("size-2 rounded-full", machine.online ? "bg-foreground" : "bg-muted-foreground/30")}
      />
      {machine.online ? "Online" : "Offline"}
    </span>
  )
}

function NameCell({ machine, canEdit }: { machine: Machine; canEdit: boolean }) {
  const rename = query.useMutation("patch", "/api/machines/{machineId}", {
    onSuccess: () => queryClient.invalidateQueries({ queryKey: ["get", "/api/machines"] }),
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not rename the machine."),
  })
  const [editing, setEditing] = useState(false)
  const [draft, setDraft] = useState(machine.name)

  if (!canEdit || machine.revoked_at) {
    return <span className="font-medium">{machine.name}</span>
  }

  const save = () => {
    setEditing(false)
    const name = draft.trim()
    if (name === "" || name === machine.name) return
    rename.mutate({ params: { path: { machineId: machine.id } }, body: { name } })
  }

  if (editing) {
    return (
      <Input
        aria-label="Machine name"
        className="h-8 w-44"
        autoFocus
        value={draft}
        disabled={rename.isPending}
        onChange={(e) => setDraft(e.target.value)}
        onBlur={save}
        onKeyDown={(e) => {
          if (e.key === "Enter") save()
          if (e.key === "Escape") {
            setDraft(machine.name)
            setEditing(false)
          }
        }}
      />
    )
  }

  return (
    <span className="inline-flex items-center gap-1">
      <span className="font-medium">{machine.name}</span>
      <Button
        variant="ghost"
        size="icon-xs"
        aria-label={`Rename ${machine.name}`}
        onClick={() => {
          setDraft(machine.name)
          setEditing(true)
        }}
      >
        <Pencil />
      </Button>
    </span>
  )
}

interface MachineTableProps {
  machines: Machine[]
  isAdmin: boolean
}

export function MachineTable({ machines, isAdmin }: MachineTableProps) {
  const [revoking, setRevoking] = useState<Machine | undefined>(undefined)
  const revoke = query.useMutation("patch", "/api/machines/{machineId}", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/api/machines"] })
      setRevoking(undefined)
    },
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not revoke the machine."),
  })

  return (
    <>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>Name</TableHead>
            <TableHead>Hostname</TableHead>
            <TableHead>OS / arch</TableHead>
            <TableHead>Agent</TableHead>
            <TableHead>Status</TableHead>
            <TableHead>Last seen</TableHead>
            <TableHead>Harnesses</TableHead>
            {isAdmin && <TableHead className="w-10">
              <span className="sr-only">Actions</span>
            </TableHead>}
          </TableRow>
        </TableHeader>
        <TableBody>
          {machines.map((machine) => (
            <TableRow key={machine.id}>
              <TableCell>
                <NameCell machine={machine} canEdit={isAdmin} />
              </TableCell>
              <TableCell className="text-muted-foreground">{machine.hostname}</TableCell>
              <TableCell className="tabular-nums">
                {machine.os}/{machine.arch}
              </TableCell>
              <TableCell className="text-muted-foreground">{machine.agent_version}</TableCell>
              <TableCell>
                <OnlineCell machine={machine} />
              </TableCell>
              <TableCell className="text-muted-foreground" title={machine.last_seen_at ?? undefined}>
                {machine.last_seen_at ? formatRelative(machine.last_seen_at) : "Never"}
              </TableCell>
              <TableCell className="text-muted-foreground">
                {machine.harnesses?.length ? machine.harnesses.join(", ") : "–"}
              </TableCell>
              {isAdmin && (
                <TableCell>
                  {!machine.revoked_at && (
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      aria-label={`Revoke ${machine.name}`}
                      onClick={() => setRevoking(machine)}
                    >
                      <X className="text-destructive" />
                    </Button>
                  )}
                </TableCell>
              )}
            </TableRow>
          ))}
        </TableBody>
      </Table>
      <ConfirmDialog
        open={revoking !== undefined}
        onOpenChange={(open) => !open && setRevoking(undefined)}
        title={`Revoke ${revoking?.name ?? ""}?`}
        description="The agent is disconnected and cannot reconnect until you re-enroll the machine."
        confirmLabel="Revoke"
        pending={revoke.isPending}
        destructive
        onConfirm={() =>
          revoking && revoke.mutate({ params: { path: { machineId: revoking.id } }, body: { revoked: true } })
        }
      />
    </>
  )
}
