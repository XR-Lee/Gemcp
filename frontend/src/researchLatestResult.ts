import type { ResearchEdge, ResearchNode } from './api'

export type HistoricalObservation = {
  node: ResearchNode
  relation: 'supports' | 'contradicts' | 'leads_to'
}

function evidenceTime(node: ResearchNode): number {
  const raw = node.occurred_at || node.created_at
  const ms = Date.parse(raw)
  return Number.isFinite(ms) ? ms : Number.NEGATIVE_INFINITY
}

// Newest scientific result on the Study, by evidence time (occurred_at),
// not Graph write time. A later-created historical import must not hide a
// newer Experiment result. Ties keep the later node in the list.
export function selectLatestResult(nodes: ResearchNode[]): ResearchNode | null {
  let latest: ResearchNode | null = null
  let latestTime = Number.NEGATIVE_INFINITY
  for (const node of nodes) {
    if (node.kind !== 'result') continue
    const time = evidenceTime(node)
    if (!latest || time >= latestTime) {
      latest = node
      latestTime = time
    }
  }
  return latest
}

const relationRank: Record<HistoricalObservation['relation'], number> = {
  supports: 0,
  contradicts: 1,
  leads_to: 2,
}

function isHistoricalObservation(node: ResearchNode): boolean {
  return node.kind === 'observation' && !node.system_written
}

// Newest historical observation. Gemcp close_run highlights stay out of this
// fallback so they are not shown twice as a result.
export function selectLatestEvidence(nodes: ResearchNode[]): ResearchNode | null {
  let latest: ResearchNode | null = null
  let latestTime = Number.NEGATIVE_INFINITY
  for (const node of nodes) {
    if (!isHistoricalObservation(node)) continue
    const time = evidenceTime(node)
    if (!latest || time >= latestTime) {
      latest = node
      latestTime = time
    }
  }
  return latest
}

function outgoingEdges(edges: ResearchEdge[]): Map<string, ResearchEdge[]> {
  const outgoing = new Map<string, ResearchEdge[]>()
  for (const edge of edges) {
    const list = outgoing.get(edge.from_id) ?? []
    list.push(edge)
    outgoing.set(edge.from_id, list)
  }
  return outgoing
}

function descendantsOf(hypothesisID: string, nodes: ResearchNode[], edges: ResearchEdge[]): ResearchNode[] {
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const outgoing = outgoingEdges(edges)
  const seen = new Set([hypothesisID])
  const queue = [hypothesisID]
  const out: ResearchNode[] = []
  while (queue.length) {
    const id = queue.shift()
    if (!id) break
    for (const edge of outgoing.get(id) ?? []) {
      if (seen.has(edge.to_id)) continue
      seen.add(edge.to_id)
      const node = byID.get(edge.to_id)
      if (!node || node.kind === 'hypothesis') continue
      out.push(node)
      queue.push(node.id)
    }
  }
  return out
}

function preferRelation(current: HistoricalObservation['relation'] | undefined, next: string): HistoricalObservation['relation'] | undefined {
  if (next !== 'supports' && next !== 'contradicts' && next !== 'leads_to') return current
  if (!current || relationRank[next] < relationRank[current]) return next
  return current
}

function directRelation(hypothesisID: string, observationID: string, edges: ResearchEdge[]): HistoricalObservation['relation'] {
  let relation: HistoricalObservation['relation'] | undefined
  for (const edge of edges) {
    const links = (edge.from_id === hypothesisID && edge.to_id === observationID) || (edge.from_id === observationID && edge.to_id === hypothesisID)
    if (!links) continue
    relation = preferRelation(relation, edge.relation)
  }
  return relation ?? 'leads_to'
}

// Observations on this hypothesis, excluding close_run highlights. Descendants
// stop at the next hypothesis. A supports or contradicts edge into the
// hypothesis is included, and those relations outrank leads_to.
export function historicalObservationsForHypothesis(hypothesisID: string, nodes: ResearchNode[], edges: ResearchEdge[]): HistoricalObservation[] {
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const chosen = new Map<string, HistoricalObservation>()
  const order: string[] = []
  const consider = (node: ResearchNode, relation: HistoricalObservation['relation']) => {
    if (!isHistoricalObservation(node)) return
    const existing = chosen.get(node.id)
    if (!existing) {
      order.push(node.id)
      chosen.set(node.id, { node, relation })
      return
    }
    const preferred = preferRelation(existing.relation, relation)
    if (preferred) chosen.set(node.id, { node, relation: preferred })
  }
  for (const node of descendantsOf(hypothesisID, nodes, edges)) {
    consider(node, directRelation(hypothesisID, node.id, edges))
  }
  for (const edge of edges) {
    if (edge.to_id !== hypothesisID || (edge.relation !== 'supports' && edge.relation !== 'contradicts')) continue
    const node = byID.get(edge.from_id)
    if (!node) continue
    consider(node, edge.relation)
  }
  return order.map((id) => chosen.get(id)).filter((item): item is HistoricalObservation => Boolean(item))
}
