import { useState, type FormEvent } from "react"
import type { components } from "@/api/schema"
import { errorOf, query, queryClient } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { formatRelative } from "@/lib/format"

type Account = components["schemas"]["Account"]

/** Where the credential lives; only api_key and admin_key are hub-stored. */
function keyLine(account: Account): string {
  if (account.secret.set) {
    const set = account.secret.updated_at ? `, set ${formatRelative(account.secret.updated_at)}` : ""
    return `Key ending in ${account.secret.last4 || "••••"}${set}.`
  }
  if (account.auth_kind === "agent_capture") {
    return "No key stored here — the agent reads this account on your machine."
  }
  if (account.auth_kind === "none") {
    return "No key needed for this account."
  }
  return "No key yet."
}

/**
 * What the hub knows about the credential (never the key itself) and, for
 * editors of key-based accounts, the form to replace it.
 */
export function KeyCard({ account, canEdit }: { account: Account; canEdit: boolean }) {
  const [value, setValue] = useState("")
  const [validate, setValidate] = useState(true)
  const [stored, setStored] = useState(false)

  const put = query.useMutation("put", "/api/accounts/{accountId}/secret", {
    onSuccess: () => {
      setStored(true)
      setValue("")
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts/{accountId}"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts"] })
    },
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setStored(false)
    put.mutate({ params: { path: { accountId: account.id } }, body: { value, validate } })
  }

  const hubStored = account.auth_kind === "api_key" || account.auth_kind === "admin_key"

  return (
    <div className="space-y-3">
      <p className="text-sm">{keyLine(account)}</p>
      {hubStored && canEdit && (
        <form onSubmit={submit} className="grid gap-3" noValidate>
          <div className="grid gap-2">
            <Label htmlFor="account-key">New key</Label>
            <Input
              id="account-key"
              type="password"
              autoComplete="off"
              required
              value={value}
              onChange={(e) => setValue(e.target.value)}
            />
          </div>
          <label className="flex cursor-pointer items-center gap-2.5 text-sm">
            <Checkbox checked={validate} onCheckedChange={(c) => setValidate(c === true)} />
            Check the key with the provider before storing it
          </label>
          {put.isError && (
            <p role="alert" className="text-sm text-destructive">
              {errorOf(put.error)?.error.message ?? "Could not store the key."}
            </p>
          )}
          {stored && put.isSuccess && (
            <p role="status" className="text-sm text-muted-foreground">
              Key stored. Only its last four characters are kept visible.
            </p>
          )}
          <div>
            <Button type="submit" size="sm" disabled={value.length === 0 || put.isPending}>
              {put.isPending ? "Storing…" : "Replace key"}
            </Button>
          </div>
        </form>
      )}
      {hubStored && !canEdit && account.secret.set && (
        <p className="text-xs text-muted-foreground">The key itself is never shown.</p>
      )}
    </div>
  )
}
