<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import {
  Bell,
  Bot,
  Boxes,
  Check,
  Clipboard,
  Cpu,
  FileText,
  FlaskConical,
  GitBranch,
  Images,
  KeyRound,
  LoaderCircle,
  LogOut,
  Network,
  Plus,
  RefreshCw,
  Server,
  ShieldCheck,
  Stethoscope,
  TriangleAlert,
  WalletCards,
  X,
} from '@lucide/vue'
import { APIError, api, githubDeployKeySettingsURL, githubFineGrainedTokenSettingsURL, type AgentReadiness, type Attempt, type BuildInfo, type DatasetBinding, type DatasetCatalogEntry, type Experiment, type ExperimentCatalog, type OperationsFeed, type PreparedProposal, type Project, type ProjectEnvironment, type ProjectWorkload, type ProposalActivity, type Repository, type RepositoryReadiness, type ResearchPlanSyncExport, type ResearchWorkspace, type RuntimeStatus, type User } from '../api'
import { localizedState, useI18n } from '../i18n'
import { pickDefaultProjectID } from '../projectSelect'
import { draftStudyFromRepository, repositoryNameFromSSHURL } from '../studyImport'
import BrandMark from './BrandMark.vue'
import ExperimentTable from './ExperimentTable.vue'
import FinanceView from './FinanceView.vue'
import LanguageToggle from './LanguageToggle.vue'
import ProviderView from './ProviderView.vue'
import NotificationView from './NotificationView.vue'
import AgentView from './AgentView.vue'
import NodeView from './NodeView.vue'
import BakeView from './BakeView.vue'
import DiagnosticsView from './DiagnosticsView.vue'
import ExperimentDetail from './ExperimentDetail.vue'
import RepositoryReadinessPanel from './RepositoryReadinessPanel.vue'
import ResearchView from './ResearchView.vue'
import RunActivityPanel from './RunActivityPanel.vue'
import WorkbenchDialog from './WorkbenchDialog.vue'
import WorkbenchSelect from './WorkbenchSelect.vue'

const props = defineProps<{ build: BuildInfo | null; user: User }>()
const emit = defineEmits<{ signedOut: [] }>()

type ViewName = 'research' | 'experiments' | 'diagnostics' | 'images' | 'finance' | 'projects' | 'agents' | 'nodes' | 'provider' | 'notifications'
const activeView = ref<ViewName>('research')
const projects = ref<Project[]>([])
const selectedProjectID = ref('')
const selectedStudyID = ref('')
const repositories = ref<Repository[]>([])
const experiments = ref<Experiment[]>([])
const runtimeStatus = ref<RuntimeStatus | null>(null)
const operationsFeed = ref<OperationsFeed | null>(null)
const researchWorkspace = ref<ResearchWorkspace | null>(null)
const experimentCatalog = ref<ExperimentCatalog | null>(null)
const hasActiveAgent = ref(false)
const agentReadiness = ref<AgentReadiness | null>(null)
const repositoryReadiness = ref<RepositoryReadiness | null>(null)
const repositoryReadinessLoading = ref(false)
const studyDialog = ref(false)
const studyBusy = ref(false)
const studyForm = reactive({
  name: '', question: '', summary: '', importSource: 'url', sshURL: '', defaultBranch: 'main',
  protocolBranch: '', protocolDocPath: '', codeRefPattern: '',
})
const planSyncDialog = ref(false)
const planSyncBusy = ref(false)
const planSyncExport = ref<ResearchPlanSyncExport | null>(null)
const planSyncError = ref('')
const operationsLoading = ref(false)
const loading = ref(false)
const error = ref('')
const signingOut = ref(false)
const selectedExperiment = ref<Experiment | null>(null)
const pendingCloseRun = ref(false)
const attempts = ref<Attempt[]>([])
const attemptsLoading = ref(false)
const attemptsError = ref('')
const stateFilter = ref('all')
const repositoryDialog = ref<'create' | 'key' | null>(null)
const selectedRepository = ref<Repository | null>(null)
const repositoryBusy = ref(false)
const dialogError = ref('')
const copied = ref('')
const repositoryForm = reactive({ name: '', sshURL: '', defaultBranch: 'main', httpsToken: '' })
const policyForm = reactive({ monthlyBudgetCNY: '' })
const policyBusy = ref(false)
const datasetBindings = ref<DatasetBinding[]>([])
const datasetSources = ref<DatasetCatalogEntry[]>([])
const datasetForm = reactive({ catalog: '', name: '', backend: 'autodl_elastic' as DatasetBinding['backend'], canonicalRoot: '/root/autodl-fs/datasets/', requiredMarkers: '', sources: '' })
const datasetBusy = ref(false)
const environments = ref<ProjectEnvironment[]>([])
const projectWorkloads = ref<ProjectWorkload[]>([])
const environmentForm = reactive({ name: '', backend: 'autodl_elastic', imageUUID: '', setDefault: false })
const environmentBusy = ref(false)
const confirmationTarget = ref<ProposalActivity | null>(null)
const confirmationChecked = ref(false)
const confirmationBusy = ref(false)
const confirmationError = ref('')
const prepareBusy = ref(false)
const prepareError = ref('')
const prepareForm = reactive({
  mode: 'argv' as 'argv' | 'workload' | 'provision',
  argv: 'python\ntools/smoke.py',
  workload: '',
  parameters: '',
  ref: '',
  fromNodeID: '',
  expectedMetric: '',
  dataset: '',
  runtimePreset: 'smoke',
  installDependencies: false,
})
const attemptedVerify = new Set<string>()
const { locale, languageTag, t } = useI18n()
const studyImportOptions = computed(() => {
  const options = repositories.value.map((repository) => ({
    value: repository.id,
    label: `${repository.name} [${repository.status}]`,
  }))
  options.push({ value: 'url', label: t('Paste a GitHub URL', '粘贴 GitHub URL') })
  options.push({ value: 'blank', label: t('Start from a question only', '只写研究问题') })
  return options
})
const importingExistingRepository = computed(() => studyForm.importSource !== 'url' && studyForm.importSource !== 'blank')
const importingNewRepository = computed(() => studyForm.importSource === 'url')
const selectedDeployKeyURL = computed(() => githubDeployKeySettingsURL(
  selectedRepository.value?.ssh_url,
  selectedRepository.value?.deploy_key_settings_url || repositoryReadiness.value?.deploy_key_settings_url,
))
const prepareOriginOptions = computed(() => {
  const options = [{ value: '', label: t('No Graph origin', '无 Graph 起点') }]
  for (const node of researchWorkspace.value?.study?.nodes ?? []) {
    if (node.kind === 'hypothesis' || node.kind === 'plan') {
      options.push({ value: node.id, label: `${node.kind} · ${node.title}` })
    }
  }
  return options
})
const confirmationDialogOpen = computed({
  get: () => confirmationTarget.value !== null,
  set: (open: boolean) => {
    if (!open && !confirmationBusy.value) closeProposalConfirmation()
  },
})
let liveRefreshInFlight = false
let projectRefreshGeneration = 0
const projectOptions = computed(() => projects.value.map((project) => ({ value: project.id, label: project.name })))

