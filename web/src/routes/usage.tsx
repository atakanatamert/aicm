import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Phosphor } from "@/components/ui/hairline"

export default function Usage() {
  return (
    <>
      <PageHeader title="Usage" description="Tokens and cost estimates by model and day." />
      <EmptyState
        figure={<Phosphor play />}
        title="No usage yet"
        description="Per-model usage shows here once machines start reporting."
      />
    </>
  )
}
