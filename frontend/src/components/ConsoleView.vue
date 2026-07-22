<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import {
  Activity,
  Bell,
  Bot,
  Boxes,
  Check,
  CircleDollarSign,
  Clipboard,
  Clock3,
  Cpu,
  FlaskConical,
  GitBranch,
  KeyRound,
  LoaderCircle,
  LogOut,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  X,
} from '@lucide/vue'
import { APIError, api, type Attempt, type BuildInfo, type Cost, type Experiment, type Project, type Repository, type RuntimeStatus, type User } from '../api'
import ExperimentTable from './ExperimentTable.vue'
import ProviderView from './ProviderView.vue'
import NotificationView from './NotificationView.vue'
import AgentView from './AgentView.vue'
import NodeView from './NodeView.vue'

const props = defineProps<{ build: BuildInfo | null; user: User }>()
const emit = defineEmits<{ signedOut: [] }>()

type ViewName = 'overview' | 'experiments' | 'projects' | 'agents' | 'nodes' | 'provider' | 'notifications'
const activeView = ref<ViewName>('overview')
const projects = ref<Project[]>([])
const selectedProjectID = ref('')
const repositories = ref<Repository[]>([])
const experiments = ref<Experiment[]>([])
const cost = ref<Cost | null>(null)
const runtimeStatus = ref<RuntimeStatus | null>(null)
const loading = ref(false)
const error = ref('')
const signingOut = ref(false)
const selectedExperiment = ref<Experiment | null>(null)
const attempts = ref<Attempt[]>([])
const attemptsLoading = ref(false)
const attemptsError = ref('')
const stateFilter = ref('all')
const repositoryDialog = ref<'create' | 'key' | 'verify' | null>(null)
const selectedRepository = ref<Repository | null>(null)
const repositoryBusy = ref(false)
const dialogError = ref('')
const copied = ref('')
const repositoryForm = reactive({ name: '', sshURL: '', defaultBranch: 'main', fingerprint: '' })

const selectedProject = computed(() => projects.value.find((project) => project.id === selectedProjectID.value) ?? null)
const runningCount = computed(() => experiments.value.filter((item) => ['provisioning', 'running', 'collecting', 'cancelling'].includes(item.state)).length)
const queuedCount = computed(() => experiments.value.filter((item) => item.state === 'queued').length)
const filteredExperiments = computed(() => stateFilter.value === 'all' ? experiments.value : experiments.value.filter((item) => item.state === stateFilter.value))
const recentExperiments = computed(() => experiments.value.slice(0, 8))
const latestAttempt = computed(() => attempts.value.at(-1) ?? null)
const viewTitle = computed(() => ({
  overview: 'Overview',
  experiments: 'Experiments',
  projects: 'Project configuration',
  agents: 'Agent access',
  nodes: 'Self-hosted nodes',
  provider: 'Private Cloud resources',
  notifications: 'Notifications',
})[activeView.value])

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('signedOut')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

async function refreshAll() {
  loading.value = true
  error.value = ''
  try {
    const [loadedProjects, loadedRuntime] = await Promise.all([api.projects(), api.runtimeStatus()])
    projects.value = loadedProjects
    runtimeStatus.value = loadedRuntime
    if (!loadedProjects.some((item) => item.id === selectedProjectID.value)) {
      selectedProjectID.value = loadedProjects[0]?.id ?? ''
    }
    await refreshProject(false)
  } catch (caught) {
    handleError(caught, 'Could not load the control-plane state.')
  } finally {
    loading.value = false
  }
}

async function refreshProject(showSpinner = true) {
  if (!selectedProjectID.value) {
    repositories.value = []
    experiments.value = []
    cost.value = null
    return
  }
  if (showSpinner) loading.value = true
  error.value = ''
  try {
    const [loadedRepositories, loadedExperiments, loadedCost] = await Promise.all([
      api.repositories(selectedProjectID.value),
      api.experiments(selectedProjectID.value),
      api.cost(selectedProjectID.value),
    ])
    repositories.value = loadedRepositories
    experiments.value = loadedExperiments
    cost.value = loadedCost
  } catch (caught) {
    handleError(caught, 'Could not refresh this project.')
  } finally {
    if (showSpinner) loading.value = false
  }
}

