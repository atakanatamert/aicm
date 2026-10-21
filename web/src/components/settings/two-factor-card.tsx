import { useEffect, useRef, useState, type FormEvent } from "react"
import { Download } from "lucide-react"
import { toast } from "sonner"
import { query, queryClient } from "@/api/client"
import { ConfirmDialog } from "@/components/machines/confirm-dialog"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { formatDate } from "@/lib/format"
import { CopyField } from "./fields"
import { copyText, messageOf } from "./helpers"

type TotpSetup = {
  secret: string
  otpauth_url: string
}

/**
 * Two-factor sign-in: set up with a QR code, save the recovery codes once,
 * turn off with password and code.
 */
export function TwoFactorCard() {
  const totp = query.useQuery("get", "/api/me/totp")
  const [setup, setSetup] = useState<TotpSetup | null>(null)
  const [codes, setCodes] = useState<string[] | null>(null)

  const begin = query.useMutation("post", "/api/me/totp/setup", {
    onSuccess: (data) => setSetup(data),
    onError: (err) => toast.error(messageOf(err, "Could not start the setup.")),
  })

  const close = () => {
    setSetup(null)
    setCodes(null)
    queryClient.invalidateQueries({ queryKey: ["get", "/api/me/totp"] })
  }

  let body = <Skeleton className="h-40 w-full" />
  if (!totp.isPending) {
    if (codes) {
      body = <RecoveryCodes codes={codes} onDone={close} />
    } else if (setup) {
      body = <SetupForm setup={setup} onCancel={() => setSetup(null)} onEnabled={setCodes} />
    } else if (totp.data?.enabled) {
      body = <EnabledStatus status={totp.data} />
    } else {
      body = <OffStatus onTurnOn={() => begin.mutate({})} pending={begin.isPending} />
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Two-factor sign-in</CardTitle>
        <CardDescription>A second factor protects your account even if the password leaks.</CardDescription>
      </CardHeader>
      <CardContent>{body}</CardContent>
    </Card>
  )
}

function OffStatus({ onTurnOn, pending }: { onTurnOn: () => void; pending: boolean }) {
  return (
    <div className="grid gap-4">
      <p className="text-sm text-muted-foreground">
        Turn on two-factor sign-in and confirm logins with an authenticator app.
      </p>
      <div>
        <Button onClick={onTurnOn} disabled={pending}>
          {pending ? "Preparing…" : "Turn on"}
        </Button>
      </div>
    </div>
  )
}

function SetupForm({ setup, onCancel, onEnabled }: { setup: TotpSetup; onCancel: () => void; onEnabled: (codes: string[]) => void }) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null)
  const [qrFailed, setQrFailed] = useState(false)
  const [code, setCode] = useState("")
  const [error, setError] = useState("")

  // Rendered locally from the otpauth URL; the secret never leaves the browser.
  // qrcode is loaded on demand: it is only needed during setup.
  useEffect(() => {
    const canvas = canvasRef.current
    if (!canvas) return
    let cancelled = false
    import("qrcode")
      .then((QR) => QR.toCanvas(canvas, setup.otpauth_url, { width: 176, margin: 1 }))
      .then(() => {
        if (!cancelled) setQrFailed(false)
      })
      .catch(() => {
        if (!cancelled) setQrFailed(true)
      })
    return () => {
      cancelled = true
    }
  }, [setup.otpauth_url])

  const enable = query.useMutation("post", "/api/me/totp/enable", {
    onSuccess: (data) => onEnabled(data.recovery_codes),
    onError: (err) => setError(messageOf(err, "That code didn't work.")),
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setError("")
    enable.mutate({ body: { code } })
  }

  return (
    <form onSubmit={submit} className="grid gap-4" noValidate>
      <div className="grid gap-2">
        <span className="text-sm">Scan with your authenticator app</span>
        {qrFailed ? (
          <p role="alert" className="text-sm text-destructive">Could not render the QR code — type the secret below instead.</p>
        ) : (
          <canvas ref={canvasRef} aria-label="QR code for the authenticator app" className="justify-self-start rounded-md border bg-white p-1" />
        )}
      </div>
      <CopyField label="Secret for manual entry" value={setup.secret} />
      <div className="grid gap-2">
        <Label htmlFor="totp-code">Code from the app</Label>
        <Input
          id="totp-code"
          className="w-32 font-mono"
          inputMode="numeric"
          autoComplete="one-time-code"
          autoFocus
          maxLength={6}
          required
          value={code}
          onChange={(e) => setCode(e.target.value)}
        />
      </div>
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      <div className="flex justify-between gap-2">
        <Button type="button" variant="ghost" onClick={onCancel}>
          Cancel
        </Button>
        <Button type="submit" disabled={enable.isPending || code.length < 6}>
          {enable.isPending ? "Confirming…" : "Confirm"}
        </Button>
      </div>
    </form>
  )
}

