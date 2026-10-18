import type { ReactNode } from "react"
import { Terrain } from "@/components/ui/hairline"
import { cn } from "@/lib/utils"

interface EmptyStateProps {
  /** A Hairline figure element; defaults to Terrain. */
  figure?: ReactNode
  title: string
  description: string
  /** One optional next step under the sentence. */
  action?: ReactNode
  className?: string
}

// Hairline fills its parent's width at a 5:4 ratio, so cap it.
export function EmptyState({ figure = <Terrain play />, title, description, action, className }: EmptyStateProps) {
  return (
    <div className={cn("flex flex-col items-center gap-3 py-12 text-center", className)}>
      <div className="w-full max-w-xs sm:max-w-md">{figure}</div>
      <h2 className="mt-2 text-base font-medium">{title}</h2>
      <p className="max-w-sm text-sm text-muted-foreground">{description}</p>
      {action}
    </div>
  )
}
