import { useState } from "react"
import { query, queryClient } from "@/api/client"
import type { components } from "@/api/schema"
import { ConfirmDialog } from "@/components/machines/confirm-dialog"
import { BudgetDialog } from "@/components/overview/budget-dialog"
import { TableSkeleton } from "@/components/settings/fields"
import { messageOf } from "@/components/settings/helpers"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { useMe } from "@/hooks/use-me"
import { formatMoney } from "@/lib/format"
import { cn } from "@/lib/utils"

type Budget = components["schemas"]["Budget"]

/**
 * One budget's numbers: the share bar, the projection line and, when the
 * month is at or heading past the amount, the reason in text next to the
 * urgent accent.
 */
export function BudgetFacts({ budget }: { budget: Budget }) {
  const urgent =
    budget.percent_used >= 100 ||
    (budget.projected.currency === budget.amount.currency &&
      budget.projected.amount_minor >= budget.amount.amount_minor)
  const reason =
    budget.percent_used >= 100
      ? "Over budget this month"
      : urgent
        ? `Projected to reach ${formatMoney(budget.amount)} — the whole budget`
        : undefined
  const pct = Math.min(100, Math.max(0, budget.percent_used))
  return (
    <div className="grid gap-1.5">
      <div
        role="progressbar"
        aria-label={`${formatMoney(budget.month_to_date)} of ${formatMoney(budget.amount)} used`}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={Math.round(pct)}
        className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
      >
        <div className={cn("h-full rounded-full", urgent ? "bg-urgent" : "bg-primary")} style={{ width: `${pct}%` }} />
      </div>
      <p className="text-xs text-muted-foreground">
        Projected {formatMoney(budget.projected)} this month
        {budget.unconverted_count > 0 && <> · {budget.unconverted_count} charges not converted</>}
        {reason && <span className={cn("font-medium", urgent && "text-urgent")}> · {reason}</span>}
      </p>
    </div>
  )
}

/** Overview card: every budget's month-to-date, projection and edit controls. */
export function BudgetsCard() {
  const { canEdit } = useMe()
  const budgets = query.useQuery("get", "/api/budgets")
  const [adding, setAdding] = useState(false)
  const [editing, setEditing] = useState<Budget | undefined>()
  const [deleting, setDeleting] = useState<Budget | undefined>()

  const remove = query.useMutation("delete", "/api/budgets/{budgetId}", {
    onSuccess: () => {
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/budgets"] })
      void queryClient.invalidateQueries({ queryKey: ["get", "/api/audit"] })
      setDeleting(undefined)
    },
    onError: (err) => messageOf(err, "Could not remove the budget."),
  })

  // Org budget first, then account budgets by name.
  const rows = budgets.data
    ? [...budgets.data].sort((a, b) => {
        if ((a.account_id ?? null) === null) return -1
        if ((b.account_id ?? null) === null) return 1
        return (a.account_name ?? "").localeCompare(b.account_name ?? "")
      })
    : []

  return (
    <Card>
      <CardContent className="px-0 pb-0">
        <div className="flex flex-wrap items-center justify-between gap-3 border-b px-4 pb-3 sm:px-6">
          <div>
            <h2 className="text-base leading-none font-semibold">Budgets</h2>
            <p className="mt-1 text-sm text-muted-foreground">
              This month's charges against a monthly amount, with where the month is heading.
            </p>
          </div>
          {canEdit && (
            <Button size="sm" onClick={() => setAdding(true)}>
              Set budget
            </Button>
          )}
        </div>
        {budgets.isPending ? (
          <div className="px-4 py-4 sm:px-6">
            <TableSkeleton rows={2} />
          </div>
        ) : budgets.isError ? (
          <p role="alert" className="px-4 py-4 text-sm text-destructive sm:px-6">
            {messageOf(budgets.error, "Could not load the budgets.")}
          </p>
        ) : rows.length === 0 ? (
          <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-4 sm:px-6">
            <p className="text-sm text-muted-foreground">
              No budgets yet — a budget compares this month's charges to a monthly amount.
            </p>
            {canEdit && (
              <Button size="sm" variant="outline" onClick={() => setAdding(true)}>
                Set a monthly budget
              </Button>
            )}
          </div>
        ) : (
          <ul>
            {rows.map((budget) => (
              <li key={budget.id} className="border-b px-4 py-3 last:border-b-0 sm:px-6">
                <div className="mb-1.5 flex flex-wrap items-baseline justify-between gap-x-3">
                  <span className="text-sm font-medium">
                    {(budget.account_id ?? null) === null ? "Whole organization" : (budget.account_name ?? "Account")}
                  </span>
                  <span className="flex items-baseline gap-2">
                    <span className="text-sm tabular-nums">
                      {formatMoney(budget.month_to_date)}{" "}
                      <span className="text-muted-foreground">of {formatMoney(budget.amount)}</span>
                    </span>
                    {canEdit && (
                      <span className="flex items-center gap-1">
                        <Button variant="ghost" size="sm" onClick={() => setEditing(budget)}>
                          Edit
                        </Button>
                        <Button variant="ghost" size="sm" onClick={() => setDeleting(budget)}>
                          Remove
                        </Button>
                      </span>
                    )}
                  </span>
                </div>
                <BudgetFacts budget={budget} />
              </li>
            ))}
          </ul>
        )}
        {remove.isError && (
          <p role="alert" className="px-4 pb-3 text-sm text-destructive sm:px-6">
            {messageOf(remove.error, "Could not remove the budget.")}
          </p>
        )}
      </CardContent>

      {(adding || editing) && (
        <BudgetDialog
          budget={editing}
          onClose={() => {
            setAdding(false)
            setEditing(undefined)
          }}
        />
      )}

      <ConfirmDialog
        open={deleting !== undefined}
        onOpenChange={(open) => !open && setDeleting(undefined)}
        title="Remove budget"
        description="This month stops being compared against the amount, and its threshold alerts stop."
        confirmLabel="Remove"
        destructive
        pending={remove.isPending}
        onConfirm={() => deleting && remove.mutate({ params: { path: { budgetId: deleting.id } } })}
      />
    </Card>
  )
}
