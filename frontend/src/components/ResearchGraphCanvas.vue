<script setup lang="ts">
import { computed, markRaw, nextTick, onMounted, onUnmounted, provide, ref, watch } from 'vue'
import type { Edge, Node, NodeMouseEvent, VueFlowStore, ViewportTransform } from '@vue-flow/core'
import { Position, VueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import { Clock3, Maximize2, Minimize2, Scan, X } from '@lucide/vue'
import type { ResearchEdge, ResearchNode } from '../api'
import { localizedState, useI18n } from '../i18n'
import { CANVAS_MAX_HEIGHT, TICK_Y, type GraphLayout } from '../researchGraphLayout'
import ResearchGraphNode from './ResearchGraphNode.vue'
import ResearchGraphTick from './ResearchGraphTick.vue'

const props = defineProps<{
  nodes: ResearchNode[]
  edges: ResearchEdge[]
  layout: GraphLayout
}>()
const emit = defineEmits<{
  openExperiment: [experimentID: string]
}>()

const { languageTag, t } = useI18n()
const nodeTypes = {
  research: markRaw(ResearchGraphNode),
  timeline: markRaw(ResearchGraphTick),
}
const flowID = 'research-graph'
const flow = ref<VueFlowStore | null>(null)
const fullscreen = ref(false)
const fittedOnce = ref(false)
const selectedID = ref('')
const viewport = ref<ViewportTransform>({ x: 0, y: 0, zoom: 1 })
provide('researchGraphSelectedID', selectedID)
const selected = computed(() => props.nodes.find((node) => node.id === selectedID.value) ?? null)
const layoutByID = computed(() => new Map(props.layout.nodes.map((item) => [item.id, item])))
const nodeByID = computed(() => new Map(props.nodes.map((node) => [node.id, node])))

const graphNodes = computed<Node[]>(() => {
  const ticks: Node[] = props.layout.ticks.map((tick) => ({
    id: tick.id,
    type: 'timeline',
    position: { x: tick.x, y: TICK_Y },
    selectable: false,
    draggable: false,
    data: { label: tick.label },
  }))
  const research = props.nodes.map((node) => {
    const placed = layoutByID.value.get(node.id)
    return {
      id: node.id,
      type: 'research',
      position: { x: placed?.x ?? 36, y: placed?.y ?? 72 },
      class: [
        placed?.active ? 'is-active-path' : '',
        placed?.unlinked ? 'is-unlinked' : '',
        placed?.outcome === 'success' ? 'is-success' : '',
        placed?.outcome === 'failure' ? 'is-failure' : '',
      ].filter(Boolean).join(' '),
      data: {
        node,
        kindLabel: kindLabel(node.kind),
        active: Boolean(placed?.active),
        next: Boolean(placed?.next),
        unlinked: Boolean(placed?.unlinked),
        outcome: placed?.outcome ?? null,
        onOpenEvidence: (experimentID: string) => emit('openExperiment', experimentID),
        onOpenDetail: (id: string) => { selectedID.value = id },
      },
      sourcePosition: Position.Right,
      targetPosition: Position.Left,
    }
  })
  return [...ticks, ...research]
})

const graphEdges = computed<Edge[]>(() => props.edges.map((edge) => {
  const placed = props.layout.edges.find((item) => item.id === edge.id)
  const active = Boolean(placed?.active)
  return {
    id: edge.id,
    source: edge.from_id,
    target: edge.to_id,
    label: relationLabel(edge.relation),
    type: 'smoothstep',
    animated: active,
    class: active ? 'graph-edge-active' : 'graph-edge-quiet',
    style: active
      ? { stroke: '#b45309', strokeWidth: 2.4 }
      : { stroke: '#d5cbb8', strokeWidth: 1.2, opacity: 0.55 },
  }
}))

const incoming = computed(() => props.edges.filter((edge) => edge.to_id === selectedID.value))
const outgoing = computed(() => props.edges.filter((edge) => edge.from_id === selectedID.value))

function relationLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    leads_to: ['leads to', '引出'],
    compares: ['compares', '对比'],
    supersedes: ['supersedes', '替代'],
    supports: ['supports', '支持'],
    contradicts: ['contradicts', '反驳'],
    produced: ['produced', '产生'],
  }
  const label = labels[value] ?? [value, value]
  return t(label[0], label[1])
}

function kindLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    question: ['Question', '问题'],
    hypothesis: ['Hypothesis', '假设'],
    plan: ['Plan', '计划'],
    run: ['Run', '运行'],
    result: ['Result', '结果'],
    observation: ['Observation', '观察'],
    decision: ['Decision', '决策'],
  }
  const label = labels[value] ?? [value, value]
  return t(label[0], label[1])
}

