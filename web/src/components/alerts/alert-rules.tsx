import { useState } from "react"
import { toast } from "sonner"
import { errorOf, query } from "@/api/client"
import type { components } from "@/api/schema"
import { buildParams, draftOf, ALERT_KINDS } from "@/components/alerts/alert-kinds"
import { invalidateAlertQueries, useAccounts } from "@/components/alerts/queries"
import { RuleRow } from "@/components/alerts/rule-row"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"

type AlertRule = components["schemas"]["AlertRule"]
type AlertKind = components["schemas"]["AlertKind"]
type AlertParams = components["schemas"]["AlertParams"]

/** What the editor is changing for one rule kind. */
interface KindDraft {
  enabled: boolean
  draft: Record<string, string>
}

/** Drafts for every kind, seeded from what is stored in this scope. */
function initialDrafts(rules: AlertRule[], inheritedOf: (kind: AlertKind) => AlertRule | undefined): Record<AlertKind, KindDraft> {
  const seeded = {} as Record<AlertKind, KindDraft>
  for (const meta of ALERT_KINDS) {
    const stored = rules.find((rule) => rule.kind === meta.kind)
    const effective = stored ?? inheritedOf(meta.kind)
    seeded[meta.kind] = {
      enabled: effective?.enabled ?? true,
      draft: draftOf(meta, effective?.params),
    }
  }
  return seeded
}

/** True when the editor changed the kind's switch or any of its fields. */
function isDirty(meta: (typeof ALERT_KINDS)[number], current: KindDraft, initial: KindDraft): boolean {
  if (current.enabled !== initial.enabled) return true
  return meta.fields.some((field) => (current.draft[field.key] ?? "") !== (initial.draft[field.key] ?? ""))
}

/** Local edit state for one RulesBlock: every kind's switch and fields. */
function useDrafts(initial: Record<AlertKind, KindDraft>) {
  const [drafts, setDrafts] = useState(initial)
  const setKind = (kind: AlertKind, patch: Partial<KindDraft>) =>
    setDrafts((prev) => ({ ...prev, [kind]: { ...prev[kind], ...patch } }))
  const setField = (kind: AlertKind, key: keyof AlertParams, value: string) =>
    setDrafts((prev) => ({ ...prev, [kind]: { ...prev[kind], draft: { ...prev[kind].draft, [key]: value } } }))
  return { drafts, setKind, setField }
}

interface AlertRulesPanelProps {
  canEdit: boolean
  isAdmin: boolean
}

function RulesSkeleton() {
  return (
    <div className="grid gap-3 px-6 py-4">
      {Array.from({ length: 3 }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}

interface PendingSave {
  kind: AlertKind
  enabled: boolean
  params: AlertParams
}

/** One account override block: account picker, its rows, one Save bar. */
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
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not save the rules."),
  })
  const saveAccount = query.useMutation("put", "/api/alert-rules", {
    onSuccess: () => invalidateAlertQueries(),
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not save the rules."),
  })

  // account_id is absent (undefined) on org defaults, null-typed in the schema.
  const orgRuleOf = (kind: AlertKind) =>
    orgRules.data?.find((rule) => rule.kind === kind && (rule.account_id ?? null) === null)

  return (
    <section aria-label="Alert rules" className="grid gap-4 py-6">
      <Card>
        <CardHeader>
          <h3 className="text-base leading-none font-semibold">Org defaults</h3>
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
            <RulesBlock
              scope="org"
              rules={orgRules.data.filter((rule) => (rule.account_id ?? null) === null)}
              inheritedOf={() => undefined}
              canEdit={isAdmin}
              saving={saveOrg.isPending}
              onSave={async (kinds) => {
                for (const { kind, enabled, params } of kinds) {
                  await saveOrg.mutateAsync({ body: { kind, enabled, params } })
                }
                toast.success("Alert rules saved.")
              }}
            />
          )}
        </CardContent>
      </Card>

      <Card>
        <CardHeader>
          <h3 className="text-base leading-none font-semibold">Per-account overrides</h3>
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
            <RulesBlock
              key={accountId}
              scope={accountId}
              rules={accountRules.data}
              inheritedOf={orgRuleOf}
              canEdit={canEdit}
              saving={saveAccount.isPending}
              onSave={async (kinds) => {
                for (const { kind, enabled, params } of kinds) {
                  await saveAccount.mutateAsync({ body: { account_id: accountId, kind, enabled, params } })
                }
                toast.success("Alert rules saved.")
              }}
            />
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

/**
 * All rule kinds of one scope with a single Save bar: edits collect in local
 * state and persist together, not one Save per row.
 */
function RulesBlock({
  scope,
  rules,
  inheritedOf,
  canEdit,
  saving,
  onSave,
}: {
  scope: string
  rules: AlertRule[]
  inheritedOf: (kind: AlertKind) => AlertRule | undefined
  canEdit: boolean
  saving: boolean
  onSave: (kinds: PendingSave[]) => Promise<void>
}) {
  const [initial] = useState<Record<AlertKind, KindDraft>>(() => initialDrafts(rules, inheritedOf))
  const { drafts, setKind, setField } = useDrafts(initial)

  const pending: PendingSave[] = []
  let allValid = true
  for (const meta of ALERT_KINDS) {
    const current = drafts[meta.kind]
    if (isDirty(meta, current, initial[meta.kind])) {
      const built = buildParams(meta, current.draft)
      allValid = allValid && built.valid
      pending.push({ kind: meta.kind, enabled: current.enabled, params: built.params })
    }
  }

  return (
    <div>
      <div>
        {ALERT_KINDS.map((meta) => (
          <RuleRow
            key={meta.kind}
            meta={meta}
            scope={scope}
            canEdit={canEdit}
            rule={rules.find((rule) => rule.kind === meta.kind)}
            inherited={inheritedOf(meta.kind)}
            enabled={drafts[meta.kind].enabled}
            draft={drafts[meta.kind].draft}
            onEnabled={(enabled) => setKind(meta.kind, { enabled })}
            onDraft={(key, value) => setField(meta.kind, key, value)}
          />
        ))}
      </div>
      {canEdit && (
        <div className="flex items-center justify-end gap-3 border-t px-4 py-3 sm:px-6">
          {!allValid && <p className="text-xs text-destructive">Numbers only where numbers belong.</p>}
          <Button
            variant="outline"
            size="sm"
            disabled={pending.length === 0 || !allValid || saving}
            onClick={() => void onSave(pending)}
          >
            {saving ? "Saving…" : "Save changes"}
          </Button>
        </div>
      )}
    </div>
  )
}
