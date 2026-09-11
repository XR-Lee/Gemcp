<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Activity, Box, CheckCircle2, Clock3, Copy, Cpu, Folder, GitCommitHorizontal, ListTree, LoaderCircle, Radio, Save, Server, SquareTerminal, TriangleAlert, X } from '@lucide/vue'
import { APIError, api, type Attempt, type Experiment, type ProjectWorkload } from '../api'
import { useI18n } from '../i18n'
import WorkbenchDialog from './WorkbenchDialog.vue'

const props = defineProps<{ experiment: Experiment; attempts: Attempt[]; loading: boolean; error: string }>()
const emit = defineEmits<{ close: []; saved: [workload: ProjectWorkload] }>()
const { languageTag, t } = useI18n()
const saveOpen = ref(false)
const saveName = ref('oneshot')
const saveYAML = ref('')
const saveError = ref('')
const saveBusy = ref(false)
const copied = ref(false)
let previewGeneration = 0

const context = computed(() => props.experiment.execution_context)
const runtime = computed(() => context.value?.runtime_info)
const latestAttempt = computed(() => props.attempts.at(-1) ?? null)
const logTail = computed(() => props.experiment.log_tail || latestAttempt.value?.log_tail || '')
const metrics = computed(() => props.experiment.metrics ?? latestAttempt.value?.metrics)
const live = computed(() => ['starting', 'running', 'stopping'].includes(props.experiment.state))

function dateTime(value?: string) {
  if (!value) return '—'
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'medium' }).format(new Date(value))
}
function money(value?: number) { return `CNY ${((value ?? 0) / 1000).toFixed(3)}` }
function stateLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    queued: ['Queued', '排队中'], starting: ['Starting', '启动中'], running: ['Running', '运行中'], stopping: ['Stopping', '停止中'],
    succeeded: ['Succeeded', '已成功'], failed: ['Failed', '已失败'], cancelled: ['Cancelled', '已取消'], timed_out: ['Timed out', '已超时'],
    reserved: ['Reserved', '已预留'], provisioning: ['Provisioning', '创建资源中'], ready: ['Ready', '已就绪'], deleting: ['Cleaning up', '清理中'], deleted: ['Cleaned up', '已清理'],
  }
  const label = labels[value] ?? [value, value]
  return t(label[0], label[1])
}
function timelineLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    'experiment.created': ['Experiment created', 'Experiment 已创建'], experiment_created: ['Experiment created', 'Experiment 已创建'],
    'attempt.created': ['Attempt created', 'Attempt 已创建'], attempt_created: ['Attempt created', 'Attempt 已创建'],
    'attempt.started': ['Attempt started', 'Attempt 已启动'], attempt_started: ['Attempt started', 'Attempt 已启动'],
    'attempt.finished': ['Attempt finished', 'Attempt 已结束'],
    'runner.bootstrap_stage': ['Runner startup stage', 'Runner 启动阶段'], runner_source_download: ['Source downloaded', '源码已下载'],
    'experiment.started': ['Workload started', 'Workload 已启动'], runner_started: ['Runner started', 'Runner 已启动'], workload_started: ['Workload started', 'Workload 已启动'],
    'experiment.runner_finished': ['Runner finished', 'Runner 已结束'], 'experiment.finalized': ['Run finalized', '运行已结束'], finalized: ['Run finalized', '运行已结束'],
    'experiment.terminal': ['Experiment terminal', 'Experiment 已终止'], 'experiment.cancel_requested': ['Cancellation requested', '已请求取消'],
    'experiment.cleanup_complete': ['Cleanup complete', '清理完成'], cleanup_complete: ['Cleanup complete', '清理完成'], heartbeat: ['Runtime heartbeat', '运行心跳'],
  }
  const fallback = value.replaceAll('_', ' ').replaceAll('.', ' ')
  const label = labels[value] ?? [fallback, fallback]
  return t(label[0], label[1])
}
function metricsJSON(value?: Record<string, unknown>) { return value ? JSON.stringify(value, null, 2) : '' }
function sourceLabel(value?: string) { return value === 'node_binding' ? t('Node binding', 'Node 绑定') : t('Runner observed', 'Runner 实测') }
function workspacePolicy(value?: string) { return value === 'container_fixed' ? t('Fixed container path', '固定容器路径') : t('Ephemeral Runner workspace', 'Runner 临时工作区') }
function cleanupLabel() {
  const observation = props.experiment.backend_observation
  if (observation?.cleanup_complete) return t('Complete', '已完成')
  if (observation && ['deleted', 'succeeded', 'failed', 'cancelled', 'timed_out', 'lost'].includes(observation.state)) return t('Evidence not reported', '尚未上报清理证据')
  return t('Pending', '等待中')
}
function validWorkloadName(value: string) {
  return /^[a-z0-9][a-z0-9_-]{0,63}$/.test(value)
}
function openSaveDialog() {
  saveName.value = props.experiment.saved_workload || 'oneshot'
  saveYAML.value = ''
  saveError.value = ''
  copied.value = false
  saveOpen.value = true
}
async function previewWorkload(name: string) {
  const generation = ++previewGeneration
  saveError.value = ''
  if (!validWorkloadName(name)) {
    saveYAML.value = ''
    return
  }
  try {
    const preview = await api.previewProjectWorkload(props.experiment.project_id, {
      experiment_id: props.experiment.id, name,
    })
    if (generation !== previewGeneration) return
    saveYAML.value = preview.manifest_yaml
  } catch (caught) {
    if (generation !== previewGeneration) return
    saveYAML.value = ''
    saveError.value = caught instanceof APIError ? caught.message : t('Could not preview this workload.', '无法预览该工作负载。')
  }
}
async function saveWorkload() {
  const name = saveName.value.trim()
  if (!validWorkloadName(name) || saveBusy.value) return
  saveBusy.value = true
  saveError.value = ''
  try {
    const saved = await api.saveProjectWorkload(props.experiment.project_id, {
      experiment_id: props.experiment.id, name,
    })
    emit('saved', saved)
    saveOpen.value = false
  } catch (caught) {
    saveError.value = caught instanceof APIError ? caught.message : t('Could not save this workload.', '无法保存该工作负载。')
  } finally {
    saveBusy.value = false
  }
}
async function copyYAML() {
  if (!saveYAML.value) return
  await navigator.clipboard.writeText(saveYAML.value)
  copied.value = true
}

