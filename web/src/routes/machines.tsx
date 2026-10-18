import { useState } from "react"
import { Plus } from "lucide-react"
import { errorOf, query } from "@/api/client"
import { AddMachineDialog } from "@/components/machines/add-machine-dialog"
import { EnrollTokens } from "@/components/machines/enroll-tokens"
import { MachineTable } from "@/components/machines/machine-table"
import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Button } from "@/components/ui/button"
import { Cabinet } from "@/components/ui/hairline"
import { Skeleton } from "@/components/ui/skeleton"
import { useMe } from "@/hooks/use-me"

export default function Machines() {
  const { isAdmin } = useMe()
  const machines = query.useQuery("get", "/api/machines")
  const [addOpen, setAddOpen] = useState(false)

  return (
    <>
      <PageHeader
        title="Machines"
        description="Enrolled agents reporting from your machines."
        actions={
          isAdmin ? (
            <Button size="sm" onClick={() => setAddOpen(true)}>
              <Plus />
              Add a machine
            </Button>
          ) : undefined
        }
      />

      <div className="mx-auto w-full max-w-6xl px-4 sm:px-6">
        {machines.isPending ? (
          <div className="grid gap-2 py-4">
            {Array.from({ length: 3 }, (_, i) => (
              <Skeleton key={i} className="h-10 w-full" />
            ))}
          </div>
        ) : machines.isError ? (
          <p role="alert" className="py-6 text-sm text-destructive">
            {errorOf(machines.error)?.error.message ?? "Could not load the machines."}
          </p>
        ) : machines.data.length === 0 ? (
          <div className="flex flex-col items-center">
            <EmptyState
              figure={<Cabinet play />}
              title="No machines yet"
              description="Add a machine and run the install command on it — the agent appears here once it dials in."
            />
            {isAdmin && (
              <Button size="sm" className="mb-6" onClick={() => setAddOpen(true)}>
                <Plus />
                Add a machine
              </Button>
            )}
          </div>
        ) : (
          <section aria-label="Machines" className="py-4">
            <MachineTable machines={machines.data} isAdmin={isAdmin} />
          </section>
        )}

        {isAdmin && (
          <div className="py-8">
            <EnrollTokens />
          </div>
        )}
      </div>

      <AddMachineDialog open={addOpen} onOpenChange={setAddOpen} />
    </>
  )
}
