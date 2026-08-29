<script setup lang="ts">
import { computed, inject, type Ref, ref } from 'vue'
import type { NodeProps } from '@vue-flow/core'
import { Handle, Position } from '@vue-flow/core'
import type { ResearchNode } from '../api'
import { localizedState, useI18n } from '../i18n'
import type { NodeOutcome } from '../researchGraphLayout'

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
const outcomeLabel = computed(() => {
  if (props.data.outcome === 'success') return t('Success', '成功')
  if (props.data.outcome === 'failure') return t('Failure', '失败')
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
      'is-success': data.outcome === 'success',
      'is-failure': data.outcome === 'failure',
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
      <small>{{ data.kindLabel }} · {{ localizedState(data.node.status) }}<template v-if="data.active"> · {{ t('Active path', 'Active path') }}</template><template v-else-if="data.next"> · {{ t('Next', '下一步') }}</template><template v-else-if="data.unlinked"> · {{ t('Unlinked', '未连接') }}</template></small>
      <span v-if="outcomeLabel" class="flow-node-outcome" :data-outcome="data.outcome">{{ outcomeLabel }}</span>
    </div>
    <strong>{{ data.node.title }}</strong>
    <p v-if="data.node.summary">{{ data.node.summary }}</p>
    <code v-if="data.node.metric_name">{{ data.node.metric_name }} {{ data.node.metric_value }}</code>
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
