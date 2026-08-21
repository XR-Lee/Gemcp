<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import {
  Bell,
  Bot,
  Boxes,
  Check,
  Clipboard,
  Cpu,
  FlaskConical,
  GitBranch,
  KeyRound,
  LoaderCircle,
  LogOut,
  Network,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Stethoscope,
  WalletCards,
  X,
} from '@lucide/vue'
import { APIError, api, type Attempt, type BuildInfo, type Experiment, type OperationsFeed, type Project, type Repository, type ResearchWorkspace, type RuntimeStatus, type User } from '../api'
import { localizedState, useI18n } from '../i18n'
import BrandMark from './BrandMark.vue'
import ExperimentTable from './ExperimentTable.vue'
import FinanceView from './FinanceView.vue'
import LanguageToggle from './LanguageToggle.vue'
import ProviderView from './ProviderView.vue'
import NotificationView from './NotificationView.vue'
import AgentView from './AgentView.vue'
import NodeView from './NodeView.vue'
import DiagnosticsView from './DiagnosticsView.vue'
import ExperimentDetail from './ExperimentDetail.vue'
import ResearchView from './ResearchView.vue'
import RunActivityPanel from './RunActivityPanel.vue'
import WorkbenchDialog from './WorkbenchDialog.vue'
import WorkbenchSelect from './WorkbenchSelect.vue'

const props = defineProps<{ build: BuildInfo | null; user: User }>()
const emit = defineEmits<{ signedOut: [] }>()

type ViewName = 'research' | 'experiments' | 'diagnostics' | 'finance' | 'projects' | 'agents' | 'nodes' | 'provider' | 'notifications'
const activeView = ref<ViewName>('research')
const projects = ref<Project[]>([])
const selectedProjectID = ref('')
const selectedStudyID = ref('')
const repositories = ref<Repository[]>([])
const experiments = ref<Experiment[]>([])
const runtimeStatus = ref<RuntimeStatus | null>(null)
const operationsFeed = ref<OperationsFeed | null>(null)
const researchWorkspace = ref<ResearchWorkspace | null>(null)
const studyDialog = ref(false)
const studyBusy = ref(false)
const studyForm = reactive({ name: '', question: '', summary: '' })
const operationsLoading = ref(false)
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
const { languageTag, t } = useI18n()
let liveRefreshInFlight = false
let projectRefreshGeneration = 0
const projectOptions = computed(() => projects.value.map((project) => ({ value: project.id, label: project.name })))

const selectedProject = computed(() => projects.value.find((project) => project.id === selectedProjectID.value) ?? null)
const filteredExperiments = computed(() => stateFilter.value === 'all' ? experiments.value : experiments.value.filter((item) => item.state === stateFilter.value))
const viewTitle = computed(() => ({
  research: t('Research', '研究'),
  experiments: t('Evidence', '证据'),
  diagnostics: t('Diagnostics', '诊断'),
  finance: t('Budget and ledger', '预算与账本'),
  projects: t('Project configuration', 'Project 配置'),
  agents: t('Agent access', 'Agent 访问'),
  nodes: t('Self-hosted nodes', '自托管节点'),
  provider: t('Private Cloud resources', '私有云资源'),
  notifications: t('Notifications', '通知'),
})[activeView.value])
const viewEyebrow = computed(() => ['research', 'experiments'].includes(activeView.value) ? t('Research', '研究') : t('Lab', '实验室'))

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
    handleError(caught, t('Could not load the control-plane state.', '无法加载控制平面状态。'))
  } finally {
    loading.value = false
  }
}

