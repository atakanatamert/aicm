import type { ReactNode } from "react"

interface PageHeaderProps {
  title: string
  description: string
  /** Right-aligned controls for the page's primary action. */
  actions?: ReactNode
}

export function PageHeader({ title, description, actions }: PageHeaderProps) {
  return (
    <div className="border-b px-6 py-5">
      <div className={actions ? "flex flex-wrap items-center justify-between gap-3" : undefined}>
        <div className="min-w-0">
          <h1 className="text-lg font-semibold tracking-tight">{title}</h1>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
        {actions && <div className="flex shrink-0 items-center gap-2">{actions}</div>}
      </div>
    </div>
  )
}