watch([saveOpen, saveName], ([open, name]) => {
  if (open) void previewWorkload(String(name).trim())
})
</script>

<template>
  <div class="run-detail-backdrop" @click.self="$emit('close')">
    <section class="run-detail" role="dialog" aria-modal="true" :aria-label="t('Experiment details', '实验详情')">
      <header class="detail-header">
        <div class="detail-title">
          <span class="state-marker" :data-state="experiment.state"><Radio v-if="live" :size="15" /><CheckCircle2 v-else-if="experiment.state === 'succeeded'" :size="15" /><TriangleAlert v-else :size="15" /></span>
          <span><small>EXPERIMENT</small><strong>{{ experiment.id.slice(0, 12) }}</strong></span>
          <span class="state-pill" :data-state="experiment.state">{{ stateLabel(experiment.state) }}</span>
          <span v-if="live" class="live-copy"><span />{{ t('Updates every 5 seconds', '每 5 秒更新') }}</span>
        </div>
        <div class="detail-header-meta"><span>{{ t('Updated', '更新于') }} {{ dateTime(experiment.updated_at) }}</span><button class="detail-close" type="button" :title="t('Close details', '关闭详情')" @click="$emit('close')"><X :size="18" /></button></div>
      </header>

      <div class="detail-scroll">
        <section class="run-summary">
          <div class="summary-source"><GitCommitHorizontal :size="18" /><span><small>{{ t('Source', '源码') }}</small><strong>{{ context?.repository_name || experiment.repository_id }}</strong><code>{{ context?.requested_ref || '—' }} · {{ experiment.commit_sha }}</code></span></div>
          <div><Clock3 :size="18" /><span><small>{{ t('Runtime limit', '运行上限') }}</small><strong>{{ Math.round(experiment.max_runtime_seconds / 60) }} {{ t('minutes', '分钟') }}</strong><code>{{ money(experiment.reserved_cost_milli) }} {{ t('reserved', '已预留') }} · {{ money(experiment.estimated_cost_milli) }} {{ experiment.budget_finalized_at ? t('final', '已结算') : t('estimated', '估算') }}</code></span></div>
          <div><Server :size="18" /><span><small>{{ t('Backend', 'Backend') }}</small><strong>{{ context?.backend || '—' }}</strong><code>{{ context?.region || t('Managed region', '托管区域') }}</code></span></div>
          <div><Activity :size="18" /><span><small>{{ t('Attempt', 'Attempt') }}</small><strong>{{ latestAttempt ? `#${latestAttempt.number} · ${stateLabel(latestAttempt.state)}` : t('Not dispatched', '尚未调度') }}</strong><code>{{ latestAttempt?.last_heartbeat_at ? `${t('Heartbeat', '心跳')} ${dateTime(latestAttempt.last_heartbeat_at)}` : '—' }}</code></span></div>
        </section>

        <section class="detail-section command-section">
          <div class="section-title"><SquareTerminal :size="17" /><span><strong>{{ t('Execution request', '执行请求') }}</strong><small>{{ t('Immutable command approved for this run', '本次运行批准的不可变命令') }}</small></span><span class="authority-chip">{{ experiment.execution_mode || 'shell' }}</span>
            <button v-if="experiment.savable_workload" class="text-button save-workload-button" type="button" @click="openSaveDialog"><Save :size="14" />{{ t('Save as workload', '另存为工作负载') }}</button>
          </div>
          <pre>{{ experiment.execution_mode === 'argv' && experiment.argv ? JSON.stringify(experiment.argv) : experiment.command }}</pre>
          <div class="command-meta">
            <span>{{ t('Agent', 'Agent') }} <strong>{{ context?.agent_label || context?.agent_token_prefix || t('Owner or legacy submission', 'Owner 或历史提交') }}</strong></span>
            <span>{{ t('Proposal', '提案') }} <code>{{ context?.proposal_id || '—' }}</code></span>
            <span v-if="context?.workload">{{ t('Named workload', '命名工作负载') }} <code>{{ context.workload }}</code></span>
            <span v-if="experiment.saved_workload">{{ t('Saved as', '已保存为') }} <code>{{ experiment.saved_workload }}</code></span>
          </div>
        </section>

        <section class="detail-grid">
          <article class="detail-section">
            <div class="section-title"><Box :size="17" /><span><strong>{{ t('Environment', '运行环境') }}</strong><small>{{ t('Resolved request', '已解析请求') }}</small></span></div>
            <dl class="fact-list">
              <div><dt>{{ t('Environment', 'Environment') }}</dt><dd>{{ context?.environment_name || experiment.environment_id }}</dd></div>
              <div><dt>{{ t('Image', '镜像') }}</dt><dd><code>{{ context?.image || '—' }}</code></dd></div>
              <div><dt>{{ t('Resource profile', '资源规格') }}</dt><dd>{{ context?.resource_profile_name || experiment.resource_profile_id }}</dd></div>
              <div><dt>{{ t('Repository remote', '仓库地址') }}</dt><dd><code>{{ context?.repository_ssh_url || '—' }}</code></dd></div>
            </dl>
          </article>

          <article class="detail-section gpu-section">
            <div class="section-title"><Cpu :size="17" /><span><strong>{{ t('GPU assignment', 'GPU 分配') }}</strong><small>{{ t('Requested compared with observed', '请求值与实际观测值') }}</small></span></div>
            <div class="comparison-row"><span><small>{{ t('Requested', '请求') }}</small><strong>{{ context?.gpu_num ?? 0 }}× {{ context?.gpu_models?.join(', ') || '—' }}</strong><code>{{ context?.resource_profile_name || '—' }}</code></span><span class="comparison-arrow">→</span><span><small>{{ t('Observed', '实际') }}</small><strong>{{ runtime?.gpu_devices?.length ? `${runtime.gpu_devices.length}× ${runtime.gpu_devices.map((gpu) => gpu.name).join(', ')}` : runtime ? t('No GPU device observation reported', '未上报 GPU device 观测') : t('Awaiting runtime report', '等待运行时上报') }}</strong><code>{{ runtime?.cuda_visible_devices ? `CUDA_VISIBLE_DEVICES=${runtime.cuda_visible_devices}` : '—' }}</code></span></div>
            <div v-if="runtime?.gpu_devices?.length" class="gpu-list"><div v-for="gpu in runtime.gpu_devices" :key="`${gpu.index}-${gpu.uuid}`"><span>GPU {{ gpu.index }}</span><strong>{{ gpu.name }}</strong><code>{{ gpu.uuid }}</code></div></div>
          </article>

          <article class="detail-section path-section">
            <div class="section-title"><Folder :size="17" /><span><strong>{{ t('Paths', '运行路径') }}</strong><small>{{ runtime ? sourceLabel(runtime.source) : t('Expected paths until start', '启动前显示预期路径') }}</small></span></div>
            <dl class="fact-list">
              <div><dt>{{ t('Workspace policy', '工作区策略') }}</dt><dd>{{ workspacePolicy(context?.workspace_policy) }}</dd></div>
              <div><dt>{{ t('Expected workspace', '预期工作区') }}</dt><dd><code>{{ context?.workspace_path || t('Allocated by Runner at start', '由 Runner 启动时分配') }}</code></dd></div>
              <div><dt>{{ t('Observed working directory', '实际工作目录') }}</dt><dd><code>{{ runtime?.working_directory || t('Awaiting start', '等待启动') }}</code></dd></div>
              <div><dt>{{ t('Container output', '容器输出目录') }}</dt><dd><code>{{ context?.container_output_path || '—' }}</code></dd></div>
              <div><dt>{{ t('Observed output directory', '实际输出目录') }}</dt><dd><code>{{ runtime?.output_directory || t('Awaiting start', '等待启动') }}</code></dd></div>
              <div><dt>{{ t('Managed artifact path', '托管产物路径') }}</dt><dd><code>{{ experiment.output_path }}</code></dd></div>
              <div v-if="experiment.artifacts?.length"><dt>{{ t('Registered artifacts', '已登记产物') }}</dt><dd><code>{{ experiment.artifacts.join(', ') }}</code></dd></div>
              <div v-else><dt>{{ t('Registered artifacts', '已登记产物') }}</dt><dd>{{ t('None registered yet', '尚未登记') }}</dd></div>
              <div><dt>{{ t('Settlement', '结算') }}</dt><dd>{{ experiment.budget_finalized_at ? t('Budget finalized', '预算已结算') : t('Reservation open', '预留未结算') }}</dd></div>
            </dl>
          </article>

          <article class="detail-section backend-section">
            <div class="section-title"><Server :size="17" /><span><strong>{{ t('Backend lifecycle', 'Backend 生命周期') }}</strong><small>{{ t('Resource state and cleanup evidence', '资源状态与清理证据') }}</small></span></div>
            <dl v-if="experiment.backend_observation" class="fact-list">
              <div><dt>{{ experiment.backend_observation.kind }}</dt><dd><code>{{ experiment.backend_observation.id }}</code></dd></div>
              <div><dt>{{ t('State', '状态') }}</dt><dd>{{ stateLabel(experiment.backend_observation.state) }}<template v-if="experiment.backend_observation.status"> · {{ experiment.backend_observation.status }}</template></dd></div>
              <div v-if="experiment.backend_observation.node_label"><dt>{{ t('Node', '节点') }}</dt><dd>{{ experiment.backend_observation.node_label }}</dd></div>
              <div><dt>{{ t('Cleanup', '清理') }}</dt><dd :class="experiment.backend_observation.cleanup_complete ? 'success-text' : ''">{{ cleanupLabel() }}</dd></div>
              <div v-if="experiment.backend_observation.stop_reason"><dt>{{ t('Stop reason', '停止原因') }}</dt><dd>{{ experiment.backend_observation.stop_reason }}</dd></div>
              <div v-if="experiment.backend_observation.last_error"><dt>{{ t('Backend error', 'Backend 错误') }}</dt><dd class="danger-text">{{ experiment.backend_observation.last_error }}</dd></div>
              <div v-if="experiment.runner_stage"><dt>{{ t('Runner startup', 'Runner 启动阶段') }}</dt><dd><code>{{ experiment.runner_stage }}</code><template v-if="experiment.runner_error_type"> · {{ experiment.runner_error_type }}</template></dd></div>
              <div v-if="experiment.runner_source_downloads !== undefined"><dt>{{ t('Source downloads', '源码下载次数') }}</dt><dd>{{ experiment.runner_source_downloads }}<template v-if="experiment.runner_stage_updated_at"> · {{ dateTime(experiment.runner_stage_updated_at) }}</template></dd></div>
            </dl>
            <div v-else class="section-empty">{{ t('No backend resource has been assigned.', '尚未分配 Backend 资源。') }}</div>
          </article>
        </section>

        <section class="detail-section live-output-section">
          <div class="section-title"><Activity :size="17" /><span><strong>{{ t('Live output', '实时输出') }}</strong><small>{{ t('Bounded log tail and metrics reported by the runtime', '运行时上报的有界日志尾部与指标') }}</small></span><LoaderCircle v-if="live" :size="16" class="spinning" /></div>
          <div class="output-grid"><div><span>{{ t('Log tail', '日志尾部') }}</span><pre>{{ logTail || t('No runtime output reported yet.', '运行时尚未上报输出。') }}</pre></div><div><span>{{ t('Metrics', '指标') }}</span><pre>{{ metricsJSON(metrics) || t('No metrics reported yet.', '尚未上报指标。') }}</pre></div></div>
        </section>

        <section class="detail-grid history-grid">
          <article class="detail-section">
            <div class="section-title"><ListTree :size="17" /><span><strong>Attempts</strong><small>{{ t('Infrastructure retries and results', '基础设施重试与结果') }}</small></span><LoaderCircle v-if="loading" :size="16" class="spinning" /></div>
            <div v-if="error" class="detail-error">{{ error }}</div>
            <div v-else-if="attempts.length" class="attempt-list"><div v-for="item in attempts" :key="item.id"><span class="attempt-number">#{{ item.number }}</span><span><strong>{{ stateLabel(item.state) }}</strong><small>{{ item.provider_resource_id || t('No resource ID', '无资源 ID') }}</small></span><span><strong>{{ item.exit_code ?? '—' }}</strong><small>{{ money(item.estimated_cost_milli) }}</small></span><time>{{ dateTime(item.finished_at || item.updated_at) }}</time></div></div>
            <div v-else-if="!loading" class="section-empty">{{ t('No Attempt has been dispatched.', '尚未调度 Attempt。') }}</div>
          </article>
          <article class="detail-section">
            <div class="section-title"><ListTree :size="17" /><span><strong>{{ t('Timeline', '时间线') }}</strong><small>{{ t('Control plane and runtime milestones', '控制面与运行时里程碑') }}</small></span></div>
            <div v-if="experiment.timeline?.length" class="timeline"><div v-for="(event, index) in experiment.timeline" :key="`${event.at}-${event.code}-${index}`"><span class="timeline-dot" /><span><strong>{{ timelineLabel(event.code) }}</strong><small v-if="event.detail">{{ event.detail }}</small></span><time>{{ dateTime(event.at) }}</time></div></div>
            <div v-else class="section-empty">{{ t('No timeline events recorded.', '尚无时间线事件。') }}</div>
          </article>
        </section>

        <div v-if="experiment.failure_reason" class="failure-banner"><TriangleAlert :size="17" /><span><strong>{{ experiment.failure_code || t('Experiment failed', 'Experiment 失败') }}</strong>{{ experiment.failure_reason }}</span></div>
      </div>
    </section>
  </div>
  <WorkbenchDialog v-model:open="saveOpen" :title="t('Save as workload', '另存为工作负载')" :label="t('Save as workload', '另存为工作负载')" :description="t('Review the gemcp.yaml draft. Saving stores it on this Project. Gemcp does not write the source repository. An Agent can copy the same YAML into gemcp.yaml.', '请核对 gemcp.yaml 草稿。保存只会写入此 Project，不会改源码仓库。Agent 可以把同一段 YAML 复制进 gemcp.yaml。')">
    <form class="dialog-form" @submit.prevent="saveWorkload">
      <label>{{ t('Workload name', '工作负载名称') }}<input v-model="saveName" required maxlength="64" placeholder="oneshot" spellcheck="false" /></label>
      <p class="save-workload-note">{{ t('The whole argv becomes the entrypoint. Later prepares can use this name. A matching gemcp.yaml at the commit still wins.', '整段 argv 会成为 entrypoint。之后可以用这个名字准备实验。提交根目录若有同名 gemcp.yaml，仍以仓库为准。') }}</p>
      <pre class="workload-yaml">{{ saveYAML || t('Enter a valid name to preview the draft.', '输入合法名称后预览草稿。') }}</pre>
      <div v-if="saveError" class="form-error" role="alert">{{ saveError }}</div>
      <div class="save-workload-actions">
        <button class="secondary-button" type="button" :disabled="!saveYAML" @click="copyYAML"><Copy :size="15" />{{ copied ? t('Copied', '已复制') : t('Copy YAML', '复制 YAML') }}</button>
        <button class="primary-button" type="submit" :disabled="saveBusy || !saveYAML"><LoaderCircle v-if="saveBusy" :size="16" class="spinning" /><Save v-else :size="16" />{{ t('Save workload', '保存工作负载') }}</button>
      </div>
    </form>
  </WorkbenchDialog>