const selectedProject = computed(() => projects.value.find((project) => project.id === selectedProjectID.value) ?? null)
const filteredExperiments = computed(() => stateFilter.value === 'all' ? experiments.value : experiments.value.filter((item) => item.state === stateFilter.value))
const viewTitle = computed(() => ({
  research: t('Research', '研究'),
  experiments: t('Evidence', '证据'),
  diagnostics: t('Diagnostics', '诊断'),
  images: t('Images', '镜像'),
  finance: t('Budget and ledger', '预算与账本'),
  projects: t('Project configuration', 'Project 配置'),
  agents: t('Agent access', 'Agent 访问'),
  nodes: t('Self-hosted nodes', '自托管节点'),
  provider: t('AutoDL resources', 'AutoDL 资源'),
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
    selectedProjectID.value = pickDefaultProjectID(loadedProjects, selectedProjectID.value)
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
    experimentCatalog.value = null
    hasActiveAgent.value = false
    agentReadiness.value = null
    repositoryReadiness.value = null
    datasetBindings.value = []
    datasetSources.value = []
    environments.value = []
    projectWorkloads.value = []
    selectedStudyID.value = ''
    return
  }
  if (showSpinner) loading.value = true
  error.value = ''
  try {
    const readinessRequest = api.agentReadiness(projectID).catch((caught) => {
      if (caught instanceof APIError && caught.status === 401) throw caught
      return null
    })
    const [loadedRepositories, loadedExperiments, loadedOperations, loadedResearch, loadedCatalog, loadedAgents, loadedReadiness, loadedBindings, loadedSources, loadedEnvironments, loadedWorkloads] = await Promise.all([
      api.repositories(projectID),
      api.experiments(projectID),
      api.operations(projectID),
      api.research(projectID, selectedStudyID.value),
      api.experimentCatalog(projectID).catch((caught) => {
        if (caught instanceof APIError && caught.status === 401) throw caught
        return { project_id: projectID, repositories: [], generated_at: new Date().toISOString() }
      }),
      api.agentTokens(projectID),
      readinessRequest,
      api.datasetBindings(projectID),
      api.datasetSources(projectID).catch(() => []),
      api.environments(projectID).catch(() => []),
      api.projectWorkloads(projectID).catch((caught) => {
        if (caught instanceof APIError && caught.status === 401) throw caught
        return []
      }),
    ])
    if (generation !== projectRefreshGeneration || selectedProjectID.value !== projectID) return
    repositories.value = loadedRepositories
    experiments.value = loadedExperiments
    operationsFeed.value = loadedOperations
    researchWorkspace.value = loadedResearch
    experimentCatalog.value = loadedCatalog
    hasActiveAgent.value = (loadedAgents.tokens ?? []).some((token) => token.status === 'active')
    agentReadiness.value = loadedReadiness
    datasetBindings.value = loadedBindings
    datasetSources.value = loadedSources
    environments.value = loadedEnvironments
    projectWorkloads.value = loadedWorkloads
    await loadRepositoryReadiness(loadedRepositories[0], projectID, generation)
    if (loadedResearch.study) selectedStudyID.value = loadedResearch.study.id
    else if (loadedResearch.studies.length) selectedStudyID.value = loadedResearch.studies[0].id
    if (showSpinner) await verifyPendingGitHubRepositories(generation, projectID)
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
    const [loadedExperiments, loadedOperations, loadedResearch, loadedCatalog, detail, history] = await Promise.all([
      api.experiments(projectID),
      api.operations(projectID),
      api.research(projectID, selectedStudyID.value),
      api.experimentCatalog(projectID).catch((caught) => {
        if (caught instanceof APIError && caught.status === 401) throw caught
        return experimentCatalog.value ?? { project_id: projectID, repositories: [], generated_at: new Date().toISOString() }
      }),
      selectedID ? api.experiment(projectID, selectedID) : Promise.resolve(null),
      selectedID ? api.attempts(projectID, selectedID) : Promise.resolve([]),
    ])
    if (selectedProjectID.value !== projectID) return
    experiments.value = loadedExperiments
    operationsFeed.value = loadedOperations
    researchWorkspace.value = loadedResearch
    experimentCatalog.value = loadedCatalog
    if (loadedResearch.study) selectedStudyID.value = loadedResearch.study.id
    if (detail && selectedExperiment.value?.id === detail.id) {
      selectedExperiment.value = detail
      attempts.value = Array.isArray(detail.attempts) ? detail.attempts : history
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
      attempts.value = Array.isArray(detail.attempts) ? detail.attempts : history
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
  pendingCloseRun.value = false
  attempts.value = []
  attemptsError.value = ''
  attemptsLoading.value = false
}

function openCloseRun(experimentID: string) {
  pendingCloseRun.value = true
  openExperimentByID(experimentID)
}

async function onRunClosed() {
  pendingCloseRun.value = false
  if (selectedExperiment.value) {
    selectedExperiment.value = { ...selectedExperiment.value, closable_run: false }
  }
  if (selectedProjectID.value) {
    try {
      researchWorkspace.value = await api.research(selectedProjectID.value, selectedStudyID.value)
    } catch {
      // Keep the Evidence dialog; the Graph refresh is best-effort.
    }
  }
}

function onWorkloadSaved(workload: ProjectWorkload) {
  projectWorkloads.value = [...projectWorkloads.value.filter((item) => item.id !== workload.id && item.name !== workload.name), workload]
    .sort((left, right) => left.name.localeCompare(right.name))
  if (selectedExperiment.value) {
    selectedExperiment.value = {
      ...selectedExperiment.value,
      savable_workload: false,
      saved_workload: workload.name,
    }
  }
}

function openExperimentByID(experimentID: string) {
  const experiment = experiments.value.find((item) => item.id === experimentID)
  if (experiment) void openExperiment(experiment)
}

function activityFromPrepared(prepared: PreparedProposal): ProposalActivity {
  return {
    id: prepared.id,
    status: 'prepared',
    eligible: prepared.eligible,
    agent_label: 'Owner',
    agent_token_prefix: '',
    repository_name: prepared.repository.name,
    requested_ref: prepared.repository.requested_ref,
    commit_sha: prepared.repository.commit_sha,
    display_command: prepared.execution.display_command,
    backend: prepared.resource.backend,
    environment_name: prepared.resource.environment_name,
    image: prepared.resource.image,
    resource_profile_name: prepared.resource.resource_profile_name,
    gpu_models: prepared.resource.gpu_models ?? [],
    gpu_num: prepared.resource.gpu_num,
    runtime_preset: prepared.runtime_preset,
    max_runtime_seconds: prepared.max_runtime_seconds,
    reserved_cost_milli: prepared.reserved_cost_milli,
    checks: prepared.checks,
    confirmation_digest: prepared.confirmation_digest,
    from_node_id: prepared.from_node_id,
    expected_metric: prepared.expected_metric,
    dataset: prepared.dataset,
    workload: prepared.workload,
    parameters: prepared.parameters,
    repository_access: prepared.repository.access,
    repository_url: prepared.repository.ssh_url,
    working_directory: prepared.resource.working_directory,
    install_dependencies: prepared.install_dependencies,
    requirements_file: prepared.requirements_file,
    created_at: prepared.created_at,
    updated_at: prepared.created_at,
    expires_at: prepared.expires_at,
  }
}

function parsePrepareLines(value: string): string[] {
  return value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
}

function parsePrepareParameters(value: string): Record<string, string> {
  const parameters: Record<string, string> = {}
  for (const line of parsePrepareLines(value)) {
    const separator = line.indexOf('=')
    if (separator <= 0) continue
    parameters[line.slice(0, separator).trim()] = line.slice(separator + 1).trim()
  }
  return parameters
}

async function prepareOwnerProposal() {
  const projectID = selectedProjectID.value
  if (!projectID) return
  prepareBusy.value = true
  prepareError.value = ''
  try {
    const payload: Parameters<typeof api.prepareProposal>[1] = {
      ref: prepareForm.ref.trim() || undefined,
      from_node_id: prepareForm.fromNodeID || undefined,
      expected_metric: prepareForm.expectedMetric.trim() || undefined,
      dataset: prepareForm.dataset.trim() || undefined,
      runtime_preset: prepareForm.runtimePreset,
      install_dependencies: prepareForm.installDependencies || undefined,
    }
    if (prepareForm.mode === 'provision') {
      payload.runtime_preset = 'provision'
    } else if (prepareForm.mode === 'workload') {
      payload.workload = prepareForm.workload.trim()
      const parameters = parsePrepareParameters(prepareForm.parameters)
      if (Object.keys(parameters).length) payload.parameters = parameters
    } else {
      payload.argv = parsePrepareLines(prepareForm.argv)
    }
    const result = await api.prepareProposal(projectID, payload)
    if (result.choice_required?.length) {
      prepareError.value = t(
        `Choose one ${result.choice_required[0].field}: ${result.choice_required.map((item) => item.name).join(', ')}`,
        `请选择一个 ${result.choice_required[0].field}：${result.choice_required.map((item) => item.name).join('、')}`,
      )
      return
    }
    if (!result.proposal) {
      prepareError.value = t('Preparation did not return a proposal.', '准备未返回提案。')
      return
    }
    const activity = activityFromPrepared(result.proposal)
    if (operationsFeed.value) {
      operationsFeed.value = {
        ...operationsFeed.value,
        proposals: [activity, ...operationsFeed.value.proposals.filter((item) => item.id !== activity.id)],
      }
    } else {
      operationsFeed.value = { activities: [], proposals: [activity], generated_at: activity.created_at }
    }
    openProposalConfirmation(activity)
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('signedOut')
    else prepareError.value = caught instanceof APIError ? caught.message : t('Could not prepare this experiment.', '无法准备此实验。')
  } finally {
    prepareBusy.value = false
  }
}

function openProposalConfirmation(proposal: ProposalActivity) {
  confirmationTarget.value = proposal
  confirmationChecked.value = false
  confirmationError.value = ''
}

function closeProposalConfirmation() {
  confirmationTarget.value = null
  confirmationChecked.value = false
  confirmationError.value = ''
}

async function submitProposalConfirmation() {
  const projectID = selectedProjectID.value
  const proposal = confirmationTarget.value
  if (!projectID || !proposal || !confirmationChecked.value) return
  confirmationBusy.value = true
  confirmationError.value = ''
  try {
    const result = await api.submitPreparedProposal(projectID, proposal.id, {
      confirmation_digest: proposal.confirmation_digest,
      confirmed: true,
    })
    if (operationsFeed.value) {
      operationsFeed.value = {
        ...operationsFeed.value,
        proposals: operationsFeed.value.proposals.map((item) => item.id === proposal.id ? {
          ...item,
          status: 'submitted',
          experiment_id: result.experiment.id,
          updated_at: result.experiment.updated_at,
        } : item),
      }
    }
    const existing = experiments.value.findIndex((item) => item.id === result.experiment.id)
    if (existing >= 0) experiments.value.splice(existing, 1, result.experiment)
    else experiments.value.unshift(result.experiment)
    closeProposalConfirmation()
    await openExperiment(result.experiment)
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('signedOut')
    else confirmationError.value = caught instanceof APIError ? caught.message : t('Could not submit this prepared proposal.', '无法提交此准备提案。')
  } finally {
    confirmationBusy.value = false
  }
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

function applyStudyDraft(input: { name: string; ssh_url: string; default_branch?: string }) {
  const draft = draftStudyFromRepository(input, locale.value)
  studyForm.name = draft.name
  studyForm.question = draft.question
  studyForm.summary = draft.summary
}

function onStudyImportSourceChange(value: string) {
  studyForm.importSource = value
  if (value === 'blank') {
    studyForm.sshURL = ''
    return
  }
  if (value === 'url') {
    if (studyForm.sshURL) applyStudyDraft({ name: repositoryNameFromSSHURL(studyForm.sshURL), ssh_url: studyForm.sshURL, default_branch: studyForm.defaultBranch })
    return
  }
  const repository = repositories.value.find((item) => item.id === value)
  if (repository) applyStudyDraft(repository)
}

function onStudySSHURLInput() {
  const name = repositoryNameFromSSHURL(studyForm.sshURL)
  if (name) applyStudyDraft({ name, ssh_url: studyForm.sshURL, default_branch: studyForm.defaultBranch })
}

function openCreateStudy() {
  studyForm.name = ''
  studyForm.question = ''
  studyForm.summary = ''
  studyForm.sshURL = ''
  studyForm.defaultBranch = 'main'
  studyForm.protocolBranch = ''
  studyForm.protocolDocPath = ''
  studyForm.codeRefPattern = ''
  studyForm.importSource = repositories.value[0]?.id ?? 'url'
  if (repositories.value[0]) applyStudyDraft(repositories.value[0])
  dialogError.value = ''
  studyDialog.value = true
}

async function openPlanSync() {
  if (!selectedProject.value) return
  planSyncDialog.value = true
  planSyncError.value = ''
  planSyncBusy.value = true
  try {
    planSyncExport.value = await api.exportPlanSync(selectedProject.value.id, {
      study_id: selectedStudyID.value || undefined,
      dry_run: true,
    })
  } catch (caught) {
    planSyncExport.value = null
    planSyncError.value = caught instanceof APIError ? caught.message : t('Plan sync export failed.', '计划同步导出失败。')
  } finally {
    planSyncBusy.value = false
  }
}

async function recordPlanSync() {
  if (!selectedProject.value) return
  planSyncBusy.value = true
  planSyncError.value = ''
  try {
    planSyncExport.value = await api.exportPlanSync(selectedProject.value.id, {
      study_id: selectedStudyID.value || undefined,
      dry_run: false,
    })
  } catch (caught) {
    planSyncError.value = caught instanceof APIError ? caught.message : t('Plan sync export failed.', '计划同步导出失败。')
  } finally {
    planSyncBusy.value = false
  }
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
    let repositoryID = importingExistingRepository.value ? studyForm.importSource : ''
    let pendingKey: Repository | null = null
    if (importingNewRepository.value) {
      const sshURL = studyForm.sshURL.trim()
      if (!repositoryNameFromSSHURL(sshURL)) {
        dialogError.value = t('Use a GitHub HTTPS or SSH URL like https://github.com/owner/repository.', '请使用 https://github.com/owner/repository 或 git@github.com:owner/repository.git。')
        return
      }
      const createdRepository = await api.createRepository({
        project_id: selectedProject.value.id,
        name: studyForm.name.trim() || repositoryNameFromSSHURL(sshURL),
        ssh_url: sshURL,
        default_branch: studyForm.defaultBranch.trim() || 'main',
      })
      repositoryID = createdRepository.id
      pendingKey = createdRepository.status === 'pending_key' ? createdRepository : null
      await refreshProject(false)
    }
    const created = await api.updateResearch(selectedProject.value.id, {
      study: {
        name: studyForm.name.trim(),
        question: studyForm.question.trim(),
        summary: studyForm.summary.trim() || undefined,
        repository_id: repositoryID || undefined,
        protocol_branch: studyForm.protocolBranch.trim() || undefined,
        protocol_doc_path: studyForm.protocolDocPath.trim() || undefined,
        code_ref_pattern: studyForm.codeRefPattern.trim() || undefined,
      },
    })
    researchWorkspace.value = created
    if (created.study) selectedStudyID.value = created.study.id
    studyDialog.value = false
    const bound = pendingKey ?? repositories.value.find((item) => item.id === repositoryID)
    if (bound?.status === 'pending_key') {
      selectedRepository.value = bound
      const verified = await tryVerifyRepository(bound)
      repositoryDialog.value = verified?.status === 'active' ? null : 'key'
    }
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
      name: repositoryForm.name.trim() || undefined,
      url: repositoryForm.sshURL.trim(),
      default_branch: repositoryForm.defaultBranch.trim(),
    })
    selectedRepository.value = created
    const verified = await tryVerifyRepository(created)
    repositoryDialog.value = verified?.status === 'active' && created.access === 'public_https' ? null : 'key'
    await refreshProject(false)
    await loadRepositoryReadiness(verified ?? created)
  } catch (caught) {
    dialogError.value = caught instanceof APIError ? caught.message : t('Repository registration failed.', '仓库注册失败。')
  } finally {
    repositoryBusy.value = false
  }
}

function openKey(repository: Repository) {
  selectedRepository.value = repository
  repositoryForm.httpsToken = ''
  dialogError.value = ''
  repositoryDialog.value = 'key'
  void loadRepositoryReadiness(repository)
}

function repositoryAccessLabel(access?: string) {
  if (access === 'public_https') return t('Public HTTPS', '公开 HTTPS')
  if (access === 'https_token') return t('HTTPS token', 'HTTPS 令牌')
  return t('Deploy Key', 'Deploy Key')
}

async function tryVerifyRepository(repository: Repository, silent = false, httpsToken = '') {
  attemptedVerify.add(repository.id)
  try {
    const verified = await api.verifyRepository(repository.id, '', httpsToken)
    repositories.value = repositories.value.map((item) => item.id === verified.id ? verified : item)
    if (selectedRepository.value?.id === verified.id) selectedRepository.value = verified
    return verified
  } catch (caught) {
    if (!silent && selectedRepository.value?.id === repository.id) {
      dialogError.value = caught instanceof APIError ? caught.message : t('Repository verification failed.', '仓库验证失败。')
    }
    return null
  }
}

async function loadRepositoryReadiness(repository?: Repository | null, projectID = selectedProjectID.value, generation = projectRefreshGeneration) {
  if (!projectID || !repository) {
    if (generation === projectRefreshGeneration) repositoryReadiness.value = null
    return
  }
  repositoryReadinessLoading.value = true
  try {
    const report = await api.repositoryReadiness(repository.id, projectID)
    if (generation !== projectRefreshGeneration || selectedProjectID.value !== projectID) return
    repositoryReadiness.value = report
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) throw caught
    if (generation === projectRefreshGeneration && selectedProjectID.value === projectID) repositoryReadiness.value = null
  } finally {
    if (generation === projectRefreshGeneration) repositoryReadinessLoading.value = false
  }
}

