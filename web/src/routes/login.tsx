import { useState, type FormEvent } from "react"
import { Navigate, useNavigate, useSearchParams } from "react-router"
import { motion } from "motion/react"
import { errorOf, query } from "@/api/client"
import { Exploded } from "@/components/ui/hairline"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { APP_NAME } from "@/lib/brand"

/** Messages for ?error=… set by the SSO redirect back to /login. */
const SSO_ERRORS: Record<string, string> = {
  sso_unknown_user: "No aicm user has that email. Ask an admin to add you.",
  sso_unavailable: "Single sign-on is not reachable right now.",
}

export default function Login() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = params.get("next") ?? "/"
  const ssoError = params.get("error")
  const me = query.useQuery("get", "/api/me", undefined, { retry: false })
  const oidc = query.useQuery("get", "/api/auth/oidc", undefined, { retry: false })
  // After a 202 the password was accepted but a second factor is still needed;
  // the challenge ties the code step to that first step.
  const [challenge, setChallenge] = useState<string | null>(null)
  const login = query.useMutation("post", "/api/auth/login", {
    onSuccess: (data) => {
      if ("totp_required" in data) setChallenge(data.challenge)
      else navigate(next, { replace: true })
    },
  })
  const totp = query.useMutation("post", "/api/auth/login/totp", {
    onSuccess: () => {
      navigate(next, { replace: true })
    },
  })
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")
  const [code, setCode] = useState("")

  if (me.data) {
    return <Navigate to={next} replace />
  }

  const submitPassword = (event: FormEvent) => {
    event.preventDefault()
    login.mutate({ body: { email, password } })
  }

  const submitCode = (event: FormEvent) => {
    event.preventDefault()
    if (!challenge) return
    totp.mutate({ body: { challenge, code } })
  }

  const back = () => {
    setChallenge(null)
    setCode("")
    login.reset()
    totp.reset()
  }

  const envelope = errorOf(login.error)
  let errorMessage = ""
  if (envelope) {
    if (envelope.error.code === "invalid_login") errorMessage = "Email or password is wrong."
    else if (envelope.error.code === "too_many_attempts") errorMessage = "Too many attempts, try again in a few minutes."
    else errorMessage = envelope.error.message
  }
  const codeEnvelope = errorOf(totp.error)
  let codeMessage = ""
  if (codeEnvelope) {
    if (codeEnvelope.error.code === "invalid_code") codeMessage = "That code didn't work"
    else if (codeEnvelope.error.code === "challenge_expired") codeMessage = "Start again: the sign-in timed out"
    else if (codeEnvelope.error.code === "too_many_attempts") codeMessage = "Too many attempts"
    else codeMessage = codeEnvelope.error.message
  }

  return (
    <div className="grid min-h-svh place-items-center px-4">
      <div className="grid w-full max-w-3xl items-center gap-10 md:grid-cols-2">
        <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.15 }}>
          <Card>
            <CardHeader>
              <CardTitle className="text-lg">Log in to {APP_NAME}</CardTitle>
              <CardDescription>
                {challenge
                  ? "Enter the 6-digit code from your authenticator app."
                  : "Use the account your administrator created for you."}
              </CardDescription>
            </CardHeader>
            <CardContent className="grid gap-4">
              {challenge ? (
                <form onSubmit={submitCode} className="grid gap-4" noValidate>
                  <div className="grid gap-2">
                    <Label htmlFor="code">Code</Label>
                    <Input
                      id="code"
                      inputMode="numeric"
                      autoComplete="one-time-code"
                      autoFocus
                      required
                      value={code}
                      onChange={(e) => setCode(e.target.value)}
                    />
                    <p className="text-xs text-muted-foreground">
                      Use a recovery code if you do not have your authenticator.
                    </p>
                  </div>
                  {codeMessage && (
                    <p role="alert" className="text-sm text-destructive">
                      {codeMessage}
                    </p>
                  )}
                  <div className="flex justify-between gap-2">
                    <Button type="button" variant="ghost" onClick={back}>
                      Back
                    </Button>
                    <Button type="submit" disabled={totp.isPending || code.length === 0}>
                      {totp.isPending ? "Checking…" : "Log in"}
                    </Button>
                  </div>
                </form>
              ) : (
                <>
                  <form onSubmit={submitPassword} className="grid gap-4" noValidate>
                    <div className="grid gap-2">
                      <Label htmlFor="email">Email</Label>
                      <Input id="email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
                    </div>
                    <div className="grid gap-2">
                      <Label htmlFor="password">Password</Label>
                      <Input id="password" type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
                    </div>
                    {errorMessage && (
                      <p role="alert" className="text-sm text-destructive">
                        {errorMessage}
                      </p>
                    )}
                    <Button type="submit" disabled={login.isPending}>
                      {login.isPending ? "Logging in…" : "Log in"}
                    </Button>
                  </form>
                  {oidc.data?.enabled && oidc.data.start_url && (
                    <Button variant="outline" asChild>
                      <a href={oidc.data.start_url}>Sign in with {oidc.data.name || "SSO"}</a>
                    </Button>
                  )}
                  {ssoError && (
                    <p role="alert" className="text-sm text-destructive">
                      {SSO_ERRORS[ssoError] ?? "Single sign-on sign-in failed. Try again."}
                    </p>
                  )}
                </>
              )}
            </CardContent>
          </Card>
        </motion.div>
        <Exploded play label={`${APP_NAME} illustration`} className="hidden max-w-md md:block" />
      </div>
    </div>
  )
}
