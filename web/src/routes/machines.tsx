import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Cabinet } from "@/components/ui/hairline"

export default function Machines() {
  return (
    <>
      <PageHeader title="Machines" description="Enrolled agents reporting from your machines." />
      <EmptyState
        figure={<Cabinet play />}
        title="No machines yet"
        description="Enroll an agent with an enrollment token and it appears here."
      />
    </>
  )
}
