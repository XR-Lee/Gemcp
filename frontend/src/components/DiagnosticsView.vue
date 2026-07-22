<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, ChevronRight, CircleX, LoaderCircle, Play, RefreshCw, Square, Stethoscope } from '@lucide/vue'
import {
  APIError,
  api,
  type DiagnosticBackend,
  type DiagnosticInput,
  type DiagnosticOptions,
  type DiagnosticPreflight,
  type DiagnosticRun,
  type DiagnosticRunSummary,
  type DiagnosticSuite,
  type Project,
} from '../api'
import { localizedState, useI18n } from '../i18n'

const props = defineProps<{ active: boolean; project: Project | null }>()
const emit = defineEmits<{ unauthorized: [] }>()
const { languageTag, locale, t } = useI18n()

const options = ref<DiagnosticOptions | null>(null)
const runs = ref<DiagnosticRunSummary[]>([])
const selectedRun = ref<DiagnosticRun | null>(null)
const preflight = ref<DiagnosticPreflight | null>(null)
const loading = ref(false)
const preflighting = ref(false)
const submitting = ref(false)
const cancelling = ref(false)
const cancelConfirming = ref(false)
const confirmed = ref(false)
const error = ref('')
const idempotencyKey = ref('')
let pollTimer: number | undefined
let loadSequence = 0
let preflightSequence = 0
let selectionSequence = 0

const form = reactive<DiagnosticInput>({
  backend: 'autodl_private',
  suite: 'gpu_connectivity',
  repository_id: '',
  environment_id: '',
  resource_profile_id: '',
  commit_sha: '',
})

const environments = computed(() => options.value?.environments.filter((item) => item.backend === form.backend) ?? [])
const profiles = computed(() => options.value?.resource_profiles.filter((item) => item.backend === form.backend) ?? [])
const configurationIssue = computed(() => {
  if (!options.value) return ''
  if (!options.value.repositories.length) return t('Register and verify an active repository before running diagnostics.', '运行诊断前请注册并验证一个活跃仓库。')
  if (!environments.value.length) return t('No approved Environment is available for this backend.', '此后端没有可用的已批准 Environment。')
  if (!profiles.value.length) return t('No active Resource Profile is available for this backend.', '此后端没有可用的活跃 Resource Profile。')
  return ''
})
const canPreflight = computed(() => Boolean(
  props.project && form.repository_id && form.environment_id && form.resource_profile_id && /^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$/.test(form.commit_sha.trim()),
))
const hasActiveRuns = computed(() => runs.value.some((run) => !terminal(run.experiment.state)))

function terminal(state: string) {
  return ['succeeded', 'failed', 'cancelled', 'timed_out', 'budget_stopped', 'provider_error'].includes(state)
}

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

function syncSelections() {
  if (!options.value) return
  if (!options.value.repositories.some((item) => item.id === form.repository_id)) {
    form.repository_id = options.value.repositories[0]?.id ?? ''
  }
  if (!environments.value.some((item) => item.id === form.environment_id)) {
    form.environment_id = environments.value[0]?.id ?? ''
  }
  if (!profiles.value.some((item) => item.id === form.resource_profile_id)) {
    form.resource_profile_id = profiles.value[0]?.id ?? ''
  }
}

