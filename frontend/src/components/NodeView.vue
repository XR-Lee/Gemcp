<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import { Check, Clipboard, Cpu, HardDrive, LoaderCircle, Plus, RefreshCw, Server, ShieldCheck, Trash2, X } from '@lucide/vue'
import {
  APIError, api, type NodeEnrollment, type NodeEnrollmentIssue, type NodeList, type Project,
  type SelfHostedNode, type SelfHostedRuntimeList,
} from '../api'
import { localizedState, useI18n } from '../i18n'

const props = defineProps<{ active: boolean; projects: Project[] }>()
const emit = defineEmits<{ unauthorized: [] }>()
const data = ref<NodeList>({ nodes: [], enrollments: [], assignments: [] })
const loading = ref(false)
const initialized = ref(false)
const error = ref('')
const createDialog = ref(false)
const creating = ref(false)
const createError = ref('')
const reveal = ref<NodeEnrollmentIssue | null>(null)
const approveTarget = ref<NodeEnrollment | null>(null)
const approveError = ref('')
const approving = ref(false)
const revokeTarget = ref<NodeEnrollment | null>(null)
const revoking = ref(false)
const copied = ref(false)
const { languageTag, locale, t } = useI18n()
const setupLanguage = ref<'zh' | 'en'>(locale.value)
const runtimes = ref<SelfHostedRuntimeList>({ environments: [], resource_profiles: [] })
const runtimeProjectID = ref(props.projects[0]?.id ?? '')
const runtimeLoading = ref(false)
const runtimeDialog = ref(false)
const runtimeBusy = ref(false)
const runtimeError = ref('')
const createForm = reactive({ label: '', expires: '30' })
const approveForm = reactive({ pairingCode: '', projects: {} as Record<string, boolean> })
const runtimeForm = reactive({ projectID: '', name: '', image: '', gpuNames: '', cpuLimit: 8, memoryGB: 32, makeDefault: false })
let timer: number | undefined

const activeNodes = computed(() => data.value.nodes.filter((node) => node.status === 'active').length)
const onlineNodes = computed(() => data.value.nodes.filter((node) => node.observed_state === 'online').length)
const totalGPUs = computed(() => data.value.nodes.reduce((sum, node) => sum + (node.capabilities.gpus?.length ?? 0), 0))
const activeAssignments = computed(() => data.value.assignments.filter((item) => ['starting', 'running', 'stopping', 'collecting'].includes(item.state)).length)
const runtimeRows = computed(() => runtimes.value.resource_profiles.map((profile) => ({
  profile, environment: runtimes.value.environments.find((item) => item.name === profile.name),
})))
const reportedGPUNames = computed(() => [...new Set(data.value.nodes.flatMap((node) => node.capabilities.gpus?.map((gpu) => gpu.name) ?? []))].sort())
const localizedSetupURL = computed(() => {
  if (!reveal.value) return ''
  const setupURL = new URL(reveal.value.setup_url)
  setupURL.searchParams.set('lang', setupLanguage.value)
  return setupURL.toString()
})

function apiMessage(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return ''
  }
  return caught instanceof APIError ? caught.message : fallback
}

async function load() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const result = await api.nodes()
    data.value = { ...result, nodes: result.nodes ?? [], enrollments: result.enrollments ?? [], assignments: result.assignments ?? [] }
    initialized.value = true
  } catch (caught) {
    error.value = apiMessage(caught, t('Could not load Self-hosted nodes.', '无法加载自托管节点。'))
  } finally {
    loading.value = false
  }
}

async function loadRuntimes() {
  if (!runtimeProjectID.value || runtimeLoading.value) return
  runtimeLoading.value = true
  runtimeError.value = ''
  try {
    const result = await api.selfHostedRuntimes(runtimeProjectID.value)
    runtimes.value = { environments: result.environments ?? [], resource_profiles: result.resource_profiles ?? [] }
  } catch (caught) {
    runtimeError.value = apiMessage(caught, t('Could not load Self-hosted runtime configuration.', '无法加载自托管运行时配置。'))
  } finally {
    runtimeLoading.value = false
  }
}

function openRuntime() {
  Object.assign(runtimeForm, {
    projectID: runtimeProjectID.value || props.projects[0]?.id || '', name: '', image: '',
    gpuNames: reportedGPUNames.value.join(', '), cpuLimit: 8, memoryGB: 32, makeDefault: false,
  })
  runtimeError.value = ''
  runtimeDialog.value = true
}

