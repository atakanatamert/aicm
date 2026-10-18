import { EmptyState } from "@/components/empty-state"
import { PageHeader } from "@/components/page-header"
import { Riffle } from "@/components/ui/hairline"

export default function Settings() {
  return (
    <>
      <PageHeader title="Settings" description="Users, grants, SMTP, currency and the audit log." />
      <EmptyState
        figure={<Riffle play />}
        title="Nothing to configure yet"
        description="Users, grants, SMTP, display currency and the audit log are managed here."
      />
    </>
  )
}
