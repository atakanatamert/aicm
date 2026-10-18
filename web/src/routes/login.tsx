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

export default function Login() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = params.get("next") ?? "/"
  const me = query.useQuery("get", "/api/me", undefined, { retry: false })
  const login = query.useMutation("post", "/api/auth/login", {
    onSuccess: () => {
      navigate(next, { replace: true })
    },
  })
  const [email, setEmail] = useState("")
  const [password, setPassword] = useState("")

  if (me.data) {
    return <Navigate to={next} replace />
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    login.mutate({ body: { email, password } })
  }

  const envelope = errorOf(login.error)
  let errorMessage = ""
  if (envelope) {
    if (envelope.error.code === "invalid_login") errorMessage = "Email or password is wrong."
    else if (envelope.error.code === "too_many_attempts") errorMessage = "Too many attempts, try again in a few minutes."
    else errorMessage = envelope.error.message
  }

  return (
    <div className="grid min-h-svh place-items-center px-4">
      <div className="grid w-full max-w-3xl items-center gap-10 md:grid-cols-2">
        <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.15 }}>
          <Card>
            <CardHeader>
              <CardTitle className="text-lg">Log in to {APP_NAME}</CardTitle>
              <CardDescription>Use the account your administrator created for you.</CardDescription>
            </CardHeader>
            <CardContent>
              <form onSubmit={submit} className="grid gap-4" noValidate>
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
            </CardContent>
          </Card>
        </motion.div>
        <Exploded play label={`${APP_NAME} illustration`} className="hidden max-w-md md:block" />
      </div>
    </div>
  )
}
