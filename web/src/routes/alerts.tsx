import { AlertList } from "@/components/alerts/alert-list"
import { AlertRulesPanel } from "@/components/alerts/alert-rules"
import { useAccounts } from "@/components/alerts/queries"
import { PageHeader } from "@/components/page-header"
import { useMe } from "@/hooks/use-me"

export default function Alerts() {
  const { canEdit, isAdmin } = useMe()
  const accounts = useAccounts()
  const accountNames: Record<string, string> = Object.fromEntries(
    (accounts.data ?? []).map((account) => [account.id, account.name]),
  )

  return (
    <>
      <PageHeader title="Alerts" description="Quota and renewal alerts in one inbox." />
      <AlertList accountNames={accountNames} />
      <AlertRulesPanel canEdit={canEdit} isAdmin={isAdmin} />
    </>
  )
}
