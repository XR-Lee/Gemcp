<script setup lang="ts">
import { ChevronRight, FlaskConical } from '@lucide/vue'
import type { Experiment } from '../api'
import { localizedState, useI18n } from '../i18n'

withDefaults(defineProps<{ experiments: Experiment[]; compact?: boolean }>(), { compact: false })
const emit = defineEmits<{ select: [experiment: Experiment] }>()
const { languageTag, t } = useI18n()

function shortID(value: string) {
  return value.slice(0, 8)
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat(languageTag.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

function formatMoney(milli: number) {
  return `CNY ${(milli / 1000).toFixed(2)}`
}

function stateLabel(value: string) {
  return localizedState(value)
}
</script>

<template>
  <div v-if="experiments.length" class="table-scroll">
    <table class="data-table experiment-table">
      <thead><tr><th>{{ t('State', '状态') }}</th><th>Experiment</th><th>{{ t('Graph', 'Graph') }}</th><th>Commit</th><th>{{ t('Submitted', '提交时间') }}</th><th>{{ t('Reservation', '预留') }}</th><th v-if="!compact">{{ t('Runtime', '运行时长') }}</th><th :aria-label="t('Open', '打开')" /></tr></thead>
      <tbody>
        <tr v-for="experiment in experiments" :key="experiment.id" tabindex="0" @click="emit('select', experiment)" @keydown.enter="emit('select', experiment)">
          <td><span class="state-badge" :data-state="experiment.state"><span />{{ stateLabel(experiment.state) }}</span></td>
          <td><code>{{ shortID(experiment.id) }}</code></td>
          <td>
            <span class="graph-link-badge" :data-orphan="experiment.orphaned ? 'true' : 'false'">
              {{ experiment.orphaned ? t('Off-graph', '未入图') : experiment.graph_linked ? t('On-graph', '已入图') : t('Lab only', '仅 Lab') }}
            </span>
          </td>
          <td><code>{{ experiment.commit_sha.slice(0, 9) }}</code></td>
          <td>{{ formatTime(experiment.created_at) }}</td>
          <td>{{ formatMoney(experiment.reserved_cost_milli) }}</td>
          <td v-if="!compact">{{ Math.round(experiment.max_runtime_seconds / 60) }} {{ t('min', '分钟') }}</td>
          <td class="row-arrow"><ChevronRight :size="15" /></td>
        </tr>
      </tbody>
    </table>
  </div>
  <div v-else class="empty-state compact-empty">
    <span class="empty-icon"><FlaskConical :size="21" /></span>
    <h3>{{ t('No experiments', '暂无实验') }}</h3>
    <p>{{ t('Agent submissions for this project will appear here.', 'Agent 提交到此 Project 的实验会显示在这里。') }}</p>
  </div>
</template>
