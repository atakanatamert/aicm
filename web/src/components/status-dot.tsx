import { cn } from "@/lib/utils"

type Tone = "ok" | "bad" | "idle"

const DOT: Record<Tone, string> = {
  ok: "bg-status-ok",
  bad: "bg-status-bad",
  idle: "bg-muted-foreground/40",
}

const TEXT: Record<Tone, string> = {
  ok: "text-foreground",
  bad: "text-status-bad",
  idle: "text-muted-foreground",
}

/** A coloured dot plus its words: green working, red broken, grey unknown. */
export function StatusDot({ tone, children, title }: { tone: Tone; children: React.ReactNode; title?: string }) {
  return (
    <span className={cn("inline-flex items-center gap-1.5", TEXT[tone])} title={title}>
      <span aria-hidden className={cn("size-2 shrink-0 rounded-full", DOT[tone])} />
      {children}
    </span>
  )
}