function dateTime(value?: string) {
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function neighborTitle(id: string) {
  return nodeByID.value.get(id)?.title ?? id
}

function onNodeDoubleClick(payload: NodeMouseEvent) {
  if (payload.node.type === 'timeline') return
  selectedID.value = payload.node.id
}

function onPaneClick() {
  selectedID.value = ''
}

function onPaneReady(instance: VueFlowStore) {
  flow.value = instance
  if (fittedOnce.value) return
  fittedOnce.value = true
  void instance.fitView({ padding: 0.16 })
}

async function fitGraph() {
  await nextTick()
  await flow.value?.fitView({ padding: 0.16, duration: 180 })
}

function toggleFullscreen() {
  fullscreen.value = !fullscreen.value
}

function onKey(event: KeyboardEvent) {
  if (event.key !== 'Escape') return
  if (selectedID.value) {
    selectedID.value = ''
    return
  }
  if (fullscreen.value) fullscreen.value = false
}

watch(fullscreen, (open) => {
  document.body.style.overflow = open ? 'hidden' : ''
})

onMounted(() => window.addEventListener('keydown', onKey))
onUnmounted(() => {
  window.removeEventListener('keydown', onKey)
  document.body.style.overflow = ''
})
</script>

<template>
  <Teleport to="body" :disabled="!fullscreen">
  <div class="graph-shell" :class="{ 'is-fullscreen': fullscreen }" :style="fullscreen ? undefined : { maxHeight: `${CANVAS_MAX_HEIGHT}px` }">
    <div class="graph-toolbar">
      <span class="graph-axis-label"><Clock3 :size="13" />{{
        layout.axis === 'time'
          ? t('Exploration time left to right', '探索时间从左到右')
          : t('Exploration sequence left to right; times are clustered in one session', '探索序从左到右；这次写入挤在同一时段')
      }}</span>
      <div class="graph-toolbar-actions">
        <button class="secondary-button small-button" type="button" :aria-label="t('Fit graph', '适配窗口')" @click="fitGraph">
          <Scan :size="15" />{{ t('Fit', '适配') }}
        </button>
        <button class="secondary-button small-button" type="button" :aria-label="fullscreen ? t('Exit fullscreen', '退出全屏') : t('Fullscreen graph', '全屏 Graph')" @click="toggleFullscreen">
          <Minimize2 v-if="fullscreen" :size="15" /><Maximize2 v-else :size="15" />
          {{ fullscreen ? t('Exit', '退出') : t('Fullscreen', '全屏') }}
        </button>
      </div>
    </div>
    <div class="graph-stage">
      <div class="graph-canvas" data-testid="research-graph-canvas">
        <VueFlow
          :id="flowID"
          v-model:viewport="viewport"
          :nodes="graphNodes"
          :edges="graphEdges"
          :node-types="nodeTypes"
          :min-zoom="0.18"
          :max-zoom="1.8"
          :fit-view-on-init="false"
          :nodes-draggable="false"
          :nodes-connectable="false"
          :elements-selectable="true"
          :zoom-on-double-click="false"
          :zoom-on-scroll="false"
          :pan-on-scroll="true"
          :pan-on-drag="true"
          @node-double-click="onNodeDoubleClick"
          @pane-click="onPaneClick"
          @pane-ready="onPaneReady"
        >
          <Background pattern-color="#d8d0c4" :gap="18" />
          <MiniMap pannable zoomable />
          <Controls :show-fit-view="false" :show-interactive="false" />
        </VueFlow>
      </div>
      <aside v-if="selected" class="graph-detail" :aria-label="t('Node detail', '节点细节')">
        <header>
          <div>
            <small>{{ kindLabel(selected.kind) }} · {{ localizedState(selected.status) }}</small>
            <span
              v-if="layoutByID.get(selected.id)?.outcome"
              class="flow-node-outcome"
              :data-outcome="layoutByID.get(selected.id)?.outcome"
            >{{ layoutByID.get(selected.id)?.outcome === 'failure' ? t('Failure', '失败') : t('Success', '成功') }}</span>
            <strong>{{ selected.title }}</strong>
          </div>
          <button class="icon-button" type="button" :title="t('Close', '关闭')" @click="selectedID = ''"><X :size="16" /></button>
        </header>
        <p v-if="selected.summary">{{ selected.summary }}</p>
        <p v-else class="graph-detail-empty">{{ t('No summary on this node.', '这个节点还没有摘要。') }}</p>
        <dl>
          <div v-if="selected.metric_name">
            <dt>{{ t('Metric', '指标') }}</dt>
            <dd>{{ selected.metric_name }} {{ selected.metric_value }}</dd>
          </div>
          <div>
            <dt>{{ t('Recorded', '记录于') }}</dt>
            <dd>{{ dateTime(selected.created_at) }}</dd>
          </div>
          <div>
            <dt>{{ t('Updated', '更新于') }}</dt>
            <dd>{{ dateTime(selected.updated_at) }}</dd>
          </div>
        </dl>
        <div v-if="incoming.length || outgoing.length" class="graph-detail-edges">
          <h4>{{ t('On the Graph', '在 Graph 上') }}</h4>
          <p v-for="edge in incoming" :key="edge.id">{{ neighborTitle(edge.from_id) }} → {{ relationLabel(edge.relation) }}</p>
          <p v-for="edge in outgoing" :key="edge.id">{{ relationLabel(edge.relation) }} → {{ neighborTitle(edge.to_id) }}</p>
        </div>
        <button
          v-if="selected.experiment_id"
          class="secondary-button small-button"
          type="button"
          @click="emit('openExperiment', selected.experiment_id ?? '')"
        >
          {{ t('Open evidence', '打开证据') }}
        </button>
        <p class="graph-detail-hint">{{ t('Double-click another node, or press Esc.', '双击另一个节点，或按 Esc。') }}</p>
      </aside>
    </div>
  </div>
  </Teleport>
</template>