async function createRuntime() {
  runtimeBusy.value = true
  runtimeError.value = ''
  try {
    await api.createSelfHostedRuntime(runtimeForm.projectID, {
      name: runtimeForm.name.trim(), image: runtimeForm.image.trim(),
      gpu_names: runtimeForm.gpuNames.split(',').map((item) => item.trim()).filter(Boolean),
      cpu_limit: Number(runtimeForm.cpuLimit), memory_gb: Number(runtimeForm.memoryGB), make_default: runtimeForm.makeDefault,
    })
    runtimeProjectID.value = runtimeForm.projectID
    runtimeDialog.value = false
    await loadRuntimes()
  } catch (caught) {
    runtimeError.value = apiMessage(caught, t('Could not create Self-hosted runtime configuration.', '无法创建自托管运行时配置。'))
  } finally {
    runtimeBusy.value = false
  }
}

function schedule() {
  if (timer !== undefined) window.clearTimeout(timer)
  timer = undefined
  if (!props.active) return
  timer = window.setTimeout(async () => {
    if (props.active && document.visibilityState === 'visible') await load()
    schedule()
  }, 15_000)
}

function openCreate() {
  Object.assign(createForm, { label: '', expires: '30' })
  createError.value = ''
  createDialog.value = true
}

async function createEnrollment() {
  creating.value = true
  createError.value = ''
  try {
    const result = await api.issueNodeEnrollment({ label: createForm.label.trim(), setup_expires_in_minutes: Number(createForm.expires) })
    data.value.enrollments = [result.enrollment, ...data.value.enrollments.filter((item) => item.id !== result.enrollment.id)]
    createDialog.value = false
    reveal.value = result
  } catch (caught) {
    createError.value = apiMessage(caught, t('Could not create node enrollment.', '无法创建节点注册。'))
  } finally {
    creating.value = false
  }
}

async function copySetupURL() {
  if (!reveal.value) return
  try {
    await navigator.clipboard.writeText(localizedSetupURL.value)
    copied.value = true
    window.setTimeout(() => (copied.value = false), 1600)
  } catch {
    error.value = t('Clipboard access was denied.', '剪贴板访问被拒绝。')
  }
}

function openApprove(enrollment: NodeEnrollment) {
  approveTarget.value = enrollment
  approveForm.pairingCode = ''
  approveForm.projects = {}
  approveError.value = ''
  if (props.projects.length === 1) approveForm.projects[props.projects[0].id] = true
}

async function approveEnrollment() {
  if (!approveTarget.value) return
  const projectIDs = props.projects.filter((project) => approveForm.projects[project.id]).map((project) => project.id)
  if (!projectIDs.length) {
    approveError.value = t('Select at least one Project.', '请至少选择一个 Project。')
    return
  }
  approving.value = true
  approveError.value = ''
  try {
    await api.approveNodeEnrollment(approveTarget.value.id, {
      pairing_code: approveForm.pairingCode.trim().toUpperCase(), project_ids: projectIDs,
    })
    approveTarget.value = null
    await load()
  } catch (caught) {
    approveError.value = apiMessage(caught, t('Could not approve node enrollment.', '无法批准节点注册。'))
  } finally {
    approving.value = false
  }
}

async function revokeEnrollment() {
  if (!revokeTarget.value) return
  revoking.value = true
  try {
    await api.revokeNodeEnrollment(revokeTarget.value.id)
    revokeTarget.value = null
    await load()
  } catch (caught) {
    error.value = apiMessage(caught, t('Could not revoke node enrollment.', '无法撤销节点注册。'))
  } finally {
    revoking.value = false
  }
}

