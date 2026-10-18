import { useState, type FormEvent } from "react"
import { Plus } from "lucide-react"
import type { components } from "@/api/schema"
import { query, queryClient } from "@/api/client"
import { MeterRow } from "@/components/quotas/meter-row"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { MeterHistory } from "./meter-history"
import { ErrorLine, SectionCard } from "./section"

type Account = components["schemas"]["Account"]
type AccountQuota = components["schemas"]["AccountQuota"]
type MeterDef = components["schemas"]["MeterDef"]
type MeterView = components["schemas"]["MeterView"]
type MeterUnit = components["schemas"]["MeterUnit"]
type ResetKind = components["schemas"]["ResetKind"]

const UNITS: { value: MeterUnit; label: string }[] = [
  { value: "percent", label: "Percent used" },
  { value: "count", label: "Count" },
  { value: "tokens", label: "Tokens" },
  { value: "requests", label: "Requests" },
  { value: "credits", label: "Credits" },
  { value: "currency", label: "Money (minor units)" },
]

const RESETS: { value: ResetKind; label: string }[] = [
  { value: "never", label: "Never (a wallet)" },
  { value: "rolling", label: "Rolling window" },
  { value: "fixed", label: "Fixed time the provider announces" },
  { value: "calendar_month", label: "1st of the month" },
]

function refreshMeters() {
  void queryClient.invalidateQueries({ queryKey: ["get", "/api/quotas"] })
  void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts/{accountId}/meters/{meterKey}/readings"] })
}

