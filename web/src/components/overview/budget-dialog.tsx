import { useState, type FormEvent } from "react"
import { X } from "lucide-react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { COMMON_CURRENCIES, messageOf } from "@/components/settings/helpers"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { useMe } from "@/hooks/use-me"
import { currencyExponent } from "@/lib/format"

type Budget = components["schemas"]["Budget"]

/** Just what the dialog needs from the surrounding account page. */
interface FixedAccount {
  id: string
  name: string
}

export interface BudgetDialogProps {
  /** Edit an existing budget; absent creates one. */
  budget?: Budget
  /** Pin the scope to one account (account detail); absent lets the editor pick. */
  fixedAccount?: FixedAccount
  onClose: () => void
}

/** Create or replace the org-wide or one account's monthly budget. */
export function BudgetDialog({ budget, fixedAccount, onClose }: BudgetDialogProps) {
  const { displayCurrency } = useMe()
  const accounts = query.useQuery("get", "/api/accounts")
  const activeAccounts = (accounts.data ?? []).filter((account) => account.status === "active")

  const [accountId, setAccountId] = useState(
    budget?.account_id ?? fixedAccount?.id ?? "",
  )
  const initialAmount = budget
    ? budget.amount.amount_minor / 10 ** currencyExponent(budget.amount.currency)
    : undefined
  const [amount, setAmount] = useState(initialAmount !== undefined ? String(initialAmount) : "")
  const [currency, setCurrency] = useState(budget?.amount.currency ?? displayCurrency)
  const [thresholds, setThresholds] = useState<number[]>(budget?.thresholds ?? [80, 100])
  const [thresholdDraft, setThresholdDraft] = useState("")
  const [error, setError] = useState("")

  const exp = currencyExponent(currency)
  const value = Number(amount)
  const amountMinor = Math.round(value * 10 ** exp)
  const amountValid = Number.isFinite(value) && value > 0 && amountMinor > 0

  const save = query.useMutation("put", "/api/budgets", {
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/budgets"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
      toast.success("Budget saved.")
      onClose()
    },
    onError: (err) => setError(messageOf(err, "Could not save the budget.")),
  })

  const addThreshold = () => {
    const n = Number(thresholdDraft)
    if (!Number.isInteger(n) || n < 1 || n > 1000) return
    setThresholds((prev) => [...prev.filter((t) => t !== n), n].sort((a, b) => a - b))
    setThresholdDraft("")
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!amountValid) return
    setError("")
    save.mutate({
      body: {
        // Absent account_id is the org-wide budget; undefined drops the key.
        account_id: fixedAccount?.id ?? (accountId === "" ? undefined : accountId),
        amount: { amount_minor: amountMinor, currency },
        thresholds,
      },
    })
  }

  const currencies = COMMON_CURRENCIES.some((c) => c.code === currency)
    ? COMMON_CURRENCIES
    : [{ code: currency, name: currency }, ...COMMON_CURRENCIES]

  return (
    <Dialog open onOpenChange={(open) => !open && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{budget ? "Edit budget" : "Set a monthly budget"}</DialogTitle>
          <DialogDescription>
            Month-to-date charges and the projected end of month are measured against it.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4">
          {!fixedAccount && (
            <div className="grid gap-2">
              <Label htmlFor="budget-scope">Scope</Label>
              <Select
                value={accountId === "" ? "org" : accountId}
                onValueChange={(value) => setAccountId(value === "org" ? "" : value)}
              >
                <SelectTrigger id="budget-scope" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="org">Whole organization</SelectItem>
                  {activeAccounts.map((account) => (
                    <SelectItem key={account.id} value={account.id}>
                      {account.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}
          {fixedAccount && (
            <div className="grid gap-2">
              <Label>Scope</Label>
              <p className="text-sm">{fixedAccount.name}</p>
            </div>
          )}
          <div className="grid gap-2 sm:grid-cols-[1fr_auto]">
            <div className="grid gap-2">
              <Label htmlFor="budget-amount">Monthly amount</Label>
              <Input
                id="budget-amount"
                type="number"
                min={0}
                step="any"
                inputMode="decimal"
                required
                aria-invalid={!amountValid}
                placeholder="200"
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="budget-currency">Currency</Label>
              <Select value={currency} onValueChange={setCurrency}>
                <SelectTrigger id="budget-currency" className="w-28">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {currencies.map((c) => (
                    <SelectItem key={c.code} value={c.code}>
                      {c.code}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="budget-threshold">Alert thresholds</Label>
            <div className="flex flex-wrap items-center gap-2">
              {thresholds.map((t) => (
                <span
                  key={t}
                  className="inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs"
                >
                  {t}%
                  <button
                    type="button"
                    aria-label={`Remove the ${t}% threshold`}
                    className="text-muted-foreground hover:text-foreground"
                    onClick={() => setThresholds((prev) => prev.filter((x) => x !== t))}
                  >
                    <X aria-hidden className="size-3" />
                  </button>
                </span>
              ))}
              <Input
                id="budget-threshold"
                type="number"
                min={1}
                max={1000}
                placeholder="Add %"
                className="h-8 w-24"
                value={thresholdDraft}
                onChange={(e) => setThresholdDraft(e.target.value)}
                onKeyDown={(e) => {
                  if (e.key === "Enter") {
                    e.preventDefault()
                    addThreshold()
                  }
                }}
              />
              <Button type="button" variant="outline" size="sm" disabled={thresholdDraft === ""} onClick={addThreshold}>
                Add
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">An alert fires when this month's spend crosses one.</p>
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
          <DialogFooter>
            <Button type="button" variant="outline" onClick={onClose}>
              Cancel
            </Button>
            <Button type="submit" disabled={!amountValid || save.isPending}>
              {save.isPending ? "Saving…" : "Save budget"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
