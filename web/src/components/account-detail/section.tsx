import type { ReactNode } from "react"
import { errorOf } from "@/api/client"
import { Card, CardContent, CardDescription, CardHeader } from "@/components/ui/card"
import { cn } from "@/lib/utils"

interface SectionCardProps {
  title: string
  description?: string
  /** Rendered on the right of the title, e.g. an Edit button. */
  action?: ReactNode
  /** Urgent border; the caller states the reason in text next to it. */
  urgent?: boolean
  children: ReactNode
}

/** One block of the account page. The urgent accent needs a text reason nearby. */
export function SectionCard({ title, description, action, urgent, children }: SectionCardProps) {
  return (
    <Card className={cn("gap-4 py-5", urgent && "border-urgent")}>
      <CardHeader className="flex flex-row flex-wrap items-start justify-between gap-2">
        <div className="space-y-1">
          <h2 className="text-base leading-none font-semibold">{title}</h2>
          {description && <CardDescription>{description}</CardDescription>}
        </div>
        {action}
      </CardHeader>
      <CardContent>{children}</CardContent>
    </Card>
  )
}

/** Inline error from the API's error envelope, or a fallback. */
export function ErrorLine({ error, fallback }: { error: unknown; fallback: string }) {
  return (
    <p role="alert" className="text-sm text-destructive">
      {errorOf(error)?.error.message ?? fallback}
    </p>
  )
}
