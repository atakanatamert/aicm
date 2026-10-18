import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Vault } from "@/components/ui/hairline"

export default function Accounts() {
  return (
    <>
      <PageHeader title="Accounts" description="Provider subscriptions and API accounts." />
      <EmptyState
        figure={<Vault play />}
        title="No accounts yet"
        description="Provider accounts appear here once the agent discovers them or you add one."
      />
    </>
  )
}
