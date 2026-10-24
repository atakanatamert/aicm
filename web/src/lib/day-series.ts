const DAY_MS = 86_400_000

/** Epoch ms of a "YYYY-MM-DD" UTC calendar day (any time suffix is ignored). */
function utcDay(day: string): number {
  return Date.parse(`${day.slice(0, 10)}T00:00:00Z`)
}

/**
 * Every UTC calendar day from `from` to `to` inclusive, as "YYYY-MM-DD".
 * Returns [] for unparseable or reversed bounds.
 */
export function utcDaysBetween(from: string, to: string): string[] {
  const start = utcDay(from)
  const end = utcDay(to)
  if (Number.isNaN(start) || Number.isNaN(end)) return []
  const days: string[] = []
  for (let t = start; t <= end; t += DAY_MS) days.push(new Date(t).toISOString().slice(0, 10))
  return days
}

/**
 * Fills day gaps with `empty(day)` so a categorical axis shows idle days as
 * zero instead of joining distant points. Spans `from`..`to` when given,
 * otherwise the first to last day present. Points outside the span are kept.
 */
export function fillDays<P extends { day: string }>(
  points: P[],
  empty: (day: string) => P,
  range: { from?: string; to?: string } = {},
): P[] {
  const byDay = new Map(points.map((p) => [p.day.slice(0, 10), p]))
  const known = [...byDay.keys()].sort()
  const from = range.from ?? known[0]
  const to = range.to ?? known[known.length - 1]
  if (!from || !to) return points
  for (const day of utcDaysBetween(from, to)) {
    if (!byDay.has(day)) byDay.set(day, empty(day))
  }
  return [...byDay.entries()].sort(([a], [b]) => a.localeCompare(b)).map(([, p]) => p)
}
