import { useSearchParams } from "react-router"
import { AlertList } from "@/components/alerts/alert-list"
import { AlertRulesPanel } from "@/components/alerts/alert-rules"
import { useAccounts } from "@/components/alerts/queries"
import { PageHeader } from "@/components/page-header"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { useMe } from "@/hooks/use-me"

export default function Alerts() {
  const { canEdit, isAdmin } = useMe()
  const [searchParams, setSearchParams] = useSearchParams()
  // Inbox is the default; Rules lives behind ?tab=rules.
  const tab = searchParams.get("tab") === "rules" ? "rules" : "inbox"
  const accounts = useAccounts()
  const accountNames: Record<string, string> = Object.fromEntries(
    (accounts.data ?? []).map((account) => [account.id, account.name]),
  )

  return (
    <>
      <PageHeader title="Alerts" description="Quota and renewal alerts in one inbox." />
      <div className="mx-auto w-full max-w-6xl px-4 sm:px-6">
        <Tabs value={tab} onValueChange={(value) => setSearchParams(value === "inbox" ? {} : { tab: value })}>
          <div className="pb-1 pt-4">
            <TabsList>
              <TabsTrigger value="inbox">Inbox</TabsTrigger>
              <TabsTrigger value="rules">Rules</TabsTrigger>
            </TabsList>
          </div>
          <TabsContent value="inbox">
            <AlertList accountNames={accountNames} />
          </TabsContent>
          <TabsContent value="rules">
            <AlertRulesPanel canEdit={canEdit} isAdmin={isAdmin} />
          </TabsContent>
        </Tabs>
      </div>
    </>
  )
}