async function verifyPendingGitHubRepositories(generation: number, projectID: string) {
  const pending = repositories.value.filter((item) => item.status === 'pending_key' && !attemptedVerify.has(item.id))
  for (const repository of pending) {
    const verified = await tryVerifyRepository(repository, true)
    if (generation !== projectRefreshGeneration || selectedProjectID.value !== projectID) return
    if (verified?.status === 'active' && repositoryDialog.value && selectedRepository.value?.id === verified.id) {
      repositoryDialog.value = null
    }
  }
}

async function verifyRepository(repository = selectedRepository.value) {
  if (!repository) return
  selectedRepository.value = repository
  repositoryBusy.value = true
  dialogError.value = ''
  try {
    const verified = await tryVerifyRepository(repository)
    if (verified?.status === 'active') {
      await refreshProject(false)
      await loadRepositoryReadiness(verified)
      repositoryDialog.value = 'key'
      dialogError.value = ''
      return
    }
    repositoryDialog.value = 'key'
    await loadRepositoryReadiness(repository)
    if (!dialogError.value) {
      dialogError.value = t('Add this Gemcp public key as a read-only GitHub Deploy Key, or paste a fine-grained HTTPS token with Contents: Read and activate with the token. Then verify again.', '请先把这把 Gemcp 公钥加为只读 GitHub Deploy Key，或粘贴一把 Contents: Read 的细粒度 HTTPS 令牌并激活。然后再验证。')
    }
  } catch (caught) {
    dialogError.value = caught instanceof APIError ? caught.message : t('Repository verification failed.', '仓库验证失败。')
    repositoryDialog.value = 'key'
  } finally {
    repositoryBusy.value = false
  }
}