</template>

<style scoped>
.run-detail-backdrop { position: fixed; inset: 0; z-index: 100; padding: 24px; display: flex; justify-content: flex-end; background: rgba(25, 34, 29, .38); backdrop-filter: blur(2px); }
.run-detail { width: min(1120px, calc(100vw - 48px)); height: calc(100vh - 48px); min-width: 0; overflow: hidden; background: #f3f6f4; border: 1px solid #dce3de; box-shadow: 0 18px 52px rgba(24, 38, 31, .22); }
.detail-header { height: 68px; padding: 0 18px 0 20px; display: flex; align-items: center; justify-content: space-between; gap: 20px; background: #fff; border-bottom: 1px solid #dfe5e1; }
.detail-title, .detail-header-meta { min-width: 0; display: flex; align-items: center; gap: 11px; }
.detail-title > span:nth-child(2) small, .detail-title > span:nth-child(2) strong { display: block; }
.detail-title > span:nth-child(2) small { color: #89928d; font-size: 8px; line-height: 12px; }
.detail-title > span:nth-child(2) strong { color: #26342d; font: 600 14px/20px ui-monospace, monospace; }
.state-marker { width: 30px; height: 30px; display: grid; place-items: center; color: #5f6c65; background: #edf1ee; border-radius: 5px; }
.state-marker[data-state='running'], .state-marker[data-state='starting'] { color: #176b4d; background: #daf0e5; }
.state-marker[data-state='succeeded'] { color: #176b4d; background: #daf0e5; }
.state-marker[data-state='failed'], .state-marker[data-state='timed_out'] { color: #9c4038; background: #fbe3e0; }
.state-pill, .authority-chip { padding: 3px 7px; color: #59655f; background: #edf1ee; border-radius: 4px; font-size: 9px; font-weight: 700; text-transform: uppercase; }
.state-pill[data-state='running'], .state-pill[data-state='starting'] { color: #176b4d; background: #dff1e8; }
.state-pill[data-state='failed'], .state-pill[data-state='timed_out'] { color: #933e37; background: #fde5e2; }
.live-copy { display: flex; align-items: center; gap: 5px; color: #66736c; font-size: 9px; }
.live-copy > span { width: 6px; height: 6px; background: #20a36b; border-radius: 50%; box-shadow: 0 0 0 3px #dff2e9; }
.detail-header-meta { color: #7d8882; font-size: 9px; }
.detail-close { width: 32px; height: 32px; display: grid; place-items: center; color: #526058; background: transparent; border: 1px solid #dce2de; border-radius: 5px; cursor: pointer; }
.detail-scroll { height: calc(100% - 68px); padding: 16px; overflow: auto; }
.run-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); background: #fff; border: 1px solid #dfe5e1; }
.run-summary > div { min-width: 0; padding: 13px 15px; display: flex; align-items: flex-start; gap: 9px; }
.run-summary > div + div { border-left: 1px solid #e6ebe7; }
.run-summary svg { flex: 0 0 auto; margin-top: 2px; color: #40765f; }
.run-summary span, .run-summary small, .run-summary strong, .run-summary code { min-width: 0; display: block; }
.run-summary small { color: #88918c; font-size: 9px; line-height: 14px; }
.run-summary strong { overflow: hidden; color: #344139; font-size: 11px; line-height: 17px; text-overflow: ellipsis; white-space: nowrap; }
.run-summary code { overflow: hidden; color: #747f79; font-size: 8px; line-height: 14px; text-overflow: ellipsis; white-space: nowrap; }
.detail-section { min-width: 0; background: #fff; border: 1px solid #dfe5e1; }
.command-section, .live-output-section { margin-top: 12px; }
.section-title { min-height: 52px; padding: 9px 13px; display: flex; align-items: center; gap: 9px; background: #fafcfb; border-bottom: 1px solid #e7ebe8; }
.section-title > svg { flex: 0 0 auto; color: #3f745d; }
.section-title > span:nth-child(2) { min-width: 0; flex: 1; }
.section-title strong, .section-title small { display: block; }
.section-title strong { color: #344139; font-size: 11px; line-height: 16px; }
.section-title small { color: #818b85; font-size: 9px; line-height: 14px; }
.command-section > pre { margin: 0; padding: 14px; overflow: auto; color: #dcebe3; background: #1d2a23; font: 10px/17px ui-monospace, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.command-meta { padding: 8px 13px; display: flex; flex-wrap: wrap; gap: 20px; color: #77817b; border-top: 1px solid #e7ebe8; font-size: 9px; }
.command-meta strong, .command-meta code { color: #45534b; }
.save-workload-button { margin-left: auto; }
.save-workload-note { margin: 0; color: #66736c; font-size: 10px; line-height: 16px; }
.workload-yaml { margin: 0; padding: 12px; min-height: 140px; overflow: auto; color: #dcebe3; background: #1d2a23; font: 10px/16px ui-monospace, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.save-workload-actions { display: flex; justify-content: flex-end; gap: 8px; }
.detail-grid { margin-top: 12px; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; align-items: start; }
.fact-list { margin: 0; }
.fact-list > div { min-width: 0; padding: 8px 13px; display: grid; grid-template-columns: 136px minmax(0, 1fr); gap: 12px; border-top: 1px solid #edf0ee; }
.fact-list > div:first-child { border-top: 0; }
.fact-list dt { color: #7e8882; font-size: 9px; line-height: 15px; }
.fact-list dd { min-width: 0; margin: 0; color: #39463e; font-size: 10px; line-height: 15px; overflow-wrap: anywhere; }
.fact-list code { font-size: 9px; }
.comparison-row { min-height: 92px; padding: 13px; display: grid; grid-template-columns: minmax(0, 1fr) 20px minmax(0, 1fr); align-items: center; gap: 9px; }
.comparison-row small, .comparison-row strong, .comparison-row code { display: block; min-width: 0; }
.comparison-row small { color: #85908a; font-size: 9px; }
.comparison-row strong { margin-top: 3px; color: #344139; font-size: 10px; line-height: 15px; }
.comparison-row code { margin-top: 4px; color: #657169; font-size: 8px; overflow-wrap: anywhere; }
.comparison-arrow { color: #9aa39e; text-align: center; }
.gpu-list { border-top: 1px solid #e9edea; }
.gpu-list > div { padding: 7px 13px; display: grid; grid-template-columns: 50px minmax(120px, .8fr) minmax(0, 1fr); gap: 8px; font-size: 9px; }
.gpu-list span { color: #79847d; }.gpu-list strong { color: #3f4d45; }.gpu-list code { overflow-wrap: anywhere; color: #68756d; font-size: 8px; }
.section-empty { padding: 24px 13px; color: #818b85; font-size: 10px; text-align: center; }
.success-text { color: #19714f !important; font-weight: 700; }.danger-text { color: #9b4038 !important; }
.output-grid { display: grid; grid-template-columns: 1.6fr .8fr; }
.output-grid > div + div { border-left: 1px solid #e3e8e4; }
.output-grid > div > span { padding: 7px 11px; display: block; color: #7b867f; background: #f5f7f6; border-bottom: 1px solid #e3e8e4; font-size: 9px; }
.output-grid pre { height: 210px; margin: 0; padding: 12px; overflow: auto; color: #d9e9e0; background: #1e2a24; font: 9px/16px ui-monospace, monospace; white-space: pre-wrap; overflow-wrap: anywhere; }
.history-grid { grid-template-columns: 1.05fr .95fr; }
.attempt-list > div, .timeline > div { min-width: 0; padding: 8px 12px; display: grid; align-items: center; gap: 9px; border-top: 1px solid #edf0ee; }
.attempt-list > div:first-child, .timeline > div:first-child { border-top: 0; }
.attempt-list > div { grid-template-columns: 30px minmax(0, 1.1fr) minmax(80px, .6fr) 128px; }
.attempt-number { color: #44725e; font: 700 10px/16px ui-monospace, monospace; }
.attempt-list strong, .attempt-list small, .timeline strong, .timeline small { display: block; min-width: 0; }
.attempt-list strong, .timeline strong { color: #3d4a42; font-size: 10px; line-height: 15px; }
.attempt-list small, .timeline small { overflow: hidden; color: #7e8982; font-size: 8px; line-height: 13px; text-overflow: ellipsis; white-space: nowrap; }
.attempt-list time, .timeline time { color: #828d86; font-size: 8px; text-align: right; }
.timeline > div { grid-template-columns: 10px minmax(0, 1fr) 128px; }
.timeline-dot { width: 7px; height: 7px; background: #4a8069; border: 2px solid #dcece4; border-radius: 50%; }
.detail-error, .failure-banner { color: #933e37; background: #fff2f0; }
.detail-error { padding: 12px; font-size: 10px; }.failure-banner { margin-top: 12px; padding: 11px 13px; display: flex; align-items: flex-start; gap: 9px; border: 1px solid #f2cdc8; font-size: 10px; }.failure-banner strong { display: block; margin-bottom: 2px; }
@media (max-width: 900px) {
  .run-detail-backdrop { padding: 0; }.run-detail { width: 100vw; height: 100vh; border: 0; }
  .detail-header-meta > span, .live-copy { display: none; }.detail-scroll { padding: 10px; }
  .run-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); }.run-summary > div:nth-child(3) { border-left: 0; border-top: 1px solid #e6ebe7; }.run-summary > div:nth-child(4) { border-top: 1px solid #e6ebe7; }
  .detail-grid, .history-grid { grid-template-columns: 1fr; }.output-grid { grid-template-columns: 1fr; }.output-grid > div + div { border-left: 0; border-top: 1px solid #e3e8e4; }.output-grid pre { height: 180px; }
}
@media (max-width: 540px) {
  .detail-header { height: 58px; padding: 0 10px; }.detail-scroll { height: calc(100% - 58px); }.state-pill { padding: 3px 5px; font-size: 8px; }
  .run-summary { grid-template-columns: 1fr; }.run-summary > div + div { border-left: 0; border-top: 1px solid #e6ebe7; }
  .fact-list > div { grid-template-columns: 105px minmax(0, 1fr); }.command-meta { display: grid; gap: 4px; }
  .comparison-row { grid-template-columns: 1fr; }.comparison-arrow { transform: rotate(90deg); }.gpu-list > div { grid-template-columns: 44px minmax(0, 1fr); }.gpu-list code { grid-column: 2; }
  .attempt-list > div { grid-template-columns: 28px minmax(0, 1fr) 70px; }.attempt-list time { display: none; }.timeline > div { grid-template-columns: 10px minmax(0, 1fr); }.timeline time { grid-column: 2; text-align: left; }
}
</style>
