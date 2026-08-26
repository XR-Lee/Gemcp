import type { ResearchEdge, ResearchNode } from './api'

export const RANK_RELATIONS = new Set(['leads_to', 'produced', 'supersedes'])

export const COLUMN_WIDTH = 292
export const ROW_HEIGHT = 176
export const ORIGIN_X = 36
export const ORIGIN_Y = 72
export const TICK_Y = 10
export const CANVAS_MAX_HEIGHT = 560

export type NodeOutcome = 'success' | 'failure' | null

export type GraphLayoutNode = {
  id: string
  x: number
  y: number
  rank: number
  lane: number
  bucket: string
  active: boolean
  next: boolean
  unlinked: boolean
  outcome: NodeOutcome
}

export type GraphLayoutEdge = {
  id: string
  active: boolean
}

export type TimelineTick = {
  id: string
  x: number
  at: string
  label: string
}

export type GraphLayout = {
  nodes: GraphLayoutNode[]
  edges: GraphLayoutEdge[]
  activeNodeIDs: string[]
  ticks: TimelineTick[]
  axis: 'time' | 'lineage'
}

export type GraphLayoutInput = {
  nodes: ResearchNode[]
  edges: ResearchEdge[]
  focusNodeIDs?: string[]
}

export function layoutResearchGraph(input: GraphLayoutInput): GraphLayout {
  const nodes = input.nodes ?? []
  const edges = input.edges ?? []
  const byID = new Map(nodes.map((node) => [node.id, node]))
  const rankEdges = edges.filter((edge) => RANK_RELATIONS.has(edge.relation) && byID.has(edge.from_id) && byID.has(edge.to_id))
  const incoming = groupBy(rankEdges, (edge) => edge.to_id)
  const incident = new Set<string>()
  for (const edge of edges) {
    if (!byID.has(edge.from_id) || !byID.has(edge.to_id)) continue
    incident.add(edge.from_id)
    incident.add(edge.to_id)
  }

  const ranks = assignRanks(nodes, rankEdges)
  const lanes = assignLanes(nodes, ranks, incoming)
  const timeline = assignTimeline(nodes, ranks)
  const allIncoming = groupBy(edges.filter((edge) => byID.has(edge.from_id) && byID.has(edge.to_id)), (edge) => edge.to_id)
  const tip = pickActiveTip(nodes, incident)
  const activeNodeIDs = tip ? collectActivePath(tip, allIncoming) : []
  const active = new Set(activeNodeIDs)
  const nextIDs = new Set((input.focusNodeIDs ?? []).filter((id) => byID.has(id)))

  return {
    nodes: nodes.map((node) => {
      const rank = ranks.get(node.id) ?? 0
      const lane = lanes.get(node.id) ?? 0
      const column = timeline.columns.get(node.id) ?? rank
      return {
        id: node.id,
        x: ORIGIN_X + column * COLUMN_WIDTH,
        y: ORIGIN_Y + lane * ROW_HEIGHT,
        rank,
        lane,
        bucket: timeline.buckets.get(node.id) ?? String(rank),
        active: active.has(node.id),
        next: nextIDs.has(node.id),
        unlinked: !incident.has(node.id) && node.kind !== 'question',
        outcome: nodeOutcome(node),
      }
    }),
    edges: edges.map((edge) => ({
      id: edge.id,
      active: active.has(edge.from_id) && active.has(edge.to_id),
    })),
    activeNodeIDs,
    ticks: timeline.ticks,
    axis: timeline.axis,
  }
}

export function nodeTimelineAt(node: ResearchNode) {
  return node.occurred_at || node.created_at
}

export function assignTimeline(nodes: ResearchNode[], ranks: Map<string, number>) {
  const stamps = nodes
    .map((node) => {
      const iso = nodeTimelineAt(node)
      return { id: node.id, at: Date.parse(iso), iso, rank: ranks.get(node.id) ?? 0 }
    })
    .filter((item) => Number.isFinite(item.at))
    .sort((left, right) => left.at - right.at || left.rank - right.rank)
  if (!stamps.length) {
    return { axis: 'lineage' as const, columns: new Map<string, number>(), buckets: new Map<string, string>(), ticks: [] as TimelineTick[] }
  }
  const span = stamps[stamps.length - 1].at - stamps[0].at
  const axis = span > 30 * 60 * 1000 ? 'time' as const : 'lineage' as const
  const columns = new Map<string, number>()
  const buckets = new Map<string, string>()
  const ticks: TimelineTick[] = []
  if (axis === 'lineage') {
    const rankTimes = new Map<number, string>()
    for (const item of stamps) {
      columns.set(item.id, item.rank)
      buckets.set(item.id, `rank-${item.rank}`)
      if (!rankTimes.has(item.rank)) rankTimes.set(item.rank, item.iso)
    }
    for (const [rank, at] of [...rankTimes.entries()].sort((left, right) => left[0] - right[0])) {
      ticks.push({
        id: `tick-rank-${rank}`,
        x: ORIGIN_X + rank * COLUMN_WIDTH,
        at,
        label: formatTick(at, 'lineage', rank, span),
      })
    }
    return { axis, columns, buckets, ticks }
  }
  const keys: string[] = []
  for (const item of stamps) {
    const key = timeBucket(item.at, span)
    if (!keys.includes(key)) keys.push(key)
    columns.set(item.id, keys.indexOf(key))
    buckets.set(item.id, key)
  }
  for (const [index, key] of keys.entries()) {
    const first = stamps.find((item) => timeBucket(item.at, span) === key)
    ticks.push({
      id: `tick-${key}`,
      x: ORIGIN_X + index * COLUMN_WIDTH,
      at: first?.iso ?? key,
      label: formatTick(first?.iso ?? key, 'time', 0, span),
    })
  }
  return { axis, columns, buckets, ticks }
}

