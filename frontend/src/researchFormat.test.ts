import { describe, expect, it } from 'vitest'
import { formatMetric, normalizeVerdict } from './researchFormat'

describe('formatMetric', () => {
  it('keeps four significant digits without exponent notation', () => {
    expect(formatMetric(0.8642000001)).toBe('0.8642')
    expect(formatMetric(0.123456)).toBe('0.1235')
    expect(formatMetric(86.4)).toBe('86.4')
    expect(formatMetric(0.91)).toBe('0.91')
    expect(formatMetric(12345.678)).toBe('12345.678')
    expect(formatMetric(3)).toBe('3')
  })

  it('renders nothing for a missing value', () => {
    expect(formatMetric(undefined)).toBe('')
    expect(formatMetric(null)).toBe('')
    expect(formatMetric(Number.NaN)).toBe('')
  })
})

describe('normalizeVerdict', () => {
  it('accepts only the three close_run verdicts', () => {
    expect(normalizeVerdict('supports')).toBe('supports')
    expect(normalizeVerdict('contradicts')).toBe('contradicts')
    expect(normalizeVerdict('inconclusive')).toBe('inconclusive')
    expect(normalizeVerdict('succeeded')).toBeNull()
    expect(normalizeVerdict(undefined)).toBeNull()
  })
})
