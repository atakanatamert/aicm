import { useId } from "react"
import { Copy } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Skeleton } from "@/components/ui/skeleton"
import { copyText } from "./helpers"

/** Read-only field for a one-time secret (API token, temporary password). */
export function CopyField({ label, value }: { label: string; value: string }) {
  const id = useId()
  return (
    <div className="grid gap-1.5">
      <Label htmlFor={id}>{label}</Label>
      <div className="flex gap-2">
        <Input
          id={id}
          readOnly
          value={value}
          className="font-mono text-sm"
          onFocus={(e) => e.currentTarget.select()}
        />
        <Button
          type="button"
          variant="outline"
          size="icon"
          aria-label={`Copy ${label}`}
          onClick={() => copyText(value)}
        >
          <Copy />
        </Button>
      </div>
    </div>
  )
}

/** Placeholder rows while a list loads. */
export function TableSkeleton({ rows = 5 }: { rows?: number }) {
  return (
    <div className="space-y-2" aria-hidden>
      {Array.from({ length: rows }, (_, i) => (
        <Skeleton key={i} className="h-10 w-full" />
      ))}
    </div>
  )
}

/** A labeled JSON block, used for the audit log's before/after diff. */
export function JsonBlock({ label, value }: { label: string; value: object | null | undefined }) {
  return (
    <div className="grid min-w-0 gap-1">
      <span className="text-xs font-medium text-muted-foreground">{label}</span>
      <pre className="max-h-64 overflow-auto rounded-md border bg-muted/40 p-3 font-mono text-xs">
        {value ? JSON.stringify(value, null, 2) : "—"}
      </pre>
    </div>
  )
}
