// Four significant digits without exponent notation, matching the plan-sync
// markdown. The exact stored value stays available as a tooltip.
export function formatMetric(value?: number | null): string {
  if (value === undefined || value === null || !Number.isFinite(value)) return ''
  const text = value.toPrecision(4)
  if (/e/i.test(text)) return String(value)
  return String(Number(text))
}

// close_run records supports or contradicts only as an explicit judgment of a
// measured metric; everything else, including failed runs, is inconclusive.
export type ResearchVerdict = 'supports' | 'contradicts' | 'inconclusive'

export function normalizeVerdict(value?: string): ResearchVerdict | null {
  if (value === 'supports' || value === 'contradicts' || value === 'inconclusive') return value
  return null
}