function RecoveryCodes({ codes, onDone }: { codes: string[]; onDone: () => void }) {
  const download = () => {
    const url = URL.createObjectURL(new Blob([codes.join("\n") + "\n"], { type: "text/plain" }))
    const a = document.createElement("a")
    a.href = url
    a.download = "recovery-codes.txt"
    a.click()
    URL.revokeObjectURL(url)
  }

  return (
    <div className="grid gap-4">
      <p className="text-sm">
        Each recovery code works once instead of an authenticator code. <span className="font-medium text-destructive">They are shown only this once — save them now.</span>
      </p>
      <div className="grid grid-cols-2 gap-2 sm:grid-cols-2">
        {codes.map((code) => (
          <code key={code} className="rounded-md border bg-muted/40 px-2 py-1.5 text-center font-mono text-sm">
            {code}
          </code>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <Button variant="outline" onClick={() => copyText(codes.join("\n"))}>
          Copy
        </Button>
        <Button variant="outline" onClick={download}>
          <Download />
          Download
        </Button>
      </div>
      <div>
        <Button onClick={onDone}>I saved them</Button>
      </div>
    </div>
  )
}

function EnabledStatus({ status }: { status: { enabled_at?: string | null; recovery_codes_left: number } }) {
  const [open, setOpen] = useState(false)
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")
  const [error, setError] = useState("")

  const disable = query.useMutation("delete", "/api/me/totp", {
    onSuccess: () => {
      toast.success("Two-factor turned off.")
      setOpen(false)
      setPassword("")
      setCode("")
      setError("")
      queryClient.invalidateQueries({ queryKey: ["get", "/api/me/totp"] })
    },
    onError: (err) => setError(messageOf(err, "Could not turn two-factor off.")),
  })

  const submit = () => {
    setError("")
    disable.mutate({ body: { password, code } })
  }

  return (
    <div className="grid gap-4">
      <p className="text-sm text-muted-foreground">
        On{status.enabled_at ? ` since ${formatDate(status.enabled_at)}` : ""} · {status.recovery_codes_left} recovery codes left
      </p>
      <div>
        <Button variant="outline" onClick={() => setOpen(true)}>
          Turn off
        </Button>
      </div>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Turn off two-factor?"
        description="Your account will again be protected by the password alone. You can turn it back on at any time."
        confirmLabel="Turn off"
        destructive
        pending={disable.isPending}
        onConfirm={submit}
      >
        <div className="grid gap-4 pb-4">
          <div className="grid gap-2">
            <Label htmlFor="totp-off-password">Password</Label>
            <Input
              id="totp-off-password"
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
          <div className="grid gap-2">
            <Label htmlFor="totp-off-code">Code</Label>
            <Input
              id="totp-off-code"
              className="w-32 font-mono"
              inputMode="numeric"
              autoComplete="one-time-code"
              maxLength={6}
              required
              value={code}
              onChange={(e) => setCode(e.target.value)}
            />
          </div>
          {error && (
            <p role="alert" className="text-sm text-destructive">
              {error}
            </p>
          )}
        </div>
      </ConfirmDialog>
    </div>
  )
}
