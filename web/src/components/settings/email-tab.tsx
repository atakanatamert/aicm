import { useState, type FormEvent } from "react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { TableSkeleton } from "./fields"
import { messageOf } from "./helpers"

type Settings = components["schemas"]["Settings"]
type Tls = components["schemas"]["SmtpSettings"]["tls"]
type SmtpUpdate = NonNullable<components["schemas"]["SettingsUpdate"]["smtp"]>

const TLS_OPTIONS: { value: Tls; label: string }[] = [
  { value: "none", label: "None" },
  { value: "starttls", label: "STARTTLS" },
  { value: "tls", label: "TLS (implicit)" },
]

export function EmailTab() {
  const settings = query.useQuery("get", "/api/settings")
  const [savedAt, setSavedAt] = useState(0)

  return (
    <section className="px-6 py-6">
      <Card className="max-w-xl">
        {settings.isPending ? (
          <CardContent>
            <TableSkeleton rows={4} />
          </CardContent>
        ) : settings.isError ? (
          <CardContent>
            <p role="alert" className="text-sm text-destructive">
              {messageOf(settings.error, "Could not load the settings.")}
            </p>
          </CardContent>
        ) : (
          <EmailForm key={savedAt} initial={settings.data} onSaved={() => setSavedAt(Date.now())} />
        )}
      </Card>
    </section>
  )
}

function EmailForm({ initial, onSaved }: { initial: Settings; onSaved: () => void }) {
  const [host, setHost] = useState(initial.smtp.host)
  const [port, setPort] = useState(String(initial.smtp.port))
  const [tls, setTls] = useState<Tls>(initial.smtp.tls)
  const [username, setUsername] = useState(initial.smtp.username)
  const [from, setFrom] = useState(initial.smtp.from)
  // "keep": a password is set and left alone. "edit": a new one was typed.
  // "clear": send the empty string, which deletes the stored password.
  const [passwordState, setPasswordState] = useState<"keep" | "edit" | "clear">(
    initial.smtp.password.set ? "keep" : "edit",
  )
  const [password, setPassword] = useState("")
  const [error, setError] = useState("")
  const [testTo, setTestTo] = useState("")
  const [testError, setTestError] = useState("")

  const portValid = /^\d+$/.test(port) && Number(port) >= 1 && Number(port) <= 65535
  const save = query.useMutation("patch", "/api/settings", {
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: ["get", "/api/settings"] })
      queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
      onSaved()
    },
    onError: (err) => setError(messageOf(err, "Could not save the email settings.")),
  })
  const send = query.useMutation("post", "/api/settings/smtp/test", {
    onSuccess: () => {
      toast.success("Test email sent.")
      setTestError("")
    },
    onError: (err) => setTestError(messageOf(err, "Could not send the test email.")),
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setError("")
    const smtp: SmtpUpdate = { host, port: Number(port), username, tls, from }
    if (passwordState === "clear") smtp.password = ""
    else if (passwordState === "edit" && password.length > 0) smtp.password = password
    save.mutate({ body: { smtp } })
  }

  return (
    <form onSubmit={submit} className="grid gap-6">
      <CardHeader>
        <CardTitle className="text-base">Email</CardTitle>
        <CardDescription>Sends renewal and quota alerts.</CardDescription>
      </CardHeader>
      <CardContent className="grid gap-4">
        <div className="grid gap-4 sm:grid-cols-[1fr_8rem]">
          <div className="grid gap-2">
            <Label htmlFor="smtp-host">SMTP host</Label>
            <Input
              id="smtp-host"
              placeholder="smtp.example.com"
              required
              value={host}
              onChange={(e) => setHost(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="smtp-port">Port</Label>
            <Input
              id="smtp-port"
              type="number"
              min={1}
              max={65535}
              required
              aria-invalid={!portValid}
              value={port}
              onChange={(e) => setPort(e.target.value)}
            />
          </div>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="smtp-tls">Encryption</Label>
          <Select value={tls} onValueChange={(value) => setTls(value as Tls)}>
            <SelectTrigger id="smtp-tls" className="w-full sm:w-64">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {TLS_OPTIONS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        </div>
        <div className="grid gap-2">
          <Label htmlFor="smtp-username">Username</Label>
          <Input
            id="smtp-username"
            autoComplete="off"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
          />
        </div>
        <div className="grid gap-2">
          <Label htmlFor="smtp-password">Password</Label>
          {passwordState === "keep" ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm text-muted-foreground">
                Set, ends in {initial.smtp.password.last4 ?? "…"}
              </span>
              <Button type="button" variant="outline" size="sm" onClick={() => setPasswordState("edit")}>
                Replace
              </Button>
              <Button type="button" variant="ghost" size="sm" onClick={() => setPasswordState("clear")}>
                Clear
              </Button>
            </div>
          ) : passwordState === "clear" ? (
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm text-muted-foreground">Removed when you save.</span>
              <Button type="button" variant="outline" size="sm" onClick={() => setPasswordState("keep")}>
                Undo
              </Button>
            </div>
          ) : (
            <Input
              id="smtp-password"
              type="password"
              autoComplete="new-password"
              placeholder={initial.smtp.password.set ? "New password" : "SMTP password"}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}
        </div>
        <div className="grid gap-2">
          <Label htmlFor="smtp-from">From address</Label>
          <Input
            id="smtp-from"
            type="email"
            placeholder="aicm@example.com"
            value={from}
            onChange={(e) => setFrom(e.target.value)}
          />
        </div>
        {error && (
          <p role="alert" className="text-sm text-destructive">
            {error}
          </p>
        )}
        <div className="grid gap-2 rounded-lg border bg-muted/30 p-4">
          <Label htmlFor="smtp-test">Send test email</Label>
          <div className="flex gap-2">
            <Input
              id="smtp-test"
              type="email"
              placeholder="you@example.com"
              required
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
            />
            <Button
              type="button"
              variant="outline"
              disabled={send.isPending || testTo.length === 0}
              onClick={() => send.mutate({ body: { to: testTo } })}
            >
              {send.isPending ? "Sending…" : "Send"}
            </Button>
          </div>
          <p className="text-xs text-muted-foreground">Uses the saved SMTP settings, not the form above.</p>
          {testError && (
            <p role="alert" className="text-sm text-destructive">
              {testError}
            </p>
          )}
        </div>
      </CardContent>
      <CardFooter className="gap-2">
        <Button
          type="submit"
          disabled={save.isPending || !portValid || host.length === 0}
        >
          {save.isPending ? "Saving…" : "Save changes"}
        </Button>
        {save.isSuccess && <span className="text-sm text-muted-foreground">Saved.</span>}
      </CardFooter>
    </form>
  )
}
