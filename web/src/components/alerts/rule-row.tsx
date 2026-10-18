import { useState } from "react"
import type { components } from "@/api/schema"
import { buildParams, draftOf, type KindMeta } from "@/components/alerts/alert-kinds"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

type AlertRule = components["schemas"]["AlertRule"]
type AlertParams = components["schemas"]["AlertParams"]

interface RuleRowProps {
  meta: KindMeta
  /** The row being edited (org default or account override); absent when nothing is stored yet. */
  rule: AlertRule | undefined
  /** The org default, shown as the fallback in the account section. */
  inherited: AlertRule | undefined
  /** Unique per section ("org" or the account id) so field ids never collide. */
  scope: string
  canEdit: boolean
  saving: boolean
  onSave: (enabled: boolean, params: AlertParams) => void
}

/** One alert-rule kind: enable switch plus the kind's parameter inputs. */
export function RuleRow({ meta, rule, inherited, scope, canEdit, saving, onSave }: RuleRowProps) {
  const effective = rule ?? inherited
  const initialEnabled = effective?.enabled ?? true
  const initialDraft = draftOf(meta, effective?.params)
  const [enabled, setEnabled] = useState(initialEnabled)
  const [draft, setDraft] = useState<Record<string, string>>(initialDraft)

  const { params, valid } = buildParams(meta, draft)
  const dirty = enabled !== initialEnabled || JSON.stringify(draft) !== JSON.stringify(initialDraft)

  if (!canEdit) {
    return (
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b px-4 py-3 last:border-b-0 sm:px-6">
        <span className="w-56 text-sm">{meta.label}</span>
        <span className="w-24 text-sm text-muted-foreground">{initialEnabled ? "On" : "Off"}</span>
        {meta.fields.map((field) => (
          <div key={field.key} className="grid gap-0.5">
            <span className="text-xs text-muted-foreground">{field.label}</span>
            <span className="text-sm tabular-nums">{initialDraft[field.key] || "–"}</span>
          </div>
        ))}
      </div>
    )
  }

  return (
    <div className="flex flex-wrap items-center gap-x-6 gap-y-3 border-b px-4 py-3 last:border-b-0 sm:px-6">
      <div className="flex w-56 items-center gap-2">
        <Switch id={`rule-${scope}-${meta.kind}-enabled`} checked={enabled} onCheckedChange={setEnabled} />
        <Label htmlFor={`rule-${scope}-${meta.kind}-enabled`} className="font-normal">
          {meta.label}
        </Label>
      </div>
      {meta.fields.map((field) => (
        <div key={field.key} className="grid gap-1">
          <Label htmlFor={`rule-${scope}-${meta.kind}-${field.key}`} className="text-xs text-muted-foreground">
            {field.label}
          </Label>
          <Input
            id={`rule-${scope}-${meta.kind}-${field.key}`}
            className="h-8 w-28 tabular-nums"
            inputMode={field.list ? "numeric" : "decimal"}
            placeholder={field.placeholder}
            value={draft[field.key] ?? ""}
            onChange={(e) => setDraft((d) => ({ ...d, [field.key]: e.target.value }))}
          />
        </div>
      ))}
      {!valid && <p className="text-xs text-destructive">Numbers only{meta.fields.some((f) => f.list) ? ", comma separated" : ""}.</p>}
      <Button
        className="ml-auto"
        variant="outline"
        size="sm"
        disabled={!dirty || !valid || saving}
        onClick={() => onSave(enabled, params)}
      >
        {saving ? "Saving…" : "Save"}
      </Button>
    </div>
  )
}
