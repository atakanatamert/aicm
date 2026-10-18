import { useState, type FormEvent } from "react"
import { Navigate, useNavigate, useSearchParams } from "react-router"
import { motion } from "motion/react"
import { errorOf, query } from "@/api/client"
import { Padlock } from "@/components/ui/hairline"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { APP_NAME } from "@/lib/brand"

const MIN_PASSWORD_LENGTH = 12

export default function Setup() {
  const navigate = useNavigate()
  const [params] = useSearchParams()
  const next = params.get("next") ?? "/"
  const setup = query.useQuery("get", "/api/setup", undefined, { retry: false })
  const create = query.useMutation("post", "/api/setup", {
    onSuccess: () => {
      navigate(next, { replace: true })
    },
    onError: (err: unknown) => {
      // Someone else created the first admin while we were here.
      if (errorOf(err)?.error.code === "setup_done") {
        navigate("/login", { replace: true })
      }
    },
  })
  const [email, setEmail] = useState("")
  const [name, setName] = useState("")
  const [password, setPassword] = useState("")
  const [confirm, setConfirm] = useState("")
  const [formError, setFormError] = useState("")

  if (setup.isPending) {
    return <div className="grid min-h-svh place-items-center text-sm text-muted-foreground">Loading…</div>
  }
  if (setup.data && !setup.data.needed) {
    return <Navigate to={next} replace />
  }

  const submit = (event: FormEvent) => {
    event.preventDefault()
    setFormError("")
    if (password.length < MIN_PASSWORD_LENGTH) {
      setFormError(`Password must be at least ${MIN_PASSWORD_LENGTH} characters.`)
      return
    }
    if (password !== confirm) {
      setFormError("Passwords do not match.")
      return
    }
    create.mutate({ body: { email, name, password } })
  }

  const serverError = errorOf(create.error)?.error.message

  return (
    <div className="grid min-h-svh place-items-center px-4">
      <motion.div initial={{ opacity: 0 }} animate={{ opacity: 1 }} transition={{ duration: 0.15 }} className="grid w-full max-w-3xl items-center gap-10 md:grid-cols-2">
        <Card>
          <CardHeader>
            <CardTitle className="text-lg">Welcome to {APP_NAME}</CardTitle>
            <CardDescription>Create the first admin account. This only shows once.</CardDescription>
          </CardHeader>
          <CardContent>
            <form onSubmit={submit} className="grid gap-4" noValidate>
              <div className="grid gap-2">
                <Label htmlFor="email">Email</Label>
                <Input id="email" type="email" autoComplete="email" required value={email} onChange={(e) => setEmail(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="name">Name</Label>
                <Input id="name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} />
              </div>
              <div className="grid gap-2">
                <Label htmlFor="password">Password</Label>
                <Input id="password" type="password" autoComplete="new-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
                <p className="text-xs text-muted-foreground">At least {MIN_PASSWORD_LENGTH} characters.</p>
              </div>
              <div className="grid gap-2">
                <Label htmlFor="confirm">Confirm password</Label>
                <Input id="confirm" type="password" autoComplete="new-password" required value={confirm} onChange={(e) => setConfirm(e.target.value)} />
              </div>
              {(formError || serverError) && (
                <p role="alert" className="text-sm text-destructive">
                  {formError || serverError}
                </p>
              )}
              <Button type="submit" disabled={create.isPending}>
                {create.isPending ? "Creating…" : "Create admin"}
              </Button>
            </form>
          </CardContent>
        </Card>
        <Padlock play label={`${APP_NAME} illustration`} className="hidden max-w-md md:block" />
      </motion.div>
    </div>
  )
}