function timeBucket(at: number, span: number) {
  const date = new Date(at)
  if (span > 14 * 24 * 60 * 60 * 1000) return date.toISOString().slice(0, 10)
  if (span > 36 * 60 * 60 * 1000) return date.toISOString().slice(0, 13)
  return date.toISOString().slice(0, 16)
}

function formatTick(value: string, axis: 'time' | 'lineage', rank = 0, span = 0) {
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return axis === 'lineage' ? `Step ${rank + 1}` : value
  const stamp = date.toLocaleString('en-GB', tickOptions(span))
  if (axis === 'lineage') return `${rank + 1} · ${stamp}`
  return stamp
}

function tickOptions(span: number): Intl.DateTimeFormatOptions {
  if (span > 300 * 24 * 60 * 60 * 1000) return { month: 'short', year: 'numeric' }
  if (span > 36 * 60 * 60 * 1000) return { day: 'numeric', month: 'short', year: 'numeric' }
  return { day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit' }
}

function assignRanks(nodes: ResearchNode[], rankEdges: ResearchEdge[]) {
  const ranks = new Map<string, number>()
  const incomingCount = new Map<string, number>()
  for (const node of nodes) incomingCount.set(node.id, 0)
  for (const edge of rankEdges) incomingCount.set(edge.to_id, (incomingCount.get(edge.to_id) ?? 0) + 1)
  for (const node of nodes) {
    if ((incomingCount.get(node.id) ?? 0) === 0) ranks.set(node.id, node.kind === 'question' ? 0 : 1)
  }
  let changed = true
  let guard = 0
  while (changed && guard < nodes.length + 2) {
    changed = false
    guard += 1
    for (const edge of rankEdges) {
      const fromRank = ranks.get(edge.from_id)
      if (fromRank === undefined) continue
      const next = fromRank + 1
      const current = ranks.get(edge.to_id)
      if (current === undefined || current < next) {
        ranks.set(edge.to_id, next)
        changed = true
      }
    }
  }
  for (const node of nodes) {
    if (!ranks.has(node.id)) ranks.set(node.id, node.kind === 'question' ? 0 : 1)
  }
  return ranks
}

function assignLanes(nodes: ResearchNode[], ranks: Map<string, number>, incoming: Map<string, ResearchEdge[]>) {
  const lanes = new Map<string, number>()
  const ordered = [...nodes].sort((left, right) => {
    const rankDelta = (ranks.get(left.id) ?? 0) - (ranks.get(right.id) ?? 0)
    if (rankDelta !== 0) return rankDelta
    return nodeTimelineAt(left).localeCompare(nodeTimelineAt(right))
  })
  const usedAtRank = new Map<number, Set<number>>()
  let nextRootLane = 0
  for (const node of ordered) {
    const rank = ranks.get(node.id) ?? 0
    const parents = (incoming.get(node.id) ?? []).map((edge) => edge.from_id)
    let preferred = node.kind === 'question' ? 0 : nextRootLane
    if (parents.length) {
      preferred = Math.min(...parents.map((id) => lanes.get(id) ?? 0))
    } else if (node.kind !== 'question') {
      nextRootLane += 1
    }
    const taken = usedAtRank.get(rank) ?? new Set<number>()
    let lane = preferred
    while (taken.has(lane)) lane += 1
    taken.add(lane)
    usedAtRank.set(rank, taken)
    lanes.set(node.id, lane)
  }
  return lanes
}

const FAILURE_STATES = new Set(['failed', 'cancelled', 'timed_out', 'budget_stopped', 'provider_error', 'rejected'])

export function nodeOutcome(node: ResearchNode): NodeOutcome {
  const states = [node.status, node.experiment_state].filter((value): value is string => Boolean(value)).map((value) => value.toLowerCase())
  if (states.some((state) => FAILURE_STATES.has(state))) return 'failure'
  if (states.includes('succeeded')) return 'success'
  return null
}

export function pickActiveTip(nodes: ResearchNode[], incident: Set<string>) {
  const linked = nodes.filter((node) => incident.has(node.id) || node.kind === 'question')
  const pool = linked.length ? linked : nodes
  return [...pool].sort((left, right) => {
    const occurred = nodeTimelineAt(right).localeCompare(nodeTimelineAt(left))
    if (occurred !== 0) return occurred
    return right.updated_at.localeCompare(left.updated_at)
  })[0]?.id
}

function collectActivePath(tip: string, incoming: Map<string, ResearchEdge[]>) {
  const path = new Set<string>([tip])
  const seen = new Set<string>()
  const back = [tip]
  while (back.length) {
    const current = back.pop()
    if (!current || seen.has(current)) continue
    seen.add(current)
    path.add(current)
    for (const edge of incoming.get(current) ?? []) back.push(edge.from_id)
  }
  return [...path]
}

function groupBy<T>(items: T[], key: (item: T) => string) {
  const groups = new Map<string, T[]>()
  for (const item of items) {
    const group = groups.get(key(item)) ?? []
    group.push(item)
    groups.set(key(item), group)
  }
  return groups
}
