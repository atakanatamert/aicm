import { useState, type FormEvent } from "react"
import type { components } from "@/api/schema"
import { errorOf, query, queryClient } from "@/api/client"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { currencyExponent, formatDate, formatMoney } from "@/lib/format"
import { cn } from "@/lib/utils"

type Account = components["schemas"]["Account"]
type BillingCycle = components["schemas"]["BillingCycle"]
type PromoKind = components["schemas"]["PromoKind"]

const CYCLES: { value: BillingCycle; label: string }[] = [
  { value: "none", label: "No cycle (pay as you go)" },
  { value: "weekly", label: "Weekly" },
  { value: "monthly", label: "Monthly" },
  { value: "yearly", label: "Yearly" },
]

const PROMOS: { value: PromoKind | "none"; label: string }[] = [
  { value: "none", label: "No promo" },
  { value: "trial", label: "Trial" },
  { value: "discount", label: "Discount" },
]

const DAY_MS = 86_400_000

/** Whole days until an ISO date (UTC midnight); negative once past. */
function daysUntil(iso: string, now: Date): number {
  return Math.ceil((Date.parse(iso) - now.getTime()) / DAY_MS)
}

/**
 * Renewal urgency text, or null. Urgent: a renewal tomorrow, or within
 * 7 days with auto-renew off.
 */
function renewalWarning(account: Account, now: Date): string | null {
  const renewsOn = account.plan.renews_on
  if (!renewsOn) return null
  const days = daysUntil(renewsOn, now)
  if (days < 0 || days > 7) return null
  if (days <= 1) return `Renews ${days <= 0 ? "today" : "tomorrow"}.`
  if (!account.plan.auto_renew) return `Renews in ${days} days and auto-renew is off.`
  return null
}

