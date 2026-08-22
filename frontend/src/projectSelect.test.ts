import { describe, expect, it } from 'vitest'
import type { Project } from './api'
import { pickDefaultProjectID } from './projectSelect'

function project(id: string, extra: Partial<Project> = {}): Project {
  return {
    id, name: id, slug: id, status: 'active',
    monthly_budget_milli: 1, max_experiment_milli: 1, max_concurrency: 1,
    max_runtime_seconds: 1, timeout_extension_seconds: 1, termination_grace_seconds: 1,
    timezone: 'UTC',
    ...extra,
  }
}

describe('pickDefaultProjectID', () => {
  it('keeps the current project when it still exists', () => {
    expect(pickDefaultProjectID([
      project('alpha', { created_at: '2026-01-01T00:00:00Z' }),
      project('beta', { created_at: '2026-08-01T00:00:00Z' }),
    ], 'alpha')).toBe('alpha')
  })

  it('opens the newest imported project when nothing is selected', () => {
    expect(pickDefaultProjectID([
      project('alpha', { name: 'default', created_at: '2026-01-01T00:00:00Z' }),
      project('beta', { name: 'DynamicPointMamba', created_at: '2026-08-21T00:00:00Z' }),
    ])).toBe('beta')
  })
})
