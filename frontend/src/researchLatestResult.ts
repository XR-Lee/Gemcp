import type { ResearchNode } from './api'

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
