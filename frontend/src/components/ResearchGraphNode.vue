<script setup lang="ts">
import type { NodeProps } from '@vue-flow/core'
import { Handle, Position } from '@vue-flow/core'
import type { ResearchNode } from '../api'
import { localizedState, useI18n } from '../i18n'

type GraphNodeData = {
  node: ResearchNode
  kindLabel: string
  onOpenEvidence?: (experimentID: string) => void
}

defineProps<NodeProps<GraphNodeData>>()
const { t } = useI18n()
</script>

<template>
  <article class="flow-node" :data-kind="data.node.kind" :data-state="data.node.status">
    <Handle id="target" type="target" :position="Position.Left" />
    <small>{{ data.kindLabel }} · {{ localizedState(data.node.status) }}</small>
    <strong>{{ data.node.title }}</strong>
    <p v-if="data.node.summary">{{ data.node.summary }}</p>
    <code v-if="data.node.metric_name">{{ data.node.metric_name }} {{ data.node.metric_value }}</code>
    <button
      v-if="data.node.experiment_id"
      class="text-button nodrag nopan"
      type="button"
      @click="data.onOpenEvidence?.(data.node.experiment_id)"
    >
      {{ t('Evidence', '证据') }} {{ data.node.experiment_state ? localizedState(data.node.experiment_state) : '' }}
    </button>
    <Handle id="source" type="source" :position="Position.Right" />
  </article>
</template>
