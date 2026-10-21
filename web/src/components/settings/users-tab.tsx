import { useState, type FormEvent } from "react"
import { MoreHorizontal, RefreshCw } from "lucide-react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { ConfirmDialog } from "@/components/machines/confirm-dialog"
import { EmptyState } from "@/components/empty-state"
import { StatusDot } from "@/components/status-dot"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Switch } from "@/components/ui/switch"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Lockers } from "@/components/ui/hairline"
import { formatRelative } from "@/lib/format"
import { useMe } from "@/hooks/use-me"
import { CopyField, TableSkeleton } from "./fields"
import { generatePassword, messageOf, toastMutationError } from "./helpers"

type User = components["schemas"]["User"]
type Role = components["schemas"]["Role"]

const ROLES: { value: Role; label: string }[] = [
  { value: "admin", label: "Admin" },
  { value: "editor", label: "Editor" },
  { value: "viewer", label: "Viewer" },
]

function refresh() {
  queryClient.invalidateQueries({ queryKey: ["get", "/api/users"] })
  queryClient.invalidateQueries({ queryKey: ["get", "/api/groups"] })
  queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
}

export function UsersTab() {
  const { isAdmin } = useMe()
  const users = query.useQuery("get", "/api/users")
  const [inviteOpen, setInviteOpen] = useState(false)
  const [resetTarget, setResetTarget] = useState<User | null>(null)
  const [totpTarget, setTotpTarget] = useState<User | null>(null)

  return (
    <section className="py-6">
      <div className="mb-4 flex items-center justify-between gap-4">
        <p className="text-sm text-muted-foreground">
          Admins manage everything, editors change data, viewers only look. Invited people sign in with the
          temporary password you share with them.
        </p>
        <Button onClick={() => setInviteOpen(true)}>
          Invite user
        </Button>
      </div>

      {users.isPending ? (
        <TableSkeleton />
      ) : users.isError ? (
        <p role="alert" className="text-sm text-destructive">
          {messageOf(users.error, "Could not load the users.")}
        </p>
      ) : users.data.length === 0 ? (
        <EmptyState
          figure={<Lockers play />}
          title="No users yet"
          description="Invite the people who should see or manage this organization."
        />
      ) : (
        <div className="overflow-x-auto rounded-lg border">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Email</TableHead>
                <TableHead>Name</TableHead>
                <TableHead>Role</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Last login</TableHead>
                <TableHead className="text-right">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {users.data.map((user) => (
                <UserRow
                  key={user.id}
                  user={user}
                  isAdmin={isAdmin}
                  onReset={() => setResetTarget(user)}
                  onDisableTotp={() => setTotpTarget(user)}
                />
              ))}
            </TableBody>
          </Table>
        </div>
      )}

      <InviteDialog open={inviteOpen} onOpenChange={setInviteOpen} />
      <ResetPasswordDialog user={resetTarget} onDone={() => setResetTarget(null)} />
      <ResetTotpDialog user={totpTarget} onDone={() => setTotpTarget(null)} />
    </section>
  )
}

