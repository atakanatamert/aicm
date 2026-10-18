import type { components } from "@/api/schema"
import { draftOf, type KindMeta } from "@/components/alerts/alert-kinds"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"

type AlertRule = components["schemas"]["AlertRule"]
type AlertParams = components["schemas"]["AlertParams"]

interface RuleRowProps {
  meta: KindMeta
  /** The stored rule for this scope; absent when nothing is stored yet. */
  rule: AlertRule | undefined
  /** The org default, shown as the fallback in the account section. */
  inherited: AlertRule | undefined
  /** Unique per section ("org" or the account id) so field ids never collide. */
  scope: string
  canEdit: boolean
  enabled: boolean
  draft: Record<string, string>
  onEnabled: (enabled: boolean) => void
  onDraft: (key: keyof AlertParams, value: string) => void
}

/** One alert-rule kind: enable switch plus the kind's parameter inputs. The
 * section's Save bar below the rows persists every change at once. */
export function RuleRow({ meta, rule, inherited, scope, canEdit, enabled, draft, onEnabled, onDraft }: RuleRowProps) {
  const effective = rule ?? inherited
  const initialDraft = draftOf(meta, effective?.params)

  if (!canEdit) {
    return (
      <div className="flex flex-wrap items-center gap-x-6 gap-y-2 border-b px-4 py-3 last:border-b-0 sm:px-6">
        <span className="w-56 text-sm">{meta.label}</span>
        <span className="w-24 text-sm text-muted-foreground">{(effective?.enabled ?? true) ? "On" : "Off"}</span>
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
        <Switch
          id={`rule-${scope}-${meta.kind}-enabled`}
          checked={enabled}
          onCheckedChange={onEnabled}
        />
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
            onChange={(e) => onDraft(field.key, e.target.value)}
          />
        </div>
      ))}
    </div>
  )
}
