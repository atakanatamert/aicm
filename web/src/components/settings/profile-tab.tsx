import { useState, type FormEvent } from "react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { EmptyState } from "@/components/empty-state"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Terminal } from "@/components/ui/hairline"
import { formatDate, formatRelative } from "@/lib/format"
import { CopyField } from "./fields"
import { MCP_HINT, messageOf } from "./helpers"

type ApiTokenCreated = components["schemas"]["ApiTokenCreated"]

export function ProfileTab() {
  return (
    <div className="grid grid-cols-1 items-start gap-6 py-6 xl:grid-cols-2">
      <PasswordCard />
      <TokensCard />
    </div>
  )
}

function PasswordCard() {
  const [current, setCurrent] = useState("")
  const [next, setNext] = useState("")
  const [confirm, setConfirm] = useState("")
  const [error, setError] = useState("")
  const change = query.useMutation("post", "/api/me/password", {
    onSuccess: () => {
      toast.success("Password changed. Your other sessions were signed out.")
      setCurrent("")
      setNext("")
      setConfirm("")
      setError("")
    },
    onError: (err) => setError(messageOf(err, "Could not change the password.")),
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (next.length < 12) {
      setError("The new password needs at least 12 characters.")
      return
    }
    if (next !== confirm) {
      setError("The new passwords do not match.")
      return
    }
    setError("")
    change.mutate({ body: { current_password: current, new_password: next } })
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Password</CardTitle>
        <CardDescription>Changing your password signs out your other sessions.</CardDescription>
      </CardHeader>
      <CardContent>
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <div className="grid gap-2">
            <Label htmlFor="pw-current">Current password</Label>
            <Input
              id="pw-current"
              type="password"
              autoComplete="current-password"
              required
              value={current}
              onChange={(e) => setCurrent(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="pw-new">New password</Label>
            <Input
              id="pw-new"
              type="password"
              autoComplete="new-password"
              minLength={12}
              required
              value={next}
              onChange={(e) => setNext(e.target.value)}
            />
            <p className="text-xs text-muted-foreground">At least 12 characters.</p>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="pw-confirm">Repeat new password</Label>
            <Input
              id="pw-confirm"
              type="password"
              autoComplete="new-password"
              required
              value={confirm}
              onChange={(e) => setConfirm(e.target.value)}
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <div>
            <Button type="submit" disabled={change.isPending}>
              {change.isPending ? "Changing…" : "Change password"}
            </Button>
          </div>
        </form>
      </CardContent>
    </Card>
  )
}

function TokensCard() {
  const tokens = query.useQuery("get", "/api/me/tokens")
  const [createOpen, setCreateOpen] = useState(false)
  const [created, setCreated] = useState<ApiTokenCreated | null>(null)
  const [name, setName] = useState("")
  const [expires, setExpires] = useState("")
  const [error, setError] = useState("")

  const create = query.useMutation("post", "/api/me/tokens", {
    onSuccess: (token) => {
      setCreated(token)
      queryClient.invalidateQueries({ queryKey: ["get", "/api/me/tokens"] })
    },
    onError: (err) => setError(messageOf(err, "Could not create the token.")),
  })
  const revoke = query.useMutation("delete", "/api/me/tokens/{tokenId}", {
    onSuccess: () => {
      toast.success("Token revoked.")
      queryClient.invalidateQueries({ queryKey: ["get", "/api/me/tokens"] })
    },
    onError: (err) => toast.error(messageOf(err, "Could not revoke the token.")),
  })

  const closeCreate = (open: boolean) => {
    setCreateOpen(open)
    if (!open) {
      setCreated(null)
      setName("")
      setExpires("")
      setError("")
    }
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setError("")
    create.mutate({
      body: {
        name,
        expires_at: expires ? new Date(`${expires}T23:59:59Z`).toISOString() : null,
      },
    })
  }

  return (
    <Card>
      <CardHeader className="flex-row items-start justify-between space-y-0">
        <div className="grid gap-1.5">
          <CardTitle className="text-base">API tokens</CardTitle>
          <CardDescription>
            Personal tokens for the agent and MCP tools. The secret is shown once at creation.
          </CardDescription>
        </div>
        <Button size="sm" onClick={() => setCreateOpen(true)}>
          New token
        </Button>
      </CardHeader>
      <CardContent>
        {tokens.isPending ? (
          <div className="space-y-2" aria-hidden>
            {Array.from({ length: 3 }, (_, i) => (
              <div key={i} className="h-8 w-full animate-pulse rounded-md bg-muted" />
            ))}
          </div>
        ) : (tokens.data?.length ?? 0) === 0 ? (
          <EmptyState
            figure={<Terminal play style={{ maxWidth: 200 }} />}
            title="No API tokens yet"
            description="Create one to connect the agent's MCP tools under your account."
            className="py-6"
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Name</TableHead>
                <TableHead>Token</TableHead>
                <TableHead>Last used</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">
                  <span className="sr-only">Actions</span>
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {tokens.data?.map((token) => (
                <TableRow key={token.id} className={token.revoked_at ? "opacity-60" : undefined}>
                  <TableCell className="text-sm">{token.name}</TableCell>
                  <TableCell className="font-mono text-sm">•••• {token.last4}</TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {token.last_used_at ? formatRelative(token.last_used_at) : "Never"}
                  </TableCell>
                  <TableCell className="text-sm text-muted-foreground">
                    {token.expires_at ? formatDate(token.expires_at) : "Never"}
                  </TableCell>
                  <TableCell className="text-right">
                    {token.revoked_at ? (
                      <Badge variant="outline">Revoked</Badge>
                    ) : (
                      <Button
                        variant="ghost"
                        size="sm"
                        className="text-muted-foreground hover:text-destructive focus-visible:text-destructive"
                        aria-label={`Revoke token ${token.name}`}
                        onClick={() => revoke.mutate({ params: { path: { tokenId: token.id } } })}
                      >
                        Revoke
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>

      <Dialog open={createOpen} onOpenChange={closeCreate}>
        <DialogContent className="sm:max-w-md">
          {created ? (
            <>
              <DialogHeader>
                <DialogTitle>Copy your token now</DialogTitle>
                <DialogDescription>It is shown only this once — store it somewhere safe.</DialogDescription>
              </DialogHeader>
              <CopyField label="Token" value={created.token} />
              <div className="grid gap-1.5">
                <span className="text-xs font-medium text-muted-foreground">
                  Connect the agent's MCP server with it
                </span>
                <code className="overflow-x-auto rounded-md border bg-muted/40 p-3 font-mono text-xs">
                  {MCP_HINT}
                </code>
              </div>
              <DialogFooter>
                <Button onClick={() => closeCreate(false)}>Done</Button>
              </DialogFooter>
            </>
          ) : (
            <form onSubmit={submit} className="grid gap-4" noValidate>
              <DialogHeader>
                <DialogTitle>New API token</DialogTitle>
                <DialogDescription>
                  The token inherits your role. Everything it does lands in the audit log under your name.
                </DialogDescription>
              </DialogHeader>
              <div className="grid gap-2">
                <Label htmlFor="token-name">Name</Label>
                <Input
                  id="token-name"
                  required
                  maxLength={100}
                  placeholder="Work laptop"
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="token-expires">Expires (optional)</Label>
                <Input
                  id="token-expires"
                  type="date"
                  value={expires}
                  onChange={(e) => setExpires(e.target.value)}
                />
                <p className="text-xs text-muted-foreground">Leave empty for a token that does not expire.</p>
              </div>
              {error && (
                <p role="alert" className="text-sm text-destructive">
                  {error}
                </p>
              )}
              <DialogFooter>
                <Button type="button" variant="ghost" onClick={() => closeCreate(false)}>
                  Cancel
                </Button>
                <Button type="submit" disabled={create.isPending || name.length === 0}>
                  {create.isPending ? "Creating…" : "Create token"}
                </Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>
    </Card>
  )
}