async function activateWithHTTPSToken(repository = selectedRepository.value) {
  if (!repository) return
  const token = repositoryForm.httpsToken.trim()
  if (!token) {
    dialogError.value = t('Paste a GitHub fine-grained token with Contents: Read on this repository.', '请粘贴一把对此仓库有 Contents: Read 的 GitHub 细粒度令牌。')
    return
  }
  selectedRepository.value = repository
  repositoryBusy.value = true
  dialogError.value = ''
  try {
    const verified = await tryVerifyRepository(repository, false, token)
    repositoryForm.httpsToken = ''
    if (verified?.status === 'active') {
      await refreshProject(false)
      await loadRepositoryReadiness(verified)
      dialogError.value = ''
      return
    }
    await loadRepositoryReadiness(repository)
  } finally {
    repositoryBusy.value = false
  }
}

async function copy(value: string, name: string) {
  await navigator.clipboard.writeText(value)
  copied.value = name
  window.setTimeout(() => (copied.value = ''), 1600)
}

watch(selectedProject, (project) => {
  if (!project) return
  policyForm.monthlyBudgetCNY = (project.monthly_budget_milli / 1000).toString()
}, { immediate: true })

async function savePolicy() {
  if (!selectedProject.value) return
  policyBusy.value = true
  error.value = ''
  try {
    const updated = await api.updateProject(selectedProject.value.id, {
      monthly_budget_milli: Math.round(Number(policyForm.monthlyBudgetCNY) * 1000),
      max_experiment_milli: Math.round(Number(policyForm.monthlyBudgetCNY) * 1000),
    })
    projects.value = projects.value.map((project) => project.id === updated.id ? updated : project)
  } catch (caught) {
    handleError(caught, t('Could not update the Project budget.', '无法更新 Project 预算。'))
  } finally {
    policyBusy.value = false
  }
}

function parseDatasetSources(raw: string) {
  return raw.split('\n').map((line) => line.trim()).filter(Boolean).map((line) => {
    const [url, relativePath, sha256] = line.split(/\s+/)
    return { url, relative_path: relativePath || '', sha256 }
  }).filter((source) => source.url && source.relative_path)
}

function applyDatasetCatalog(name: string) {
  datasetForm.catalog = name
  const entry = datasetSources.value.find((item) => item.name === name)
  if (!entry) return
  datasetForm.name = entry.name
  datasetForm.backend = (entry.backend || 'autodl_elastic') as DatasetBinding['backend']
  datasetForm.canonicalRoot = entry.canonical_root
  datasetForm.requiredMarkers = entry.required_markers.join('\n')
}

async function registerDatasetBinding() {
  if (!selectedProject.value) return
  datasetBusy.value = true
  error.value = ''
  try {
    const markers = datasetForm.requiredMarkers.split(/[\n,]/).map((value) => value.trim()).filter(Boolean)
    const sources = parseDatasetSources(datasetForm.sources)
    await api.createDatasetBinding(selectedProject.value.id, {
      name: datasetForm.name || undefined,
      catalog: datasetForm.catalog || undefined,
      backend: datasetForm.backend,
      canonical_root: datasetForm.canonicalRoot || undefined,
      required_markers: markers,
      sources,
    })
    datasetForm.catalog = ''
    datasetForm.name = ''
    datasetForm.requiredMarkers = ''
    datasetForm.sources = ''
    datasetBindings.value = await api.datasetBindings(selectedProject.value.id)
  } catch (caught) {
    handleError(caught, t('Could not register the dataset binding.', '无法注册数据集绑定。'))
  } finally {
    datasetBusy.value = false
  }
}

async function registerEnvironment() {
  if (!selectedProject.value) return
  environmentBusy.value = true
  error.value = ''
  try {
    await api.createEnvironment(selectedProject.value.id, {
      name: environmentForm.name, backend: environmentForm.backend, image_uuid: environmentForm.imageUUID, set_default: environmentForm.setDefault,
    })
    environmentForm.name = ''
    environmentForm.imageUUID = ''
    environmentForm.setDefault = false
    environments.value = await api.environments(selectedProject.value.id)
  } catch (caught) {
    handleError(caught, t('Could not register the Environment.', '无法注册 Environment。'))
  } finally {
    environmentBusy.value = false
  }
}

async function removeEnvironment(record: ProjectEnvironment) {
  if (!selectedProject.value) return
  environmentBusy.value = true
  error.value = ''
  try {
    await api.removeEnvironment(selectedProject.value.id, record.id)
    environments.value = await api.environments(selectedProject.value.id)
  } catch (caught) {
    handleError(caught, t('Could not disable the Environment.', '无法停用 Environment。'))
  } finally {
    environmentBusy.value = false
  }
}

