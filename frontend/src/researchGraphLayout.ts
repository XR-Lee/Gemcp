import type { ResearchEdge, ResearchNode } from './api'

export const RANK_RELATIONS = new Set(['leads_to', 'produced', 'supersedes'])

// Card box from `.flow-node`. Column and row strides keep those rectangles
// from touching, including the gap between cards.
export const CARD_WIDTH = 236
export const CARD_MIN_HEIGHT = 126
export const COLUMN_GAP = 56
export const ROW_GAP = 50
export const COLUMN_WIDTH = CARD_WIDTH + COLUMN_GAP
export const ROW_HEIGHT = CARD_MIN_HEIGHT + ROW_GAP
export const ORIGIN_X = 36
export const ORIGIN_Y = 72
export const TICK_Y = 10
export const CANVAS_MAX_HEIGHT = 560

// Execution outcome on run and result nodes; scientific verdict on
// observations. The two are never mixed on one stamp.
export type NodeOutcome = 'success' | 'failure' | 'supports' | 'contradicts' | 'inconclusive' | null

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

export type CardSize = {
  width: number
  height: number
}

export type GraphLayoutInput = {
  nodes: ResearchNode[]
  edges: ResearchEdge[]
  focusNodeIDs?: string[]
  // Omit to pick time when evidence spans more than half an hour, otherwise lineage.
  axis?: 'time' | 'lineage'
  // Live card boxes from the canvas. Missing entries use the CSS card estimate.
  cardSizes?: Readonly<Record<string, CardSize>>
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
  const timeline = assignTimeline(nodes, ranks, input.axis)
  const columns = displayColumns(nodes, ranks, timeline)
  const lanes = assignLanes(nodes, ranks, columns, incoming)
  const heights = new Map(nodes.map((node) => [node.id, cardHeight(node, input.cardSizes?.[node.id]?.height)]))
  const tops = assignTops(nodes, lanes, columns, heights)
  let boxWidth = CARD_WIDTH
  for (const node of nodes) {
    const measured = input.cardSizes?.[node.id]?.width
    if (measured && measured > boxWidth) boxWidth = measured
  }
  const columnStride = Math.max(COLUMN_WIDTH, boxWidth + COLUMN_GAP)
  const allIncoming = groupBy(edges.filter((edge) => byID.has(edge.from_id) && byID.has(edge.to_id)), (edge) => edge.to_id)
  const tip = pickActiveTip(nodes, incident)
  const activeNodeIDs = tip ? collectActivePath(tip, allIncoming) : []
  const active = new Set(activeNodeIDs)
  const nextIDs = new Set((input.focusNodeIDs ?? []).filter((id) => byID.has(id)))
  const verdicts = new Map<string, 'supports' | 'contradicts'>()
  for (const edge of edges) {
    if ((edge.relation === 'supports' || edge.relation === 'contradicts') && byID.get(edge.to_id)?.kind === 'hypothesis') {
      verdicts.set(edge.from_id, edge.relation)
    }
  }

  return {
    nodes: nodes.map((node) => {
      const rank = ranks.get(node.id) ?? 0
      const lane = lanes.get(node.id) ?? 0
      const column = columns.get(node.id) ?? rank
      return {
        id: node.id,
        x: ORIGIN_X + column * columnStride,
        y: tops.get(node.id) ?? ORIGIN_Y + lane * ROW_HEIGHT,
        rank,
        lane,
        bucket: timeline.buckets.get(node.id) ?? String(rank),
        active: active.has(node.id),
        next: nextIDs.has(node.id),
        unlinked: !incident.has(node.id) && node.kind !== 'question',
        outcome: nodeOutcome(node, verdicts.get(node.id)),
      }
    }),
    edges: edges.map((edge) => ({
      id: edge.id,
      active: active.has(edge.from_id) && active.has(edge.to_id),
    })),
    activeNodeIDs,
    ticks: timeline.ticks.map((tick) => ({
      ...tick,
      x: ORIGIN_X + ((tick.x - ORIGIN_X) / COLUMN_WIDTH) * columnStride,
    })),
    axis: timeline.axis,
  }
}

// Box height for one card. A measured canvas height wins; otherwise the
// estimate follows `.flow-node` (padding, clamped summary, wrapped title).
export function cardHeight(node: ResearchNode, measured?: number) {
  if (measured && Number.isFinite(measured) && measured > 0) return measured
  const blocks = [16, lineCount(node.title || node.kind, 13) * 18]
  if (node.summary?.trim()) blocks.push(Math.min(3, lineCount(node.summary, 12)) * 17)
  if (node.metric_name && node.metric_value !== undefined) {
    blocks.push(lineCount(`${node.metric_name} ${node.metric_value}`, 11) * 16)
  }
  if (node.experiment_id) blocks.push(28)
  const height = 24 + (blocks.length - 1) * 6 + blocks.reduce((sum, item) => sum + item, 0)
  return Math.max(CARD_MIN_HEIGHT, height)
}

function lineCount(text: string, fontSize: number) {
  const trimmed = text.trim()
  if (!trimmed) return 1
  const inner = CARD_WIDTH - 28
  let width = 0
  for (const char of trimmed) {
    const code = char.codePointAt(0) ?? 0
    width += code > 0x2e80 ? fontSize : fontSize * 0.56
  }
  return Math.max(1, Math.ceil(width / inner))
}

