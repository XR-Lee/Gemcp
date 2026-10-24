import { describe, expect, it } from 'vitest'
import type { ResearchNode } from './api'
import { historicalObservationsForHypothesis, selectLatestEvidence, selectLatestResult } from './researchLatestResult'

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

describe('historical observations', () => {
  const nodes = [
    node({ id: 'h', kind: 'hypothesis', title: 'Augmentation helps', created_at: '2026-08-17T18:00:00Z' }),
    node({ id: 'follow', kind: 'hypothesis', title: 'Follow-up', created_at: '2026-08-17T18:01:00Z' }),
    node({
      id: 'paper', kind: 'observation', title: 'Paper X', summary: 'Table 2',
      metric_name: 'accuracy', metric_value: 0.93, metric_source: 'asserted',
      occurred_at: '2024-01-02T00:00:00Z', created_at: '2026-08-17T18:02:00Z',
    }),
    node({
      id: 'highlight', kind: 'observation', title: 'close_run highlight', system_written: true,
      metric_source: 'experiment', created_at: '2026-08-17T18:03:00Z',
    }),
    node({ id: 'child', kind: 'observation', title: 'Follow-up note', created_at: '2026-08-17T18:04:00Z' }),
  ]
  const edges = [
    { id: 'e1', from_id: 'h', to_id: 'paper', relation: 'leads_to' },
    { id: 'e2', from_id: 'paper', to_id: 'h', relation: 'supports' },
    { id: 'e3', from_id: 'h', to_id: 'highlight', relation: 'leads_to' },
    { id: 'e4', from_id: 'h', to_id: 'follow', relation: 'leads_to' },
    { id: 'e5', from_id: 'follow', to_id: 'child', relation: 'leads_to' },
  ]

  it('uses the newest historical observation when no result exists', () => {
    expect(selectLatestEvidence(nodes)?.title).toBe('Follow-up note')
    expect(selectLatestResult(nodes)).toBeNull()
  })

  it('keeps supports ahead of leads_to and hides close_run highlights', () => {
    const observations = historicalObservationsForHypothesis('h', nodes, edges)
    expect(observations.map((item) => [item.node.title, item.relation])).toEqual([['Paper X', 'supports']])
    expect(historicalObservationsForHypothesis('follow', nodes, edges).map((item) => item.node.title)).toEqual(['Follow-up note'])
  })
})
