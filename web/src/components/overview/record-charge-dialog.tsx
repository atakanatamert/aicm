import { useState, type FormEvent } from "react"
import type { components } from "@/api/schema"
import { errorOf, query, queryClient } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle, DialogTrigger } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { currencyExponent } from "@/lib/format"
import { isoDay } from "./spend-format"

type ChargeKind = components["schemas"]["ChargeKind"]

const KINDS: { value: ChargeKind; label: string }[] = [
  { value: "topup", label: "Top-up" },
  { value: "subscription", label: "Subscription" },
  { value: "invoice", label: "Invoice" },
  { value: "refund", label: "Refund" },
]

/** Editors only: record a payment by hand, in the account's own currency. */
export function RecordChargeDialog() {
  const [open, setOpen] = useState(false)
  const [accountId, setAccountId] = useState("")
  const [kind, setKind] = useState<ChargeKind>("topup")
  const [amount, setAmount] = useState("")
  const [date, setDate] = useState(() => isoDay())
  const [description, setDescription] = useState("")

  const accounts = query.useQuery("get", "/api/accounts", undefined, { enabled: open })
  const account = accounts.data?.find((a) => a.id === accountId)
  const create = query.useMutation("post", "/api/charges", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/api/spend"] })
      queryClient.invalidateQueries({ queryKey: ["get", "/api/charges"] })
      setOpen(false)
      setAmount("")
      setDescription("")
    },
  })

  const value = Number(amount)
  const amountValid = Number.isFinite(value) && value > 0
  const errorMessage = errorOf(create.error)?.error.message

  const submit = (event: FormEvent) => {
    event.preventDefault()
    if (!account || !amountValid || !date) return
    create.mutate({
      body: {
        account_id: account.id,
        kind,
        amount: { amount_minor: Math.round(value * 10 ** currencyExponent(account.currency)), currency: account.currency },
        occurred_on: date,
        description: description.trim() || undefined,
      },
    })
  }

  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogTrigger asChild>
        <Button className="w-full sm:w-auto">
          Record a charge
        </Button>
      </DialogTrigger>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Record a charge</DialogTitle>
          <DialogDescription>A payment that left your account: top-up, subscription fee, invoice or refund.</DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="grid gap-4" noValidate>
          <div className="grid gap-2">
            <Label htmlFor="charge-account">Account</Label>
            {(accounts.data?.length ?? 0) > 0 ? (
              <Select value={accountId} onValueChange={setAccountId}>
                <SelectTrigger id="charge-account" className="w-full">
                  <SelectValue placeholder="Choose an account" />
                </SelectTrigger>
                <SelectContent>
                  {(accounts.data ?? []).map((a) => (
                    <SelectItem key={a.id} value={a.id}>
                      {a.name} ({a.currency})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            ) : (
              <p className="text-sm text-muted-foreground">{accounts.isPending ? "Loading accounts…" : "No active accounts."}</p>
            )}
          </div>
          <div className="grid gap-2">
            <Label htmlFor="charge-kind">Kind</Label>
            <Select value={kind} onValueChange={(v) => setKind(v as ChargeKind)}>
              <SelectTrigger id="charge-kind" className="w-full">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {KINDS.map((k) => (
                  <SelectItem key={k.value} value={k.value}>
                    {k.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
          <div className="grid gap-4 sm:grid-cols-2">
            <div className="grid gap-2">
              <Label htmlFor="charge-amount">Amount{account ? ` (${account.currency})` : ""}</Label>
              <Input
                id="charge-amount"
                type="number"
                inputMode="decimal"
                min="0"
                step="any"
                placeholder="0.00"
                required
                value={amount}
                onChange={(e) => setAmount(e.target.value)}
              />
            </div>
            <div className="grid gap-2">
              <Label htmlFor="charge-date">Date</Label>
              <Input id="charge-date" type="date" required value={date} onChange={(e) => setDate(e.target.value)} />
            </div>
          </div>
          <div className="grid gap-2">
            <Label htmlFor="charge-description">Description</Label>
            <Input
              id="charge-description"
              placeholder="What it was for (optional)"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
            />
          </div>
          {errorMessage && (
            <p role="alert" className="text-sm text-destructive">
              {errorMessage}
            </p>
          )}
          <DialogFooter>
            <Button type="submit" disabled={!account || !amountValid || !date || create.isPending}>
              {create.isPending ? "Recording…" : "Record charge"}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