export function nodeTimelineAt(node: ResearchNode) {
  return node.occurred_at || node.created_at
}

export function assignTimeline(nodes: ResearchNode[], ranks: Map<string, number>, preferred?: 'time' | 'lineage') {
  const stamps = nodes
    .map((node) => {
      const iso = nodeTimelineAt(node)
      return { id: node.id, at: Date.parse(iso), iso, rank: ranks.get(node.id) ?? 0 }
    })
    .filter((item) => Number.isFinite(item.at))
    .sort((left, right) => left.at - right.at || left.rank - right.rank || left.id.localeCompare(right.id))
  if (!stamps.length) {
    return { axis: 'lineage' as const, columns: new Map<string, number>(), buckets: new Map<string, string>(), ticks: [] as TimelineTick[] }
  }
  const span = stamps[stamps.length - 1].at - stamps[0].at
  const axis = preferred ?? (span > 30 * 60 * 1000 ? 'time' as const : 'lineage' as const)
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

function displayColumns(nodes: ResearchNode[], ranks: Map<string, number>, timeline: ReturnType<typeof assignTimeline>) {
  const columns = new Map<string, number>()
  if (timeline.axis === 'lineage') {
    for (const node of nodes) columns.set(node.id, ranks.get(node.id) ?? 0)
    return columns
  }
  let max = -1
  for (const node of nodes) {
    const column = timeline.columns.get(node.id)
    if (column === undefined) continue
    columns.set(node.id, column)
    if (column > max) max = column
  }
  const extra = new Map<number, number>()
  let next = max + 1
  const undated = nodes.filter((node) => !columns.has(node.id)).sort((left, right) => {
    const rankDelta = (ranks.get(left.id) ?? 0) - (ranks.get(right.id) ?? 0)
    if (rankDelta !== 0) return rankDelta
    return left.id.localeCompare(right.id)
  })
  for (const node of undated) {
    const rank = ranks.get(node.id) ?? 0
    if (!extra.has(rank)) {
      extra.set(rank, next)
      next += 1
    }
    columns.set(node.id, extra.get(rank) ?? next)
  }
  return columns
}

function assignLanes(nodes: ResearchNode[], ranks: Map<string, number>, columns: Map<string, number>, incoming: Map<string, ResearchEdge[]>) {
  const lanes = new Map<string, number>()
  const ordered = [...nodes].sort((left, right) => {
    const rankDelta = (ranks.get(left.id) ?? 0) - (ranks.get(right.id) ?? 0)
    if (rankDelta !== 0) return rankDelta
    const timeDelta = nodeTimelineAt(left).localeCompare(nodeTimelineAt(right))
    if (timeDelta !== 0) return timeDelta
    return left.id.localeCompare(right.id)
  })
  const usedAtColumn = new Map<number, Set<number>>()
  let nextRootLane = 0
  for (const node of ordered) {
    const column = columns.get(node.id) ?? ranks.get(node.id) ?? 0
    const parents = (incoming.get(node.id) ?? []).map((edge) => edge.from_id)
    let preferred = node.kind === 'question' ? 0 : nextRootLane
    if (parents.length) {
      preferred = Math.min(...parents.map((id) => lanes.get(id) ?? 0))
    } else if (node.kind !== 'question') {
      nextRootLane += 1
    }
    const taken = usedAtColumn.get(column) ?? new Set<number>()
    let lane = preferred
    while (taken.has(lane)) lane += 1
    taken.add(lane)
    usedAtColumn.set(column, taken)
    lanes.set(node.id, lane)
  }
  return lanes
}

function assignTops(nodes: ResearchNode[], lanes: Map<string, number>, columns: Map<string, number>, heights: Map<string, number>) {
  const tops = new Map<string, number>()
  const grouped = new Map<number, ResearchNode[]>()
  for (const node of nodes) {
    const column = columns.get(node.id) ?? 0
    const list = grouped.get(column) ?? []
    list.push(node)
    grouped.set(column, list)
  }
  for (const list of grouped.values()) {
    list.sort((left, right) => {
      const laneDelta = (lanes.get(left.id) ?? 0) - (lanes.get(right.id) ?? 0)
      if (laneDelta !== 0) return laneDelta
      return left.id.localeCompare(right.id)
    })
    let floor = ORIGIN_Y
    for (const node of list) {
      const lane = lanes.get(node.id) ?? 0
      const height = heights.get(node.id) ?? CARD_MIN_HEIGHT
      const y = Math.max(ORIGIN_Y + lane * ROW_HEIGHT, floor)
      tops.set(node.id, y)
      floor = y + height + ROW_GAP
    }
  }
  return tops
}

const FAILURE_STATES = new Set(['failed', 'cancelled', 'timed_out', 'budget_stopped', 'provider_error', 'rejected'])

export function nodeOutcome(node: ResearchNode, verdict?: 'supports' | 'contradicts'): NodeOutcome {
  if (node.kind === 'observation') {
    if (verdict) return verdict
    // A close_run highlight without a verdict is evidence nobody has judged yet.
    return node.system_written ? 'inconclusive' : null
  }
  if (node.kind !== 'run' && node.kind !== 'result') return null
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
