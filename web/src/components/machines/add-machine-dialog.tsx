import { useState } from "react"
import { Check, Copy } from "lucide-react"
import { toast } from "sonner"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

type EnrollTokenCreated = components["schemas"]["EnrollTokenCreated"]

// One tab per install route, in the order the card names them.
const COMMAND_TABS: { key: keyof NonNullable<EnrollTokenCreated["commands"]>; label: string }[] = [
  { key: "sh", label: "macOS / Linux" },
  { key: "brew", label: "Homebrew" },
  { key: "powershell", label: "Windows" },
  { key: "docker", label: "Docker" },
]

function CommandBlock({ command }: { command: string }) {
  const [copied, setCopied] = useState(false)

  const copy = () => {
    navigator.clipboard
      .writeText(command)
      .then(() => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1500)
      })
      .catch(() => toast.error("Could not copy. Select the command and copy it by hand."))
  }

  return (
    <div className="grid gap-1.5">
      <div className="flex justify-end">
        <Button variant="outline" size="xs" onClick={copy}>
          {copied ? <Check /> : <Copy />}
          {copied ? "Copied" : "Copy"}
        </Button>
      </div>
      <pre className="overflow-x-auto rounded-md bg-muted p-3 text-xs leading-relaxed">
        <code>{command}</code>
      </pre>
    </div>
  )
}

interface AddMachineDialogProps {
  open: boolean
  onOpenChange: (open: boolean) => void
}

export function AddMachineDialog({ open, onOpenChange }: AddMachineDialogProps) {
  const [permanent, setPermanent] = useState(false)
  const [created, setCreated] = useState<EnrollTokenCreated | undefined>(undefined)

  const create = query.useMutation("post", "/api/enroll-tokens", {
    onSuccess: (data) => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/enroll-tokens"] })
      setCreated(data)
    },
    onError: (err) => toast.error(errorOf(err)?.error.message ?? "Could not create the token."),
  })

  const handleOpenChange = (next: boolean) => {
    if (next) {
      setPermanent(false)
      setCreated(undefined)
    }
    onOpenChange(next)
  }

  const commands = created?.commands

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent className="sm:max-w-xl">
        {created === undefined ? (
          <>
            <DialogHeader>
              <DialogTitle>Add a machine</DialogTitle>
              <DialogDescription>
                Create an enrollment token, then run the install command on the new machine. The agent dials the hub and appears here.
              </DialogDescription>
            </DialogHeader>
            <fieldset className="grid gap-2">
              <legend className="text-sm font-medium">Token expiry</legend>
              <div className="flex gap-2">
                <Button variant={permanent ? "outline" : "default"} size="sm" aria-pressed={!permanent} onClick={() => setPermanent(false)}>
                  Expires in 1 hour
                </Button>
                <Button variant={permanent ? "default" : "outline"} size="sm" aria-pressed={permanent} onClick={() => setPermanent(true)}>
                  Permanent
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">A one-hour token is enough to enroll a machine right now; a permanent one keeps enrolling machines over time.</p>
            </fieldset>
            <DialogFooter>
              <Button variant="outline" onClick={() => handleOpenChange(false)}>
                Cancel
              </Button>
              <Button
                disabled={create.isPending}
                onClick={() => create.mutate({ body: permanent ? { permanent: true } : { ttl_minutes: 60 } })}
              >
                {create.isPending ? "Creating…" : "Create token"}
              </Button>
            </DialogFooter>
          </>
        ) : (
          <>
            <DialogHeader>
              <DialogTitle>Enroll the machine</DialogTitle>
              <DialogDescription>
                Run the command for its operating system. The token is shown once — copy it now.
              </DialogDescription>
            </DialogHeader>
            {commands && (
              <Tabs defaultValue="sh" className="min-w-0">
                <TabsList>
                  {COMMAND_TABS.map(
                    ({ key, label }) =>
                      commands[key] && (
                        <TabsTrigger key={key} value={key}>
                          {label}
                        </TabsTrigger>
                      ),
                  )}
                </TabsList>
                {COMMAND_TABS.map(
                  ({ key }) =>
                    commands[key] && (
                      <TabsContent key={key} value={key} className="min-w-0">
                        <CommandBlock command={commands[key] ?? ""} />
                      </TabsContent>
                    ),
                )}
              </Tabs>
            )}
            <DialogFooter>
              <Button variant="outline" onClick={() => handleOpenChange(false)}>
                Close
              </Button>
            </DialogFooter>
          </>
        )}
      </DialogContent>
    </Dialog>
  )
}
