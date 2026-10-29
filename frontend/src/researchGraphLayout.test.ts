import { describe, expect, it } from 'vitest'
import type { ResearchEdge, ResearchNode } from './api'
import { CARD_WIDTH, COLUMN_WIDTH, ROW_GAP, cardHeight, layoutResearchGraph } from './researchGraphLayout'

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
    // An observation's execution status is not a verdict on the hypothesis.
    expect(byID.latest.outcome).toBeNull()
    expect(byID.failed.outcome).toBe('failure')
  })

  it('stamps runs with execution and observations with the recorded verdict', () => {
    const layout = layoutResearchGraph({
      nodes: [
        node('q', 'question'),
        node('h', 'hypothesis', { status: 'succeeded' }),
        node('run-ok', 'run', { status: 'succeeded', experiment_state: 'succeeded' }),
        node('run-crash', 'run', { status: 'failed', experiment_state: 'failed' }),
        node('weak', 'observation', { status: 'succeeded', system_written: true }),
        node('strong', 'observation', { status: 'succeeded', system_written: true }),
        node('refuted', 'observation', { status: 'succeeded', system_written: true }),
        node('note', 'observation', { status: 'succeeded' }),
      ],
      edges: [
        edge('e1', 'q', 'h'),
        edge('e2', 'h', 'run-ok'),
        edge('e3', 'h', 'run-crash'),
        edge('e4', 'h', 'weak'),
        edge('e5', 'h', 'strong'),
        edge('e6', 'strong', 'h', 'supports'),
        edge('e7', 'h', 'refuted'),
        edge('e8', 'refuted', 'h', 'contradicts'),
        edge('e9', 'h', 'note'),
      ],
    })
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    expect(byID['run-ok'].outcome).toBe('success')
    expect(byID['run-crash'].outcome).toBe('failure')
    expect(byID.weak.outcome).toBe('inconclusive')
    expect(byID.strong.outcome).toBe('supports')
    expect(byID.refuted.outcome).toBe('contradicts')
    expect(byID.note.outcome).toBeNull()
    expect(byID.h.outcome).toBeNull()
  })

  it('keeps a same-day observation, decision, and hypothesis from sharing a point', () => {
    const nodes = [
      node('q', 'question', { occurred_at: '2025-01-01T00:00:00Z', created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z' }),
      node('h', 'hypothesis', { occurred_at: '2025-06-01T12:00:00Z', created_at: '2025-06-01T12:00:00Z', updated_at: '2025-06-01T12:00:00Z' }),
      node('obs', 'observation', { occurred_at: '2026-03-01T08:00:00Z', created_at: '2026-03-01T08:00:00Z', updated_at: '2026-03-01T08:00:00Z' }),
      node('dec', 'decision', { occurred_at: '2026-03-01T14:00:00Z', created_at: '2026-03-01T14:00:00Z', updated_at: '2026-03-01T14:00:00Z' }),
      node('next', 'hypothesis', { title: '测'.repeat(180), occurred_at: '2026-03-01T20:00:00Z', created_at: '2026-03-01T20:00:00Z', updated_at: '2026-03-01T20:00:00Z' }),
    ]
    const edges = [
      edge('e1', 'q', 'h'),
      edge('e2', 'h', 'obs'),
      edge('e3', 'obs', 'dec'),
      edge('e4', 'dec', 'next'),
    ]
    const layout = layoutResearchGraph({ nodes, edges })
    const reversed = layoutResearchGraph({ nodes: [...nodes].reverse(), edges: [...edges].reverse() })
    expect(layout.axis).toBe('time')
    const byID = Object.fromEntries(layout.nodes.map((item) => [item.id, item]))
    const sameDay = [byID.obs, byID.dec, byID.next]
    expect(new Set(sameDay.map((item) => `${item.x},${item.y}`)).size).toBe(3)
    expect(byID.obs.x).toBe(byID.dec.x)
    expect(byID.dec.x).toBe(byID.next.x)
    const boxes = layout.nodes.map((item) => ({
      ...item,
      h: cardHeight(nodes.find((entry) => entry.id === item.id)!),
    }))
    for (let i = 0; i < boxes.length; i += 1) {
      for (let j = i + 1; j < boxes.length; j += 1) {
        const left = boxes[i]
        const right = boxes[j]
        const hit = left.x < right.x + CARD_WIDTH && left.x + CARD_WIDTH > right.x && left.y < right.y + right.h && left.y + left.h > right.y
        expect(hit).toBe(false)
      }
    }
    expect(byID.dec.y).toBeGreaterThanOrEqual(byID.obs.y + boxes.find((item) => item.id === 'obs')!.h + ROW_GAP)
    const reversedByID = Object.fromEntries(reversed.nodes.map((item) => [item.id, item]))
    for (const id of Object.keys(byID)) {
      expect(reversedByID[id].x).toBe(byID[id].x)
      expect(reversedByID[id].y).toBe(byID[id].y)
    }

    const measured = layoutResearchGraph({
      nodes,
      edges,
      cardSizes: { obs: { width: CARD_WIDTH, height: 420 } },
    })
    const measuredByID = Object.fromEntries(measured.nodes.map((item) => [item.id, item]))
    expect(measuredByID.dec.y).toBeGreaterThanOrEqual(measuredByID.obs.y + 420 + ROW_GAP)

    const lineage = layoutResearchGraph({ nodes, edges, axis: 'lineage' })
    const lineageByID = Object.fromEntries(lineage.nodes.map((item) => [item.id, item]))
    expect(lineage.axis).toBe('lineage')
    expect(lineageByID.obs.x).toBe(lineageByID.h.x + COLUMN_WIDTH)
    expect(lineageByID.dec.x).toBe(lineageByID.obs.x + COLUMN_WIDTH)
    expect(lineageByID.next.x).toBe(lineageByID.dec.x + COLUMN_WIDTH)
    expect(new Set([lineageByID.obs, lineageByID.dec, lineageByID.next].map((item) => `${item.x},${item.y}`)).size).toBe(3)
  })
})
