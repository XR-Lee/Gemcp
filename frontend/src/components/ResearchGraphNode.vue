<script setup lang="ts">
import { computed, inject, type Ref, ref } from 'vue'
import type { NodeProps } from '@vue-flow/core'
import { Handle, Position } from '@vue-flow/core'
import type { ResearchNode } from '../api'
import { localizedState, useI18n } from '../i18n'
import type { NodeOutcome } from '../researchGraphLayout'
import { formatMetric } from '../researchFormat'

type GraphNodeData = {
  node: ResearchNode
  kindLabel: string
  active?: boolean
  next?: boolean
  unlinked?: boolean
  outcome?: NodeOutcome
  onOpenEvidence?: (experimentID: string) => void
  onOpenDetail?: (id: string) => void
}

const props = defineProps<NodeProps<GraphNodeData>>()
const selectedID = inject<Ref<string>>('researchGraphSelectedID', ref(''))
const selected = computed(() => selectedID.value === props.id)
const { t } = useI18n()
// Run and result stamps say whether the Experiment ran; observation stamps
// say what the evidence means for the hypothesis.
const outcomeLabel = computed(() => {
  switch (props.data.outcome) {
    case 'success': return t('Ran', '已跑完')
    case 'failure': return t('Run failed', '运行失败')
    case 'supports': return t('Supports', '支持')
    case 'contradicts': return t('Contradicts', '反驳')
    case 'inconclusive': return t('Inconclusive', '不确定')
    default: return ''
  }
})
const showStatus = computed(() => props.data.node.kind !== 'observation')
const metricText = computed(() => {
  const node = props.data.node
  if (!node.metric_name || node.metric_value === undefined) return ''
  return `${node.metric_name} ${formatMetric(node.metric_value)}`
})
const metricSource = computed(() => {
  if (props.data.node.metric_source === 'experiment') return t('measured', '实测')
  if (props.data.node.metric_source === 'asserted') return t('asserted', '声称')
  return ''
})

function openRecord() {
  const experimentID = props.data.node.experiment_id
  if (experimentID) {
    props.data.onOpenEvidence?.(experimentID)
    return
  }
  props.data.onOpenDetail?.(props.data.node.id)
}
</script>

<template>
  <article
    class="flow-node nodrag"
    :class="{
      'is-active-path': data.active,
      'is-unlinked': data.unlinked,
      'is-selected': selected,
      'is-success': data.outcome === 'success' || data.outcome === 'supports',
      'is-failure': data.outcome === 'failure',
      'is-contradicts': data.outcome === 'contradicts',
    }"
    :data-kind="data.node.kind"
    :data-state="data.node.status"
    :data-outcome="data.outcome || undefined"
    data-testid="research-graph-node"
    :title="data.node.experiment_id
      ? t('Double-click to open the experiment record', '双击打开实验记录')
      : t('Double-click for detail', '双击展开细节')"
    @click.stop
    @dblclick.stop="openRecord"
  >
    <Handle id="target" type="target" :position="Position.Left" />
    <div class="flow-node-meta">
      <small>{{ data.kindLabel }}<template v-if="showStatus"> · {{ localizedState(data.node.status) }}</template><template v-if="data.active"> · {{ t('Active path', 'Active path') }}</template><template v-else-if="data.next"> · {{ t('Next', '下一步') }}</template><template v-else-if="data.unlinked"> · {{ t('Unlinked', '未连接') }}</template></small>
      <span v-if="outcomeLabel" class="flow-node-outcome" :data-outcome="data.outcome">{{ outcomeLabel }}</span>
    </div>
    <strong>{{ data.node.title }}</strong>
    <p v-if="data.node.summary">{{ data.node.summary }}</p>
    <code v-if="metricText" :title="String(data.node.metric_value)">{{ metricText }}<span v-if="metricSource" class="flow-node-metric-source" :data-source="data.node.metric_source"> · {{ metricSource }}</span></code>
    <button
      v-if="data.node.experiment_id"
      class="text-button nodrag"
      type="button"
      @click.stop="data.onOpenEvidence?.(data.node.experiment_id)"
      @dblclick.stop
    >
      {{ t('Evidence', '证据') }} {{ data.node.experiment_state ? localizedState(data.node.experiment_state) : '' }}
    </button>
    <Handle id="source" type="source" :position="Position.Right" />
  </article>
</template>