async function load(showSpinner = true) {
  if (!props.active || !props.project) return
  const projectID = props.project.id
  const sequence = ++loadSequence
  const selectedID = selectedRun.value?.id
  if (showSpinner) loading.value = true
  error.value = ''
  try {
    const [loadedOptions, loadedRuns, loadedSelection] = await Promise.all([
      api.diagnosticOptions(projectID),
      api.diagnostics(projectID),
      selectedID ? api.diagnostic(projectID, selectedID) : Promise.resolve(null),
    ])
    if (sequence !== loadSequence || props.project?.id !== projectID) return
    options.value = loadedOptions
    runs.value = loadedRuns.runs
    syncSelections()
    if (loadedSelection && selectedRun.value?.id === selectedID) selectedRun.value = loadedSelection
  } catch (caught) {
    if (sequence !== loadSequence || props.project?.id !== projectID) return
    handleError(caught, t('Could not load backend diagnostics.', '无法加载后端诊断。'))
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

async function runPreflight() {
  if (!props.project || !canPreflight.value) return
  const projectID = props.project.id
  const input = payload()
  const sequence = ++preflightSequence
  preflighting.value = true
  error.value = ''
  preflight.value = null
  confirmed.value = false
  idempotencyKey.value = ''
  try {
    const result = await api.diagnosticPreflight(projectID, input)
    if (sequence !== preflightSequence || props.project?.id !== projectID) return
    preflight.value = result
  } catch (caught) {
    if (sequence !== preflightSequence || props.project?.id !== projectID) return
    handleError(caught, t('Diagnostic preflight failed.', '诊断预检失败。'))
  } finally {
    if (sequence === preflightSequence) preflighting.value = false
  }
}

async function submit() {
  if (!props.project || !preflight.value?.eligible || !confirmed.value) return
  const projectID = props.project.id
  submitting.value = true
  error.value = ''
  if (!idempotencyKey.value) idempotencyKey.value = `diagnostic-${crypto.randomUUID()}`
  try {
    const result = await api.submitDiagnostic(projectID, {
      ...payload(), idempotency_key: idempotencyKey.value,
      confirmation_digest: preflight.value.confirmation_digest, confirmed: true,
    })
    if (props.project?.id !== projectID) return
    selectedRun.value = result.run
    preflight.value = null
    confirmed.value = false
    await load(false)
  } catch (caught) {
    if (caught instanceof APIError && caught.code === 'DIAGNOSTIC_PROPOSAL_CHANGED') {
      preflight.value = null
      confirmed.value = false
      idempotencyKey.value = ''
    }
    handleError(caught, t('Diagnostic submission failed.', '诊断提交失败。'))
  } finally {
    submitting.value = false
  }
}

async function selectRun(run: DiagnosticRunSummary) {
  if (!props.project) return
  const projectID = props.project.id
  const sequence = ++selectionSequence
  cancelConfirming.value = false
  selectedRun.value = null
  try {
    const result = await api.diagnostic(projectID, run.id)
    if (sequence !== selectionSequence || props.project?.id !== projectID) return
    selectedRun.value = result
  } catch (caught) {
    if (sequence !== selectionSequence || props.project?.id !== projectID) return
    handleError(caught, t('Could not load diagnostic details.', '无法加载诊断详情。'))
  }
}

async function cancelSelected() {
  if (!props.project || !selectedRun.value || terminal(selectedRun.value.experiment.state)) return
  const projectID = props.project.id
  const runID = selectedRun.value.id
  const sequence = ++selectionSequence
  cancelling.value = true
  error.value = ''
  try {
    const result = await api.cancelDiagnostic(projectID, runID)
    if (sequence !== selectionSequence || props.project?.id !== projectID) return
    selectedRun.value = result
    await load(false)
  } catch (caught) {
    if (sequence !== selectionSequence || props.project?.id !== projectID) return
    handleError(caught, t('Diagnostic cancellation failed.', '取消诊断失败。'))
  } finally {
    cancelling.value = false
    cancelConfirming.value = false
  }
}

function payload(): DiagnosticInput {
  return {
    backend: form.backend,
    suite: form.suite,
    repository_id: form.repository_id,
    environment_id: form.environment_id,
    resource_profile_id: form.resource_profile_id,
    commit_sha: form.commit_sha.trim().toLowerCase(),
  }
}

function money(milli: number) {
  return `CNY ${(milli / 1000).toFixed(3)}`
}

function dateTime(value?: string) {
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function suiteLabel(value: DiagnosticSuite) {
  return value === 'gpu_connectivity' ? t('GPU connectivity', 'GPU 连通性') : t('PyTorch CUDA compute', 'PyTorch CUDA 计算')
}

function backendLabel(value: DiagnosticBackend) {
  return value === 'autodl_private' ? 'AutoDL Private' : t('Self-hosted Node', '自托管 Node')
}

function assessmentLabel(value: DiagnosticRun['assessment']['status']) {
  if (value === 'passed') return t('Passed', '通过')
  if (value === 'failed') return t('Failed', '失败')
  return t('In progress', '进行中')
}

const diagnosticChinese: Record<string, string> = {
  'Runtime status is unavailable': '运行时状态不可用',
  'Runtime status query failed': '运行时状态查询失败',
  'Scheduler dispatch is disabled': 'Scheduler 调度已禁用',
  'Scheduler heartbeat is stale': 'Scheduler 心跳已过期',
  'Scheduler is healthy': 'Scheduler 运行正常',
  'Global concurrency could not be checked': '无法检查全局并发',
  'Global concurrency is currently full': '全局并发当前已满',
  'A global execution slot is available': '存在可用的全局执行槽位',
  'Public callback URL is not configured': '未配置公开回调 URL',
  'Public callback URL is configured': '公开回调 URL 已配置',
  'Watchdog heartbeat is stale': 'Watchdog 心跳已过期',
  'Watchdog is healthy': 'Watchdog 运行正常',
  'Self-hosted execution is disabled': '自托管执行已禁用',
  'Self-hosted execution is enabled': '自托管执行已启用',
  'Self-hosted diagnostics require exactly one GPU': '自托管诊断要求恰好一张 GPU',
  'Self-hosted Resource Profile requests one GPU': '自托管 Resource Profile 请求一张 GPU',
  'Project concurrency could not be checked': '无法检查 Project 并发',
  'Project queue could not be checked': '无法检查 Project 队列',
  'Project concurrency is currently full': 'Project 并发当前已满',
  'Project has an available concurrency slot': 'Project 存在可用并发槽位',
  'Repository archiver is unavailable': '仓库归档器不可用',
  'Commit archive could not be generated': '无法生成 Commit 归档',
  'Commit archive failed safety validation': 'Commit 归档未通过安全校验',
  'Temporary archive cleanup reported an error': '临时归档清理报告错误',
  'Commit archive is readable and safe': 'Commit 归档可读且安全',
  'Self-hosted diagnostic is unmetered': '自托管诊断不计费',
  'Diagnostic exceeds the Project experiment cap': '诊断超过 Project 单次实验额度',
  'Project billing timezone is invalid': 'Project 计费时区无效',
  'Budget ledger could not be read': '无法读取预算账本',
  'Budget ledger overflowed': '预算账本数值溢出',
  'Project budget cannot reserve this diagnostic': 'Project 预算无法预留本次诊断',
  'Project budget can reserve the diagnostic': 'Project 预算可预留本次诊断',
  'AutoDL Provider service is unavailable': 'AutoDL Provider 服务不可用',
  'AutoDL Provider query failed': 'AutoDL Provider 查询失败',
  'AutoDL Provider credential and Developer API are reachable': 'AutoDL Provider 凭据和 Developer API 可访问',
  'Selected AutoDL GPU capacity is unavailable': '所选 AutoDL GPU 容量不可用',
  'Selected AutoDL GPU capacity is available': '所选 AutoDL GPU 容量可用',
  'Selected AutoDL image is visible to the Provider API': 'Provider API 可发现所选 AutoDL 镜像',
  'Selected AutoDL image was not visible in image discovery': '镜像发现中未找到所选 AutoDL 镜像',
  'Self-hosted image is not digest-pinned': '自托管镜像未固定 digest',
  'Self-hosted image is digest-pinned': '自托管镜像已固定 digest',
  'Eligible Nodes could not be queried': '无法查询符合条件的 Node',
  'Node Assignment state could not be queried': '无法查询 Node Assignment 状态',
  'An online authorized Node is ready': '已就绪一台在线且获授权的 Node',
  'No online authorized Node matches the selected profile': '没有在线且获授权的 Node 匹配所选配置',
  'Diagnostic is queued for an execution slot.': '诊断正在等待执行槽位。',
  'Backend provisioning is in progress.': '后端正在配置资源。',
  'The built-in diagnostic suite is running on the GPU.': '内置诊断套件正在 GPU 上运行。',
  'Diagnostic execution finished and managed cleanup is in progress.': '诊断执行已结束，受管清理正在进行。',
  'Diagnostic execution is terminal, but managed backend cleanup is not complete.': '诊断执行已终止，但受管后端清理尚未完成。',
  'The end-to-end backend diagnostic passed.': '端到端后端诊断已通过。',
  'Workload exited successfully but did not return a passing diagnostic result.': 'Workload 成功退出，但未返回通过的诊断结果。',
  'The backend diagnostic failed.': '后端诊断失败。',
  'Do not retry until managed backend cleanup completes.': '在受管后端清理完成前不要重试。',
  'Inspect metrics.json and the latest Attempt log tail.': '检查 metrics.json 和最新 Attempt 日志尾部。',
  'Use an image that contains a CUDA-compatible PyTorch build.': '使用包含 CUDA 兼容 PyTorch build 的镜像。',
  'Run gpu_connectivity first to separate image framework errors from GPU injection errors.': '先运行 gpu_connectivity，以区分镜像框架错误与 GPU 注入错误。',
  'Verify the backend injected exactly the GPU count requested by the Resource Profile.': '验证后端注入的 GPU 数量与 Resource Profile 请求完全一致。',
  'Verify NVIDIA driver visibility and container GPU injection for the selected backend.': '验证所选后端的 NVIDIA Driver 可见性和容器 GPU 注入。',
  'For Self-hosted Nodes, check Docker and NVIDIA Container Toolkit before retrying.': '对于自托管 Node，请在重试前检查 Docker 和 NVIDIA Container Toolkit。',
  'Verify image CUDA compatibility with the selected GPU driver.': '验证镜像 CUDA 与所选 GPU Driver 的兼容性。',
  'Inspect nvidia-smi output and the latest Attempt log tail.': '检查 nvidia-smi 输出和最新 Attempt 日志尾部。',
  'Inspect source archive and transfer checks in the preflight and timeline.': '检查预检和时间线中的源码归档与传输检查。',
  'Check the public Runner route for interrupted response bodies.': '检查公开 Runner 路由是否出现响应体中断。',
  'Check the public Runner event route, Cloudflare edge logs, and callback reachability.': '检查公开 Runner event 路由、Cloudflare edge 日志和回调可达性。',
  'Check gemcp-node service health, outbound HTTPS, Docker, and NVIDIA Container Toolkit on the selected Node.': '检查所选 Node 的 gemcp-node 服务、出站 HTTPS、Docker 和 NVIDIA Container Toolkit。',
  'Use the Runner stage timeline to identify the last completed bootstrap phase.': '使用 Runner 阶段时间线定位最后完成的 Bootstrap 阶段。',
  'Inspect gemcp-launch.log when shared storage is available.': '共享存储可用时检查 gemcp-launch.log。',
  'Inspect the latest Attempt log tail, backend observation, and timeline before retrying.': '重试前检查最新 Attempt 日志尾部、后端观测和时间线。',
  'No Attempt was created; inspect scheduler, budget, and backend availability checks.': '未创建 Attempt；检查 Scheduler、预算和后端可用性检查。',
}

const classificationChinese: Record<string, string> = {
  waiting_for_scheduler: '等待 Scheduler', backend_provisioning: '后端配置中', runner_bootstrap: 'Runner 启动中',
  suite_running: '诊断套件运行中', cleanup: '清理中', cleanup_pending: '等待清理', all_checks_passed: '全部检查通过',
  invalid_diagnostic_result: '诊断结果无效', diagnostic_failed: '诊断失败', command_failed: '命令失败',
  runner_bootstrap_failed: 'Runner Bootstrap 失败', provision_timeout: '配置超时', node_lost: 'Node 失联',
  node_unavailable: 'Node 不可用', cancelled: '已取消', timed_out: '已超时', provider_error: 'Provider 错误',
}

function diagnosticText(value: string) {
  return locale.value === 'zh' ? (diagnosticChinese[value] ?? value) : value
}

function classificationText(value: string) {
  return locale.value === 'zh' ? (classificationChinese[value] ?? value) : value
}

function resetProposal() {
  preflightSequence += 1
  preflighting.value = false
  preflight.value = null
  confirmed.value = false
  idempotencyKey.value = ''
  if (options.value) syncSelections()
}

watch(() => form.backend, resetProposal)
watch(() => [form.suite, form.repository_id, form.environment_id, form.resource_profile_id, form.commit_sha], () => {
  preflightSequence += 1
  preflighting.value = false
  preflight.value = null
  confirmed.value = false
  idempotencyKey.value = ''
})
watch(() => props.project?.id, () => {
  loadSequence += 1
  preflightSequence += 1
  selectionSequence += 1
  loading.value = false
  preflighting.value = false
  submitting.value = false
  cancelling.value = false
  cancelConfirming.value = false
  options.value = null
  runs.value = []
  selectedRun.value = null
  preflight.value = null
  confirmed.value = false
  idempotencyKey.value = ''
  form.repository_id = ''
  form.environment_id = ''
  form.resource_profile_id = ''
})
watch(() => [props.active, props.project?.id], ([active]) => {
  window.clearInterval(pollTimer)
  pollTimer = undefined
  if (active) {
    void load()
    pollTimer = window.setInterval(() => {
      if (hasActiveRuns.value) void load(false)
    }, 5000)
  } else {
    loadSequence += 1
    preflightSequence += 1
    selectionSequence += 1
  }
}, { immediate: true })
onUnmounted(() => {
  loadSequence += 1
  preflightSequence += 1
  selectionSequence += 1
  window.clearInterval(pollTimer)
})
</script>

<template>
  <section class="diagnostic-workspace page-workspace">
    <div class="section-heading page-section-heading">
      <div><h2>{{ t('Backend diagnostics', '后端诊断') }}</h2><p>{{ project?.name ?? 'Project' }}</p></div>
      <button class="icon-button" type="button" :title="t('Refresh diagnostics', '刷新诊断')" :disabled="loading" @click="load()"><RefreshCw :size="17" :class="{ spinning: loading }" /></button>
    </div>

    <div v-if="error" class="page-alert" role="alert">{{ error }}</div>

    <section class="diagnostic-config" aria-labelledby="diagnostic-config-title">
      <div class="subsection-heading"><div><h3 id="diagnostic-config-title">{{ t('New diagnostic', '新建诊断') }}</h3><p>{{ t('Immutable backend test proposal', '不可变后端测试提案') }}</p></div></div>
      <div v-if="configurationIssue" class="configuration-warning"><AlertTriangle :size="17" /><span>{{ configurationIssue }}</span></div>
      <form class="diagnostic-form" @submit.prevent="runPreflight">
        <fieldset>
          <legend>{{ t('Backend', '后端') }}</legend>
          <div class="segmented-control">
            <button type="button" :class="{ active: form.backend === 'autodl_private' }" @click="form.backend = 'autodl_private'">AutoDL Private</button>
            <button type="button" :class="{ active: form.backend === 'self_hosted' }" @click="form.backend = 'self_hosted'">{{ t('Self-hosted', '自托管') }}</button>
          </div>
        </fieldset>
        <label>{{ t('Suite', '测试套件') }}
          <select v-model="form.suite"><option v-for="suite in options?.suites ?? []" :key="suite.id" :value="suite.id">{{ suiteLabel(suite.id) }} · {{ suite.runtime_seconds }}s</option></select>
        </label>
        <label>{{ t('Repository', '仓库') }}
          <select v-model="form.repository_id"><option v-for="repository in options?.repositories ?? []" :key="repository.id" :value="repository.id">{{ repository.name }} · {{ repository.default_branch }}</option></select>
        </label>
        <label>{{ t('Environment', '环境') }}
          <select v-model="form.environment_id"><option v-for="environment in environments" :key="environment.id" :value="environment.id">{{ environment.name }}</option></select>
        </label>
        <label>{{ t('Resource profile', '资源配置') }}
          <select v-model="form.resource_profile_id"><option v-for="profile in profiles" :key="profile.id" :value="profile.id">{{ profile.name }} · {{ profile.gpu_names.join(', ') }}</option></select>
        </label>
        <label class="commit-field">Commit SHA
          <input v-model="form.commit_sha" required minlength="40" maxlength="64" spellcheck="false" autocomplete="off" placeholder="40 or 64 hexadecimal characters" />
        </label>
        <button class="primary-button diagnostic-action" type="submit" :disabled="!canPreflight || preflighting"><LoaderCircle v-if="preflighting" :size="16" class="spinning" /><Stethoscope v-else :size="16" />{{ t('Run preflight', '运行预检') }}</button>
      </form>
    </section>

    <section v-if="preflight" class="preflight-results" aria-live="polite">
      <div class="preflight-summary" :data-eligible="preflight.eligible">
        <span><CheckCircle2 v-if="preflight.eligible" :size="19" /><CircleX v-else :size="19" /></span>
        <div><strong>{{ preflight.eligible ? (preflight.checks.some((check) => check.status === 'warn') ? t('Preflight passed with warnings', '预检通过，但有警告') : t('Preflight passed', '预检通过')) : t('Preflight blocked', '预检未通过') }}</strong><small>{{ backendLabel(preflight.proposal.backend) }} · {{ suiteLabel(preflight.proposal.suite) }}</small></div>
      </div>
      <div class="diagnostic-checks">
        <div v-for="check in preflight.checks" :key="check.id" class="diagnostic-check" :data-status="check.status">
          <CheckCircle2 v-if="check.status === 'pass'" :size="17" /><AlertTriangle v-else-if="check.status === 'warn'" :size="17" /><CircleX v-else :size="17" />
          <span><strong>{{ diagnosticText(check.summary) }}</strong><small v-if="check.detail">{{ diagnosticText(check.detail) }}</small></span>
        </div>
      </div>
      <dl class="proposal-grid">
        <div><dt>{{ t('Worst-case reservation', '最坏情况预留') }}</dt><dd>{{ money(preflight.proposal.reserved_cost_milli) }}</dd></div>
        <div><dt>{{ t('Runtime limit', '运行时限') }}</dt><dd>{{ preflight.proposal.runtime_seconds }}s</dd></div>
        <div><dt>GPU</dt><dd>{{ preflight.proposal.gpu_num }} × {{ preflight.proposal.gpu_models.join(', ') }}</dd></div>
        <div><dt>{{ t('Image', '镜像') }}</dt><dd><code>{{ preflight.proposal.image_uuid }}</code></dd></div>
        <div><dt>CPU</dt><dd>{{ preflight.proposal.cpu_from }} - {{ preflight.proposal.cpu_to }}</dd></div>
        <div><dt>{{ t('Memory', '内存') }}</dt><dd>{{ preflight.proposal.memory_from_gb }} - {{ preflight.proposal.memory_to_gb }} GiB</dd></div>
        <div><dt>{{ t('Hourly ceiling', '每小时价格上限') }}</dt><dd>{{ money(preflight.proposal.price_to_milli) }} / GPU</dd></div>
        <div><dt>{{ t('Region / CUDA', '区域 / CUDA') }}</dt><dd>{{ preflight.proposal.region }} · {{ preflight.proposal.cuda_from }} - {{ preflight.proposal.cuda_to }}</dd></div>
        <div class="proposal-digest"><dt>{{ t('Proposal digest', '提案摘要') }}</dt><dd><code>{{ preflight.confirmation_digest }}</code></dd></div>
      </dl>
      <details class="diagnostic-command"><summary>{{ t('Immutable command', '不可变命令') }}</summary><pre>{{ preflight.proposal.command }}</pre></details>
      <label class="confirmation-row"><input v-model="confirmed" type="checkbox" :disabled="!preflight.eligible" /><span>{{ preflight.proposal.billable ? t('I approve this paid AutoDL diagnostic and its worst-case reservation.', '我批准此付费 AutoDL 诊断及其最坏情况预留。') : t('I approve dispatching this diagnostic to the selected Self-hosted backend.', '我批准将此诊断调度到所选自托管后端。') }}</span></label>
      <button class="primary-button" type="button" :disabled="!preflight.eligible || !confirmed || submitting" @click="submit"><LoaderCircle v-if="submitting" :size="16" class="spinning" /><Play v-else :size="16" />{{ t('Start diagnostic', '启动诊断') }}</button>
    </section>

    <section class="diagnostic-history">
      <div class="subsection-heading"><div><h3>{{ t('Diagnostic runs', '诊断记录') }}</h3><p>{{ runs.length }} {{ t('runs', '次运行') }}</p></div></div>
      <div v-if="runs.length" class="table-scroll">
        <table class="data-table">
          <thead><tr><th>{{ t('Result', '结果') }}</th><th>{{ t('Backend', '后端') }}</th><th>{{ t('Suite', '套件') }}</th><th>Experiment</th><th>{{ t('Created', '创建时间') }}</th><th /></tr></thead>
          <tbody><tr v-for="run in runs" :key="run.id" tabindex="0" @click="selectRun(run)" @keydown.enter="selectRun(run)"><td><span class="diagnostic-result" :data-result="run.assessment.status">{{ assessmentLabel(run.assessment.status) }}</span></td><td>{{ backendLabel(run.backend) }}</td><td>{{ suiteLabel(run.suite) }}</td><td><code>{{ run.experiment.id.slice(0, 8) }}</code></td><td>{{ dateTime(run.created_at) }}</td><td class="row-arrow"><ChevronRight :size="15" /></td></tr></tbody>
        </table>
      </div>
      <div v-else-if="!loading" class="empty-state compact-empty"><span class="empty-icon"><Stethoscope :size="21" /></span><h3>{{ t('No diagnostics', '暂无诊断') }}</h3></div>
    </section>

    <section v-if="selectedRun" class="diagnostic-detail">
      <div class="subsection-heading"><div><h3>{{ t('Run analysis', '运行分析') }}</h3><p><code>{{ selectedRun.id }}</code></p></div><div class="detail-actions"><span class="diagnostic-result" :data-result="selectedRun.assessment.status">{{ assessmentLabel(selectedRun.assessment.status) }}</span><button v-if="!terminal(selectedRun.experiment.state)" class="icon-button" type="button" :title="t('Cancel diagnostic', '取消诊断')" :disabled="cancelling" @click="cancelConfirming = true"><LoaderCircle v-if="cancelling" :size="16" class="spinning" /><Square v-else :size="15" /></button></div></div>
      <div class="assessment-band">
        <strong>{{ classificationText(selectedRun.assessment.classification) }}</strong><span>{{ diagnosticText(selectedRun.assessment.summary) }}</span><small>{{ t('Cleanup', '清理') }}: {{ selectedRun.assessment.cleanup_complete ? t('complete', '已完成') : t('pending', '等待中') }}</small>
      </div>
      <ul v-if="selectedRun.assessment.recommendations?.length" class="recommendation-list"><li v-for="item in selectedRun.assessment.recommendations" :key="item">{{ diagnosticText(item) }}</li></ul>
      <dl class="proposal-grid detail-observation">
        <div><dt>{{ t('Experiment state', 'Experiment 状态') }}</dt><dd>{{ localizedState(selectedRun.experiment.state) }}</dd></div>
        <div><dt>{{ t('Runner stage', 'Runner 阶段') }}</dt><dd><code>{{ selectedRun.experiment.runner_stage ?? '—' }}</code><template v-if="selectedRun.experiment.runner_error_type"> · {{ selectedRun.experiment.runner_error_type }}</template></dd></div>
        <div><dt>{{ t('Source downloads', '源码下载次数') }}</dt><dd>{{ selectedRun.experiment.runner_source_downloads ?? 0 }}</dd></div>
        <div><dt>{{ t('Backend state', '后端状态') }}</dt><dd><code>{{ selectedRun.backend_observation?.state ?? '—' }}</code></dd></div>
        <div><dt>{{ t('Estimated cost', '估算成本') }}</dt><dd>{{ money(selectedRun.experiment.estimated_cost_milli) }}</dd></div>
        <div v-if="selectedRun.backend_observation?.stop_reason"><dt>{{ t('Stop reason', '停止原因') }}</dt><dd><code>{{ selectedRun.backend_observation.stop_reason }}</code></dd></div>
        <div class="wide-observation"><dt>{{ t('Output reference', '输出引用') }}</dt><dd><code>{{ selectedRun.experiment.output_path }}</code></dd></div>
        <div v-if="selectedRun.experiment.failure_reason" class="wide-observation"><dt>{{ t('Failure', '失败原因') }}</dt><dd>{{ selectedRun.experiment.failure_code }} · {{ selectedRun.experiment.failure_reason }}</dd></div>
        <div v-if="selectedRun.backend_observation?.last_error" class="wide-observation"><dt>{{ t('Backend error', '后端错误') }}</dt><dd>{{ selectedRun.backend_observation.last_error }}</dd></div>
      </dl>
      <div v-if="selectedRun.attempts.length" class="table-scroll"><table class="data-table"><thead><tr><th>Attempt</th><th>{{ t('State', '状态') }}</th><th>{{ t('Downloads', '下载次数') }}</th><th>{{ t('Exit', '退出码') }}</th><th>{{ t('Failure', '失败') }}</th></tr></thead><tbody><tr v-for="attempt in selectedRun.attempts" :key="attempt.id"><td>#{{ attempt.number }}</td><td>{{ localizedState(attempt.state) }}</td><td>{{ attempt.source_downloads }}</td><td>{{ attempt.exit_code ?? '—' }}</td><td>{{ attempt.failure_code ?? '—' }}</td></tr></tbody></table></div>
      <div v-if="selectedRun.attempts.at(-1)?.metrics && Object.keys(selectedRun.attempts.at(-1)?.metrics ?? {}).length" class="attempt-output"><span>{{ t('Diagnostic metrics', '诊断指标') }}</span><pre>{{ JSON.stringify(selectedRun.attempts.at(-1)?.metrics, null, 2) }}</pre></div>
      <div v-if="selectedRun.attempts.at(-1)?.log_tail" class="attempt-output"><span>{{ t('Log tail', '日志尾部') }}</span><pre>{{ selectedRun.attempts.at(-1)?.log_tail }}</pre></div>
      <div class="timeline"><div v-for="event in selectedRun.timeline" :key="`${event.at}-${event.code}-${event.detail}`"><time>{{ dateTime(event.at) }}</time><span><strong>{{ event.code }}</strong><small v-if="event.detail">{{ event.detail }}</small></span></div></div>
    </section>

    <div v-if="cancelConfirming" class="modal-backdrop" @click.self="cancelConfirming = false">
      <section class="modal diagnostic-cancel-dialog" role="alertdialog" aria-modal="true" :aria-label="t('Cancel diagnostic', '取消诊断')">
        <header><div><p class="eyebrow">Diagnostic</p><h2>{{ t('Cancel diagnostic', '取消诊断') }}</h2></div></header>
        <div class="dialog-body"><p>{{ t('Gemcp will request managed backend cleanup and release the remaining reservation after settlement.', 'Gemcp 将请求受管后端清理，并在结算后释放剩余预留。') }}</p><code>{{ selectedRun?.id }}</code></div>
        <div class="dialog-actions"><button class="secondary-button" type="button" @click="cancelConfirming = false">{{ t('Keep running', '继续运行') }}</button><button class="danger-button" type="button" :disabled="cancelling" @click="cancelSelected"><LoaderCircle v-if="cancelling" :size="16" class="spinning" /><Square v-else :size="15" />{{ t('Cancel diagnostic', '取消诊断') }}</button></div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.diagnostic-workspace { min-width: 0; display: grid; gap: 24px; overflow: hidden; }
.diagnostic-workspace > *, .diagnostic-workspace section, .diagnostic-workspace form, .diagnostic-workspace label { min-width: 0; }
.diagnostic-config, .preflight-results, .diagnostic-history, .diagnostic-detail { border-top: 1px solid #dbe1dc; padding-top: 20px; }
.diagnostic-form { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px 18px; align-items: end; }
.diagnostic-form fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
.diagnostic-form legend, .diagnostic-form label { display: grid; gap: 7px; color: #68716b; font-size: 0.78rem; font-weight: 700; }
.diagnostic-form select, .diagnostic-form input { width: 100%; }
.configuration-warning { display: flex; align-items: flex-start; gap: 8px; margin-bottom: 14px; color: #7a5100; font-size: 0.8rem; }
.configuration-warning svg { flex: 0 0 auto; }
.commit-field { grid-column: 1 / -1; }
.diagnostic-action { justify-self: start; }
.preflight-results { display: grid; gap: 16px; }
.preflight-summary { display: flex; align-items: center; gap: 11px; color: #b42318; }
.preflight-summary[data-eligible="true"] { color: #16745b; }
.preflight-summary > div { display: grid; gap: 2px; color: #202421; }
.preflight-summary small { color: #68716b; }
.diagnostic-checks { border-block: 1px solid #dbe1dc; }
.diagnostic-check { display: grid; grid-template-columns: 22px minmax(0, 1fr); gap: 9px; padding: 11px 0; border-bottom: 1px solid #dbe1dc; color: #16745b; }
.diagnostic-check:last-child { border-bottom: 0; }
.diagnostic-check[data-status="warn"] { color: #946200; }
.diagnostic-check[data-status="fail"] { color: #b42318; }
.diagnostic-check span { display: grid; gap: 3px; color: #202421; }
.diagnostic-check small { color: #68716b; font-weight: 400; overflow-wrap: anywhere; }
.proposal-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); gap: 1px; background: #dbe1dc; border: 1px solid #dbe1dc; }
.proposal-grid > div { min-width: 0; background: #fff; padding: 12px; display: grid; gap: 5px; }
.proposal-grid dt { color: #68716b; font-size: 0.72rem; font-weight: 700; }
.proposal-grid dd { margin: 0; overflow-wrap: anywhere; }
.proposal-grid .proposal-digest { grid-column: 1 / -1; }
.diagnostic-command summary { cursor: pointer; color: #68716b; font-weight: 700; }
.diagnostic-command pre { max-width: 100%; max-height: 220px; overflow: auto; }
.confirmation-row { display: flex; flex-direction: row; align-items: flex-start; gap: 9px; font-size: 0.84rem; line-height: 1.45; }
.confirmation-row input { width: 17px; height: 17px; min-height: 17px; flex: 0 0 17px; margin-top: 3px; padding: 0; accent-color: #16745b; }
.diagnostic-result { display: inline-flex; align-items: center; min-height: 24px; padding: 0 8px; border-radius: 4px; background: #fff4cc; color: #7a5100; font-size: 0.72rem; font-weight: 800; }
.diagnostic-result[data-result="passed"] { background: #e4f5ed; color: #11624c; }
.diagnostic-result[data-result="failed"] { background: #feeceb; color: #a51d14; }
.assessment-band { display: grid; gap: 5px; border-left: 3px solid #16745b; padding: 3px 0 3px 13px; }
.assessment-band small { color: #68716b; }
.recommendation-list { margin: 0; padding-left: 20px; color: #68716b; line-height: 1.55; }
.detail-observation { margin-top: 16px; }
.detail-observation .wide-observation { grid-column: span 2; }
.detail-actions { display: flex; align-items: center; gap: 8px; }
.diagnostic-detail .subsection-heading code { overflow-wrap: anywhere; }
.timeline { display: grid; border-top: 1px solid #dbe1dc; margin-top: 16px; }
.timeline > div { display: grid; grid-template-columns: 180px minmax(0, 1fr); gap: 14px; padding: 10px 0; border-bottom: 1px solid #dbe1dc; }
.timeline time { color: #68716b; font-size: 0.76rem; }
.timeline span { display: grid; gap: 2px; }
.timeline small { color: #68716b; overflow-wrap: anywhere; }
.diagnostic-cancel-dialog { width: min(480px, 100%); }
.diagnostic-cancel-dialog .dialog-body { padding: 20px; display: grid; gap: 10px; }
.diagnostic-cancel-dialog p { margin: 0; color: #68716b; line-height: 1.5; }
.diagnostic-cancel-dialog code { overflow-wrap: anywhere; }
.diagnostic-cancel-dialog .dialog-actions { padding: 14px 20px; display: flex; justify-content: flex-end; gap: 9px; border-top: 1px solid #e1e6e2; }
@media (max-width: 760px) {
  .diagnostic-form { grid-template-columns: 1fr; }
  .commit-field { grid-column: auto; }
  .proposal-grid { grid-template-columns: 1fr 1fr; }
  .timeline > div { grid-template-columns: 1fr; gap: 3px; }
}
@media (max-width: 430px) { .proposal-grid { grid-template-columns: 1fr; } .proposal-grid .proposal-digest, .detail-observation .wide-observation { grid-column: auto; } }
</style>