/** Editors only: declare or change a manual meter. */
function AddMeterDialog({ account, open, onOpenChange }: { account: Account; open: boolean; onOpenChange: (open: boolean) => void }) {
  const [key, setKey] = useState("")
  const [label, setLabel] = useState("")
  const [unit, setUnit] = useState<MeterUnit>("percent")
  const [unitCurrency, setUnitCurrency] = useState(account.currency)
  const [resetKind, setResetKind] = useState<ResetKind>("rolling")
  const [limit, setLimit] = useState("")

  const post = query.useMutation("post", "/api/accounts/{accountId}/meters", {
    onSuccess: () => {
      refreshMeters()
      onOpenChange(false)
    },
  })

  const limitValue = Number(limit)
  const limitOk = limit === "" || (Number.isFinite(limitValue) && limitValue >= 0)
  const keyOk = /^[a-z0-9][a-z0-9_.-]{0,63}$/.test(key)

  const submit = (event: FormEvent) => {
    event.preventDefault()
    const def: MeterDef = {
      key,
      label: label.trim() || key,
      unit,
      reset_kind: resetKind,
      unit_currency: unit === "currency" ? unitCurrency : undefined,
      limit: limit === "" ? null : limitValue,
    }
    post.mutate({ params: { path: { accountId: account.id } }, body: def })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Add meter</DialogTitle>
          <DialogDescription>A hand-tracked quota. You enter its readings yourself.</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="meter-key">Key</Label>
              <Input
                id="meter-key"
                required
                pattern="[a-z0-9][a-z0-9_.-]{0,63}"
                placeholder="five_hour"
                value={key}
                onChange={(e) => setKey(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="meter-label">Label</Label>
              <Input id="meter-label" placeholder="5-hour window" value={label} onChange={(e) => setLabel(e.target.value)} />
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="meter-unit">Unit</Label>
              <Select value={unit} onValueChange={(v) => setUnit(v as MeterUnit)}>
                <SelectTrigger id="meter-unit" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {UNITS.map((u) => (
                    <SelectItem key={u.value} value={u.value}>
                      {u.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="meter-currency">Currency</Label>
              <Input
                id="meter-currency"
                required
                maxLength={3}
                disabled={unit !== "currency"}
                value={unitCurrency}
                onChange={(e) => setUnitCurrency(e.target.value.toUpperCase())}
              />
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="meter-reset">Resets</Label>
              <Select value={resetKind} onValueChange={(v) => setResetKind(v as ResetKind)}>
                <SelectTrigger id="meter-reset" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {RESETS.map((r) => (
                    <SelectItem key={r.value} value={r.value}>
                      {r.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="meter-limit">Limit</Label>
              <Input
                id="meter-limit"
                type="number"
                inputMode="decimal"
                min="0"
                step="any"
                placeholder="Unknown"
                value={limit}
                onChange={(e) => setLimit(e.target.value)}
              />
            </div>
          </div>
          {post.isError && <ErrorLine error={post.error} fallback="Could not save the meter." />}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!keyOk || !limitOk || post.isPending}>
              {post.isPending ? "Saving…" : "Save meter"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Editors only: enter a reading for a manual meter. */
function RecordReadingDialog({
  account,
  meter,
  open,
  onOpenChange,
}: {
  account: Account
  meter: MeterView
  open: boolean
  onOpenChange: (open: boolean) => void
}) {
  const [used, setUsed] = useState("")
  const [remaining, setRemaining] = useState("")
  const [limit, setLimit] = useState("")
  const [resetsAt, setResetsAt] = useState("")

  const post = query.useMutation("post", "/api/accounts/{accountId}/meters/{meterKey}/readings", {
    onSuccess: () => {
      refreshMeters()
      onOpenChange(false)
    },
  })

  const usedValue = used === "" || !Number.isFinite(Number(used)) ? undefined : Number(used)
  const remainingValue = remaining === "" || !Number.isFinite(Number(remaining)) ? undefined : Number(remaining)
  const anyValue = usedValue !== undefined || remainingValue !== undefined || limit !== ""

  const submit = (event: FormEvent) => {
    event.preventDefault()
    post.mutate({
      params: { path: { accountId: account.id, meterKey: meter.def.key } },
      body: {
        used: usedValue ?? null,
        remaining: remainingValue ?? null,
        limit: limit === "" ? null : Number(limit),
        resets_at: resetsAt === "" ? null : new Date(resetsAt).toISOString(),
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Record reading — {meter.def.label}</DialogTitle>
          <DialogDescription>
            In the meter's own unit ({meter.def.unit === "currency" ? `minor units of ${meter.def.unit_currency ?? account.currency}` : meter.def.unit}).
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="reading-used">Used</Label>
              <Input id="reading-used" type="number" inputMode="decimal" step="any" placeholder="—" value={used} onChange={(e) => setUsed(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="reading-remaining">Remaining</Label>
              <Input
                id="reading-remaining"
                type="number"
                inputMode="decimal"
                step="any"
                placeholder="—"
                value={remaining}
                onChange={(e) => setRemaining(e.target.value)}
              />
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="reading-limit">Limit</Label>
              <Input id="reading-limit" type="number" inputMode="decimal" step="any" placeholder="Unknown" value={limit} onChange={(e) => setLimit(e.target.value)} />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="reading-resets">Resets at</Label>
              <Input id="reading-resets" type="datetime-local" value={resetsAt} onChange={(e) => setResetsAt(e.target.value)} />
            </div>
          </div>
          {post.isError && <ErrorLine error={post.error} fallback="Could not record the reading." />}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!anyValue || post.isPending}>
              {post.isPending ? "Recording…" : "Record reading"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

function MeterItem({ account, meter, canEdit }: { account: Account; meter: MeterView; canEdit: boolean }) {
  const [recording, setRecording] = useState(false)
  return (
    <li className="space-y-2 border-b pb-4 last:border-b-0 last:pb-0">
      <MeterRow meter={meter} />
      <MeterHistory accountId={account.id} def={meter.def} />
      {canEdit && meter.manual && (
        <>
          <div>
            <Button variant="outline" size="xs" onClick={() => setRecording(true)}>
              Record reading
            </Button>
          </div>
          <RecordReadingDialog account={account} meter={meter} open={recording} onOpenChange={setRecording} />
        </>
      )}
    </li>
  )
}

/**
 * Every meter with its latest reading (provider's own words and units) plus a
 * 7-day history. Urgent border when the account is exhausted or its headroom
 * is below 0.3; the reason is stated next to it.
 */
export function MetersCard({ account, quota, canEdit }: { account: Account; quota?: AccountQuota; canEdit: boolean }) {
  const [adding, setAdding] = useState(false)
  const meters = quota?.meters ?? []
  const score = quota?.score
  const urgent = score !== undefined && (!score.feasible || score.headroom < 0.3)

  let reason: string | undefined
  if (score !== undefined && !score.feasible) {
    const binding = meters.find((m) => m.def.key === score.binding_meter)
    reason = `Exhausted: ${binding?.def.label ?? "a meter"} has nothing left.`
  } else if (score !== undefined && score.headroom < 0.3) {
    const binding = meters.find((m) => m.def.key === score.binding_meter)
    reason = `Low headroom (${Math.round(score.headroom * 100)}%): ${binding?.def.label ?? "the weakest meter"} runs out early at the current pace.`
  }

  return (
    <SectionCard
      title="Meters"
      description="As the provider reports them."
      urgent={urgent}
      action={
        canEdit ? (
          <Button variant="outline" size="sm" onClick={() => setAdding(true)}>
            <Plus />
            Add meter
          </Button>
        ) : undefined
      }
    >
      <div className="space-y-3">
        {urgent && reason && (
          <p className="text-sm font-medium text-urgent">{reason}</p>
        )}
        {meters.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No meters on this account.{canEdit && " Add one to track a quota by hand."}
          </p>
        ) : (
          <ul className="space-y-4">
            {meters.map((meter) => (
              <MeterItem key={meter.def.key} account={account} meter={meter} canEdit={canEdit} />
            ))}
          </ul>
        )}
      </div>
      {canEdit && <AddMeterDialog account={account} open={adding} onOpenChange={setAdding} />}
    </SectionCard>
  )
}
