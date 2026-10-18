import { useState, type FormEvent } from "react"
import { useNavigate } from "react-router"
import { toast } from "sonner"
import { errorOf, query, queryClient } from "@/api/client"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { SectionCard } from "./section"

/**
 * Danger zone (admins): archive the account after typing its name. History
 * is kept; the hub's lists stop showing it.
 */
export function ArchiveCard({ accountId, accountName }: { accountId: string; accountName: string }) {
  const navigate = useNavigate()
  const [open, setOpen] = useState(false)
  const [typed, setTyped] = useState("")

  const archive = query.useMutation("post", "/api/accounts/{accountId}/archive", {
    onSuccess: () => {
      toast(`Account “${accountName}” archived.`)
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/accounts"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/quotas"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/renewals"] })
      void navigate("/accounts")
    },
  })

  const submit = (event: FormEvent) => {
    event.preventDefault()
    archive.mutate({ params: { path: { accountId } }, body: { confirm: true } })
  }

  const matches = typed === accountName
  const errorMessage = errorOf(archive.error)?.error.message

  return (
    <SectionCard title="Archive" description="Hides the account and stops syncing it. Its history is kept.">
      <div>
        <Button variant="destructive" size="sm" onClick={() => setOpen(true)}>
          Archive account
        </Button>
      </div>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>Archive {accountName}?</DialogTitle>
            <DialogDescription>
              The account disappears from lists and stops syncing. There is no unarchive in the UI. Type the account name to confirm.
            </DialogDescription>
          </DialogHeader>
          <form onSubmit={submit} className="grid gap-4" noValidate>
            <div className="grid gap-2">
              <Label htmlFor="archive-confirm">Account name</Label>
              <Input
                id="archive-confirm"
                required
                autoComplete="off"
                placeholder={accountName}
                value={typed}
                onChange={(e) => setTyped(e.target.value)}
              />
            </div>
            {errorMessage && (
              <p role="alert" className="text-sm text-destructive">
                {errorMessage}
              </p>
            )}
            <DialogFooter>
              <Button type="button" variant="ghost" onClick={() => setOpen(false)}>
                Cancel
              </Button>
              <Button type="submit" variant="destructive" disabled={!matches || archive.isPending}>
                {archive.isPending ? "Archiving…" : "Archive account"}
              </Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </SectionCard>
  )
}
