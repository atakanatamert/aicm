import { useEffect, useState } from "react"
import type { FormEvent } from "react"
import { currencyExponent } from "@/lib/format"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
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
import { Select, SelectContent, SelectGroup, SelectItem, SelectLabel, SelectTrigger, SelectValue } from "@/components/ui/select"

type ProviderInfo = components["schemas"]["ProviderInfo"]
type AuthKind = components["schemas"]["AuthKind"]
type BillingCycle = components["schemas"]["BillingCycle"]
type PromoKind = components["schemas"]["PromoKind"]

const AUTH_KIND_LABEL: Record<AuthKind, string> = {
  api_key: "API key",
  admin_key: "Admin key",
  agent_capture: "Agent-captured subscription",
  none: "No key (wallet or plan)",
}

const CYCLE_LABEL: Record<BillingCycle, string> = {
  none: "One-off",
  weekly: "Weekly",
  monthly: "Monthly",
  yearly: "Yearly",
}

const PROMO_LABEL: Record<PromoKind, string> = {
  trial: "Trial",
  discount: "Discount",
}

const CYCLES: BillingCycle[] = ["none", "weekly", "monthly", "yearly"]

interface AddAccountDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  providers: ProviderInfo[]
}

/** "Add account" for editors: provider, key or plan, and the provider's options. */
export function AddAccountDialog({ open, onOpenChange, providers }: AddAccountDialogProps) {
  const [providerId, setProviderId] = useState("")
  const [name, setName] = useState("")
  const [authKind, setAuthKind] = useState<AuthKind>("api_key")
  const [secret, setSecret] = useState("")
  const [config, setConfig] = useState<Record<string, string>>({})
  const [planName, setPlanName] = useState("")
  const [price, setPrice] = useState("")
  const [currency, setCurrency] = useState("USD")
  const [cycle, setCycle] = useState<BillingCycle>("monthly")
  const [renewsOn, setRenewsOn] = useState("")
  const [autoRenew, setAutoRenew] = useState(true)
  const [promo, setPromo] = useState<"none" | PromoKind>("none")
  const [promoEndsOn, setPromoEndsOn] = useState("")
  const [allowUnofficial, setAllowUnofficial] = useState(false)
  const [formError, setFormError] = useState<string | null>(null)

  const create = query.useMutation("post", "/api/accounts")
  const resetCreate = create.reset
  const provider = providers.find((p) => p.id === providerId)
  const keyProviders = providers.filter(
    (p) => p.auth_kinds.includes("api_key") || p.auth_kinds.includes("admin_key"),
  )
  const subProviders = providers.filter((p) => !keyProviders.some((k) => k.id === p.id))

  // A fresh form each time the dialog opens.
  useEffect(() => {
    if (!open) {
      setProviderId("")
      setName("")
      setSecret("")
      setConfig({})
      setPlanName("")
      setPrice("")
      setCurrency("USD")
      setCycle("monthly")
      setRenewsOn("")
      setAutoRenew(true)
      setPromo("none")
      setPromoEndsOn("")
      setAllowUnofficial(false)
      setFormError(null)
      resetCreate()
    }
  }, [open, resetCreate])

  const pickProvider = (id: string) => {
    const p = providers.find((x) => x.id === id)
    setProviderId(id)
    setAuthKind(p?.auth_kinds[0] ?? "none")
    setCurrency(p?.currency ?? "USD")
    setConfig({})
    setAllowUnofficial(p?.unofficial ?? false)
  }

  const submit = (e: FormEvent) => {
    e.preventDefault()
    if (!provider) return

    // "199.00" → minor units of the plan currency.
    let priceMinor: number | undefined
    if (price.trim() !== "") {
      priceMinor = Math.round(Number.parseFloat(price) * 10 ** currencyExponent(currency))
      if (!Number.isFinite(priceMinor)) {
        setFormError("Enter the price as a number, like 199.00.")
        return
      }
    }
    const hasPlan = planName.trim() !== "" || priceMinor !== undefined

    create.mutate(
      {
        body: {
          provider_id: provider.id,
          name: name.trim(),
          auth_kind: authKind,
          secret: secret.trim() === "" ? undefined : secret,
          config: Object.fromEntries(Object.entries(config).filter(([, v]) => v !== "")),
          allow_unofficial: allowUnofficial,
          plan: hasPlan
            ? {
                name: planName.trim() || undefined,
                price: priceMinor !== undefined ? { amount_minor: priceMinor, currency } : undefined,
                cycle,
                renews_on: renewsOn === "" ? null : renewsOn,
                auto_renew: autoRenew,
                promo_kind: promo === "none" ? undefined : promo,
                promo_ends_on: promo === "none" || promoEndsOn === "" ? undefined : promoEndsOn,
              }
            : undefined,
        },
      },
      {
        onSuccess: () => {
          queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts"] })
          onOpenChange(false)
        },
      },
    )
  }

  // Server errors win over client-side validation notes; a rejected key gets
  // the provider's verdict, not a generic failure.
  let message = formError
  if (create.isError) {
    const err = errorOf(create.error)
    message =
      err?.error.code === "invalid_credentials"
        ? "The provider rejected this key"
        : (err?.error.message ?? "Could not add the account. Check your connection and try again.")
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-h-[85vh] overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Add account</DialogTitle>
          <DialogDescription>
            Add a provider account by hand. Keys are validated and stored encrypted; they are never shown again.
          </DialogDescription>
        </DialogHeader>
        <form onSubmit={submit} className="flex flex-col gap-4">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="add-provider">Provider</Label>
            <Select value={providerId} onValueChange={pickProvider} required>
              <SelectTrigger id="add-provider" className="w-full">
                <SelectValue placeholder="Choose a provider" />
              </SelectTrigger>
              <SelectContent>
                {keyProviders.length > 0 && (
                  <SelectGroup>
                    <SelectLabel>API keys</SelectLabel>
                    {keyProviders.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                )}
                {subProviders.length > 0 && (
                  <SelectGroup>
                    <SelectLabel>Subscriptions and wallets</SelectLabel>
                    {subProviders.map((p) => (
                      <SelectItem key={p.id} value={p.id}>
                        {p.name}
                      </SelectItem>
                    ))}
                  </SelectGroup>
                )}
              </SelectContent>
            </Select>
          </div>

          <div className="flex flex-col gap-1.5">
            <Label htmlFor="add-name">Name</Label>
            <Input
              id="add-name"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="e.g. Work Claude"
              autoComplete="off"
              required
            />
          </div>

          {provider && provider.auth_kinds.length > 1 && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="add-auth-kind">Sign-in kind</Label>
              <Select value={authKind} onValueChange={(v) => setAuthKind(v as AuthKind)}>
                <SelectTrigger id="add-auth-kind" className="w-full">
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  {provider.auth_kinds.map((k) => (
                    <SelectItem key={k} value={k}>
                      {AUTH_KIND_LABEL[k]}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
          )}

          {provider && (authKind === "api_key" || authKind === "admin_key") && (
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="add-secret">{AUTH_KIND_LABEL[authKind]}</Label>
              <Input
                id="add-secret"
                type="password"
                value={secret}
                onChange={(e) => setSecret(e.target.value)}
                autoComplete="new-password"
                required
              />
            </div>
          )}

          {provider?.config_fields.map((f) => (
            <div key={f.key} className="flex flex-col gap-1.5">
              <Label htmlFor={`add-config-${f.key}`}>{f.label}</Label>
              {f.options ? (
                <Select
                  value={config[f.key] ?? ""}
                  onValueChange={(v) => setConfig((c) => ({ ...c, [f.key]: v }))}
                >
                  <SelectTrigger id={`add-config-${f.key}`} className="w-full">
                    <SelectValue placeholder={f.required ? "Required" : "Optional"} />
                  </SelectTrigger>
                  <SelectContent>
                    {f.options.map((o) => (
                      <SelectItem key={o} value={o}>
                        {o}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              ) : (
                <Input
                  id={`add-config-${f.key}`}
                  value={config[f.key] ?? ""}
                  onChange={(e) => setConfig((c) => ({ ...c, [f.key]: e.target.value }))}
                  required={f.required}
                  autoComplete="off"
                />
              )}
              {f.help && <p className="text-xs text-muted-foreground">{f.help}</p>}
            </div>
          ))}

          {provider && (
            <fieldset className="flex flex-col gap-3 rounded-lg border p-3">
              <legend className="px-1 text-sm font-medium">Plan</legend>
              <div className="flex flex-col gap-1.5">
                <Label htmlFor="add-plan-name">Plan name</Label>
                <Input
                  id="add-plan-name"
                  value={planName}
                  onChange={(e) => setPlanName(e.target.value)}
                  placeholder="e.g. Max"
                  autoComplete="off"
                />
              </div>
              <div className="flex gap-3">
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor="add-price">Price</Label>
                  <Input
                    id="add-price"
                    inputMode="decimal"
                    value={price}
                    onChange={(e) => setPrice(e.target.value)}
                    placeholder="199.00"
                    autoComplete="off"
                  />
                </div>
                <div className="flex w-24 flex-col gap-1.5">
                  <Label htmlFor="add-currency">Currency</Label>
                  <Input
                    id="add-currency"
                    value={currency}
                    onChange={(e) => setCurrency(e.target.value.toUpperCase())}
                    maxLength={3}
                    pattern="[A-Za-z]{3}"
                    title="Three-letter ISO 4217 code"
                    required={price.trim() !== ""}
                  />
                </div>
              </div>
              <div className="flex gap-3">
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor="add-cycle">Cycle</Label>
                  <Select value={cycle} onValueChange={(v) => setCycle(v as BillingCycle)}>
                    <SelectTrigger id="add-cycle" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {CYCLES.map((c) => (
                        <SelectItem key={c} value={c}>
                          {CYCLE_LABEL[c]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor="add-renews">Renews on</Label>
                  <Input id="add-renews" type="date" value={renewsOn} onChange={(e) => setRenewsOn(e.target.value)} />
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Checkbox id="add-auto-renew" checked={autoRenew} onCheckedChange={(v) => setAutoRenew(v === true)} />
                <Label htmlFor="add-auto-renew" className="font-normal">
                  Auto-renew
                </Label>
              </div>
              <div className="flex gap-3">
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor="add-promo">Promo</Label>
                  <Select value={promo} onValueChange={(v) => setPromo(v as "none" | PromoKind)}>
                    <SelectTrigger id="add-promo" className="w-full">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="none">None</SelectItem>
                      {(Object.keys(PROMO_LABEL) as PromoKind[]).map((k) => (
                        <SelectItem key={k} value={k}>
                          {PROMO_LABEL[k]}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="flex flex-1 flex-col gap-1.5">
                  <Label htmlFor="add-promo-ends">Promo ends</Label>
                  <Input
                    id="add-promo-ends"
                    type="date"
                    value={promoEndsOn}
                    onChange={(e) => setPromoEndsOn(e.target.value)}
                    disabled={promo === "none"}
                  />
                </div>
              </div>
            </fieldset>
          )}

          {provider?.unofficial && (
            <div className="flex flex-col gap-1.5">
              <div className="flex items-center gap-2">
                <Checkbox
                  id="add-unofficial"
                  checked={allowUnofficial}
                  onCheckedChange={(v) => setAllowUnofficial(v === true)}
                />
                <Label htmlFor="add-unofficial" className="font-normal">
                  Allow unofficial endpoints
                </Label>
              </div>
              <p className="text-xs text-muted-foreground">
                This provider is read through an undocumented endpoint that can change or disappear without notice.
              </p>
            </div>
          )}

          {message && (
            <p role="alert" className="text-sm text-destructive">
              {message}
            </p>
          )}

          <DialogFooter>
            <Button type="button" variant="outline" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={create.isPending || !provider}>
              Add account
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
