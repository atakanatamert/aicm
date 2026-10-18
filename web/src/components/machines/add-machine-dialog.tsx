import { useState } from "react"
import { toast } from "sonner"
import { errorOf, query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { EnrollCommands } from "@/components/machines/enroll-commands"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"

type EnrollTokenCreated = components["schemas"]["EnrollTokenCreated"]

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
                Run the command for its operating system. The agent dials the hub and appears on Machines.
              </DialogDescription>
            </DialogHeader>
            {created && <EnrollCommands created={created} />}
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
