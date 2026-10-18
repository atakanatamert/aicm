import { useState, type FormEvent } from "react"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TableSkeleton } from "./fields"
import { COMMON_CURRENCIES, messageOf } from "./helpers"

type Settings = components["schemas"]["Settings"]
type FxSource = NonNullable<components["schemas"]["SettingsUpdate"]["fx_source"]>

export function OrganizationTab() {
  const settings = query.useQuery("get", "/api/settings")
  const [savedAt, setSavedAt] = useState(0)

  return (
    <section className="py-6">
      <Card className="max-w-xl">
        {settings.isPending ? (
          <CardContent>
            <TableSkeleton rows={3} />
          </CardContent>
        ) : settings.isError ? (
          <CardContent>
            <p role="alert" className="text-sm text-destructive">
              {messageOf(settings.error, "Could not load the settings.")}
            </p>
          </CardContent>
        ) : (
          <OrganizationForm key={savedAt} initial={settings.data} onSaved={() => setSavedAt(Date.now())} />
        )}
      </Card>
    </section>
  )
}

function OrganizationForm({ initial, onSaved }: { initial: Settings; onSaved: () => void }) {
  const known = COMMON_CURRENCIES.some((c) => c.code === initial.display_currency)
  const [currency, setCurrency] = useState(initial.display_currency)
  const [custom, setCustom] = useState(!known)
  const [fx, setFx] = useState<FxSource>(initial.fx_source)
  const [error, setError] = useState("")

  const valid = custom ? /^[A-Z]{3}$/.test(currency) : true
  const save = query.useMutation("patch", "/api/settings", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/api/settings"] })
      // The display currency also flows through /api/me to every other screen.
      queryClient.invalidateQueries({ queryKey: ["get", "/api/me"] })
      queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
      onSaved()
    },
    onError: (err) => setError(messageOf(err, "Could not save the settings.")),
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setError("")
    save.mutate({ body: { display_currency: currency, fx_source: fx } })
  }

  return (
    <form onSubmit={submit} className="grid gap-6">
      <CardHeader>
        <CardTitle className="text-base">Organization</CardTitle>
        <CardDescription>
          Converted amounts show your display currency next to what the provider bills in.
        </CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="grid gap-2">
          <Label htmlFor="display-currency">Display currency</Label>
          {custom ? (
            <div className="flex gap-2">
              <Input
                id="display-currency"
                className="w-28 font-mono uppercase"
                placeholder="USD"
                minLength={3}
                maxLength={3}
                required
                value={currency}
                onChange={(e) => setCurrency(e.target.value.toUpperCase())}
              />
              <Button type="button" variant="ghost" size="sm" onClick={() => { setCustom(false); setCurrency(initial.display_currency) }}>
                Pick from list
              </Button>
            </div>
          ) : (
            <Select
              value={currency}
              onValueChange={(value) => {
                if (value === "custom") {
                  setCustom(true)
                  setCurrency("")
                } else {
                  setCurrency(value)
                }
              }}
            >
              <SelectTrigger id="display-currency" className="w-full sm:w-64">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {COMMON_CURRENCIES.map((c) => (
                  <SelectItem key={c.code} value={c.code}>
                    {c.code} — {c.name}
                  </SelectItem>
                ))}
                <SelectItem value="custom">Other…</SelectItem>
              </SelectContent>
            </Select>
          )}
          <p className="text-xs text-muted-foreground">
            Pick any currency; exchange rates exist for the common ones.
          </p>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="fx-source">Exchange-rate source</Label>
          <Select value={fx} onValueChange={(value) => setFx(value as FxSource)}>
            <SelectTrigger id="fx-source" className="w-full sm:w-64">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="frankfurter">Frankfurter — open-source ECB data</SelectItem>
              <SelectItem value="ecb">ECB reference rates</SelectItem>
            </SelectContent>
          </Select>
        </div>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
      </CardContent>
      <CardFooter className="gap-2">
        <Button type="submit" disabled={save.isPending || !valid || (currency === initial.display_currency && fx === initial.fx_source)}>
          {save.isPending ? "Saving…" : "Save changes"}
        </Button>
        {save.isSuccess && <span className="text-sm text-muted-foreground">Saved.</span>}
      </CardFooter>
    </form>
  )
}