function UserRow({ user, isAdmin, onReset, onDisableTotp }: { user: User; isAdmin: boolean; onReset: () => void; onDisableTotp: () => void }) {
  const patch = query.useMutation("patch", "/api/users/{userId}", {
    onSuccess: (updated) => {
      toast.success(`Saved ${updated.email}.`)
      setConfirming(undefined)
      refresh()
    },
    onError: (err) => {
      toastMutationError(err, "Could not save the user.")
      setConfirming(undefined)
      refresh()
    },
  })
  // Set when the change is destructive enough to ask first: disabling a user
  // or demoting an admin.
  const [confirming, setConfirming] = useState<{ title: string; description: string; body: { role?: Role; disabled?: boolean } } | undefined>(undefined)

  return (
    <>
      <TableRow>
        <TableCell className="text-sm">
          <span className="flex flex-wrap items-center gap-1.5">
            {user.email}
            {user.totp_enabled && (
              <Badge variant="secondary" aria-label="Two-factor sign-in is on">
                2FA
              </Badge>
            )}
            {user.sso_linked && (
              <Badge variant="secondary" aria-label="Signs in with single sign-on">
                SSO
              </Badge>
            )}
          </span>
        </TableCell>
        <TableCell className="text-sm">{user.name || <span className="text-muted-foreground">—</span>}</TableCell>
        <TableCell>
          <Select
            value={user.role}
            onValueChange={(role) => {
              const next = role as Role
              if (user.role === "admin" && next !== "admin") {
                setConfirming({
                  title: `Demote ${user.email}?`,
                  description: "They lose admin access to users, groups, settings and the audit log.",
                  body: { role: next },
                })
              } else {
                patch.mutate({ params: { path: { userId: user.id } }, body: { role: next } })
              }
            }}
          >
            <SelectTrigger size="sm" className="w-28" aria-label={`Role for ${user.email}`}>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {ROLES.map((role) => (
                <SelectItem key={role.value} value={role.value}>
                  {role.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </TableCell>
        <TableCell>
          <div className="flex items-center gap-2">
            <Switch
              checked={!user.disabled}
              aria-label={`Account active for ${user.email}`}
              disabled={patch.isPending}
              onCheckedChange={(active) => {
                if (active) {
                  patch.mutate({ params: { path: { userId: user.id } }, body: { disabled: false } })
                } else {
                  setConfirming({
                    title: `Disable ${user.email}?`,
                    description: "They sign out and can no longer sign in until an admin enables them again.",
                    body: { disabled: true },
                  })
                }
              }}
            />
            <span className="text-xs">
              {user.disabled ? <StatusDot tone="bad">Disabled</StatusDot> : <StatusDot tone="ok">Active</StatusDot>}
            </span>
          </div>
        </TableCell>
        <TableCell className="text-sm text-muted-foreground">
          {user.last_login_at ? formatRelative(user.last_login_at) : "Never"}
        </TableCell>
        <TableCell className="text-right">
          {isAdmin ? (
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <Button variant="ghost" size="icon" aria-label={`Actions for ${user.email}`}>
                  <MoreHorizontal />
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem onSelect={onReset}>Reset password</DropdownMenuItem>
                {user.totp_enabled && (
                  <DropdownMenuItem variant="destructive" onSelect={onDisableTotp}>
                    Turn off two-factor
                  </DropdownMenuItem>
                )}
              </DropdownMenuContent>
            </DropdownMenu>
          ) : (
            <Button variant="ghost" size="sm" onClick={onReset}>
              Reset password
            </Button>
          )}
        </TableCell>
      </TableRow>
      <ConfirmDialog
        open={confirming !== undefined}
        onOpenChange={(open) => !open && setConfirming(undefined)}
        title={confirming?.title ?? ""}
        description={confirming?.description ?? ""}
        confirmLabel="Confirm"
        pending={patch.isPending}
        onConfirm={() => confirming && patch.mutate({ params: { path: { userId: user.id } }, body: confirming.body })}
      />
    </>
  )
}

/** Shared temporary-password field: generated, editable, regenerable. */
function TempPasswordInput({ id, value, onChange }: { id: string; value: string; onChange: (v: string) => void }) {
  return (
    <div className="grid gap-2">
      <Label htmlFor={id}>Temporary password</Label>
      <div className="flex gap-2">
        <Input
          id={id}
          className="font-mono"
          autoComplete="off"
          minLength={12}
          required
          value={value}
          onChange={(e) => onChange(e.target.value)}
        />
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label="Generate a new password"
          onClick={() => onChange(generatePassword())}
        >
          <RefreshCw />
        </Button>
      </div>
      <p className="text-xs text-muted-foreground">At least 12 characters. One was generated for you.</p>
    </div>
  )
}

function InviteDialog({ open, onOpenChange }: { open: boolean; onOpenChange: (open: boolean) => void }) {
  const [email, setEmail] = useState("")
  const [name, setName] = useState("")
  const [role, setRole] = useState<Role>("viewer")
  const [password, setPassword] = useState(generatePassword())
  const [error, setError] = useState("")
  const [invited, setInvited] = useState<{ email: string; password: string } | null>(null)

  const create = query.useMutation("post", "/api/users", {
    onSuccess: (user) => {
      setInvited({ email: user.email, password })
      refresh()
    },
    onError: (err) => setError(messageOf(err, "Could not create the user.")),
  })

  const close = (next: boolean) => {
    onOpenChange(next)
    if (!next) {
      setEmail("")
      setName("")
      setRole("viewer")
      setPassword(generatePassword())
      setError("")
      setInvited(null)
    }
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setError("")
    create.mutate({ body: { email, name, password, role } })
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent className="sm:max-w-md">
        {invited ? (
          <>
            <DialogHeader>
              <DialogTitle>{invited.email} was invited</DialogTitle>
              <DialogDescription>
                Share this temporary password with them — it is shown only this once. They can change it under
                Profile after signing in.
              </DialogDescription>
            </DialogHeader>
            <CopyField label="Temporary password" value={invited.password} />
            <DialogFooter>
              <Button onClick={() => close(false)}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={submit} className="grid gap-4" noValidate>
            <DialogHeader>
              <DialogTitle>Invite user</DialogTitle>
              <DialogDescription>
                Creates the account with a temporary password instead of sending an email.
              </DialogDescription>
            </DialogHeader>
            <div className="grid gap-2">
              <Label htmlFor="invite-email">Email</Label>
              <Input
                id="invite-email"
                type="email"
                autoComplete="off"
                required
                value={email}
                onChange={(e) => setEmail(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="invite-name">Name</Label>
              <Input id="invite-name" autoComplete="off" value={name} onChange={(e) => setName(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="invite-role">Role</Label>
              <Select value={role} onValueChange={(value) => setRole(value as Role)}>
                <SelectTrigger id="invite-role" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {ROLES.map((r) => (
                    <SelectItem key={r.value} value={r.value}>
                      {r.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <TempPasswordInput id="invite-password" value={password} onChange={setPassword} />
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => close(false)}>
                Cancel
              </Button>
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? "Creating…" : "Create user"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

function ResetPasswordDialog({ user, onDone }: { user: User | null; onDone: () => void }) {
  const [password, setPassword] = useState(generatePassword())
  const [reset, setReset] = useState<{ email: string; password: string } | null>(null)
  const [error, setError] = useState("")

  const patch = query.useMutation("patch", "/api/users/{userId}", {
    onSuccess: (updated) => {
      setReset({ email: updated.email, password })
      refresh()
    },
    onError: (err) => setError(messageOf(err, "Could not reset the password.")),
  })

  const close = () => {
    onDone()
    setPassword(generatePassword())
    setReset(null)
    setError("")
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!user) return
    setError("")
    patch.mutate({ params: { path: { userId: user.id } }, body: { password } })
  }

  return (
    <Dialog
      open={user !== null}
      onOpenChange={(open) => {
        if (!open) close()
      }}
    >
      <DialogContent className="sm:max-w-md">
        {reset ? (
          <>
            <DialogHeader>
              <DialogTitle>Password reset for {reset.email}</DialogTitle>
              <DialogDescription>
                Share this temporary password — it is shown only this once. Their other sessions were signed out.
              </DialogDescription>
            </DialogHeader>
            <CopyField label="New password" value={reset.password} />
            <DialogFooter>
              <Button onClick={close}>Done</Button>
            </DialogFooter>
          </>
        ) : (
          <form onSubmit={submit} className="grid gap-4" noValidate>
            <DialogHeader>
              <DialogTitle>Reset password</DialogTitle>
              <DialogDescription>
                Sets a new temporary password for {user?.email}. Their other sessions are signed out.
              </DialogDescription>
            </DialogHeader>
            <TempPasswordInput id="reset-password" value={password} onChange={setPassword} />
            {error && (
              <p role="alert" className="text-sm text-destructive">
                {error}
              </p>
            )}
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={close}>
                Cancel
              </Button>
              <Button type="submit" disabled={patch.isPending}>
                {patch.isPending ? "Saving…" : "Set password"}
              </Button>
            </DialogFooter>
          </form>
        )}
      </DialogContent>
    </Dialog>
  )
}

/** Admin turns off another user's two-factor, for a lost phone or lost codes. */
function ResetTotpDialog({ user, onDone }: { user: User | null; onDone: () => void }) {
  const reset = query.useMutation("delete", "/api/users/{userId}/totp", {
    onSuccess: () => {
      toast.success("Two-factor turned off.")
      refresh()
      onDone()
    },
    onError: (err) => toastMutationError(err, "Could not turn two-factor off."),
  })

  const close = () => {
    if (reset.isPending) return
    reset.reset()
    onDone()
  }

  return (
    <ConfirmDialog
      open={user !== null}
      onOpenChange={(open) => {
        if (!open) close()
      }}
      title={`Turn off two-factor for ${user?.email ?? ""}?`}
      description="Use this when the user lost their phone or recovery codes and can no longer sign in. They can turn two-factor back on from their profile."
      confirmLabel="Turn off"
      destructive
      pending={reset.isPending}
      onConfirm={() => user && reset.mutate({ params: { path: { userId: user.id } } })}
    />
  )
}
