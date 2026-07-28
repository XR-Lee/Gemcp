<script setup lang="ts">
import { computed } from 'vue'
import { Bot, CheckCircle2, Clock3, Cpu, GitBranch, LoaderCircle, SquareTerminal, TriangleAlert } from '@lucide/vue'
import type { AgentActivity, OperationsFeed, ProposalActivity } from '../api'
import { useI18n } from '../i18n'

const props = withDefaults(defineProps<{ feed: OperationsFeed | null; compact?: boolean; loading?: boolean }>(), {
  compact: false,
  loading: false,
})
const emit = defineEmits<{ openExperiment: [experimentID: string] }>()
const { languageTag, t } = useI18n()

const activities = computed(() => {
  const seen = new Set<string>()
  const result: AgentActivity[] = []
  for (const activity of props.feed?.activities ?? []) {
    const key = activity.agent_token_prefix || activity.agent_label || activity.id
    if (seen.has(key)) continue
    seen.add(key)
    result.push(activity)
    if (result.length >= (props.compact ? 3 : 8)) break
  }
  return result
})
const proposals = computed(() => (props.feed?.proposals ?? []).slice(0, props.compact ? 3 : 8))

function phaseLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    inspecting_repository: ['Inspecting repository', '检查仓库'],
    selecting_workload: ['Selecting workload', '选择运行入口'],
    preparing_proposal: ['Preparing proposal', '准备提案'],
    awaiting_confirmation: ['Awaiting confirmation', '等待确认'],
    submitting: ['Submitting', '正在提交'],
    monitoring: ['Monitoring run', '监控运行'],
    reviewing_results: ['Reviewing results', '分析结果'],
    blocked: ['Blocked', '已阻塞'],
    idle: ['Idle', '空闲'],
  }
  const label = labels[value] ?? [value, value]
  return t(label[0], label[1])
}

function activityTone(activity: AgentActivity) {
  if (activity.phase === 'blocked') return 'danger'
  if (activity.phase === 'awaiting_confirmation') return 'waiting'
  if (activity.phase === 'idle') return 'idle'
  return Date.now() - new Date(activity.at).getTime() < 120_000 ? 'active' : 'stale'
}

function activityStatus(activity: AgentActivity) {
  const tone = activityTone(activity)
  if (tone === 'active') return t('Updated recently', '刚刚更新')
  if (tone === 'stale') return t('No report for over 2 minutes', '超过 2 分钟未上报')
  if (tone === 'waiting') return t('Waiting for approval', '等待批准')
  if (tone === 'danger') return t('Blocked', '已阻塞')
  return t('Not active', '当前不活跃')
}

function proposalTone(proposal: ProposalActivity) {
  if (!proposal.eligible) return 'danger'
  if (proposal.status === 'submitted') return 'complete'
  if (proposal.status === 'expired') return 'idle'
  return 'waiting'
}

function proposalLabel(proposal: ProposalActivity) {
  if (!proposal.eligible) return t('Preflight blocked', '预检阻塞')
  if (proposal.status === 'submitted') return t('Submitted', '已提交')
  if (proposal.status === 'expired') return t('Expired', '已过期')
  return t('Approval required', '等待批准')
}

