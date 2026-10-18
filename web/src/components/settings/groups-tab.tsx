import { useState, type FormEvent } from "react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { EmptyState } from "@/components/empty-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Branches } from "@/components/ui/hairline"
import { TableSkeleton } from "./fields"
import { messageOf } from "./helpers"

type Group = components["schemas"]["Group"]
type User = components["schemas"]["User"]

function refresh() {
  queryClient.invalidateQueries({ queryKey: ["get", "/api/groups"] })
  queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
}

export function GroupsTab() {
  const groups = query.useQuery("get", "/api/groups")
  const users = query.useQuery("get", "/api/users")
  const [editing, setEditing] = useState<Group | null>(null)
  const [createOpen, setCreateOpen] = useState(false)
  const [deleteTarget, setDeleteTarget] = useState<Group | null>(null)

  const emailsOf = (group: Group): string =>
    group.member_ids
      .map((id) => users.data?.find((u) => u.id === id))
      .filter((u): u is User => u !== undefined)
      .map((u) => u.email)
      .join(", ")

  return (
    <section className="py-6">
      <div className="mb-4 flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          Groups receive grants as a whole: add an account to a group and everyone in it can use it.
        </p>
        <Button onClick={() => setCreateOpen(true)}>New group</Button>
      </div>

      {groups.isPending || users.isPending ? (
        <TableSkeleton />
      ) : groups.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {messageOf(groups.error, "Could not load the groups.")}
        </p>
      ) : groups.data.length === 0 ? (
        <EmptyState
          figure={<Branches play />}
          title="No groups yet"
          description="Create a group to grant accounts to several people at once."
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Members</TableHead>
                <TableHead className="text-right">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {groups.data.map((group) => (
                <TableRow key={group.id}>
                  <TableCell className="text-sm font-medium">{group.name}</TableCell>
                  <TableCell className="max-w-md text-sm text-muted-foreground">
                    {group.member_ids.length === 0 ? (
                      <span>No members</span>
                    ) : (
                      <span title={emailsOf(group)}>{emailsOf(group)}</span>
                    )}
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1">
                      <Button variant="ghost" size="sm" onClick={() => setEditing(group)}>
                        Edit
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => setDeleteTarget(group)}>
                        Delete
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <GroupDialog
        key={editing?.id ?? "new"}
        group={editing}
        open={createOpen || editing !== null}
        onOpenChange={(open) => {
          if (!open) {
            setCreateOpen(false)
            setEditing(null)
          }
        }}
      />
      <DeleteGroupDialog group={deleteTarget} onDone={() => setDeleteTarget(null)} />
    </section>
  )
}

function GroupDialog({
  group,
  open,
  onOpenChange,
}: {
  group: Group | null
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const users = query.useQuery("get", "/api/users")
  const [name, setName] = useState(group?.name ?? "")
  const [members, setMembers] = useState<Set<string>>(() => new Set(group?.member_ids ?? []))
  const [error, setError] = useState("")

  const create = query.useMutation("post", "/api/groups", {
    onSuccess: (created) => {
      toast(`Group “${created.name}” created.`)
      refresh()
      onOpenChange(false)
    },
    onError: (err) => setError(messageOf(err, "Could not create the group.")),
  })
  const update = query.useMutation("put", "/api/groups/{groupId}", {
    onSuccess: (updated) => {
      toast(`Group “${updated.name}” saved.`)
      refresh()
      onOpenChange(false)
    },
    onError: (err) => setError(messageOf(err, "Could not save the group.")),
  })

  const toggle = (id: string, checked: boolean) => {
    setMembers((prev) => {
      const next = new Set(prev)
      if (checked) next.add(id)
      else next.delete(id)
      return next
    })
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setError("")
    const body = { name, member_ids: [...members] }
    if (group) update.mutate({ params: { path: { groupId: group.id } }, body })
    else create.mutate({ body })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <DialogHeader>
            <DialogTitle>{group ? `Edit group ${group.name}` : "New group"}</DialogTitle>
            <DialogDescription>
              {group ? "Rename the group and change who is in it." : "Name the group and pick its members."}
            </DialogDescription>
          </DialogHeader>
          <div className="grid gap-2">
            <Label htmlFor="group-name">Name</Label>
            <Input
              id="group-name"
              required
              maxLength={100}
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label>Members</Label>
            {users.data && users.data.length > 0 ? (
              <div className="grid max-h-56 gap-0.5 overflow-y-auto rounded-md border p-1.5">
                {users.data.map((user) => (
                  <label
                    key={user.id}
                    className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-accent"
                  >
                    <Checkbox
                      checked={members.has(user.id)}
                      onCheckedChange={(checked) => toggle(user.id, checked === true)}
                      aria-label={`Add ${user.email} to the group`}
                    />
                    <span className="min-w-0 flex-1 truncate">
                      {user.email}
                      {user.name && <span className="text-muted-foreground"> — {user.name}</span>}
                    </span>
                    {user.disabled && <Badge variant="outline">Disabled</Badge>}
                  </label>
                ))}
              </div>
            ) : (
              <p className="text-sm text-muted-foreground">No users to add yet.</p>
            )}
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending || update.isPending || name.length === 0}>
              {create.isPending || update.isPending ? "Saving…" : group ? "Save group" : "Create group"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function DeleteGroupDialog({ group, onDone }: { group: Group | null; onDone: () => void }) {
  const del = query.useMutation("delete", "/api/groups/{groupId}", {
    onSuccess: () => {
      toast(`Group deleted.`)
      refresh()
      onDone()
    },
    onError: (err) => {
      toast.error(messageOf(err, "Could not delete the group."))
      onDone()
    },
  })

  return (
    <Dialog
      open={group !== null}
      onOpenChange={(open) => {
        if (!open) onDone()
      }}
    >
      <DialogContent className="sm:max-w-sm">
        <DialogHeader>
          <DialogTitle>Delete group {group?.name}?</DialogTitle>
          <DialogDescription>
            Its grants are removed too. Members keep their own access.
          </DialogDescription>
        </DialogHeader>
        <DialogFooter>
          <Button variant="ghost" onClick={onDone}>
            Cancel
          </Button>
          <Button
            variant="destructive"
            disabled={del.isPending}
            onClick={() => group && del.mutate({ params: { path: { groupId: group.id } } })}
          >
            {del.isPending ? "Deleting…" : "Delete group"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