async function openExperiment(experiment: Experiment) {
  selectedExperiment.value = experiment
  attempts.value = []
  attemptsError.value = ''
  attemptsLoading.value = true
  try {
    const [detail, history] = await Promise.all([
      api.experiment(experiment.project_id, experiment.id),
      api.attempts(experiment.project_id, experiment.id),
    ])
    if (selectedExperiment.value?.id === experiment.id) {
      selectedExperiment.value = detail
      attempts.value = history
    }
  } catch (caught) {
    if (selectedExperiment.value?.id !== experiment.id) return
    if (caught instanceof APIError && caught.status === 401) emit('signedOut')
    else attemptsError.value = caught instanceof APIError ? caught.message : 'Could not load Attempt history.'
  } finally {
    if (selectedExperiment.value?.id === experiment.id) attemptsLoading.value = false
  }
}

function closeExperiment() {
  selectedExperiment.value = null
  attempts.value = []
  attemptsError.value = ''
  attemptsLoading.value = false
}

async function signOut() {
  signingOut.value = true
  error.value = ''
  try {
    await api.logout()
    emit('signedOut')
  } catch (caught) {
    handleError(caught, 'Sign out failed. Try again after the connection recovers.')
  } finally {
    signingOut.value = false
  }
}

function openCreateRepository() {
  repositoryForm.name = ''
  repositoryForm.sshURL = ''
  repositoryForm.defaultBranch = 'main'
  dialogError.value = ''
  selectedRepository.value = null
  repositoryDialog.value = 'create'
}

async function createRepository() {
  if (!selectedProject.value) return
  repositoryBusy.value = true
  dialogError.value = ''
  try {
    const created = await api.createRepository({
      project_id: selectedProject.value.id,
      name: repositoryForm.name.trim(),
      ssh_url: repositoryForm.sshURL.trim(),
      default_branch: repositoryForm.defaultBranch.trim(),
    })
    selectedRepository.value = created
    repositoryDialog.value = 'key'
    await refreshProject(false)
  } catch (caught) {
    dialogError.value = caught instanceof APIError ? caught.message : 'Repository registration failed.'
  } finally {
    repositoryBusy.value = false
  }
}

function openKey(repository: Repository) {
  selectedRepository.value = repository
  dialogError.value = ''
  repositoryDialog.value = 'key'
}

function openVerify(repository: Repository) {
  selectedRepository.value = repository
  repositoryForm.fingerprint = repository.host_key_fingerprint ?? ''
  dialogError.value = ''
  repositoryDialog.value = 'verify'
}

async function verifyRepository() {
  if (!selectedRepository.value) return
  repositoryBusy.value = true
  dialogError.value = ''
  try {
    await api.verifyRepository(selectedRepository.value.id, repositoryForm.fingerprint.trim())
    repositoryDialog.value = null
    await refreshProject(false)
  } catch (caught) {
    dialogError.value = caught instanceof APIError ? caught.message : 'Repository verification failed.'
  } finally {
    repositoryBusy.value = false
  }
}

async function copy(value: string, name: string) {
  await navigator.clipboard.writeText(value)
  copied.value = name
  window.setTimeout(() => (copied.value = ''), 1600)
}

function money(milli = 0) {
  return `CNY ${(milli / 1000).toFixed(2)}`
}

