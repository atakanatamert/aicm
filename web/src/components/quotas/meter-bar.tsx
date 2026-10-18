import { motion } from "motion/react"

interface MeterBarProps {
  value: number
  max: number
  label: string
}

/** Thin neutral fill; animates once on mount (150 ms). Urgency lives on the card border. */
export function MeterBar({ value, max, label }: MeterBarProps) {
  const pct = max > 0 ? Math.min(100, Math.max(0, (value / max) * 100)) : 0
  return (
    <div
      role="progressbar"
      aria-label={label}
      aria-valuemin={0}
      aria-valuemax={100}
      aria-valuenow={Math.round(pct)}
      className="h-1.5 w-full overflow-hidden rounded-full bg-muted"
    >
      <motion.div
        className="h-full rounded-full bg-primary"
        initial={{ width: 0 }}
        animate={{ width: `${pct}%` }}
        transition={{ duration: 0.15, ease: "easeOut" }}
      />
    </div>
  )
}