/** Editors only: edit plan, renewal, promo, notes and the unofficial opt-in. */
function PlanEditDialog({ account, open, onOpenChange }: { account: Account; open: boolean; onOpenChange: (open: boolean) => void }) {
  const plan = account.plan
  const [name, setName] = useState(plan.name)
  const [price, setPrice] = useState(plan.price ? String(plan.price.amount_minor / 10 ** currencyExponent(plan.price.currency)) : "")
  const [cycle, setCycle] = useState<BillingCycle>(plan.cycle)
  const [renewsOn, setRenewsOn] = useState(plan.renews_on ?? "")
  const [autoRenew, setAutoRenew] = useState(plan.auto_renew)
  const [promo, setPromo] = useState<PromoKind | "none">(plan.promo_kind ?? "none")
  const [promoEndsOn, setPromoEndsOn] = useState(plan.promo_ends_on ?? "")
  const [notes, setNotes] = useState(account.notes)
  const [allowUnofficial, setAllowUnofficial] = useState(account.allow_unofficial)

  const patch = query.useMutation("patch", "/api/accounts/{accountId}", {
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts/{accountId}"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/quotas"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/renewals"] })
      onOpenChange(false)
    },
  })

  const priceValue = Number(price)
  const priceMinor = Math.round(priceValue * 10 ** currencyExponent(account.currency))
  const priceOk = price === "" ? cycle === "none" : Number.isFinite(priceValue) && priceValue >= 0
  const errorMessage = errorOf(patch.error)?.error.message

  const submit = (event: FormEvent) => {
    event.preventDefault()
    patch.mutate({
      params: { path: { accountId: account.id } },
      body: {
        plan: {
          name: name.trim(),
          cycle,
          price: cycle === "none" || price === "" ? undefined : { amount_minor: priceMinor, currency: account.currency },
          renews_on: renewsOn === "" ? null : renewsOn,
          auto_renew: autoRenew,
          promo_kind: promo === "none" ? undefined : promo,
          promo_ends_on: promo === "none" || promoEndsOn === "" ? null : promoEndsOn,
        },
        notes,
        allow_unofficial: allowUnofficial,
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85svh] overflow-y-auto sm:max-w-lg">
        <DialogHeader>
          <DialogTitle>Edit plan and renewal</DialogTitle>
          <DialogDescription>Amounts are in the account's billing currency ({account.currency}).</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <div className="grid gap-2">
            <Label htmlFor="plan-name">Plan name</Label>
            <Input id="plan-name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Pro plan" />
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="plan-price">Price ({account.currency})</Label>
              <Input
                id="plan-price"
                type="number"
                inputMode="decimal"
                min="0"
                step="any"
                placeholder={cycle === "none" ? "No fixed price" : "0.00"}
                disabled={cycle === "none"}
                value={price}
                onChange={(e) => setPrice(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="plan-cycle">Cycle</Label>
              <Select value={cycle} onValueChange={(v) => setCycle(v as BillingCycle)}>
                <SelectTrigger id="plan-cycle" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {CYCLES.map((c) => (
                    <SelectItem key={c.value} value={c.value}>
                      {c.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="plan-renews-on">Renews on</Label>
              <Input id="plan-renews-on" type="date" value={renewsOn} onChange={(e) => setRenewsOn(e.target.value)} />
            </div>
            <div className="flex items-end pb-1">
              <label className="flex cursor-pointer items-center gap-2.5 text-sm">
                <Checkbox checked={autoRenew} onCheckedChange={(c) => setAutoRenew(c === true)} />
                Auto-renew
              </label>
            </div>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="plan-promo">Promo</Label>
              <Select value={promo} onValueChange={(v) => setPromo(v as PromoKind | "none")}>
                <SelectTrigger id="plan-promo" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {PROMOS.map((p) => (
                    <SelectItem key={p.value} value={p.value}>
                      {p.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="grid gap-2">
              <Label htmlFor="plan-promo-ends">Promo ends on</Label>
              <Input
                id="plan-promo-ends"
                type="date"
                disabled={promo === "none"}
                value={promoEndsOn}
                onChange={(e) => setPromoEndsOn(e.target.value)}
              />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="plan-notes">Notes</Label>
            <Input id="plan-notes" value={notes} onChange={(e) => setNotes(e.target.value)} placeholder="Anything worth remembering" />
          </div>
          <label className="flex cursor-pointer items-center gap-2.5 text-sm">
            <Checkbox checked={allowUnofficial} onCheckedChange={(c) => setAllowUnofficial(c === true)} />
            Allow unofficial quota endpoints for this account
          </label>
          {errorMessage && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!priceOk || patch.isPending}>
              {patch.isPending ? "Saving…" : "Save plan"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}

/** Plan, price, renewal and promo facts, with the edit form for editors. */
export function PlanCard({ account, canEdit }: { account: Account; canEdit: boolean }) {
  const [editing, setEditing] = useState(false)
  const plan = account.plan
  const warning = renewalWarning(account, new Date())

  const meta: string[] = []
  if (plan.price) meta.push(formatMoney(plan.price))
  if (plan.cycle !== "none") meta.push(`billed ${plan.cycle}`)
  if (plan.renews_on) meta.push(`renews ${formatDate(plan.renews_on)}`)
  if (plan.promo_kind) {
    meta.push(plan.promo_ends_on ? `${plan.promo_kind} ends ${formatDate(plan.promo_ends_on)}` : plan.promo_kind)
  }

  return (
    <div className="space-y-3">
      <ul className="space-y-1 text-sm">
        <li className="font-medium">{plan.name || "No plan name"}</li>
        <li className="text-muted-foreground">{meta.length > 0 ? meta.join(" · ") : "No price or renewal date set."}</li>
        <li className="text-muted-foreground">
          {plan.auto_renew ? "Auto-renew is on." : "Auto-renew is off."}
          {account.allow_unofficial && " Unofficial quota endpoints allowed."}
        </li>
        {warning && (
          <li className={cn("font-medium text-urgent")}>{warning}</li>
        )}
      </ul>
      {account.notes && <p className="text-sm whitespace-pre-wrap">{account.notes}</p>}
      {canEdit && (
        <div>
          <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
            Edit
          </Button>
        </div>
      )}
      {canEdit && <PlanEditDialog account={account} open={editing} onOpenChange={setEditing} />}
      {!canEdit && plan.promo_kind && <Badge variant="outline">{plan.promo_kind}</Badge>}
    </div>
  )
}
