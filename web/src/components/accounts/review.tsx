import { useState } from "react"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Checkbox } from "@/components/ui/checkbox"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { HARNESS_NAMES } from "@/lib/harness-names"

type Account = components["schemas"]["Account"]
type ProviderInfo = components["schemas"]["ProviderInfo"]

// Harnesses live on account_machines, one row per machine that reported the
// account; the same harness on several machines shows once.
function HarnessNames({ accountId }: { accountId: string }) {
  const machines = query.useQuery("get", "/api/accounts/{accountId}/machines", { params: { path: { accountId } } })
  if (machines.isPending) return <span className="text-muted-foreground">…</span>
  if (machines.isError) return <span className="text-muted-foreground">unknown</span>
  const harnesses = [...new Set(machines.data.map((m) => m.harness))]
  return <span>{harnesses.length > 0 ? harnesses.map((h) => HARNESS_NAMES[h] ?? h).join(", ") : "unknown"}</span>
}

function PendingAccount({
  account,
  providers,
  canEdit,
}: {
  account: Account
  providers: ProviderInfo[]
  canEdit: boolean
}) {
  const [name, setName] = useState(account.name)
  const [importKey, setImportKey] = useState(true)
  const confirm = query.useMutation("post", "/api/accounts/{accountId}/confirm")
  const dismiss = query.useMutation("post", "/api/accounts/{accountId}/dismiss")
  const provider = providers.find((p) => p.id === account.provider_id)
  const err = errorOf(confirm.error) ?? errorOf(dismiss.error)

  const confirmAccount = () =>
    confirm.mutate(
      {
        params: { path: { accountId: account.id } },
        body: {
          name: name.trim() === account.name ? undefined : name.trim(),
          import_key: account.auth_kind === "api_key" ? importKey : undefined,
        },
      },
      { onSuccess: () => queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts"] }) },
    )

  const dismissAccount = () =>
    dismiss.mutate({ params: { path: { accountId: account.id } } }, {
      onSuccess: () => queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts"] }),
    })

  return (
    <Card>
      <CardHeader>
        <CardTitle className="truncate">{account.name}</CardTitle>
        <CardDescription>
          {provider?.name ?? account.provider_id} · plan {account.plan.name || "unknown"} · found via{" "}
          <HarnessNames accountId={account.id} />
        </CardDescription>
      </CardHeader>
      {canEdit && (
        <CardContent className="flex flex-col gap-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor={`pending-name-${account.id}`}>Name</Label>
            <Input
              id={`pending-name-${account.id}`}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoComplete="off"
            />
          </div>
          {account.auth_kind === "api_key" && (
            <div className="flex items-center gap-2">
              <Checkbox
                id={`pending-import-${account.id}`}
                checked={importKey}
                onCheckedChange={(v) => setImportKey(v === true)}
              />
              <Label htmlFor={`pending-import-${account.id}`} className="font-normal">
                Import API key
              </Label>
            </div>
          )}
          {err && (
            <p role="alert" className="text-sm text-destructive">
              {err.error.code === "agent_offline" ? "Open the machine that found this key and try again" : err.error.message}
            </p>
          )}
          <div className="flex gap-2">
            <Button size="sm" onClick={confirmAccount} disabled={confirm.isPending || dismiss.isPending}>
              Confirm
            </Button>
            <Button
              size="sm"
              variant="outline"
              onClick={dismissAccount}
              disabled={confirm.isPending || dismiss.isPending}
            >
              Dismiss
            </Button>
          </div>
        </CardContent>
      )}
    </Card>
  )
}

/** Review cards for accounts agents discovered on the user's machines. */
export function ReviewSection({
  accounts,
  providers,
  canEdit,
}: {
  accounts: Account[]
  providers: ProviderInfo[]
  canEdit: boolean
}) {
  return (
    <section aria-labelledby="found-on-machines" className="flex flex-col gap-3">
      <h2 id="found-on-machines" className="text-sm font-medium text-muted-foreground">
        Found on your machines
      </h2>
      <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
        {accounts.map((account) => (
          <PendingAccount key={account.id} account={account} providers={providers} canEdit={canEdit} />
        ))}
      </div>
    </section>
  )
}
