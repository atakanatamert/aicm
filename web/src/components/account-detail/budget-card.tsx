import { useState } from "react"
import { query } from "@/api/client"
import type { components } from "@/api/schema"
import { messageOf } from "@/components/settings/helpers"
import { BudgetDialog } from "@/components/overview/budget-dialog"
import { BudgetFacts } from "@/components/overview/budgets-card"
import { Button } from "@/components/ui/button"
import { useMe } from "@/hooks/use-me"
import { SectionCard } from "./section"

type Account = components["schemas"]["Account"]

/** This account's monthly budget, edited with the Overview's dialog. */
export function BudgetCard({ account }: { account: Account }) {
  const { canEdit } = useMe()
  const budgets = query.useQuery("get", "/api/budgets")
  const [editing, setEditing] = useState(false)

  const budget = budgets.data?.find((b) => (b.account_id ?? null) === account.id)
  // Urgent at 100% of the amount, or when the projection passes it; the
  // reason comes from BudgetFacts' text.
  const urgent =
    budget !== undefined &&
    (budget.percent_used >= 100 ||
      (budget.projected.currency === budget.amount.currency &&
        budget.projected.amount_minor >= budget.amount.amount_minor))

  return (
    <>
      <SectionCard
        title="Budget"
        description="This account's monthly budget against its charges."
        urgent={urgent}
        action={
          canEdit ? (
            <Button variant="outline" size="sm" onClick={() => setEditing(true)}>
              {budget ? "Edit" : "Set budget"}
            </Button>
          ) : undefined
        }
      >
        {budgets.isError ? (
          <p role="alert" className="text-sm text-destructive">
            {messageOf(budgets.error, "Could not load the budgets.")}
          </p>
        ) : budget ? (
          <BudgetFacts budget={budget} />
        ) : budgets.isPending ? null : (
          <p className="text-sm text-muted-foreground">No budget set for this account.</p>
        )}
      </SectionCard>
      {editing && <BudgetDialog budget={budget} fixedAccount={account} onClose={() => setEditing(false)} />}
    </>
  )
}