function formatDate(value?: string) {
  if (!value) return t('Never', '从未')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function formatBytes(value?: number) {
  if (!value) return '0 GB'
  return `${(value / 1024 ** 3).toFixed(value >= 100 * 1024 ** 3 ? 0 : 1)} GB`
}

function gpuLabel(node: SelfHostedNode) {
  const gpus = node.capabilities.gpus ?? []
  if (!gpus.length) return t('Not reported', '未上报')
  return gpus.map((gpu) => `${gpu.name} · ${formatBytes(gpu.memory_bytes)}`).join(', ')
}

function projectLabel(node: SelfHostedNode) {
  const names = node.project_ids.map((id) => props.projects.find((project) => project.id === id)?.name ?? id)
  return names.length ? names.join(', ') : t('No access', '无访问权限')
}

watch(() => props.active, (active) => {
  if (active && !initialized.value) load()
  schedule()
}, { immediate: true })
watch(() => props.projects, (projects) => {
  if (!projects.some((project) => project.id === runtimeProjectID.value)) runtimeProjectID.value = projects[0]?.id ?? ''
}, { immediate: true })
watch(runtimeProjectID, () => {
  if (props.active) loadRuntimes()
}, { immediate: true })
watch(locale, (value) => (setupLanguage.value = value))
onMounted(schedule)
onUnmounted(() => timer !== undefined && window.clearTimeout(timer))
</script>

<template>
  <section class="node-page">
    <header class="node-heading">
      <div>
        <p class="eyebrow">{{ t('Compute fleet', '算力集群') }}</p>
        <h1>{{ t('Self-hosted nodes', '自托管节点') }}</h1>
        <p class="page-subtitle">{{ t('Enroll, authorize, and monitor your private GPU machines.', '注册、授权并监控您的私有 GPU 主机。') }}</p>
      </div>
      <div class="heading-actions">
        <button class="secondary-button icon-command" type="button" :disabled="loading" @click="load">
          <RefreshCw :size="16" :class="{ spinning: loading }" /> {{ t('Refresh', '刷新') }}
        </button>
        <button class="primary-button icon-command" type="button" @click="openCreate">
          <Plus :size="16" /> {{ t('Create enrollment', '创建注册') }}
        </button>
      </div>
    </header>

    <div class="node-metrics" :aria-label="t('Node summary', '节点摘要')">
      <div class="node-metric"><Server :size="18" /><span>{{ t('Active nodes', '活跃节点') }}</span><strong>{{ activeNodes }}</strong></div>
      <div class="node-metric"><ShieldCheck :size="18" /><span>{{ t('Online now', '当前在线') }}</span><strong>{{ onlineNodes }}</strong></div>
      <div class="node-metric"><Cpu :size="18" /><span>{{ t('GPU slots', 'GPU 插槽') }}</span><strong>{{ totalGPUs }}</strong></div>
      <div class="node-metric"><HardDrive :size="18" /><span>{{ t('Active workloads', '活跃工作负载') }}</span><strong>{{ activeAssignments }}</strong></div>
    </div>

    <div v-if="error" class="inline-alert danger" role="alert">{{ error }}</div>

    <div class="node-workspace">
      <section class="node-section" aria-labelledby="node-fleet-heading">
        <div class="section-heading-row">
          <div><p class="eyebrow">{{ t('Fleet', '集群') }}</p><h2 id="node-fleet-heading">{{ t('Machines', '主机') }}</h2></div>
          <span class="record-count">{{ data.nodes.length }} {{ t('nodes', '个节点') }}</span>
        </div>
        <div class="table-scroll">
          <table class="data-table node-table">
            <thead><tr><th>{{ t('Node', '节点') }}</th><th>{{ t('State', '状态') }}</th><th>GPU</th><th>Projects</th><th>{{ t('Storage free', '可用存储') }}</th><th>{{ t('Last seen', '最后在线') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading && !initialized"><td colspan="6" class="empty-cell"><LoaderCircle :size="18" class="spinning" /> {{ t('Loading nodes', '正在加载节点') }}</td></tr>
              <tr v-else-if="data.nodes.length === 0"><td colspan="6" class="empty-cell">{{ t('No nodes have completed enrollment.', '尚无节点完成注册。') }}</td></tr>
              <tr v-for="node in data.nodes" :key="node.id">
                <td>
                  <div class="primary-cell"><strong>{{ node.label }}</strong><span>{{ node.hostname || t('Hostname pending', '等待主机名') }} · {{ node.agent_version || t('Version pending', '等待版本') }}</span></div>
                </td>
                <td><span class="state-badge" :class="node.observed_state">{{ localizedState(node.observed_state) }}</span></td>
                <td><span class="gpu-cell">{{ gpuLabel(node) }}</span></td>
                <td><span class="project-cell" :title="projectLabel(node)">{{ projectLabel(node) }}</span></td>
                <td>{{ formatBytes(node.storage.available_bytes) }}</td>
                <td>{{ formatDate(node.last_seen_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="node-section" aria-labelledby="node-workload-heading">
        <div class="section-heading-row">
          <div><p class="eyebrow">{{ t('Workloads', '工作负载') }}</p><h2 id="node-workload-heading">{{ t('Assignment history', 'Assignment 历史') }}</h2></div>
          <span class="record-count">{{ data.assignments.length }} Attempts</span>
        </div>
        <div class="table-scroll">
          <table class="data-table assignment-table">
            <thead><tr><th>Experiment</th><th>{{ t('Node', '节点') }}</th><th>{{ t('State', '状态') }}</th><th>Attempt</th><th>{{ t('Started', '开始时间') }}</th><th>{{ t('Last heartbeat', '最后心跳') }}</th><th>{{ t('Result', '结果') }}</th></tr></thead>
            <tbody>
              <tr v-if="loading && !initialized"><td colspan="7" class="empty-cell"><LoaderCircle :size="18" class="spinning" /> {{ t('Loading Assignments', '正在加载 Assignments') }}</td></tr>
              <tr v-else-if="data.assignments.length === 0"><td colspan="7" class="empty-cell">{{ t('No workload has been assigned to this fleet.', '尚未向此集群分配工作负载。') }}</td></tr>
              <tr v-for="assignment in data.assignments" :key="assignment.id">
                <td><code>{{ assignment.experiment_id.slice(0, 12) }}</code></td>
                <td><strong>{{ assignment.node_label }}</strong></td>
                <td><span class="state-badge" :class="assignment.state">{{ localizedState(assignment.state) }}</span></td>
                <td>#{{ assignment.attempt_number }}</td>
                <td>{{ formatDate(assignment.started_at) }}</td>
                <td>{{ formatDate(assignment.last_heartbeat_at) }}</td>
                <td><span v-if="assignment.exit_code !== undefined">{{ t('Exit', '退出码') }} {{ assignment.exit_code }}</span><span v-else-if="assignment.failure_code" class="failure-text">{{ assignment.failure_code }}</span><span v-else class="muted">{{ t('Pending', '等待中') }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="node-section" aria-labelledby="node-runtime-heading">
        <div class="section-heading-row runtime-heading-row">
          <div><p class="eyebrow">{{ t('Execution', '执行') }}</p><h2 id="node-runtime-heading">{{ t('Runtime configuration', '运行时配置') }}</h2></div>
          <div class="runtime-actions">
            <select v-model="runtimeProjectID" class="compact-select" :aria-label="t('Runtime Project', '运行时 Project')">
              <option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option>
            </select>
            <button class="primary-button small-button icon-command" type="button" :disabled="projects.length === 0" @click="openRuntime"><Plus :size="15" /> {{ t('Add runtime', '添加运行时') }}</button>
          </div>
        </div>
        <div v-if="runtimeError && !runtimeDialog" class="section-alert inline-alert danger" role="alert">{{ runtimeError }}</div>
        <div class="table-scroll">
          <table class="data-table runtime-table">
            <thead><tr><th>{{ t('Name', '名称') }}</th><th>{{ t('Image digest', '镜像摘要') }}</th><th>{{ t('GPU models', 'GPU 型号') }}</th><th>CPU</th><th>{{ t('Memory', '内存') }}</th><th>{{ t('Default', '默认') }}</th></tr></thead>
            <tbody>
              <tr v-if="runtimeLoading"><td colspan="6" class="empty-cell"><LoaderCircle :size="18" class="spinning" /> {{ t('Loading runtimes', '正在加载运行时') }}</td></tr>
              <tr v-else-if="runtimeRows.length === 0"><td colspan="6" class="empty-cell">{{ t('No Self-hosted runtime is configured for this Project.', '此 Project 尚未配置自托管运行时。') }}</td></tr>
              <tr v-for="row in runtimeRows" :key="row.profile.id">
                <td><strong>{{ row.profile.name }}</strong></td>
                <td><code class="image-reference" :title="row.environment?.image">{{ row.environment?.image ?? t('Environment missing', '缺少 Environment') }}</code></td>
                <td><span class="gpu-cell">{{ row.profile.gpu_names.join(', ') }}</span></td>
                <td>{{ row.profile.cpu_limit }}</td>
                <td>{{ row.profile.memory_gb }} GB</td>
                <td><span v-if="row.profile.is_default && row.environment?.is_default" class="state-badge online">{{ t('Default', '默认') }}</span><span v-else class="muted">{{ t('Optional', '可选') }}</span></td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section class="node-section" aria-labelledby="node-enrollment-heading">
        <div class="section-heading-row">
          <div><p class="eyebrow">{{ t('Access', '访问') }}</p><h2 id="node-enrollment-heading">{{ t('Enrollment activity', '注册活动') }}</h2></div>
          <span v-if="data.truncated" class="record-count">{{ t('Newest 500', '最新 500 条') }}</span>
        </div>
        <div class="table-scroll">
          <table class="data-table enrollment-table">
            <thead><tr><th>{{ t('Label', '标签') }}</th><th>{{ t('Status', '状态') }}</th><th>{{ t('Pairing code', '配对码') }}</th><th>{{ t('Expires', '过期时间') }}</th><th>{{ t('Created', '创建时间') }}</th><th :aria-label="t('Actions', '操作')"></th></tr></thead>
            <tbody>
              <tr v-if="loading && !initialized"><td colspan="6" class="empty-cell"><LoaderCircle :size="18" class="spinning" /> {{ t('Loading enrollments', '正在加载注册记录') }}</td></tr>
              <tr v-else-if="data.enrollments.length === 0"><td colspan="6" class="empty-cell">{{ t('No enrollment activity.', '暂无注册活动。') }}</td></tr>
              <tr v-for="enrollment in data.enrollments" :key="enrollment.id">
                <td><strong>{{ enrollment.label }}</strong></td>
                <td><span class="state-badge" :class="enrollment.status">{{ localizedState(enrollment.status) }}</span></td>
                <td><code v-if="enrollment.pairing_code" class="pairing-code">{{ enrollment.pairing_code }}</code><span v-else class="muted">{{ t('Waiting for claim', '等待认领') }}</span></td>
                <td>{{ formatDate(enrollment.expires_at) }}</td>
                <td>{{ formatDate(enrollment.created_at) }}</td>
                <td>
                  <div class="row-actions">
                    <button v-if="enrollment.status === 'claimed'" class="table-command approve" type="button" :title="t('Approve enrollment', '批准注册')" :aria-label="t('Approve enrollment', '批准注册')" @click="openApprove(enrollment)"><ShieldCheck :size="16" /></button>
                    <button v-if="['pending', 'claimed', 'approved', 'completed'].includes(enrollment.status)" class="table-command danger" type="button" :title="t('Revoke enrollment', '撤销注册')" :aria-label="t('Revoke enrollment', '撤销注册')" @click="revokeTarget = enrollment"><Trash2 :size="16" /></button>
                  </div>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>
    </div>

    <div v-if="createDialog" class="modal-backdrop" @click.self="createDialog = false">
      <form class="modal-card node-dialog" @submit.prevent="createEnrollment">
        <div class="modal-heading"><div><p class="eyebrow">{{ t('New machine', '新主机') }}</p><h2>{{ t('Create enrollment', '创建注册') }}</h2></div><button class="close-button" type="button" :aria-label="t('Close', '关闭')" @click="createDialog = false"><X :size="18" /></button></div>
        <label class="field-label">{{ t('Node label', '节点标签') }}<input v-model="createForm.label" class="text-input" required maxlength="120" autocomplete="off" placeholder="lab-gpu-01" /></label>
        <label class="field-label">{{ t('Setup link lifetime', 'Setup Link 有效期') }}<select v-model="createForm.expires" class="text-input"><option value="15">15 {{ t('minutes', '分钟') }}</option><option value="30">30 {{ t('minutes', '分钟') }}</option><option value="60">1 {{ t('hour', '小时') }}</option><option value="240">4 {{ t('hours', '小时') }}</option></select></label>
        <div v-if="createError" class="inline-alert danger" role="alert">{{ createError }}</div>
        <div class="modal-actions"><button class="secondary-button" type="button" @click="createDialog = false">{{ t('Cancel', '取消') }}</button><button class="primary-button icon-command" type="submit" :disabled="creating"><LoaderCircle v-if="creating" :size="16" class="spinning" /><Plus v-else :size="16" /> {{ t('Create', '创建') }}</button></div>
      </form>
    </div>

    <div v-if="runtimeDialog" class="modal-backdrop" @click.self="runtimeDialog = false">
      <form class="modal-card node-dialog runtime-dialog" @submit.prevent="createRuntime">
        <div class="modal-heading"><div><p class="eyebrow">{{ t('Project compute', 'Project 算力') }}</p><h2>{{ t('Add Self-hosted runtime', '添加自托管运行时') }}</h2></div><button class="close-button" type="button" :aria-label="t('Close', '关闭')" @click="runtimeDialog = false"><X :size="18" /></button></div>
        <div class="runtime-form-grid">
          <label class="field-label">Project<select v-model="runtimeForm.projectID" class="text-input" required><option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
          <label class="field-label">{{ t('Runtime name', '运行时名称') }}<input v-model="runtimeForm.name" class="text-input" required maxlength="120" autocomplete="off" placeholder="local-3090" /></label>
          <label class="field-label full-runtime-field">{{ t('OCI image pinned by digest', '按摘要固定的 OCI 镜像') }}<input v-model="runtimeForm.image" class="text-input code-input" required autocomplete="off" placeholder="registry.example/train@sha256:..." /></label>
          <label class="field-label full-runtime-field">{{ t('Accepted GPU models', '允许的 GPU 型号') }}<input v-model="runtimeForm.gpuNames" class="text-input" required autocomplete="off" placeholder="NVIDIA GeForce RTX 3090" /></label>
          <label class="field-label">{{ t('CPU limit', 'CPU 限制') }}<input v-model.number="runtimeForm.cpuLimit" class="text-input" required type="number" min="1" max="1024" /></label>
          <label class="field-label">{{ t('Memory limit (GB)', '内存限制 (GB)') }}<input v-model.number="runtimeForm.memoryGB" class="text-input" required type="number" min="1" max="4096" /></label>
        </div>
        <label class="runtime-checkbox"><input v-model="runtimeForm.makeDefault" type="checkbox" /><span>{{ t('Use as the Project default Environment and Resource Profile', '设为此 Project 的默认 Environment 和 Resource Profile') }}</span></label>
        <div v-if="runtimeError" class="inline-alert danger" role="alert">{{ runtimeError }}</div>
        <div class="modal-actions"><button class="secondary-button" type="button" @click="runtimeDialog = false">{{ t('Cancel', '取消') }}</button><button class="primary-button icon-command" type="submit" :disabled="runtimeBusy"><LoaderCircle v-if="runtimeBusy" :size="16" class="spinning" /><Plus v-else :size="16" /> {{ t('Create runtime', '创建运行时') }}</button></div>
      </form>
    </div>

    <div v-if="reveal" class="modal-backdrop" @click.self="reveal = null">
      <section class="modal-card node-dialog" role="dialog" aria-modal="true" aria-labelledby="setup-url-heading">
        <div class="modal-heading"><div><p class="eyebrow">{{ t('Enrollment created', '注册已创建') }}</p><h2 id="setup-url-heading">Setup Link</h2></div><button class="close-button" type="button" :aria-label="t('Close', '关闭')" @click="reveal = null"><X :size="18" /></button></div>
        <div class="setup-language-row"><span>{{ t('Handoff language', '交接语言') }}</span><div class="segmented-control" role="group" :aria-label="t('Node setup language', '节点设置语言')"><button type="button" :class="{ active: setupLanguage === 'zh' }" @click="setupLanguage = 'zh'">中文</button><button type="button" :class="{ active: setupLanguage === 'en' }" @click="setupLanguage = 'en'">English</button></div></div>
        <div class="secret-display"><code>{{ localizedSetupURL }}</code><button class="table-command" type="button" :title="copied ? t('Copied', '已复制') : t('Copy setup link', '复制 Setup Link')" :aria-label="t('Copy setup link', '复制 Setup Link')" @click="copySetupURL"><Check v-if="copied" :size="17" /><Clipboard v-else :size="17" /></button></div>
        <p class="dialog-note">{{ t('This link is shown once and expires', '此链接仅显示一次，将于') }} {{ formatDate(reveal.enrollment.expires_at) }} {{ t('It opens a self-contained handoff with the exact release, host checks, build, installation, secret-handling, and approval workflow for a trusted coding Agent.', '过期。它会打开面向可信编码 Agent 的自包含交接说明，包含精确版本、主机检查、构建、安装、secret 处理和批准流程。') }}</p>
        <div class="modal-actions"><button class="primary-button" type="button" @click="reveal = null">{{ t('Done', '完成') }}</button></div>
      </section>
    </div>

    <div v-if="approveTarget" class="modal-backdrop" @click.self="approveTarget = null">
      <form class="modal-card node-dialog" @submit.prevent="approveEnrollment">
        <div class="modal-heading"><div><p class="eyebrow">{{ t('Authorize machine', '授权主机') }}</p><h2>{{ t('Approve', '批准') }} {{ approveTarget.label }}</h2></div><button class="close-button" type="button" :aria-label="t('Close', '关闭')" @click="approveTarget = null"><X :size="18" /></button></div>
        <label class="field-label">{{ t('Pairing code', '配对码') }}<input v-model="approveForm.pairingCode" class="text-input pairing-input" required maxlength="9" autocomplete="off" placeholder="XXXX-XXXX" /></label>
        <fieldset class="project-options"><legend>{{ t('Project access', 'Project 访问权限') }}</legend><label v-for="project in projects" :key="project.id"><input v-model="approveForm.projects[project.id]" type="checkbox" /> <span>{{ project.name }}</span></label><p v-if="projects.length === 0" class="muted">{{ t('Create a Project before approving this node.', '批准此节点前请先创建 Project。') }}</p></fieldset>
        <div v-if="approveError" class="inline-alert danger" role="alert">{{ approveError }}</div>
        <div class="modal-actions"><button class="secondary-button" type="button" @click="approveTarget = null">{{ t('Cancel', '取消') }}</button><button class="primary-button icon-command" type="submit" :disabled="approving || projects.length === 0"><LoaderCircle v-if="approving" :size="16" class="spinning" /><ShieldCheck v-else :size="16" /> {{ t('Approve', '批准') }}</button></div>
      </form>
    </div>

    <div v-if="revokeTarget" class="modal-backdrop" @click.self="revokeTarget = null">
      <section class="modal-card node-dialog compact-dialog" role="alertdialog" aria-modal="true">
        <div class="modal-heading"><div><p class="eyebrow">{{ t('Enrollment access', '注册访问') }}</p><h2>{{ t('Revoke', '撤销') }} {{ revokeTarget.label }}?</h2></div><button class="close-button" type="button" :aria-label="t('Close', '关闭')" @click="revokeTarget = null"><X :size="18" /></button></div>
        <p class="dialog-note">{{ t('The setup link and associated Node credential will stop working. Active workloads are not force-stopped by credential revocation.', 'Setup Link 和关联的节点凭据将立即失效。撤销凭据不会强制停止活跃工作负载。') }}</p>
        <div class="modal-actions"><button class="secondary-button" type="button" @click="revokeTarget = null">{{ t('Cancel', '取消') }}</button><button class="danger-button icon-command" type="button" :disabled="revoking" @click="revokeEnrollment"><LoaderCircle v-if="revoking" :size="16" class="spinning" /><Trash2 v-else :size="16" /> {{ t('Revoke', '撤销') }}</button></div>
      </section>
    </div>
  </section>
</template>

<style scoped>
.node-page {
  width: 100%;
  min-width: 0;
  padding: 30px clamp(18px, 3vw, 42px) 48px;
}
.node-heading {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 24px;
}
.node-heading h1 { margin: 0; color: #202421; font-size: 24px; line-height: 31px; }
.page-subtitle { margin: 5px 0 0; color: #69736c; font-size: 13px; }
.heading-actions, .row-actions, .modal-actions { display: flex; align-items: center; gap: 9px; }
.icon-command { white-space: nowrap; }
.node-metrics {
  margin: 25px 0;
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  border: 1px solid #dce2dd;
  border-radius: 7px;
  background: #fff;
}
.node-metric {
  min-width: 0;
  height: 78px;
  padding: 15px 17px;
  display: grid;
  grid-template-columns: auto 1fr;
  grid-template-rows: auto auto;
  column-gap: 10px;
  border-right: 1px solid #e2e7e3;
}
.node-metric:last-child { border-right: 0; }
.node-metric svg { grid-row: 1 / 3; align-self: center; color: #547065; }
.node-metric span { color: #727c75; font-size: 11px; }
.node-metric strong { color: #202421; font-size: 20px; line-height: 24px; }
.inline-alert { margin: 0 0 16px; padding: 10px 12px; border-radius: 5px; font-size: 12px; line-height: 18px; }
.inline-alert.danger { color: #8b302b; background: #fff0ee; border: 1px solid #efc1bc; }
.node-workspace { background: #fff; border-top: 1px solid #dce2dd; border-bottom: 1px solid #dce2dd; }
.node-section + .node-section { border-top: 1px solid #dce2dd; }
.section-heading-row {
  min-height: 67px;
  padding: 12px 15px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
}
.section-heading-row h2 { margin: 0; color: #28302b; font-size: 15px; }
.record-count { color: #7a847d; font-size: 11px; }
.node-table th:nth-child(1) { width: 18%; }
.node-table th:nth-child(2) { width: 10%; }
.node-table th:nth-child(3) { width: 24%; }
.node-table th:nth-child(4) { width: 20%; }
.data-table tbody tr:hover { background: #f8faf8; }
.primary-cell { display: grid; gap: 3px; }
.primary-cell strong, .data-table td > strong { color: #29342e; font-size: 12px; }
.primary-cell span { color: #838c86; font-size: 10px; }
.gpu-cell, .project-cell { display: block; max-width: 260px; overflow: hidden; text-overflow: ellipsis; }
.empty-cell { height: 92px !important; color: #7b847e !important; text-align: center; }
.empty-cell svg { margin-right: 6px; vertical-align: -4px; }
.muted { color: #929a95; font-size: 11px; }
.pairing-code {
  padding: 4px 6px;
  color: #3d4d44 !important;
  background: #eef2ef;
  border-radius: 4px;
  font-weight: 700;
}
.state-badge::before { content: ''; width: 7px; height: 7px; border-radius: 50%; background: #929a95; }
.state-badge.online::before, .state-badge.claimed::before, .state-badge.completed::before,
.state-badge.running::before, .state-badge.succeeded::before { background: #26815f; }
.state-badge.offline::before, .state-badge.pending::before, .state-badge.approved::before,
.state-badge.starting::before, .state-badge.stopping::before, .state-badge.collecting::before { background: #bf8627; }
.state-badge.quarantined::before, .state-badge.revoked::before, .state-badge.expired::before,
.state-badge.failed::before, .state-badge.timed_out::before, .state-badge.lost::before { background: #b64c45; }
.failure-text { color: #9b3e38; }
.table-command, .close-button {
  width: 32px;
  height: 32px;
  display: inline-grid;
  place-items: center;
  color: #59645d;
  background: #fff;
  border: 1px solid #ccd4ce;
  border-radius: 5px;
  cursor: pointer;
}
.table-command:hover { color: #1f5f49; background: #eff5f1; }
.table-command.danger:hover { color: #9d3d37; background: #fff1ef; border-color: #e2b9b4; }
.table-command.approve { color: #1f6a50; }
.row-actions { justify-content: flex-end; }
.modal-card {
  width: min(520px, 100%);
  max-height: calc(100vh - 44px);
  padding: 20px;
  overflow-y: auto;
  background: #fff;
  border: 1px solid #cfd7d1;
  border-radius: 7px;
  box-shadow: 0 18px 50px rgb(14 20 16 / 24%);
}
.compact-dialog { width: min(460px, 100%); }
.modal-heading { margin-bottom: 20px; display: flex; align-items: flex-start; justify-content: space-between; gap: 20px; }
.modal-heading h2 { margin: 0; color: #242b27; font-size: 17px; }
.close-button { border: 0; background: transparent; }
.close-button:hover { background: #eef2ef; }
.field-label + .field-label, .field-label + .project-options { margin-top: 16px; }
.pairing-input { text-transform: uppercase; font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.project-options { margin: 17px 0 0; padding: 13px; display: grid; gap: 9px; border: 1px solid #dce2dd; border-radius: 5px; }
.project-options legend { padding: 0 4px; color: #505a54; font-size: 12px; font-weight: 650; }
.project-options label { min-height: 26px; flex-direction: row; align-items: center; gap: 8px; font-size: 12px; }
.project-options input { width: 16px; height: 16px; min-height: 0; margin: 0; accent-color: #216e55; }
.modal-actions { margin-top: 21px; justify-content: flex-end; }
.setup-language-row { margin-bottom: 10px; display: flex; align-items: center; justify-content: space-between; gap: 12px; }
.setup-language-row > span { color: #59645d; font-size: 12px; font-weight: 650; }
.setup-language-row .segmented-control { flex: 0 0 auto; }
.secret-display { min-width: 0; padding: 9px 9px 9px 12px; display: flex; align-items: center; gap: 10px; background: #f0f3f1; border: 1px solid #d7ded9; border-radius: 5px; }
.secret-display code { min-width: 0; flex: 1; overflow-wrap: anywhere; color: #24342b; font-size: 11px; line-height: 18px; }
.dialog-note { margin: 13px 0 0; color: #69736c; font-size: 12px; line-height: 19px; }
.runtime-actions { display: flex; align-items: center; gap: 9px; }
.compact-select { width: min(210px, 32vw); min-height: 34px; height: 34px; }
.section-alert { margin: 0 15px 14px; }
.image-reference { display: block; max-width: 310px; overflow: hidden; text-overflow: ellipsis; }
.runtime-dialog { width: min(620px, 100%); }
.runtime-form-grid { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.runtime-form-grid .field-label { margin: 0; }
.full-runtime-field { grid-column: 1 / -1; }
.code-input { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; }
.runtime-checkbox { margin-top: 17px; min-height: 28px; flex-direction: row; align-items: center; gap: 8px; }
.runtime-checkbox input { width: 16px; height: 16px; min-height: 0; margin: 0; accent-color: #216e55; }
@media (max-width: 900px) {
  .node-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .node-metric:nth-child(2) { border-right: 0; }
  .node-metric:nth-child(-n + 2) { border-bottom: 1px solid #e2e7e3; }
}
@media (max-width: 640px) {
  .node-page { padding: 21px 14px 34px; }
  .node-heading { align-items: stretch; flex-direction: column; }
  .heading-actions { display: grid; grid-template-columns: 1fr 1fr; }
  .node-metrics { grid-template-columns: 1fr 1fr; }
  .node-metric { height: 72px; padding: 12px; }
  .node-metric strong { font-size: 17px; }
  .runtime-heading-row { align-items: stretch; flex-direction: column; }
  .runtime-actions { display: grid; grid-template-columns: 1fr 1fr; }
  .compact-select { width: 100%; }
  .runtime-form-grid { grid-template-columns: 1fr; }
  .full-runtime-field { grid-column: auto; }
  .modal-backdrop { padding: 10px; align-items: flex-end; }
  .modal-card { max-height: calc(100vh - 20px); }
}
</style>
