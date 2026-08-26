import { describe, expect, it } from 'vitest'
import type { ResearchEdge, ResearchNode } from './api'
import { COLUMN_WIDTH, layoutResearchGraph } from './researchGraphLayout'

function node(id: string, kind: string, extra: Partial<ResearchNode> = {}): ResearchNode {
  return {
    id, kind, title: id, status: 'open',
    created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:00Z',
    ...extra,
  }
}

function edge(id: string, from: string, to: string, relation = 'leads_to'): ResearchEdge {
  return { id, from_id: from, to_id: to, relation }
}

describe('layoutResearchGraph', () => {
  it('places evidence after its hypothesis instead of in a kind column', () => {
    const layout = layoutResearchGraph({
      nodes: [
        node('q', 'question'),
        node('h', 'hypothesis', { created_at: '2026-08-21T00:01:00Z', updated_at: '2026-08-21T00:01:00Z' }),
        node('o', 'observation', { created_at: '2026-08-21T00:02:00Z', updated_at: '2026-08-21T00:02:00Z' }),
        node('d', 'decision', { created_at: '2026-08-21T00:03:00Z', updated_at: '2026-08-21T00:03:00Z' }),
      ],
      edges: [
        edge('e1', 'q', 'h'),
        edge('e2', 'h', 'o'),
        edge('e3', 'o', 'd'),
      ],
      focusNodeIDs: ['h'],
    })
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    expect(byID.q.rank).toBe(0)
    expect(byID.h.rank).toBe(1)
    expect(byID.o.rank).toBe(2)
    expect(byID.d.rank).toBe(3)
    expect(byID.o.x).toBe(byID.h.x + COLUMN_WIDTH)
    expect(byID.o.unlinked).toBe(false)
    expect(byID.d.active).toBe(true)
    expect(byID.h.active).toBe(true)
    expect(byID.q.active).toBe(true)
    expect(layout.edges.find((item) => item.id === 'e2')?.active).toBe(true)
    expect(byID.h.next).toBe(true)
    expect(layout.axis).toBe('lineage')
    expect(layout.ticks.map((tick) => tick.x)).toEqual([
      byID.q.x, byID.h.x, byID.o.x, byID.d.x,
    ])
  })

  it('uses exploration time as the x-axis when records span more than a session', () => {
    const layout = layoutResearchGraph({
      nodes: [
        node('q', 'question', { created_at: '2026-01-18T10:00:00Z', updated_at: '2026-01-18T10:00:00Z' }),
        node('h', 'hypothesis', { created_at: '2026-01-28T10:00:00Z', updated_at: '2026-01-28T10:00:00Z' }),
        node('o', 'observation', { created_at: '2026-02-10T10:00:00Z', updated_at: '2026-02-10T10:00:00Z' }),
      ],
      edges: [
        edge('e1', 'q', 'h'),
        edge('e2', 'h', 'o'),
      ],
    })
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    expect(layout.axis).toBe('time')
    expect(byID.q.x).toBeLessThan(byID.h.x)
    expect(byID.h.x).toBeLessThan(byID.o.x)
    expect(layout.ticks).toHaveLength(3)
  })

  it('places nodes by commit/evidence time, not the MCP write burst', () => {
    const written = { created_at: '2026-08-22T02:19:00Z', updated_at: '2026-08-22T02:19:30Z' }
    const layout = layoutResearchGraph({
      nodes: [
        node('q', 'question', { ...written, occurred_at: '2024-03-12T00:00:00Z' }),
        node('old', 'hypothesis', { ...written, occurred_at: '2024-06-01T00:00:00Z' }),
        node('new', 'observation', { ...written, occurred_at: '2025-11-02T18:04:00Z' }),
      ],
      edges: [
        edge('e1', 'q', 'old'),
        edge('e2', 'old', 'new'),
      ],
    })
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    expect(layout.axis).toBe('time')
    expect(byID.q.x).toBeLessThan(byID.old.x)
    expect(byID.old.x).toBeLessThan(byID.new.x)
    expect(layout.ticks.some((tick) => /2024|2025/.test(tick.label))).toBe(true)
    expect(byID.new.active).toBe(true)
    expect(byID.old.active).toBe(true)
  })

  it('keeps competing hypotheses on the same rank and marks orphans', () => {
    const layout = layoutResearchGraph({
      nodes: [
        node('q', 'question'),
        node('h1', 'hypothesis', { created_at: '2026-08-21T00:01:00Z', updated_at: '2026-08-21T00:01:00Z' }),
        node('h2', 'hypothesis', { created_at: '2026-08-21T00:02:00Z', updated_at: '2026-08-21T00:02:00Z' }),
        node('orphan', 'observation', { created_at: '2026-08-21T00:03:00Z', updated_at: '2026-08-21T00:03:00Z' }),
      ],
      edges: [
        edge('e1', 'q', 'h1'),
        edge('e2', 'q', 'h2'),
        edge('e3', 'h1', 'h2', 'compares'),
      ],
    })
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    expect(byID.h1.rank).toBe(byID.h2.rank)
    expect(byID.h1.lane).not.toBe(byID.h2.lane)
    expect(byID.orphan.unlinked).toBe(true)
    expect(byID.orphan.active).toBe(false)
  })

  it('draws the active path to the newest timestamp, not a stale next-action seed', () => {
    const layout = layoutResearchGraph({
      nodes: [
        node('q', 'question'),
        node('old', 'hypothesis', { created_at: '2026-08-21T00:01:00Z', updated_at: '2026-08-21T00:01:00Z' }),
        node('fresh', 'hypothesis', { created_at: '2026-08-21T00:02:00Z', updated_at: '2026-08-21T00:02:00Z' }),
        node('latest', 'observation', {
          created_at: '2026-08-21T00:10:00Z', updated_at: '2026-08-21T00:10:00Z', status: 'succeeded',
        }),
        node('failed', 'result', {
          created_at: '2026-08-21T00:04:00Z', updated_at: '2026-08-21T00:04:00Z', status: 'failed',
        }),
      ],
      edges: [
        edge('e1', 'q', 'old'),
        edge('e2', 'q', 'fresh'),
        edge('e3', 'fresh', 'latest'),
        edge('e4', 'old', 'failed'),
      ],
      focusNodeIDs: ['old'],
    })
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    expect(byID.latest.active).toBe(true)
    expect(byID.fresh.active).toBe(true)
    expect(byID.q.active).toBe(true)
    expect(byID.old.active).toBe(false)
    expect(byID.failed.active).toBe(false)
    expect(byID.old.next).toBe(true)
    expect(byID.latest.outcome).toBe('success')
    expect(byID.failed.outcome).toBe('failure')
  })
})