async function refreshProject(showSpinner = true) {
  const generation = ++projectRefreshGeneration
  const projectID = selectedProjectID.value
  if (!projectID) {
    repositories.value = []
    experiments.value = []
    operationsFeed.value = null
    researchWorkspace.value = null
    selectedStudyID.value = ''
    return
  }
  if (showSpinner) loading.value = true
  error.value = ''
  try {
    const [loadedRepositories, loadedExperiments, loadedOperations, loadedResearch] = await Promise.all([
      api.repositories(projectID),
      api.experiments(projectID),
      api.operations(projectID),
      api.research(projectID, selectedStudyID.value),
    ])
    if (generation !== projectRefreshGeneration || selectedProjectID.value !== projectID) return
    repositories.value = loadedRepositories
    experiments.value = loadedExperiments
    operationsFeed.value = loadedOperations
    researchWorkspace.value = loadedResearch
    if (loadedResearch.study) selectedStudyID.value = loadedResearch.study.id
    else if (loadedResearch.studies.length === 1) selectedStudyID.value = loadedResearch.studies[0].id
  } catch (caught) {
    if (generation !== projectRefreshGeneration) return
    handleError(caught, t('Could not refresh this project.', '无法刷新此 Project。'))
  } finally {
    if (showSpinner && generation === projectRefreshGeneration) loading.value = false
  }
}

