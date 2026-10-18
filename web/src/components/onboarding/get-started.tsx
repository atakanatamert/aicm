import { useState } from "react"
import { Link } from "react-router"
import { toast } from "sonner"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { AGENT_READS_TEXT, EnrollCommands } from "@/components/machines/enroll-commands"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader } from "@/components/ui/card"
import { useMe } from "@/hooks/use-me"

type EnrollTokenCreated = components["schemas"]["EnrollTokenCreated"]

/**
 * The first-run import panel on Overview: install the agent on this computer,
 * or add API accounts by hand. Shown while the org has no accounts and no
 * machines; viewers never see it.
 */
export function GetStarted() {
  const { isAdmin } = useMe()
  const [created, setCreated] = useState<EnrollTokenCreated | undefined>(undefined)
  const create = query.useMutation("post", "/api/enroll-tokens", {
    onSuccess: (data) => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/enroll-tokens"] })
      setCreated(data)
    },
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not create the token."),
  })

  // Poll machines while waiting for the agent, pending accounts once it has
  // connected, so "Connected" and "Found N accounts" appear without a reload.
  const machines = query.useQuery("get", "/api/machines", undefined, {
    refetchInterval: (q) => (!created || (q.state.data?.length ?? 0) > 0 ? false : 5000),
  })
  const connected = (machines.data?.length ?? 0) > 0
  const machine = machines.data?.[0]
  const pending = query.useQuery("get", "/api/accounts", { params: { query: { status: "pending" } } }, {
    refetchInterval: connected ? 5000 : false,
  })
  const found = pending.data?.length ?? 0

  return (
    <Card className="w-full py-6">
      <CardHeader>
        <h2 className="text-lg font-semibold tracking-tight">Import your AI accounts</h2>
      </CardHeader>
      <CardContent>
        <ol className="grid gap-6">
          <li className="flex gap-3">
            <span
              aria-hidden
              className="mt-0.5 grid size-5 shrink-0 place-items-center rounded-full bg-muted text-xs font-medium"
            >
              1
            </span>
            <div className="grid min-w-0 gap-2">
              <h3 className="text-sm font-medium">
                {isAdmin ? "Install the agent on this computer" : "Ask an admin to add this computer"}
              </h3>
              {isAdmin && (
                <>
                  <p className="max-w-prose text-sm text-muted-foreground">{AGENT_READS_TEXT}</p>
                  {!created ? (
                    <div>
                      <Button
                        size="sm"
                        disabled={create.isPending}
                        onClick={() => create.mutate({ body: { ttl_minutes: 60 } })}
                      >
                        {create.isPending ? "Creating…" : "Show the install command"}
                      </Button>
                    </div>
                  ) : (
                    <>
                      <div className="max-w-xl">
                        <EnrollCommands created={created} />
                      </div>
                      {!connected ? (
                        <p aria-live="polite" className="text-sm text-muted-foreground">
                          Waiting for the agent to connect…
                        </p>
                      ) : (
                        <div className="grid gap-2">
                          <p aria-live="polite" className="text-sm font-medium">
                            Connected: {machine?.name}
                          </p>
                          {found > 0 && (
                            <div className="flex flex-wrap items-center gap-3">
                              <p className="text-sm font-medium">
                                Found {found} {found === 1 ? "account" : "accounts"}
                              </p>
                              <Button size="sm" asChild>
                                <Link to="/accounts">Review and confirm</Link>
                              </Button>
                            </div>
                          )}
                        </div>
                      )}
                    </>
                  )}
                </>
              )}
            </div>
          </li>
          <li className="flex gap-3">
            <span
              aria-hidden
              className="mt-0.5 grid size-5 shrink-0 place-items-center rounded-full bg-muted text-xs font-medium"
            >
              2
            </span>
            <div className="grid min-w-0 gap-2">
              <h3 className="text-sm font-medium">Add API accounts</h3>
              <p className="max-w-prose text-sm text-muted-foreground">
                Paste an API key, or enter a plan by hand — no agent needed.
              </p>
              <div>
                <Button size="sm" asChild>
                  <Link to="/accounts?add=1">Add account</Link>
                </Button>
              </div>
            </div>
          </li>
        </ol>
      </CardContent>
    </Card>
  )
}
