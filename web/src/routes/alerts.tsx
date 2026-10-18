import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Relay } from "@/components/ui/hairline"

export default function Alerts() {
  return (
    <>
      <PageHeader title="Alerts" description="Quota and renewal alerts in one inbox." />
      <EmptyState
        figure={<Relay play />}
        title="No alerts yet"
        description="Alerts land here as they fire, until you mark them read."
      />
    </>
  )
}
