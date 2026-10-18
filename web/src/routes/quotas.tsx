import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Elevator } from "@/components/ui/hairline"

export default function Quotas() {
  return (
    <>
      <PageHeader title="Quotas" description="Provider meters and renewals, sorted by urgency." />
      <EmptyState
        figure={<Elevator play />}
        title="No meters yet"
        description="Each account's quota and renewal shows here as a provider-native meter."
      />
    </>
  )
}