async function refreshLive() {
  if (liveRefreshInFlight || document.visibilityState !== 'visible' || !selectedProjectID.value || (!['research', 'experiments'].includes(activeView.value) && !selectedExperiment.value)) return
  liveRefreshInFlight = true
  operationsLoading.value = true
  const projectID = selectedProjectID.value
  try {
    const selectedID = selectedExperiment.value?.id
    const [loadedExperiments, loadedOperations, loadedResearch, detail, history] = await Promise.all([
      api.experiments(projectID),
      api.operations(projectID),
      api.research(projectID, selectedStudyID.value),
      selectedID ? api.experiment(projectID, selectedID) : Promise.resolve(null),
      selectedID ? api.attempts(projectID, selectedID) : Promise.resolve([]),
    ])
    if (selectedProjectID.value !== projectID) return
    experiments.value = loadedExperiments
    operationsFeed.value = loadedOperations
    researchWorkspace.value = loadedResearch
    if (loadedResearch.study) selectedStudyID.value = loadedResearch.study.id
    if (detail && selectedExperiment.value?.id === detail.id) {
      selectedExperiment.value = detail
      attempts.value = history
    }
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('signedOut')
  } finally {
    liveRefreshInFlight = false
    operationsLoading.value = false
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
    else attemptsError.value = caught instanceof APIError ? caught.message : t('Could not load Attempt history.', '无法加载 Attempt 历史。')
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

function openExperimentByID(experimentID: string) {
  const experiment = experiments.value.find((item) => item.id === experimentID)
  if (experiment) void openExperiment(experiment)
}

async function signOut() {
  signingOut.value = true
  error.value = ''
  try {
    await api.logout()
    emit('signedOut')
  } catch (caught) {
    handleError(caught, t('Sign out failed. Try again after the connection recovers.', '退出失败，请在连接恢复后重试。'))
  } finally {
    signingOut.value = false
  }
}

function openCreateStudy() {
  studyForm.name = ''
  studyForm.question = ''
  studyForm.summary = ''
  dialogError.value = ''
  studyDialog.value = true
}

async function selectStudy(studyID: string) {
  selectedStudyID.value = studyID
  if (!selectedProjectID.value) return
  try {
    researchWorkspace.value = await api.research(selectedProjectID.value, studyID)
  } catch (caught) {
    handleError(caught, t('Could not load this Study.', '无法加载此 Study。'))
  }
}

async function createStudy() {
  if (!selectedProject.value) return
  studyBusy.value = true
  dialogError.value = ''
  try {
    const created = await api.updateResearch(selectedProject.value.id, {
      study: { name: studyForm.name.trim(), question: studyForm.question.trim(), summary: studyForm.summary.trim() || undefined },
    })
    researchWorkspace.value = created
    if (created.study) selectedStudyID.value = created.study.id
    studyDialog.value = false
  } catch (caught) {
    dialogError.value = caught instanceof APIError ? caught.message : t('Study creation failed.', 'Study 创建失败。')
  } finally {
    studyBusy.value = false
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
    dialogError.value = caught instanceof APIError ? caught.message : t('Repository registration failed.', '仓库注册失败。')
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
    dialogError.value = caught instanceof APIError ? caught.message : t('Repository verification failed.', '仓库验证失败。')
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
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function stateLabel(value: string) {
  return localizedState(value)
}

const { resume: resumeLive } = useIntervalFn(() => { void refreshLive() }, 5_000, { immediate: false })

onMounted(async () => {
  await refreshAll()
  resumeLive()
})
</script>

<template>
  <div class="app-shell console-shell">
    <aside class="sidebar">
      <div class="brand">
        <BrandMark :size="19" />
        <div><strong>Gemcp</strong><span>{{ t('Research workbench', '研究工作台') }}</span></div>
      </div>
      <nav :aria-label="t('Primary navigation', '主导航')">
        <p class="nav-group">{{ t('Research', '研究') }}</p>
        <button class="nav-item" :class="{ active: activeView === 'research' }" type="button" :aria-label="t('Research', '研究')" :title="t('Research', '研究')" @click="activeView = 'research'"><Network :size="17" /><span>{{ t('Research', '研究') }}</span></button>
        <button class="nav-item" :class="{ active: activeView === 'experiments' }" type="button" :aria-label="t('Evidence', '证据')" :title="t('Evidence', '证据')" @click="activeView = 'experiments'"><FlaskConical :size="17" /><span>{{ t('Evidence', '证据') }}</span></button>
        <p class="nav-group">{{ t('Lab', '实验室') }}</p>
        <button class="nav-item" :class="{ active: activeView === 'diagnostics' }" type="button" :aria-label="t('Diagnostics', '诊断')" :title="t('Diagnostics', '诊断')" @click="activeView = 'diagnostics'"><Stethoscope :size="17" /><span>{{ t('Diagnostics', '诊断') }}</span></button>
        <button class="nav-item" :class="{ active: activeView === 'finance' }" type="button" :aria-label="t('Finance', '财务')" :title="t('Finance', '财务')" @click="activeView = 'finance'"><WalletCards :size="17" /><span>{{ t('Finance', '财务') }}</span></button>
        <button class="nav-item" :class="{ active: activeView === 'projects' }" type="button" aria-label="Project" title="Project" @click="activeView = 'projects'"><Boxes :size="17" /><span>Project</span></button>
        <button class="nav-item" :class="{ active: activeView === 'agents' }" type="button" :aria-label="t('Agents', 'Agent')" :title="t('Agents', 'Agent')" @click="activeView = 'agents'"><Bot :size="17" /><span>Agent</span></button>
        <button v-if="runtimeStatus?.self_hosted_enabled" class="nav-item" :class="{ active: activeView === 'nodes' }" type="button" :aria-label="t('Nodes', '节点')" :title="t('Nodes', '节点')" @click="activeView = 'nodes'"><Cpu :size="17" /><span>{{ t('Nodes', '节点') }}</span></button>
        <button class="nav-item" :class="{ active: activeView === 'provider' }" type="button" aria-label="Provider" title="Provider" @click="activeView = 'provider'"><Server :size="17" /><span>Provider</span></button>
        <button class="nav-item" :class="{ active: activeView === 'notifications' }" type="button" :aria-label="t('Alerts', '告警')" :title="t('Alerts', '告警')" @click="activeView = 'notifications'"><Bell :size="17" /><span>{{ t('Alerts', '告警') }}</span></button>
        <button class="nav-item nav-bottom" type="button" :aria-label="t('Sign out', '退出登录')" :title="t('Sign out', '退出登录')" :disabled="signingOut" @click="signOut"><LogOut :size="17" /><span>{{ t('Sign out', '退出登录') }}</span></button>
      </nav>
      <div class="sidebar-user"><span>{{ props.user.email }}</span><small>{{ props.user.role }}</small></div>
    </aside>

    <main class="console-main">
      <header class="topbar">
        <div>
          <p class="eyebrow">{{ viewEyebrow }}</p>
          <h1>{{ viewTitle }}</h1>
        </div>
        <div class="topbar-actions">
          <label v-if="!['finance', 'nodes', 'provider', 'notifications'].includes(activeView)" class="project-select"><span>Project</span><WorkbenchSelect v-model="selectedProjectID" :aria-label="t('Project', 'Project')" :options="projectOptions" @update:model-value="() => { void refreshProject() }" /></label>
          <span class="status online"><span class="status-dot" />{{ t('Online', '在线') }}</span>
          <LanguageToggle />
          <button v-if="!['finance', 'nodes', 'provider', 'notifications'].includes(activeView)" class="icon-button" type="button" :title="t('Refresh project', '刷新 Project')" :disabled="loading" @click="refreshAll"><RefreshCw :size="17" :class="{ spinning: loading }" /></button>
        </div>
      </header>

      <div v-if="error" class="page-alert" role="alert">{{ error }}<button type="button" :title="t('Dismiss', '关闭')" @click="error = ''"><X :size="16" /></button></div>

      <ResearchView v-if="activeView === 'research'" :workspace="researchWorkspace" :loading="loading" :selected-study-id="selectedStudyID" :project="selectedProject" :repositories="repositories" :experiments="experiments" @select-study="selectStudy" @open-experiment="openExperimentByID" @create-study="openCreateStudy" />

      <section v-else-if="activeView === 'experiments'" class="page-workspace">
        <div class="section-heading page-section-heading"><div><h2>{{ t('Evidence', '证据') }}</h2><p>{{ t('Linked Experiments remain the execution evidence behind the Graph.', '关联的 Experiment 仍是 Graph 背后的执行证据。') }}</p></div><div class="segmented-control" :aria-label="t('Experiment state filter', '实验状态筛选')"><button v-for="filter in ['all', 'queued', 'running', 'succeeded', 'failed']" :key="filter" type="button" :class="{ active: stateFilter === filter }" @click="stateFilter = filter">{{ filter === 'all' ? t('all', '全部') : stateLabel(filter) }}</button></div></div>
        <div class="experiment-operations"><RunActivityPanel :feed="operationsFeed" :loading="operationsLoading" @open-experiment="openExperimentByID" /></div>
        <ExperimentTable :experiments="filteredExperiments" @select="openExperiment" />
      </section>

      <DiagnosticsView v-else-if="activeView === 'diagnostics'" :active="true" :project="selectedProject" @unauthorized="emit('signedOut')" />

      <section v-else-if="activeView === 'projects'" class="page-workspace project-workspace">
        <div class="section-heading page-section-heading"><div><h2>{{ selectedProject?.name ?? 'Project' }}</h2><p>{{ selectedProject?.slug }} / {{ selectedProject?.status }}</p></div><span class="version-chip">{{ selectedProject?.timezone }}</span></div>
        <div v-if="selectedProject" class="policy-grid">
          <div><span>{{ t('Monthly budget', '月度预算') }}</span><strong>{{ money(selectedProject.monthly_budget_milli) }}</strong></div>
          <div><span>{{ t('Experiment cap', '实验上限') }}</span><strong>{{ money(selectedProject.max_experiment_milli) }}</strong></div>
          <div><span>{{ t('Concurrency', '并发数') }}</span><strong>{{ selectedProject.max_concurrency }}</strong></div>
          <div><span>{{ t('Maximum runtime', '最长运行时间') }}</span><strong>{{ Math.round(selectedProject.max_runtime_seconds / 3600) }} h</strong></div>
        </div>
        <div class="subsection-heading"><div><h2>{{ t('Private repositories', '私有仓库') }}</h2><p>{{ t('Read-only Deploy Keys and pinned GitHub host identity.', '只读 Deploy Key 和固定的 GitHub 主机身份。') }}</p></div><button class="primary-button small-button" type="button" @click="openCreateRepository"><Plus :size="16" />{{ t('Register repository', '注册仓库') }}</button></div>
        <div v-if="repositories.length" class="table-scroll">
          <table class="data-table repository-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Name', '名称') }}</th><th>SSH URL</th><th>{{ t('Branch', '分支') }}</th><th>{{ t('Verified', '已验证') }}</th><th>{{ t('Actions', '操作') }}</th></tr></thead>
            <tbody><tr v-for="repository in repositories" :key="repository.id"><td><span class="state-badge" :data-state="repository.status"><span />{{ stateLabel(repository.status) }}</span></td><td>{{ repository.name }}</td><td><code>{{ repository.ssh_url }}</code></td><td><code>{{ repository.default_branch }}</code></td><td>{{ dateTime(repository.last_verified_at) }}</td><td><div class="table-actions"><button class="icon-button" type="button" :title="t('View Deploy public key', '查看 Deploy 公钥')" @click="openKey(repository)"><KeyRound :size="16" /></button><button class="icon-button" type="button" :title="t('Verify repository', '验证仓库')" @click="openVerify(repository)"><ShieldCheck :size="16" /></button></div></td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><GitBranch :size="21" /></span><h3>{{ t('No repositories registered', '尚未注册仓库') }}</h3><p>{{ t('Register the private GitHub repository used by the first experiment.', '注册首个实验使用的私有 GitHub 仓库。') }}</p></div>
      </section>

      <FinanceView v-if="activeView === 'finance'" :active="true" :projects="projects" @unauthorized="emit('signedOut')" />
      <AgentView v-if="activeView === 'agents'" :active="true" :project="selectedProject" @unauthorized="emit('signedOut')" />
      <NodeView v-if="activeView === 'nodes'" :active="true" :projects="projects" :build="props.build" @unauthorized="emit('signedOut')" />
      <div v-show="activeView === 'provider'" class="persistent-view"><ProviderView :active="activeView === 'provider'" @unauthorized="emit('signedOut')" /></div>
      <NotificationView v-if="activeView === 'notifications'" :active="true" @unauthorized="emit('signedOut')" />

      <footer class="console-footer"><span>{{ props.build?.name ?? 'Gemcp' }} {{ props.build?.version ?? 'dev' }}</span><span>Commit {{ props.build?.commit ?? t('unknown', '未知') }}</span><span>{{ t('Operational estimates only', '仅为运行估算') }}</span></footer>
    </main>
  </div>

  <ExperimentDetail v-if="selectedExperiment" :experiment="selectedExperiment" :attempts="attempts" :loading="attemptsLoading" :error="attemptsError" @close="closeExperiment" />

  <WorkbenchDialog v-model:open="studyDialog" :title="t('Create study', '创建 Study')" :label="t('Create study', '创建 Study')" :description="t('Name the scientific question. This does not start a workload.', '先写下科学问题。这不会启动任何 workload。')">
    <form class="dialog-form" @submit.prevent="createStudy">
      <label>{{ t('Name', '名称') }}<input v-model="studyForm.name" required maxlength="80" placeholder="objbg-scan" spellcheck="false" /></label>
      <label>{{ t('Research question', '研究问题') }}<textarea v-model="studyForm.question" required rows="4" maxlength="400" :placeholder="t('What should this Study answer?', '这个 Study 要回答什么问题？')"></textarea></label>
      <label>{{ t('Summary', '摘要') }}<textarea v-model="studyForm.summary" rows="3" maxlength="800" :placeholder="t('Optional scientific context. No prompts or credentials.', '可选科学背景。不要写 prompt 或凭据。')"></textarea></label>
      <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
      <button class="primary-button" type="submit" :disabled="studyBusy"><LoaderCircle v-if="studyBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ t('Create study', '创建 Study') }}</button>
    </form>
  </WorkbenchDialog>

  <div v-if="repositoryDialog" class="modal-backdrop" @click.self="repositoryDialog = null">
    <section class="modal" role="dialog" aria-modal="true" :aria-label="repositoryDialog === 'create' ? t('Register repository', '注册仓库') : repositoryDialog === 'verify' ? t('Verify repository', '验证仓库') : t('Deploy public key', 'Deploy 公钥')">
      <header><div><p class="eyebrow">{{ t('Private Git', '私有 Git') }}</p><h2>{{ repositoryDialog === 'create' ? t('Register repository', '注册仓库') : repositoryDialog === 'verify' ? t('Verify host and access', '验证主机和访问') : t('Deploy public key', 'Deploy 公钥') }}</h2></div><button class="icon-button" type="button" :title="t('Close', '关闭')" @click="repositoryDialog = null"><X :size="17" /></button></header>
      <form v-if="repositoryDialog === 'create'" class="dialog-form" @submit.prevent="createRepository">
        <label>{{ t('Name', '名称') }}<input v-model="repositoryForm.name" required /></label>
        <label>GitHub SSH URL<input v-model="repositoryForm.sshURL" placeholder="git@github.com:owner/repository.git" required spellcheck="false" /></label>
        <label>{{ t('Default branch', '默认分支') }}<input v-model="repositoryForm.defaultBranch" required spellcheck="false" /></label>
        <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
        <button class="primary-button" type="submit" :disabled="repositoryBusy"><LoaderCircle v-if="repositoryBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ t('Generate Deploy Key', '生成 Deploy Key') }}</button>
      </form>
      <div v-else-if="repositoryDialog === 'key' && selectedRepository" class="key-panel">
        <p>{{ t('Add this public key to', '将此公钥添加到') }} <strong>{{ selectedRepository.name }}</strong>，{{ t('as a read-only GitHub Deploy Key.', '设为只读 GitHub Deploy Key。') }}</p>
        <div class="code-box"><code>{{ selectedRepository.deploy_public_key }}</code><button class="icon-button" type="button" :title="copied === 'deploy-key' ? t('Copied', '已复制') : t('Copy Deploy public key', '复制 Deploy 公钥')" @click="copy(selectedRepository.deploy_public_key ?? '', 'deploy-key')"><Check v-if="copied === 'deploy-key'" :size="16" /><Clipboard v-else :size="16" /></button></div>
        <button class="primary-button" type="button" @click="openVerify(selectedRepository)"><ShieldCheck :size="16" />{{ t('Continue to verification', '继续验证') }}</button>
      </div>
      <form v-else-if="selectedRepository" class="dialog-form" @submit.prevent="verifyRepository">
        <div class="repository-summary"><GitBranch :size="17" /><span><strong>{{ selectedRepository.name }}</strong><small>{{ selectedRepository.ssh_url }}</small></span></div>
        <label>{{ t('Trusted GitHub host-key fingerprint', '可信 GitHub 主机密钥指纹') }}<input v-model="repositoryForm.fingerprint" placeholder="SHA256:..." required spellcheck="false" /></label>
        <p class="form-note">{{ t("Use the current fingerprint from GitHub's official HTTPS documentation, not an unverified key scan.", '请使用 GitHub 官方 HTTPS 文档中的当前指纹，不要使用未经验证的密钥扫描结果。') }}</p>
        <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
        <button class="primary-button" type="submit" :disabled="repositoryBusy"><LoaderCircle v-if="repositoryBusy" :size="16" class="spinning" /><ShieldCheck v-else :size="16" />{{ t('Verify repository', '验证仓库') }}</button>
      </form>
    </section>
  </div>
</template>