function formatTime(value: string) {
  return new Intl.DateTimeFormat(languageTag.value, { month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit' }).format(new Date(value))
}

function money(value: number) {
  return `CNY ${(value / 1000).toFixed(3)}`
}

function checkSummary(proposal: ProposalActivity) {
  const failed = proposal.checks.filter((check) => check.status === 'fail').length
  const warnings = proposal.checks.filter((check) => check.status === 'warn').length
  if (failed) return t(`${failed} failed checks`, `${failed} 项失败`)
  if (warnings) return t(`${warnings} warnings`, `${warnings} 项警告`)
  return t('All checks passed', '全部检查通过')
}
</script>

<template>
  <div class="run-activity-panel">
    <section class="activity-section">
      <div class="activity-subheading">
        <div><Bot :size="17" /><span><strong>{{ t('Agent activity', 'Agent 动态') }}</strong><small>{{ t('Latest reported phase per Agent', '每个 Agent 最近上报的阶段') }}</small></span></div>
        <LoaderCircle v-if="loading" :size="17" class="spinning" />
      </div>
      <div v-if="activities.length" class="activity-list">
        <button v-for="activity in activities" :key="activity.id" class="activity-row" type="button" :disabled="!activity.experiment_id" @click="activity.experiment_id && emit('openExperiment', activity.experiment_id)">
          <span class="activity-indicator" :data-tone="activityTone(activity)"><LoaderCircle v-if="activityTone(activity) === 'active'" :size="14" class="spinning" /><TriangleAlert v-else-if="activityTone(activity) === 'danger'" :size="14" /><Clock3 v-else :size="14" /></span>
          <span class="activity-primary"><strong>{{ phaseLabel(activity.phase) }}</strong><small>{{ activity.agent_label || activity.agent_token_prefix || t('Agent', 'Agent') }} · {{ activityStatus(activity) }}</small></span>
          <span class="activity-target"><code>{{ activity.repository_remote || t('Project context', 'Project 上下文') }}</code><small>{{ activity.ref || activity.experiment_id || activity.proposal_id || '—' }}</small></span>
          <time>{{ formatTime(activity.at) }}</time>
        </button>
      </div>
      <div v-else class="activity-empty">{{ t('No Agent has reported activity yet.', '尚无 Agent 上报活动。') }}</div>
    </section>

    <section class="activity-section proposal-section">
      <div class="activity-subheading">
        <div><GitBranch :size="17" /><span><strong>{{ t('Prepared proposals', '准备中的提案') }}</strong><small>{{ t('Resolved source, environment, GPU and approval state', '已解析的源码、环境、GPU 与批准状态') }}</small></span></div>
      </div>
      <div v-if="proposals.length" class="proposal-list">
        <button v-for="proposal in proposals" :key="proposal.id" class="proposal-row" type="button" :disabled="!proposal.experiment_id" @click="proposal.experiment_id && emit('openExperiment', proposal.experiment_id)">
          <span class="activity-indicator" :data-tone="proposalTone(proposal)"><CheckCircle2 v-if="proposalTone(proposal) === 'complete'" :size="14" /><TriangleAlert v-else-if="proposalTone(proposal) === 'danger'" :size="14" /><Clock3 v-else :size="14" /></span>
          <span class="proposal-source"><strong>{{ proposal.repository_name }} · {{ proposal.requested_ref }}</strong><code>{{ proposal.commit_sha.slice(0, 12) }}</code></span>
          <span class="proposal-command"><SquareTerminal :size="13" /><code>{{ proposal.display_command }}</code></span>
          <span class="proposal-runtime"><Cpu :size="13" /><span>{{ proposal.environment_name }} · {{ proposal.gpu_num }}× {{ proposal.gpu_models.join(', ') }}</span></span>
          <span class="proposal-result" :data-tone="proposalTone(proposal)"><strong>{{ proposalLabel(proposal) }}</strong><small>{{ checkSummary(proposal) }} · {{ money(proposal.reserved_cost_milli) }}</small></span>
        </button>
      </div>
      <div v-else class="activity-empty">{{ t('No prepared proposals.', '暂无准备中的提案。') }}</div>
    </section>
  </div>
</template>

<style scoped>
.run-activity-panel { min-width: 0; }
.activity-section + .activity-section { border-top: 1px solid #e2e7e3; }
.activity-subheading { min-height: 54px; padding: 10px 16px; display: flex; align-items: center; justify-content: space-between; gap: 14px; background: #fafcfb; }
.activity-subheading > div { min-width: 0; display: flex; align-items: center; gap: 9px; color: #3f705b; }
.activity-subheading span, .activity-subheading strong, .activity-subheading small { min-width: 0; display: block; }
.activity-subheading strong { color: #334039; font-size: 12px; line-height: 17px; }
.activity-subheading small { color: #7c8780; font-size: 10px; line-height: 15px; }
.activity-list, .proposal-list { min-width: 0; }
.activity-row, .proposal-row { width: 100%; min-width: 0; padding: 10px 16px; display: grid; align-items: center; gap: 12px; color: inherit; background: #fff; border: 0; border-top: 1px solid #edf0ee; text-align: left; }
.activity-row { grid-template-columns: 28px minmax(125px, .7fr) minmax(220px, 1.6fr) 132px; }
.proposal-row { grid-template-columns: 28px minmax(150px, .85fr) minmax(180px, 1.25fr) minmax(180px, 1fr) minmax(150px, .75fr); }
.activity-row:not(:disabled), .proposal-row:not(:disabled) { cursor: pointer; }
.activity-row:not(:disabled):hover, .proposal-row:not(:disabled):hover { background: #f7faf8; }
.activity-row:disabled, .proposal-row:disabled { opacity: 1; }
.activity-indicator { width: 26px; height: 26px; display: grid; place-items: center; color: #69756e; background: #eef2ef; border-radius: 5px; }
.activity-indicator[data-tone='active'] { color: #176b4d; background: #dff1e8; }
.activity-indicator[data-tone='waiting'] { color: #84610e; background: #fff1c9; }
.activity-indicator[data-tone='danger'] { color: #9a3f37; background: #fde5e2; }
.activity-indicator[data-tone='complete'] { color: #176b4d; background: #dff1e8; }
.activity-primary, .activity-target, .proposal-source, .proposal-result { min-width: 0; }
.activity-primary strong, .activity-primary small, .activity-target code, .activity-target small, .proposal-source strong, .proposal-source code, .proposal-result strong, .proposal-result small { display: block; min-width: 0; }
.activity-primary strong, .proposal-source strong, .proposal-result strong { color: #344039; font-size: 11px; line-height: 16px; }
.activity-primary small, .activity-target small, .proposal-source code, .proposal-result small { margin-top: 2px; color: #7c8780; font-size: 9px; line-height: 14px; }
.activity-target code, .proposal-command code { overflow: hidden; color: #4a5750; font-size: 10px; text-overflow: ellipsis; white-space: nowrap; }
.activity-row time { color: #7f8983; font-size: 9px; text-align: right; white-space: nowrap; }
.proposal-command, .proposal-runtime { min-width: 0; display: flex; align-items: center; gap: 6px; color: #66736c; }
.proposal-command svg, .proposal-runtime svg { flex: 0 0 auto; }
.proposal-runtime span { overflow: hidden; font-size: 10px; line-height: 15px; text-overflow: ellipsis; white-space: nowrap; }
.proposal-result { text-align: right; }
.proposal-result[data-tone='danger'] strong { color: #9a3f37; }
.proposal-result[data-tone='waiting'] strong { color: #7b5c13; }
.activity-empty { padding: 22px 16px; color: #818b85; border-top: 1px solid #edf0ee; font-size: 11px; text-align: center; }
@media (max-width: 820px) {
  .activity-row { grid-template-columns: 28px minmax(0, 1fr); }
  .activity-target, .activity-row time { grid-column: 2; text-align: left; }
  .proposal-row { grid-template-columns: 28px minmax(0, 1fr); }
  .proposal-command, .proposal-runtime, .proposal-result { grid-column: 2; text-align: left; }
}
</style>
