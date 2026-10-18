import { useState } from "react"
import { toast } from "sonner"
import { errorOf, query } from "@/api/client"
import type { components } from "@/api/schema"
import { ALERT_KINDS } from "@/components/alerts/alert-kinds"
import { invalidateAlertQueries, useAccounts } from "@/components/alerts/queries"
import { RuleRow } from "@/components/alerts/rule-row"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"

type AlertParams = components["schemas"]["AlertParams"]

interface AlertRulesPanelProps {
  canEdit: boolean
  isAdmin: boolean
}

function RulesSkeleton() {
  return (
    <div className="grid gap-3 px-6 py-4">
      {ALERT_KINDS.map((meta) => (
        <Skeleton key={meta.kind} className="h-8 w-full max-w-xl" />
      ))}
    </div>
  )
}

export function AlertRulesPanel({ canEdit, isAdmin }: AlertRulesPanelProps) {
  const orgRules = query.useQuery("get", "/api/alert-rules")
  const accounts = useAccounts()
  const activeAccounts = (accounts.data ?? []).filter((account) => account.status === "active")
  const [accountId, setAccountId] = useState("")

  const accountRules = query.useQuery(
    "get",
    "/api/alert-rules",
    { params: { query: { account_id: accountId } } },
    { enabled: accountId !== "" },
  )

  const saveOrg = query.useMutation("put", "/api/alert-rules", {
    onSuccess: () => invalidateAlertQueries(),
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not save the rule."),
  })
  const saveAccount = query.useMutation("put", "/api/alert-rules", {
    onSuccess: () => invalidateAlertQueries(),
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not save the rule."),
  })

  // account_id is absent (undefined) on org defaults, null-typed in the schema.
  const orgRuleOf = (kind: string) =>
    orgRules.data?.find((rule) => rule.kind === kind && (rule.account_id ?? null) === null)
  const savingOrgKind = saveOrg.isPending ? saveOrg.variables?.body.kind : undefined
  const savingAccountKind = saveAccount.isPending ? saveAccount.variables?.body.kind : undefined

  return (
    <section aria-label="Alert rules" className="grid gap-4 px-4 py-6 sm:px-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-base">Org defaults</CardTitle>
          <CardDescription>
            Applied to every account. {isAdmin ? "As an admin you can change them here." : "Only admins can change them."}
          </CardDescription>
        </CardHeader>
        <CardContent className="p-0">
          {orgRules.isPending ? (
            <RulesSkeleton />
          ) : orgRules.isError ? (
            <p role="alert" className="px-6 py-4 text-sm text-destructive">
              {errorOf(orgRules.error)?.error.message ?? "Could not load the alert rules."}
            </p>
          ) : (
            <div>
              {ALERT_KINDS.map((meta) => (
                <RuleRow
                  key={meta.kind}
                  meta={meta}
                  rule={orgRuleOf(meta.kind)}
                  inherited={undefined}
                  scope="org"
                  canEdit={isAdmin}
                  saving={savingOrgKind === meta.kind}
                  onSave={(enabled: boolean, params: AlertParams) =>
                    saveOrg.mutate({ body: { kind: meta.kind, enabled, params } })
                  }
                />
              ))}
            </div>
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle className="text-base">Per-account overrides</CardTitle>
          <CardDescription>
            {canEdit
              ? "Tailor the rules for one account. A kind without a stored override follows the org default; saving stores an override."
              : "Overrides for one account; kinds without one follow the org default."}
          </CardDescription>
        </CardHeader>
        <CardContent className="grid gap-3 p-0 pb-4">
          <div className="grid gap-1.5 px-6 pt-2 sm:grid-cols-[auto_1fr] sm:items-center sm:gap-3">
            <Label htmlFor="override-account" className="text-sm">
              Account
            </Label>
            <select
              id="override-account"
              value={accountId}
              onChange={(e) => setAccountId(e.target.value)}
              className="h-8 max-w-xs rounded-md border border-input bg-transparent px-2 text-sm shadow-xs outline-none focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50"
            >
              <option value="">Choose an account…</option>
              {activeAccounts.map((account) => (
                <option key={account.id} value={account.id}>
                  {account.name}
                </option>
              ))}
            </select>
          </div>
          {accountId === "" ? (
            <p className="px-6 pb-2 text-sm text-muted-foreground">Choose an account to see and change its overrides.</p>
          ) : accountRules.isPending ? (
            <RulesSkeleton />
          ) : accountRules.isError ? (
            <p role="alert" className="px-6 pb-2 text-sm text-destructive">
              {errorOf(accountRules.error)?.error.message ?? "Could not load this account's rules."}
            </p>
          ) : (
            <div>
              {ALERT_KINDS.map((meta) => (
                <RuleRow
                  key={`${accountId}:${meta.kind}`}
                  meta={meta}
                  rule={accountRules.data?.find((rule) => rule.kind === meta.kind && rule.account_id === accountId)}
                  inherited={orgRuleOf(meta.kind)}
                  scope={accountId}
                  canEdit={canEdit}
                  saving={savingAccountKind === meta.kind}
                  onSave={(enabled: boolean, params: AlertParams) =>
                    saveAccount.mutate({ body: { account_id: accountId, kind: meta.kind, enabled, params } })
                  }
                />
              ))}
            </div>
          )}
          {accountId !== "" && !canEdit && (
            <p className="px-6 pb-2 text-xs text-muted-foreground">
              You have view access; these rules can be changed by editors and admins.
            </p>
          )}
        </CardContent>
      </Card>
    </section>
  )
}
