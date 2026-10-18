import { useState, type FormEvent } from "react"
import type { components } from "@/api/schema"
import { query, queryClient } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Label } from "@/components/ui/label"
import { ErrorLine, SectionCard } from "./section"

type AccountGrants = components["schemas"]["AccountGrants"]
type Group = components["schemas"]["Group"]
type User = components["schemas"]["User"]

function toggleId(ids: Set<string>, id: string, checked: boolean) {
  const next = new Set(ids)
  if (checked) next.add(id)
  else next.delete(id)
  return next
}

/** The grant form itself; remounted with fresh grants via `key`. */
function GrantsForm({ accountId, grants }: { accountId: string; grants: AccountGrants }) {
  const users = query.useQuery("get", "/api/users")
  const groups = query.useQuery("get", "/api/groups")
  const [userIds, setUserIds] = useState<Set<string>>(new Set(grants.user_ids))
  const [groupIds, setGroupIds] = useState<Set<string>>(new Set(grants.group_ids))
  const [saved, setSaved] = useState(false)

  const put = query.useMutation("put", "/api/accounts/{accountId}/grants", {
    onSuccess: () => {
      setSaved(true)
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts/{accountId}/grants"] })
    },
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setSaved(false)
    const body: AccountGrants = { user_ids: [...userIds], group_ids: [...groupIds] }
    put.mutate({ params: { path: { accountId } }, body })
  }

  return (
    <form onSubmit={submit} className="space-y-4">
      <div className="grid gap-2">
        <Label>Users</Label>
        {users.data && users.data.length > 0 ? (
          <div className="grid max-h-44 gap-0.5 overflow-y-auto rounded-md border p-1.5">
            {users.data.map((user: User) => (
              <label key={user.id} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-accent">
                <Checkbox
                  checked={userIds.has(user.id)}
                  onCheckedChange={(c) => setUserIds(toggleId(userIds, user.id, c === true))}
                  aria-label={`Grant ${user.email} access`}
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
          <p className="text-sm text-muted-foreground">No other users.</p>
        )}
      </div>
      <div className="grid gap-2">
        <Label>Groups</Label>
        {groups.data && groups.data.length > 0 ? (
          <div className="grid max-h-44 gap-0.5 overflow-y-auto rounded-md border p-1.5">
            {groups.data.map((group: Group) => (
              <label key={group.id} className="flex cursor-pointer items-center gap-2.5 rounded-md px-2 py-1.5 text-sm hover:bg-accent">
                <Checkbox
                  checked={groupIds.has(group.id)}
                  onCheckedChange={(c) => setGroupIds(toggleId(groupIds, group.id, c === true))}
                  aria-label={`Grant the ${group.name} group access`}
                />
                <span className="min-w-0 flex-1 truncate">{group.name}</span>
                <span className="text-xs text-muted-foreground">{group.member_ids.length} members</span>
              </label>
            ))}
          </div>
        ) : (
          <p className="text-sm text-muted-foreground">No groups yet. Create one in Settings.</p>
        )}
      </div>
      {put.isError && <ErrorLine error={put.error} fallback="Could not save access." />}
      {saved && put.isSuccess && <p role="status" className="text-sm text-muted-foreground">Access saved.</p>}
      <div>
        <Button type="submit" size="sm" disabled={put.isPending}>
          {put.isPending ? "Saving…" : "Save access"}
        </Button>
      </div>
    </form>
  )
}

/** Who can see this account (admins). The lists include admins implicitly. */
export function AccessCard({ accountId }: { accountId: string }) {
  const grants = query.useQuery("get", "/api/accounts/{accountId}/grants", { params: { path: { accountId } } })
  const pending = grants.isPending
  const error = grants.isError ? <ErrorLine error={grants.error} fallback="Could not load grants." /> : null

  return (
    <SectionCard title="Access" description="Who can see this account. Admins always can.">
      {pending ? (
        <div className="space-y-2" aria-hidden>
          <div className="h-16 rounded-md bg-muted" />
          <div className="h-16 rounded-md bg-muted" />
        </div>
      ) : (
        <>
          {error}
          {grants.data && <GrantsForm accountId={accountId} grants={grants.data} />}
        </>
      )}
    </SectionCard>
  )
}
