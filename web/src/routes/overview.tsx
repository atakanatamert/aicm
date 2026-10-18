import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Terrain } from "@/components/ui/hairline"

export default function Overview() {
  return (
    <>
      <PageHeader title="Overview" description="Money out this month and year, by provider." />
      <EmptyState
        figure={<Terrain play />}
        title="No spend yet"
        description="Money out this month and year shows here once accounts report charges."
      />
    </>
  )
}