function dateTime(value?: string) {
  if (!value) return 'Not set'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function metricsJSON(value?: Record<string, unknown>) {
  return JSON.stringify(value ?? {}, null, 2)
}

function stateLabel(value: string) {
  return value.replaceAll('_', ' ')
}

onMounted(refreshAll)
</script>

<template>
  <div class="app-shell console-shell">
    <aside class="sidebar">
      <div class="brand">
        <span class="brand-mark"><FlaskConical :size="19" /></span>
        <div><strong>Gemcp</strong><span>GPU control plane</span></div>
      </div>
      <nav aria-label="Primary navigation">
        <button class="nav-item" :class="{ active: activeView === 'overview' }" type="button" aria-label="Overview" title="Overview" @click="activeView = 'overview'"><Activity :size="17" /><span>Overview</span></button>
        <button class="nav-item" :class="{ active: activeView === 'experiments' }" type="button" aria-label="Experiments" title="Experiments" @click="activeView = 'experiments'"><FlaskConical :size="17" /><span>Experiments</span></button>
        <button class="nav-item" :class="{ active: activeView === 'projects' }" type="button" aria-label="Project" title="Project" @click="activeView = 'projects'"><Boxes :size="17" /><span>Project</span></button>
        <button class="nav-item" :class="{ active: activeView === 'agents' }" type="button" aria-label="Agents" title="Agents" @click="activeView = 'agents'"><Bot :size="17" /><span>Agents</span></button>
        <button v-if="runtimeStatus?.self_hosted_enabled" class="nav-item" :class="{ active: activeView === 'nodes' }" type="button" aria-label="Nodes" title="Nodes" @click="activeView = 'nodes'"><Cpu :size="17" /><span>Nodes</span></button>
        <button class="nav-item" :class="{ active: activeView === 'provider' }" type="button" aria-label="Provider" title="Provider" @click="activeView = 'provider'"><Server :size="17" /><span>Provider</span></button>
        <button class="nav-item" :class="{ active: activeView === 'notifications' }" type="button" aria-label="Alerts" title="Alerts" @click="activeView = 'notifications'"><Bell :size="17" /><span>Alerts</span></button>
        <button class="nav-item nav-bottom" type="button" aria-label="Sign out" title="Sign out" :disabled="signingOut" @click="signOut"><LogOut :size="17" /><span>Sign out</span></button>
      </nav>
      <div class="sidebar-user"><span>{{ props.user.email }}</span><small>{{ props.user.role }}</small></div>
    </aside>

    <main class="console-main">
      <header class="topbar">
        <div>
          <p class="eyebrow">Operations</p>
          <h1>{{ viewTitle }}</h1>
        </div>
        <div class="topbar-actions">
          <label v-if="!['nodes', 'provider', 'notifications'].includes(activeView)" class="project-select"><span>Project</span><select v-model="selectedProjectID" @change="refreshProject()"><option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
          <span class="status online"><span class="status-dot" />Online</span>
          <button v-if="!['nodes', 'provider', 'notifications'].includes(activeView)" class="icon-button" type="button" title="Refresh project" :disabled="loading" @click="refreshAll"><RefreshCw :size="17" :class="{ spinning: loading }" /></button>
        </div>
      </header>

      <div v-if="error" class="page-alert" role="alert">{{ error }}<button type="button" title="Dismiss" @click="error = ''"><X :size="16" /></button></div>

      <template v-if="activeView === 'overview'">
        <section class="content-band">
          <div class="metric-grid">
            <div class="metric"><span>Running</span><strong>{{ runningCount }}</strong><small>Active provider lifecycle</small></div>
            <div class="metric"><span>Queued</span><strong>{{ queuedCount }}</strong><small>Durable FIFO-ready queue</small></div>
            <div class="metric accent-green"><span>Committed this period</span><strong>{{ money(cost?.committed_milli) }}</strong><small>{{ cost?.period ?? 'Current project period' }}</small></div>
            <div class="metric accent-coral"><span>Available budget</span><strong>{{ money(cost?.available_milli) }}</strong><small>Operational estimate</small></div>
          </div>
        </section>
        <section class="workspace">
          <div class="section-heading"><div><h2>Recent experiments</h2><p>Latest Agent submissions for {{ selectedProject?.name ?? 'this project' }}.</p></div><button class="text-button" type="button" @click="activeView = 'experiments'">View all</button></div>
          <ExperimentTable :experiments="recentExperiments" compact @select="openExperiment" />
        </section>
        <section class="status-band">
          <div><Server :size="18" /><span><strong>{{ !runtimeStatus?.scheduler_enabled ? (runtimeStatus?.scheduler_healthy ? 'Dispatch disabled' : 'Reconciler unavailable') : runtimeStatus.scheduler_healthy ? 'Scheduler healthy' : 'Scheduler unavailable' }}</strong><small>{{ runtimeStatus?.scheduler_enabled ? `Global concurrency ${runtimeStatus.global_concurrency}` : 'Reconciliation remains active' }}</small></span></div>
          <div><ShieldCheck :size="18" /><span><strong>{{ runtimeStatus?.watchdog_healthy ? 'Watchdog healthy' : 'Watchdog unavailable' }}</strong><small>{{ runtimeStatus?.watchdog_heartbeat ? dateTime(runtimeStatus.watchdog_heartbeat.last_seen_at) : 'No heartbeat' }}</small></span></div>
          <div><Bell :size="18" /><span><strong>{{ runtimeStatus?.notification_worker_healthy ? 'Notification worker healthy' : 'Notification worker unavailable' }}</strong><small>{{ runtimeStatus?.notification_heartbeat ? dateTime(runtimeStatus.notification_heartbeat.last_seen_at) : 'No heartbeat' }}</small></span></div>
          <div><CircleDollarSign :size="18" /><span><strong>{{ money(cost?.monthly_budget_milli) }}</strong><small>Monthly hard budget</small></span></div>
        </section>
      </template>

      <section v-else-if="activeView === 'experiments'" class="page-workspace">
        <div class="section-heading page-section-heading"><div><h2>Experiments</h2><p>Immutable specifications and current lifecycle state.</p></div><div class="segmented-control" aria-label="Experiment state filter"><button v-for="filter in ['all', 'queued', 'running', 'succeeded', 'failed']" :key="filter" type="button" :class="{ active: stateFilter === filter }" @click="stateFilter = filter">{{ filter }}</button></div></div>
        <ExperimentTable :experiments="filteredExperiments" @select="openExperiment" />
      </section>

      <section v-else-if="activeView === 'projects'" class="page-workspace project-workspace">
        <div class="section-heading page-section-heading"><div><h2>{{ selectedProject?.name ?? 'Project' }}</h2><p>{{ selectedProject?.slug }} / {{ selectedProject?.status }}</p></div><span class="version-chip">{{ selectedProject?.timezone }}</span></div>
        <div v-if="selectedProject" class="policy-grid">
          <div><span>Monthly budget</span><strong>{{ money(selectedProject.monthly_budget_milli) }}</strong></div>
          <div><span>Experiment cap</span><strong>{{ money(selectedProject.max_experiment_milli) }}</strong></div>
          <div><span>Concurrency</span><strong>{{ selectedProject.max_concurrency }}</strong></div>
          <div><span>Maximum runtime</span><strong>{{ Math.round(selectedProject.max_runtime_seconds / 3600) }} h</strong></div>
        </div>
        <div class="subsection-heading"><div><h2>Private repositories</h2><p>Read-only Deploy Keys and pinned GitHub host identity.</p></div><button class="primary-button small-button" type="button" @click="openCreateRepository"><Plus :size="16" />Register repository</button></div>
        <div v-if="repositories.length" class="table-scroll">
          <table class="data-table repository-table">
            <thead><tr><th>Status</th><th>Name</th><th>SSH URL</th><th>Branch</th><th>Verified</th><th>Actions</th></tr></thead>
            <tbody><tr v-for="repository in repositories" :key="repository.id"><td><span class="state-badge" :data-state="repository.status"><span />{{ stateLabel(repository.status) }}</span></td><td>{{ repository.name }}</td><td><code>{{ repository.ssh_url }}</code></td><td><code>{{ repository.default_branch }}</code></td><td>{{ dateTime(repository.last_verified_at) }}</td><td><div class="table-actions"><button class="icon-button" type="button" title="View Deploy public key" @click="openKey(repository)"><KeyRound :size="16" /></button><button class="icon-button" type="button" title="Verify repository" @click="openVerify(repository)"><ShieldCheck :size="16" /></button></div></td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><GitBranch :size="21" /></span><h3>No repositories registered</h3><p>Register the private GitHub repository used by the first experiment.</p></div>
      </section>

      <AgentView v-if="activeView === 'agents'" :active="true" :project="selectedProject" @unauthorized="emit('signedOut')" />
      <NodeView v-if="activeView === 'nodes'" :active="true" :projects="projects" @unauthorized="emit('signedOut')" />
      <div v-show="activeView === 'provider'" class="persistent-view"><ProviderView :active="activeView === 'provider'" @unauthorized="emit('signedOut')" /></div>
      <NotificationView v-if="activeView === 'notifications'" :active="true" @unauthorized="emit('signedOut')" />

      <footer class="console-footer"><span>{{ props.build?.name ?? 'Gemcp' }} {{ props.build?.version ?? 'dev' }}</span><span>Commit {{ props.build?.commit ?? 'unknown' }}</span><span>Operational estimates only</span></footer>
    </main>
  </div>

  <div v-if="selectedExperiment" class="modal-backdrop" @click.self="closeExperiment">
    <section class="detail-panel" role="dialog" aria-modal="true" aria-label="Experiment details">
      <header><div><p class="eyebrow">Experiment</p><h2>{{ selectedExperiment.id.slice(0, 12) }}</h2></div><button class="icon-button" type="button" title="Close details" @click="closeExperiment"><X :size="17" /></button></header>
      <div class="detail-state"><span class="state-badge" :data-state="selectedExperiment.state"><span />{{ stateLabel(selectedExperiment.state) }}</span><span>Desired: {{ selectedExperiment.desired_state }}</span></div>
      <dl class="detail-list">
        <div><dt>Commit</dt><dd><code>{{ selectedExperiment.commit_sha }}</code></dd></div>
        <div><dt>Command</dt><dd><code>{{ selectedExperiment.command }}</code></dd></div>
        <div><dt>Reserved cost</dt><dd>{{ money(selectedExperiment.reserved_cost_milli) }}</dd></div>
        <div><dt>Maximum runtime</dt><dd>{{ Math.round(selectedExperiment.max_runtime_seconds / 60) }} minutes</dd></div>
        <div><dt>Created</dt><dd>{{ dateTime(selectedExperiment.created_at) }}</dd></div>
        <div><dt>Output path</dt><dd><code>{{ selectedExperiment.output_path }}</code></dd></div>
        <div v-if="selectedExperiment.failure_reason"><dt>Failure</dt><dd>{{ selectedExperiment.failure_reason }}</dd></div>
      </dl>
      <section class="attempt-history">
        <div class="attempt-heading"><div><h3>Attempts</h3><p>Immutable infrastructure retries and the latest Runner result.</p></div><LoaderCircle v-if="attemptsLoading" :size="18" class="spinning" /></div>
        <div v-if="attemptsError" class="form-error">{{ attemptsError }}</div>
        <div v-else-if="attempts.length" class="table-scroll">
          <table class="data-table attempt-table">
            <thead><tr><th>#</th><th>State</th><th>Provider ID</th><th>Exit</th><th>Estimate</th><th>Finished</th></tr></thead>
            <tbody><tr v-for="item in attempts" :key="item.id"><td>{{ item.number }}</td><td><span class="state-badge" :data-state="item.state"><span />{{ stateLabel(item.state) }}</span></td><td><code>{{ item.provider_resource_id ?? 'Pending' }}</code></td><td>{{ item.exit_code ?? '—' }}</td><td>{{ money(item.estimated_cost_milli) }}</td><td>{{ dateTime(item.finished_at) }}</td></tr></tbody>
          </table>
        </div>
        <div v-else-if="!attemptsLoading" class="attempt-empty">No Attempt has been dispatched.</div>
        <template v-if="latestAttempt">
          <dl v-if="latestAttempt.failure_reason || latestAttempt.retry_reason" class="attempt-result">
            <div v-if="latestAttempt.retry_reason"><dt>Retry reason</dt><dd>{{ latestAttempt.retry_reason }}</dd></div>
            <div v-if="latestAttempt.failure_reason"><dt>Attempt failure</dt><dd>{{ latestAttempt.failure_reason }}</dd></div>
          </dl>
          <div v-if="latestAttempt.metrics && Object.keys(latestAttempt.metrics).length" class="attempt-output"><span>Metrics</span><pre>{{ metricsJSON(latestAttempt.metrics) }}</pre></div>
          <div v-if="latestAttempt.log_tail" class="attempt-output"><span>Log tail</span><pre>{{ latestAttempt.log_tail }}</pre></div>
        </template>
      </section>
    </section>
  </div>

  <div v-if="repositoryDialog" class="modal-backdrop" @click.self="repositoryDialog = null">
    <section class="modal" role="dialog" aria-modal="true" :aria-label="repositoryDialog === 'create' ? 'Register repository' : repositoryDialog === 'verify' ? 'Verify repository' : 'Deploy public key'">
      <header><div><p class="eyebrow">Private Git</p><h2>{{ repositoryDialog === 'create' ? 'Register repository' : repositoryDialog === 'verify' ? 'Verify host and access' : 'Deploy public key' }}</h2></div><button class="icon-button" type="button" title="Close" @click="repositoryDialog = null"><X :size="17" /></button></header>
      <form v-if="repositoryDialog === 'create'" class="dialog-form" @submit.prevent="createRepository">
        <label>Name<input v-model="repositoryForm.name" required /></label>
        <label>GitHub SSH URL<input v-model="repositoryForm.sshURL" placeholder="git@github.com:owner/repository.git" required spellcheck="false" /></label>
        <label>Default branch<input v-model="repositoryForm.defaultBranch" required spellcheck="false" /></label>
        <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
        <button class="primary-button" type="submit" :disabled="repositoryBusy"><LoaderCircle v-if="repositoryBusy" :size="16" class="spinning" /><Plus v-else :size="16" />Generate Deploy Key</button>
      </form>
      <div v-else-if="repositoryDialog === 'key' && selectedRepository" class="key-panel">
        <p>Add this public key to <strong>{{ selectedRepository.name }}</strong> as a read-only GitHub Deploy Key.</p>
        <div class="code-box"><code>{{ selectedRepository.deploy_public_key }}</code><button class="icon-button" type="button" :title="copied === 'deploy-key' ? 'Copied' : 'Copy Deploy public key'" @click="copy(selectedRepository.deploy_public_key ?? '', 'deploy-key')"><Check v-if="copied === 'deploy-key'" :size="16" /><Clipboard v-else :size="16" /></button></div>
        <button class="primary-button" type="button" @click="openVerify(selectedRepository)"><ShieldCheck :size="16" />Continue to verification</button>
      </div>
      <form v-else-if="selectedRepository" class="dialog-form" @submit.prevent="verifyRepository">
        <div class="repository-summary"><GitBranch :size="17" /><span><strong>{{ selectedRepository.name }}</strong><small>{{ selectedRepository.ssh_url }}</small></span></div>
        <label>Trusted GitHub host-key fingerprint<input v-model="repositoryForm.fingerprint" placeholder="SHA256:..." required spellcheck="false" /></label>
        <p class="form-note">Use the current fingerprint from GitHub's official HTTPS documentation, not an unverified key scan.</p>
        <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
        <button class="primary-button" type="submit" :disabled="repositoryBusy"><LoaderCircle v-if="repositoryBusy" :size="16" class="spinning" /><ShieldCheck v-else :size="16" />Verify repository</button>
      </form>
    </section>
  </div>
</template>
