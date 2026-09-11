import { describe, expect, it } from 'vitest'
import type { ResearchNode } from './api'
import { selectLatestResult } from './researchLatestResult'

function node(partial: Partial<ResearchNode> & Pick<ResearchNode, 'id' | 'kind' | 'title' | 'created_at'>): ResearchNode {
  return { status: 'succeeded', updated_at: partial.created_at, ...partial }
}

describe('selectLatestResult', () => {
  it('prefers evidence time over later Graph write time', () => {
    const historical = node({
      id: 'older-evidence', kind: 'result', title: 'Imported 2024 table',
      occurred_at: '2024-03-12T00:00:00Z', created_at: '2026-08-17T18:10:00Z',
    })
    const experiment = node({
      id: 'newer-evidence', kind: 'result', title: 'OBJ-BG smoke accuracy',
      occurred_at: '2026-08-17T17:55:00Z', created_at: '2026-08-17T18:00:00Z',
    })
    expect(selectLatestResult([historical, experiment])?.title).toBe('OBJ-BG smoke accuracy')
    expect(selectLatestResult([experiment, historical])?.title).toBe('OBJ-BG smoke accuracy')
  })

  it('falls back to created_at when occurred_at is missing', () => {
    const first = node({ id: 'a', kind: 'result', title: 'First write', created_at: '2026-08-17T18:00:00Z' })
    const second = node({ id: 'b', kind: 'result', title: 'Second write', created_at: '2026-08-17T18:05:00Z' })
    expect(selectLatestResult([first, second])?.title).toBe('Second write')
  })

  it('ignores non-result nodes', () => {
    expect(selectLatestResult([
      node({ id: 'q', kind: 'question', title: 'Q', created_at: '2026-08-17T18:10:00Z' }),
    ])).toBeNull()
  })
})
