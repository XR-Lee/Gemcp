<script setup lang="ts">
import { ChevronRight, FlaskConical } from '@lucide/vue'
import type { Experiment } from '../api'

withDefaults(defineProps<{ experiments: Experiment[]; compact?: boolean }>(), { compact: false })
const emit = defineEmits<{ select: [experiment: Experiment] }>()

function shortID(value: string) {
  return value.slice(0, 8)
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat(undefined, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit' }).format(new Date(value))
}

function formatMoney(milli: number) {
  return `CNY ${(milli / 1000).toFixed(2)}`
}

function stateLabel(value: string) {
  return value.replaceAll('_', ' ')
}
</script>

<template>
  <div v-if="experiments.length" class="table-scroll">
    <table class="data-table experiment-table">
      <thead><tr><th>State</th><th>Experiment</th><th>Commit</th><th>Submitted</th><th>Reservation</th><th v-if="!compact">Runtime</th><th aria-label="Open" /></tr></thead>
      <tbody>
        <tr v-for="experiment in experiments" :key="experiment.id" tabindex="0" @click="emit('select', experiment)" @keydown.enter="emit('select', experiment)">
          <td><span class="state-badge" :data-state="experiment.state"><span />{{ stateLabel(experiment.state) }}</span></td>
          <td><code>{{ shortID(experiment.id) }}</code></td>
          <td><code>{{ experiment.commit_sha.slice(0, 9) }}</code></td>
          <td>{{ formatTime(experiment.created_at) }}</td>
          <td>{{ formatMoney(experiment.reserved_cost_milli) }}</td>
          <td v-if="!compact">{{ Math.round(experiment.max_runtime_seconds / 60) }} min</td>
          <td class="row-arrow"><ChevronRight :size="15" /></td>
        </tr>
      </tbody>
    </table>
  </div>
  <div v-else class="empty-state compact-empty">
    <span class="empty-icon"><FlaskConical :size="21" /></span>
    <h3>No experiments</h3>
    <p>Agent submissions for this project will appear here.</p>
  </div>
</template>