async function removeDatasetBinding(binding: DatasetBinding) {
  if (!selectedProject.value) return
  datasetBusy.value = true
  error.value = ''
  try {
    await api.removeDatasetBinding(selectedProject.value.id, binding.id)
    datasetBindings.value = await api.datasetBindings(selectedProject.value.id)
  } catch (caught) {
    handleError(caught, t('Could not disable the dataset binding.', '无法停用数据集绑定。'))
  } finally {
    datasetBusy.value = false
  }
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
        <button class="nav-item" :class="{ active: activeView === 'images' }" type="button" :aria-label="t('Images', '镜像')" :title="t('Images', '镜像')" @click="activeView = 'images'"><Images :size="17" /><span>{{ t('Images', '镜像') }}</span></button>
        <button class="nav-item" :class="{ active: activeView === 'finance' }" type="button" :aria-label="t('Finance', '财务')" :title="t('Finance', '财务')" @click="activeView = 'finance'"><WalletCards :size="17" /><span>{{ t('Finance', '财务') }}</span></button>
        <button class="nav-item" :class="{ active: activeView === 'projects' }" type="button" aria-label="Project" title="Project" @click="activeView = 'projects'"><Boxes :size="17" /><span>Project</span></button>
        <button class="nav-item" :class="{ active: activeView === 'agents' }" type="button" :aria-label="t('Agents', 'Agent')" :title="t('Agents', 'Agent')" @click="activeView = 'agents'"><Bot :size="17" /><span>Agent</span></button>
        <button v-if="runtimeStatus?.self_hosted_enabled || runtimeStatus?.ssh_cloud_enabled" class="nav-item" :class="{ active: activeView === 'nodes' }" type="button" :aria-label="t('Nodes', '节点')" :title="t('Nodes', '节点')" @click="activeView = 'nodes'"><Cpu :size="17" /><span>{{ t('Nodes', '节点') }}</span></button>
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

      <div v-if="activeView === 'experiments' && runtimeStatus && !runtimeStatus.scheduler_enabled" class="scheduler-guidance" role="status">
        <TriangleAlert :size="17" />
        <span><strong>{{ t('Scheduler is disabled', '调度器已关闭') }}</strong>{{ t('Prepared proposals can still be confirmed, but their Experiments remain queued. Set GEMCP_SCHEDULER_ENABLED=true and restart Gemcp when an execution backend is ready.', '准备好的提案仍可确认，但对应 Experiment 会保持 queued。执行后端就绪后，请设置 GEMCP_SCHEDULER_ENABLED=true 并重启 Gemcp。') }}</span>
      </div>

      <ResearchView v-if="activeView === 'research'" :workspace="researchWorkspace" :loading="loading" :selected-study-id="selectedStudyID" :project="selectedProject" :repositories="repositories" :experiments="experiments" :has-active-agent="hasActiveAgent" :readiness="agentReadiness" :readiness-loading="loading" :runtime="runtimeStatus" :catalog="experimentCatalog" :repository-readiness="repositoryReadiness" :repository-readiness-loading="repositoryReadinessLoading" @select-study="selectStudy" @open-experiment="openExperimentByID" @close-run="openCloseRun" @create-study="openCreateStudy" @open-agents="activeView = 'agents'" @handshake="activeView = 'agents'" @open-nodes="activeView = 'nodes'" @verify-repository="selectedRepository && verifyRepository(selectedRepository)" @open-projects="activeView = 'projects'" @export-plan-sync="openPlanSync" />

      <section v-else-if="activeView === 'experiments'" class="page-workspace">
        <div class="section-heading page-section-heading"><div><h2>{{ t('Evidence', '证据') }}</h2><p>{{ t('Linked Experiments remain the execution evidence behind the Graph.', '关联的 Experiment 仍是 Graph 背后的执行证据。') }}</p></div><div class="segmented-control" :aria-label="t('Experiment state filter', '实验状态筛选')"><button v-for="filter in ['all', 'queued', 'running', 'succeeded', 'failed']" :key="filter" type="button" :class="{ active: stateFilter === filter }" @click="stateFilter = filter">{{ filter === 'all' ? t('all', '全部') : stateLabel(filter) }}</button></div></div>
        <form class="dialog-form dataset-form owner-prepare-form" @submit.prevent="prepareOwnerProposal">
          <div class="subsection-heading"><div><h2>{{ t('Prepare experiment', '准备实验') }}</h2><p>{{ t('Owners can prepare a zero-cost proposal here without an Agent Token. Confirm the digest before Gemcp spends.', 'Owner 可在此准备零成本提案，无需 Agent Token。确认摘要后 Gemcp 才会花费。') }}</p></div></div>
          <label>{{ t('Kind', '类型') }}
            <select v-model="prepareForm.mode">
              <option value="argv">{{ t('One-shot argv', '一次性 argv') }}</option>
              <option value="workload">{{ t('Named workload', '命名工作负载') }}</option>
              <option value="provision">{{ t('Provision dataset', '拉取数据集') }}</option>
            </select>
          </label>
          <label v-if="prepareForm.mode === 'argv'">{{ t('Arguments', '参数') }}<textarea v-model="prepareForm.argv" rows="3" required maxlength="4000" :placeholder="t('One argument per line', '一行一个参数')" spellcheck="false"></textarea></label>
          <label v-if="prepareForm.mode === 'workload'">{{ t('Workload', '工作负载') }}<input v-model="prepareForm.workload" required maxlength="80" placeholder="objbg-smoke" spellcheck="false" /></label>
          <div v-if="prepareForm.mode === 'workload' && projectWorkloads.length" class="saved-workload-names">
            <span>{{ t('Saved on this Project', '已保存在此 Project') }}</span>
            <button v-for="item in projectWorkloads" :key="item.id" type="button" class="text-button" @click="prepareForm.workload = item.name">{{ item.name }}</button>
          </div>
          <label v-if="prepareForm.mode === 'workload'">{{ t('Parameters', '参数') }}<textarea v-model="prepareForm.parameters" rows="2" maxlength="2000" :placeholder="t('name=value, one per line', 'name=value，一行一个')" spellcheck="false"></textarea></label>
          <label>{{ t('Ref', 'Ref') }}<input v-model="prepareForm.ref" maxlength="255" :placeholder="t('Default branch if omitted', '省略则用默认分支')" spellcheck="false" /></label>
          <label>{{ t('Graph origin', 'Graph 起点') }}
            <select v-model="prepareForm.fromNodeID">
              <option v-for="option in prepareOriginOptions" :key="option.value || 'none'" :value="option.value">{{ option.label }}</option>
            </select>
          </label>
          <label>{{ t('Runtime preset', '运行预设') }}
            <select v-model="prepareForm.runtimePreset" :disabled="prepareForm.mode === 'provision'">
              <option value="smoke">smoke</option>
              <option value="probe">probe</option>
              <option value="train">train</option>
            </select>
          </label>
          <label>{{ t('Dataset', '数据集') }}<input v-model="prepareForm.dataset" maxlength="100" placeholder="scanobjectnn-objbg" spellcheck="false" /></label>
          <label>{{ t('Expected metric', '预期指标') }}<input v-model="prepareForm.expectedMetric" maxlength="80" placeholder="overall_accuracy" spellcheck="false" /></label>
          <label class="checkbox-row"><input v-model="prepareForm.installDependencies" type="checkbox" />{{ t('Install Python requirements from the commit', '从该提交安装 Python 依赖') }}</label>
          <div v-if="prepareError" class="form-error" role="alert">{{ prepareError }}</div>
          <button class="primary-button small-button" type="submit" :disabled="prepareBusy"><LoaderCircle v-if="prepareBusy" :size="16" class="spinning" /><FlaskConical v-else :size="16" />{{ t('Prepare proposal', '准备提案') }}</button>
        </form>
        <div class="experiment-operations"><RunActivityPanel :feed="operationsFeed" :loading="operationsLoading" :confirming-proposal-id="confirmationBusy ? confirmationTarget?.id : ''" @open-experiment="openExperimentByID" @confirm-proposal="openProposalConfirmation" /></div>
        <ExperimentTable :experiments="filteredExperiments" @select="openExperiment" />
      </section>

      <DiagnosticsView v-else-if="activeView === 'diagnostics'" :active="true" :project="selectedProject" @unauthorized="emit('signedOut')" />

      <BakeView v-else-if="activeView === 'images'" :active="true" :project="selectedProject" @unauthorized="emit('signedOut')" />

      <section v-else-if="activeView === 'projects'" class="page-workspace project-workspace">
        <div class="section-heading page-section-heading"><div><h2>{{ selectedProject?.name ?? 'Project' }}</h2><p>{{ selectedProject?.slug }} / {{ selectedProject?.status }}</p></div><span class="version-chip">{{ selectedProject?.timezone }}</span></div>
        <form v-if="selectedProject" class="policy-form" @submit.prevent="savePolicy">
          <div class="section-heading"><div><h2>{{ t('Budget allocation', '预算分配') }}</h2><p>{{ t('Assign the spending limit to the Project. Agent registration and compute selection are separate.', '只给 Project 分配费用预算；Agent 注册和计算资源选择彼此独立。') }}</p></div></div>
          <div class="policy-grid">
            <label><span>{{ t('Monthly budget (CNY)', '月度预算 (CNY)') }}</span><input v-model="policyForm.monthlyBudgetCNY" type="number" min="0.001" step="0.001" required /></label>
          </div>
          <div class="policy-actions"><button class="primary-button small-button" type="submit" :disabled="policyBusy"><LoaderCircle v-if="policyBusy" :size="16" class="spinning" />{{ t('Save budget', '保存预算') }}</button><p>{{ t('This one Project budget is also the per-run ceiling. Existing runtime configuration is unchanged.', '这一个 Project 预算同时作为单次运行上限；现有运行时配置保持不变。') }}</p></div>
        </form>
        <div class="subsection-heading"><div><h2>{{ t('AutoDL dataset bindings', 'AutoDL 数据集绑定') }}</h2><p>{{ t('Register /root/autodl-fs roots and optional HTTPS sources. Agents then prepare_experiment with runtime_preset=provision. Do not use workspace datasets for Public Elastic.', '登记 /root/autodl-fs 根路径和可选 HTTPS 来源。Agent 再用 runtime_preset=provision 准备实验。公有云弹性不要用 workspace dataset。') }}</p></div></div>
        <form class="dialog-form dataset-form" @submit.prevent="registerDatasetBinding">
          <label>{{ t('Catalog', '数据集目录') }}
            <select :value="datasetForm.catalog" @change="applyDatasetCatalog(($event.target as HTMLSelectElement).value)">
              <option value="">{{ t('Custom path', '自定义路径') }}</option>
              <option v-for="entry in datasetSources" :key="entry.name" :value="entry.name">{{ entry.display_name }}</option>
            </select>
          </label>
          <label>{{ t('Name', '名称') }}<input v-model="datasetForm.name" :required="!datasetForm.catalog" maxlength="100" placeholder="scanobjectnn-objbg" spellcheck="false" /></label>
          <label>{{ t('Backend', '后端') }}
            <select v-model="datasetForm.backend">
              <option value="autodl_elastic">{{ t('Public Elastic', '公有云弹性') }}</option>
              <option value="autodl_private">{{ t('Private Cloud', '私有云') }}</option>
              <option value="ssh_cloud">Cloud SSH</option>
            </select>
          </label>
          <label>{{ t('Canonical root', '规范根路径') }}<input v-model="datasetForm.canonicalRoot" :required="!datasetForm.catalog" maxlength="1024" placeholder="/root/autodl-fs/datasets/ScanObjectNN" spellcheck="false" /></label>
          <label>{{ t('Required markers', '必需文件') }}<textarea v-model="datasetForm.requiredMarkers" rows="2" maxlength="2000" :placeholder="t('Optional relative files, one per line', '可选相对文件，一行一个')"></textarea></label>
          <label>{{ t('HTTPS sources', 'HTTPS 来源') }}<textarea v-model="datasetForm.sources" rows="3" maxlength="8000" :placeholder="t('url relative_path [sha256], one file per line', 'url 相对路径 [sha256]，一行一个文件')"></textarea></label>
          <button class="primary-button small-button" type="submit" :disabled="datasetBusy"><LoaderCircle v-if="datasetBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ t('Register dataset', '注册数据集') }}</button>
        </form>
        <div v-if="datasetBindings.length" class="table-scroll">
          <table class="data-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Name', '名称') }}</th><th>{{ t('Backend', '后端') }}</th><th>{{ t('Root', '根路径') }}</th><th>{{ t('Variable', '变量') }}</th><th>{{ t('Sources', '来源') }}</th><th>{{ t('Actions', '操作') }}</th></tr></thead>
            <tbody>
              <tr v-for="binding in datasetBindings" :key="binding.id">
                <td><span class="state-badge" :data-state="binding.status"><span />{{ stateLabel(binding.status) }}</span></td>
                <td>{{ binding.name }}</td>
                <td>{{ binding.backend }}</td>
                <td><code>{{ binding.canonical_root }}</code></td>
                <td><code>{{ binding.environment_variable }}</code></td>
                <td>{{ binding.sources?.length || 0 }}</td>
                <td><button v-if="binding.status === 'active'" class="icon-button" type="button" :title="t('Disable dataset binding', '停用数据集绑定')" :disabled="datasetBusy" @click="removeDatasetBinding(binding)"><X :size="16" /></button></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Boxes :size="21" /></span><h3>{{ t('No AutoDL dataset bindings', '尚未注册 AutoDL 数据集') }}</h3><p>{{ t('Register a catalog or /root/autodl-fs root with HTTPS sources, then confirm a provision run before probe or train.', '先登记目录或 /root/autodl-fs 根路径及 HTTPS 来源，再确认一次 provision，然后才能 probe/train。') }}</p></div>
        <div class="subsection-heading"><div><h2>{{ t('AutoDL environments', 'AutoDL 环境') }}</h2><p>{{ t('Register a Provider-visible image so Agents are not locked to the first-run UUID. Official image-* IDs can be entered here.', '登记 Provider 可见镜像，避免 Agent 被首次安装的 UUID 锁死。官方 image-* 可在此填写。') }}</p></div></div>
        <form class="dialog-form dataset-form" @submit.prevent="registerEnvironment">
          <label>{{ t('Name', '名称') }}<input v-model="environmentForm.name" required maxlength="100" placeholder="torch-train" spellcheck="false" /></label>
          <label>{{ t('Backend', '后端') }}
            <select v-model="environmentForm.backend">
              <option value="autodl_elastic">{{ t('Public Elastic', '公有云弹性') }}</option>
              <option value="autodl_private">{{ t('Private Cloud', '私有云') }}</option>
            </select>
          </label>
          <label>Image UUID<input v-model="environmentForm.imageUUID" required maxlength="128" placeholder="image-6c15b8aad2" spellcheck="false" /></label>
          <label class="checkbox-row"><input v-model="environmentForm.setDefault" type="checkbox" />{{ t('Set as default', '设为默认') }}</label>
          <button class="primary-button small-button" type="submit" :disabled="environmentBusy"><LoaderCircle v-if="environmentBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ t('Register environment', '注册环境') }}</button>
        </form>
        <div v-if="environments.length" class="table-scroll">
          <table class="data-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Name', '名称') }}</th><th>{{ t('Backend', '后端') }}</th><th>Image</th><th>{{ t('Default', '默认') }}</th><th>{{ t('Actions', '操作') }}</th></tr></thead>
            <tbody>
              <tr v-for="record in environments" :key="record.id">
                <td><span class="state-badge" :data-state="record.status"><span />{{ stateLabel(record.status) }}</span></td>
                <td>{{ record.name }}</td>
                <td>{{ record.backend }}</td>
                <td><code>{{ record.image_uuid }}</code></td>
                <td>{{ record.is_default ? t('Yes', '是') : t('No', '否') }}</td>
                <td><button v-if="record.status === 'approved'" class="icon-button" type="button" :title="t('Disable environment', '停用环境')" :disabled="environmentBusy" @click="removeEnvironment(record)"><X :size="16" /></button></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div class="subsection-heading"><div><h2>{{ t('Repositories', '仓库') }}</h2><p>{{ t('Paste a GitHub HTTPS or SSH URL. Public repositories activate without a Deploy Key.', '粘贴 GitHub HTTPS 或 SSH URL。公开仓库无需 Deploy Key 即可激活。') }}</p></div><button class="primary-button small-button" type="button" @click="openCreateRepository"><Plus :size="16" />{{ t('Register repository', '注册仓库') }}</button></div>
        <div v-if="repositories.length" class="table-scroll">
          <table class="data-table repository-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Name', '名称') }}</th><th>SSH URL</th><th>{{ t('Branch', '分支') }}</th><th>{{ t('Verified', '已验证') }}</th><th>{{ t('Actions', '操作') }}</th></tr></thead>
            <tbody><tr v-for="repository in repositories" :key="repository.id"><td><span class="state-badge" :data-state="repository.status"><span />{{ stateLabel(repository.status) }}</span></td><td>{{ repository.name }}</td><td><code>{{ repository.ssh_url }}</code></td><td><code>{{ repository.default_branch }}</code></td><td>{{ dateTime(repository.last_verified_at) }}</td><td><div class="table-actions"><button v-if="repository.access !== 'public_https'" class="icon-button" type="button" :title="t('View Deploy public key', '查看 Deploy 公钥')" @click="openKey(repository)"><KeyRound :size="16" /></button><button v-if="repository.status !== 'active'" class="icon-button" type="button" :title="t('Verify repository', '验证仓库')" @click="verifyRepository(repository)"><ShieldCheck :size="16" /></button></div></td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><GitBranch :size="21" /></span><h3>{{ t('No repositories registered', '尚未注册仓库') }}</h3><p>{{ t('Paste a GitHub URL for the first experiment. Public repositories skip the Deploy Key.', '粘贴首个实验使用的 GitHub URL。公开仓库无需 Deploy Key。') }}</p></div>
      </section>

      <FinanceView v-if="activeView === 'finance'" :active="true" :projects="projects" @unauthorized="emit('signedOut')" />
      <AgentView v-if="activeView === 'agents'" :active="true" :project="selectedProject" :runtime="runtimeStatus" @unauthorized="emit('signedOut')" @open-nodes="activeView = 'nodes'" />
      <NodeView v-if="activeView === 'nodes'" :active="true" :projects="projects" :build="props.build" :ssh-cloud-enabled="Boolean(runtimeStatus?.ssh_cloud_enabled)" @unauthorized="emit('signedOut')" @open-agents="activeView = 'agents'" />
      <div v-show="activeView === 'provider'" class="persistent-view"><ProviderView :active="activeView === 'provider'" @unauthorized="emit('signedOut')" /></div>
      <NotificationView v-if="activeView === 'notifications'" :active="true" @unauthorized="emit('signedOut')" />

      <footer class="console-footer"><span>{{ props.build?.name ?? 'Gemcp' }} {{ props.build?.version ?? 'dev' }}</span><span>Commit {{ props.build?.commit ?? t('unknown', '未知') }}</span><span>{{ t('Operational estimates only', '仅为运行估算') }}</span></footer>
    </main>
  </div>

  <ExperimentDetail v-if="selectedExperiment" :experiment="selectedExperiment" :attempts="attempts" :loading="attemptsLoading" :error="attemptsError" :auto-close-run="pendingCloseRun" @close="closeExperiment" @saved="onWorkloadSaved" @closed="onRunClosed" />

  <WorkbenchDialog v-model:open="confirmationDialogOpen" :title="t('Confirm prepared proposal', '确认准备提案')" :label="t('Confirm prepared proposal', '确认准备提案')" :description="t('Review the immutable execution request and authorize this exact digest before Gemcp creates the Experiment.', '请核对不可变执行请求，并授权当前精确摘要，然后 Gemcp 才会创建 Experiment。')">
    <form v-if="confirmationTarget" class="dialog-form proposal-confirmation-form" @submit.prevent="submitProposalConfirmation">
      <dl class="proposal-confirmation-grid">
        <div class="span-two"><dt>{{ t('Repository and ref', '仓库与 ref') }}</dt><dd><strong>{{ confirmationTarget.repository_name }}</strong><code>{{ confirmationTarget.requested_ref }}</code></dd></div>
        <div v-if="confirmationTarget.repository_access"><dt>{{ t('Repository access', '仓库访问') }}</dt><dd>{{ repositoryAccessLabel(confirmationTarget.repository_access) }}</dd></div>
        <div v-if="confirmationTarget.repository_url" class="wide"><dt>{{ t('Remote', '远程') }}</dt><dd><code>{{ confirmationTarget.repository_url }}</code></dd></div>
        <div><dt>{{ t('Commit', '提交') }}</dt><dd><code>{{ confirmationTarget.commit_sha }}</code></dd></div>
        <div v-if="confirmationTarget.workload"><dt>{{ t('Named workload', '命名工作负载') }}</dt><dd><code>{{ confirmationTarget.workload }}</code></dd></div>
        <div v-if="confirmationTarget.dataset"><dt>{{ t('Dataset', '数据集') }}</dt><dd><code>{{ confirmationTarget.dataset }}</code></dd></div>
        <div v-if="confirmationTarget.from_node_id"><dt>{{ t('Graph origin', 'Graph 起点') }}</dt><dd><code>{{ confirmationTarget.from_node_id }}</code></dd></div>
        <div v-if="confirmationTarget.expected_metric"><dt>{{ t('Expected metric', '预期指标') }}</dt><dd>{{ confirmationTarget.expected_metric }}</dd></div>
        <div v-if="confirmationTarget.working_directory" class="wide"><dt>{{ t('Working directory', '工作目录') }}</dt><dd><code>{{ confirmationTarget.working_directory }}</code></dd></div>
        <div v-if="confirmationTarget.install_dependencies" class="wide"><dt>{{ t('Dependency install', '依赖安装') }}</dt><dd><code>python -m pip install --user -r {{ confirmationTarget.requirements_file || 'requirements.gemcp.txt' }}</code></dd></div>
        <div class="wide"><dt>{{ t('Immutable command', '不可变命令') }}</dt><dd><code>{{ confirmationTarget.display_command }}</code></dd></div>
        <div><dt>{{ t('Backend', '后端') }}</dt><dd>{{ confirmationTarget.backend }}</dd></div>
        <div><dt>{{ t('Environment', '环境') }}</dt><dd>{{ confirmationTarget.environment_name }}</dd></div>
        <div><dt>{{ t('Resource profile', '资源规格') }}</dt><dd>{{ confirmationTarget.resource_profile_name }}</dd></div>
        <div><dt>GPU</dt><dd>{{ confirmationTarget.gpu_num }}× {{ confirmationTarget.gpu_models.join(', ') }}</dd></div>
        <div><dt>{{ t('Runtime', '运行时间') }}</dt><dd>{{ confirmationTarget.runtime_preset || 'smoke' }} · {{ confirmationTarget.max_runtime_seconds || '—' }}s</dd></div>
        <div><dt>{{ t('Worst-case reservation', '最坏情况预留') }}</dt><dd><strong>CNY {{ (confirmationTarget.reserved_cost_milli / 1000).toFixed(3) }}</strong></dd></div>
        <div class="wide"><dt>{{ t('Expires', '过期时间') }}</dt><dd>{{ dateTime(confirmationTarget.expires_at) }}</dd></div>
        <div class="wide digest"><dt>{{ t('Confirmation digest', '确认摘要') }}</dt><dd><code>{{ confirmationTarget.confirmation_digest }}</code></dd></div>
      </dl>
      <div class="proposal-checks">
        <strong>{{ t('Preflight checks', '预检项') }}</strong>
        <ul><li v-for="check in confirmationTarget.checks" :key="check.id" :data-status="check.status"><span>{{ check.status }}</span>{{ check.summary }}</li></ul>
      </div>
      <label class="proposal-confirmation-check"><input v-model="confirmationChecked" type="checkbox" :disabled="confirmationBusy" /><span>{{ t('I confirm this exact proposal and authorize its worst-case CNY reservation.', '我确认此精确提案，并授权其最坏情况 CNY 费用预留。') }}</span></label>
      <div v-if="confirmationError" class="form-error" role="alert">{{ confirmationError }}</div>
      <button class="primary-button" type="submit" :disabled="confirmationBusy || !confirmationChecked"><LoaderCircle v-if="confirmationBusy" :size="16" class="spinning" /><ShieldCheck v-else :size="16" />{{ t('Confirm and start', '确认并启动') }}</button>
    </form>
  </WorkbenchDialog>

  <WorkbenchDialog v-model:open="studyDialog" :title="t('Import study', '从仓库导入')" :label="t('Import study', '从仓库导入')" :description="t('Start from an existing research repository. This binds the Study to that repo and does not start a workload.', '从已有研究仓库开始。Study 会绑上该仓库，不会启动作业。')">
    <form class="dialog-form" @submit.prevent="createStudy">
      <label class="study-import-label">
        <span>{{ t('Import from', '导入来源') }}</span>
        <WorkbenchSelect v-model="studyForm.importSource" :aria-label="t('Import from', '导入来源')" :options="studyImportOptions" @update:model-value="onStudyImportSourceChange" />
      </label>
      <label v-if="importingNewRepository">{{ t('GitHub URL', 'GitHub URL') }}<input v-model="studyForm.sshURL" required maxlength="512" placeholder="https://github.com/owner/repository" spellcheck="false" @input="onStudySSHURLInput" /></label>
      <label v-if="importingNewRepository">{{ t('Default branch', '默认分支') }}<input v-model="studyForm.defaultBranch" maxlength="255" placeholder="main" spellcheck="false" /></label>
      <label>{{ t('Name', '名称') }}<input v-model="studyForm.name" required maxlength="80" placeholder="objbg-scan" spellcheck="false" /></label>
      <label>{{ t('Research question', '研究问题') }}<textarea v-model="studyForm.question" required rows="4" maxlength="400" :placeholder="t('What should this Study answer?', '这个 Study 要回答什么问题？')"></textarea></label>
      <label>{{ t('Summary', '摘要') }}<textarea v-model="studyForm.summary" rows="3" maxlength="800" :placeholder="t('Optional scientific context. No prompts or credentials.', '可选科学背景。不要写 prompt 或凭据。')"></textarea></label>
      <label>{{ t('Protocol branch', '协议分支') }}<input v-model="studyForm.protocolBranch" maxlength="255" placeholder="research-plan" spellcheck="false" /></label>
      <label>{{ t('Protocol doc path', '协议文档路径') }}<input v-model="studyForm.protocolDocPath" maxlength="512" placeholder="research-plan/STATUS.md" spellcheck="false" /></label>
      <label>{{ t('Allowed code refs', '允许的代码 ref') }}<input v-model="studyForm.codeRefPattern" maxlength="255" placeholder="autoresearch/*" spellcheck="false" /></label>
      <p class="form-note">{{ t('Route binding is optional. Protocol branch is docs-only. When Allowed code refs is set, live Experiments must pass a matching ref — omitting ref is refused. Do not dump training code onto the protocol branch.', '路由绑定可选。协议分支只写文档。设置了允许的代码 ref 时，活实验必须传入匹配的 ref，省略会被拒绝。不要把训练代码写进协议分支。') }}</p>
      <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
      <button class="primary-button" type="submit" :disabled="studyBusy"><LoaderCircle v-if="studyBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ importingNewRepository ? t('Import repository and study', '导入仓库并创建 Study') : t('Create study', '创建 Study') }}</button>
    </form>
  </WorkbenchDialog>

  <div v-if="repositoryDialog" class="modal-backdrop" @click.self="repositoryDialog = null">
    <section class="modal" role="dialog" aria-modal="true" :aria-label="repositoryDialog === 'create' ? t('Register repository', '注册仓库') : t('Deploy public key', 'Deploy 公钥')">
      <header><div><p class="eyebrow">{{ t('GitHub', 'GitHub') }}</p><h2>{{ repositoryDialog === 'create' ? t('Register repository', '注册仓库') : t('Deploy public key', 'Deploy 公钥') }}</h2></div><button class="icon-button" type="button" :title="t('Close', '关闭')" @click="repositoryDialog = null"><X :size="17" /></button></header>
      <form v-if="repositoryDialog === 'create'" class="dialog-form" @submit.prevent="createRepository">
        <label>{{ t('Name', '名称') }}<input v-model="repositoryForm.name" :placeholder="t('Optional; defaults to the repository name', '可选，默认用仓库名')" /></label>
        <label>{{ t('GitHub URL', 'GitHub URL') }}<input v-model="repositoryForm.sshURL" placeholder="https://github.com/owner/repository" required spellcheck="false" /></label>
        <label>{{ t('Default branch', '默认分支') }}<input v-model="repositoryForm.defaultBranch" required spellcheck="false" /></label>
        <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
        <button class="primary-button" type="submit" :disabled="repositoryBusy"><LoaderCircle v-if="repositoryBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ t('Register repository', '注册仓库') }}</button>
      </form>
      <div v-else-if="repositoryDialog === 'key' && selectedRepository" class="key-panel">
        <p>{{ t('Add this public key to', '将此公钥添加到') }} <strong>{{ selectedRepository.name }}</strong> {{ t('as a read-only GitHub Deploy Key.', '设为只读 GitHub Deploy Key。') }}</p>
        <div class="code-box"><code>{{ selectedRepository.deploy_public_key }}</code><button class="icon-button" type="button" :title="copied === 'deploy-key' ? t('Copied', '已复制') : t('Copy Deploy public key', '复制 Deploy 公钥')" @click="copy(selectedRepository.deploy_public_key ?? '', 'deploy-key')"><Check v-if="copied === 'deploy-key'" :size="16" /><Clipboard v-else :size="16" /></button></div>
        <p class="form-note">{{ t('The usual GitHub action is a read-only Deploy Key on this repository.', '通常的 GitHub 操作是给这个仓库加一把只读 Deploy Key。') }} <a v-if="selectedDeployKeyURL" class="form-note-link" :href="selectedDeployKeyURL" target="_blank" rel="noreferrer">{{ t('GitHub repository → Deploy keys', 'GitHub 仓库 → Deploy keys') }}</a></p>
        <p v-if="selectedRepository.status === 'pending_key' || selectedRepository.pending_note" class="form-note">{{ selectedRepository.pending_note || t('Graph and experiment-catalog observation writes remain possible while status is pending_key. Prepare of a git-backed Experiment still requires verify.', '仓库仍是 pending_key 时，Graph 和实验目录观察写入仍然可以。基于 git 的 Experiment 准备仍需先验证。') }}</p>
        <p class="form-note">{{ t('If Deploy Keys are disabled, paste a GitHub fine-grained token with Contents: Read on this one repository. Gemcp stores it encrypted and never shows it again. Personal machine SSH cannot help: Gemcp does not inherit SSH_AUTH_SOCK.', '若 Deploy Keys 被关闭，请粘贴一把仅对此仓库有 Contents: Read 的 GitHub 细粒度令牌。Gemcp 会加密保存且不再回显。个人机器 SSH 帮不上忙：Gemcp 不会继承 SSH_AUTH_SOCK。') }} <a class="form-note-link" :href="selectedRepository.https_token_settings_url || githubFineGrainedTokenSettingsURL" target="_blank" rel="noreferrer">{{ t('Settings → Fine-grained tokens', 'Settings → Fine-grained tokens') }}</a></p>
        <label>{{ t('GitHub HTTPS token', 'GitHub HTTPS 令牌') }}<input v-model="repositoryForm.httpsToken" type="password" autocomplete="off" spellcheck="false" :placeholder="selectedRepository.https_token_configured ? t('Token stored. Paste a replacement to rotate.', '令牌已保存。粘贴新令牌可轮换。') : t('Fine-grained token, write-only', '细粒度令牌，只写不回显')" /></label>
        <RepositoryReadinessPanel :readiness="repositoryReadiness" :loading="repositoryReadinessLoading" @verify="verifyRepository(selectedRepository)" @register-defaults="repositoryDialog = null; activeView = 'projects'" />
        <p class="form-note">{{ t('Gemcp pins GitHub with the official Ed25519 host fingerprint for Deploy Key verify. HTTPS token verify does not use host SSH.', 'Deploy Key 验证使用 GitHub 官方 Ed25519 主机指纹。HTTPS 令牌验证不走主机 SSH。') }}</p>
        <div v-if="dialogError" class="form-error">{{ dialogError }}</div>
        <div class="table-actions">
          <button class="primary-button" type="button" :disabled="repositoryBusy" @click="activateWithHTTPSToken(selectedRepository)"><LoaderCircle v-if="repositoryBusy && repositoryForm.httpsToken" :size="16" class="spinning" /><ShieldCheck v-else :size="16" />{{ t('Activate with HTTPS token', '用 HTTPS 令牌激活') }}</button>
          <button class="primary-button" type="button" :disabled="repositoryBusy" @click="verifyRepository(selectedRepository)"><LoaderCircle v-if="repositoryBusy && !repositoryForm.httpsToken" :size="16" class="spinning" /><ShieldCheck v-else :size="16" />{{ t('Verify repository', '验证仓库') }}</button>
        </div>
      </div>
    </section>
  </div>

  <WorkbenchDialog
    v-model:open="planSyncDialog"
    :title="t('Export research-plan sync', '导出 research-plan 同步')"
    :label="t('Export research-plan sync', '导出 research-plan 同步')"
    :description="t('Dry-run a docs-only markdown amendment for the protocol branch. Gemcp never pushes git, never force-pushes, and never writes frozen recipe files. Metrics stay on the Graph.', '先干跑一份只写文档的协议分支补丁。Gemcp 不会推 git、不会 force-push、也不会写冻结配方文件。指标仍在 Graph 上。')"
  >
    <div class="dialog-form">
      <p v-if="planSyncBusy && !planSyncExport" class="form-note">{{ t('Building the amendment…', '正在生成补丁…') }}</p>
      <template v-else-if="planSyncExport">
        <p class="form-note">{{ t('Target', '目标') }} <code>{{ planSyncExport.target_path }}</code> {{ t('on', '于') }} <code>{{ planSyncExport.target_branch }}</code></p>
        <p v-if="planSyncExport.warning" class="form-note">{{ planSyncExport.warning }}</p>
        <label>
          <span>{{ t('Markdown patch', 'Markdown 补丁') }}</span>
          <textarea class="attach-prompt" readonly rows="16" spellcheck="false" :value="planSyncExport.markdown"></textarea>
        </label>
        <p v-if="planSyncExport.recorded" class="form-note">{{ t('Audit receipt recorded. Apply this markdown on the docs-only protocol branch yourself.', '已写入审计回执。请自行把这份 markdown 应用到只写文档的协议分支。') }}</p>
      </template>
      <div v-if="planSyncError" class="form-error" role="alert">{{ planSyncError }}</div>
      <button v-if="planSyncExport && !planSyncExport.recorded" class="primary-button" type="button" :disabled="planSyncBusy" @click="recordPlanSync">
        <LoaderCircle v-if="planSyncBusy" :size="16" class="spinning" /><FileText v-else :size="16" />
        {{ t('Record audit receipt', '记录审计回执') }}
      </button>
    </div>
  </WorkbenchDialog>
</template>
