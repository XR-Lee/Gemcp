<script setup lang="ts">
import { computed, ref } from 'vue'
import { Bot, Check, Clipboard, FileText, FlaskConical, GitBranch, Network, Plus, Sparkles } from '@lucide/vue'
import { motion } from 'motion-v'
import type { AgentReadiness, Experiment, ExperimentCatalog, Project, Repository, RepositoryReadiness, ResearchHypothesis, ResearchWorkspace, RuntimeStatus } from '../api'
import { localizedState, useI18n } from '../i18n'
import { buildResearchAttachPrompt } from '../researchAttachPrompt'
import { formatMetric, normalizeVerdict } from '../researchFormat'
import { historicalObservationsForHypothesis, selectLatestEvidence, selectLatestResult } from '../researchLatestResult'
import { layoutResearchGraph, type CardSize } from '../researchGraphLayout'
import AgentReadinessPanel from './AgentReadinessPanel.vue'
import RepositoryReadinessPanel from './RepositoryReadinessPanel.vue'
import ResearchGraphCanvas from './ResearchGraphCanvas.vue'
import WorkbenchDialog from './WorkbenchDialog.vue'
import WorkbenchSelect from './WorkbenchSelect.vue'

const props = defineProps<{
  workspace: ResearchWorkspace | null
  loading: boolean
  selectedStudyId: string
  project?: Project | null
  repositories?: Repository[]
  experiments?: Experiment[]
  hasActiveAgent?: boolean
  readiness?: AgentReadiness | null
  readinessLoading?: boolean
  runtime?: RuntimeStatus | null
  catalog?: ExperimentCatalog | null
  repositoryReadiness?: RepositoryReadiness | null
  repositoryReadinessLoading?: boolean
}>()
const emit = defineEmits<{
  selectStudy: [studyID: string]
  openExperiment: [experimentID: string]
  closeRun: [experimentID: string]
  createStudy: []
  openAgents: []
  openNodes: []
  handshake: []
  verifyRepository: []
  openProjects: []
  exportPlanSync: []
}>()
const { locale, languageTag, t } = useI18n()
const attachOpen = ref(false)
const copied = ref(false)
const attachPrompt = computed(() => buildResearchAttachPrompt({
  locale: locale.value,
  project: props.project,
  repositories: props.repositories,
  experiments: props.experiments,
  workspace: props.workspace,
}))

async function copyAttachPrompt() {
  await navigator.clipboard.writeText(attachPrompt.value)
  copied.value = true
  window.setTimeout(() => (copied.value = false), 1600)
}

const study = computed(() => props.workspace?.study ?? null)
const studies = computed(() => props.workspace?.studies ?? [])
const nodes = computed(() => study.value?.nodes ?? [])
const edges = computed(() => study.value?.edges ?? [])
const latestResult = computed(() => selectLatestResult(nodes.value))
const latestEvidence = computed(() => (latestResult.value ? null : selectLatestEvidence(nodes.value)))

function verdictLabel(value?: string) {
  switch (normalizeVerdict(value)) {
    case 'supports': return t('Supports', '支持')
    case 'contradicts': return t('Contradicts', '反驳')
    case 'inconclusive': return t('Inconclusive', '不确定')
    default: return ''
  }
}

function agentReadingLabel(value?: string) {
  if (value === 'conflict') return t('Conflicts', '冲突')
  return verdictLabel(value)
}

function relationLabel(value?: string) {
  switch (value) {
    case 'supports': return t('Supports', '支持')
    case 'contradicts': return t('Contradicts', '反驳')
    case 'leads_to': return t('Leads to', '引出')
    default: return ''
  }
}

function historicalObservations(hypothesisID: string) {
  return historicalObservationsForHypothesis(hypothesisID, nodes.value, edges.value)
}

function metricSourceLabel(value?: string) {
  if (value === 'experiment') return t('measured', '实测')
  if (value === 'asserted') return t('asserted', '声称')
  return ''
}

// One line per hypothesis: how the closed evidence is split, so a reader sees
// the state of the claim without opening each run.
function verdictTally(hypothesis: ResearchHypothesis) {
  const counts = { supports: 0, contradicts: 0, inconclusive: 0 }
  for (const record of hypothesis.experiments) {
    const verdict = normalizeVerdict(record.verdict)
    if (verdict) counts[verdict] += 1
  }
  if (counts.supports + counts.contradicts + counts.inconclusive === 0) return ''
  return [
    `${t('Supports', '支持')} ${counts.supports}`,
    `${t('Contradicts', '反驳')} ${counts.contradicts}`,
    `${t('Inconclusive', '不确定')} ${counts.inconclusive}`,
  ].join(' · ')
}
const selectedStudyModel = computed({
  get: () => props.selectedStudyId || study.value?.id || '',
  set: (value: string) => emit('selectStudy', value),
})
const studyOptions = computed(() => studies.value.map((item) => ({ value: item.id, label: item.name })))
const needsAgentHandoff = computed(() => Boolean(study.value && !study.value.plan))
const boundRepositoryName = computed(() => study.value?.repository?.name || props.repositories?.[0]?.name || '')
const agentHandoffDetail = computed(() => {
  const repository = boundRepositoryName.value
  if (repository) {
    return t(
      `This Study is registered against ${repository}. Open that repository locally and enable Gemcp MCP in that directory, not as a global MCP. Then give the Agent the attach prompt so it can map experimental branches, hypotheses, conclusions, and evidence onto the Graph.`,
      `Study 已经注册，并绑上了 ${repository}。请在本地打开这个仓库，在该目录里开启 Gemcp MCP，不要开成全局 MCP。再把入图 Prompt 给 Agent，让它把实验分支、假设、结论和证据画到 Graph 上。`,
    )
  }
  return t(
    'This Study is registered. Open the research repository locally and enable Gemcp MCP in that directory, not as a global MCP. Then give the Agent the attach prompt so it can map experimental branches, hypotheses, conclusions, and evidence onto the Graph.',
    'Study 已经注册。请在本地打开研究仓库，在该目录里开启 Gemcp MCP，不要开成全局 MCP。再把入图 Prompt 给 Agent，让它把实验分支、假设、结论和证据画到 Graph 上。',
  )
})
const focusNodeIDs = computed(() => (props.workspace?.next_actions ?? []).map((action) => action.from_node_id).filter((id): id is string => Boolean(id)))
const graphAxis = ref<'time' | 'lineage' | undefined>()
const cardSizes = ref<Record<string, CardSize>>({})
function onCardSizes(next: Record<string, CardSize>) {
  const prev = cardSizes.value
  const nextKeys = Object.keys(next)
  const unchanged = nextKeys.length === Object.keys(prev).length && nextKeys.every((key) => prev[key]?.width === next[key]?.width && prev[key]?.height === next[key]?.height)
  if (!unchanged) cardSizes.value = next
}
const graphLayout = computed(() => layoutResearchGraph({
  nodes: nodes.value,
  edges: edges.value,
  focusNodeIDs: focusNodeIDs.value,
  axis: graphAxis.value,
  cardSizes: cardSizes.value,
}))
const catalogRepositories = computed(() => {
  if (props.catalog?.repositories?.length) return props.catalog.repositories
  return (props.repositories ?? []).map((repository) => ({
    id: repository.id,
    name: repository.name,
    ssh_url: repository.ssh_url,
    default_branch: repository.default_branch,
    status: repository.status,
    last_verified_at: repository.last_verified_at,
    observation_writes_allowed: repository.observation_writes_allowed,
    pending_note: repository.pending_note,
    rows: [],
  }))
})
const studyRoute = computed(() => study.value?.route ?? null)
function dateTime(value?: string) {
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}
</script>

<template>
  <section class="research-page">
    <motion.div class="research-heading" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
      <div>
        <p class="eyebrow">{{ t('Research', '研究') }}</p>
        <h2>{{ study?.name ?? t('No study yet', '还没有 Study') }}</h2>
        <p>{{ study?.question ?? t('Import an existing research repository, or write a question. Docker execution stays in the Lab layer.', '从已有研究仓库导入，或先写下问题。Docker 执行仍留在 Lab 层。') }}</p>
        <p v-if="study?.repository" class="research-repo"><GitBranch :size="13" />{{ study.repository.name }} · {{ study.repository.ssh_url }}</p>
        <p v-if="studyRoute" class="research-route" data-testid="study-route">
          {{ t('Route', '路由') }}
          <template v-if="studyRoute.protocol_branch"> · {{ t('protocol', '协议') }} <code>{{ studyRoute.protocol_branch }}</code></template>
          <template v-if="studyRoute.protocol_doc_path"> · <code>{{ studyRoute.protocol_doc_path }}</code></template>
          <template v-if="studyRoute.code_ref_pattern"> · {{ t('code refs', '代码 ref') }} <code>{{ studyRoute.code_ref_pattern }}</code></template>
        </p>
      </div>
      <div class="research-heading-actions">
        <WorkbenchSelect
          v-if="studies.length"
          v-model="selectedStudyModel"
          :aria-label="t('Study', 'Study')"
          :options="studyOptions"
        />
        <button class="secondary-button small-button" type="button" :aria-label="t('Attach prompt', '入图 Prompt')" @click="attachOpen = true"><Clipboard :size="16" />{{ t('Attach prompt', '入图 Prompt') }}</button>
        <button v-if="study" class="secondary-button small-button" type="button" data-testid="export-plan-sync" @click="emit('exportPlanSync')"><FileText :size="16" />{{ t('Export plan sync', '导出计划同步') }}</button>
        <button class="secondary-button small-button" type="button" @click="emit('createStudy')"><Plus :size="16" />{{ t('New study', '新建 Study') }}</button>
      </div>
    </motion.div>

    <AgentReadinessPanel
      :readiness="readiness ?? null"
      :loading="readinessLoading"
      compact
      :runtime="runtime"
      @open-agents="emit('openAgents')"
      @handshake="emit('handshake')"
      @open-nodes="emit('openNodes')"
    />

    <section v-if="catalogRepositories.length" class="research-catalog" data-testid="repo-experiment-catalog">
      <div class="section-heading">
        <div>
          <h2>{{ t('Registered experiment repository', '已注册实验仓库') }}</h2>
          <p>{{ t('Original registration next to extracted research-branch rows. Catalog ingest never starts a workload.', '原始注册数据与研究分支分析数据并排。写入目录不会启动作业。') }}</p>
        </div>
        <span class="live-label">{{ catalogRepositories.length }} {{ t('repositories', '个仓库') }}</span>
      </div>
      <article v-for="repository in catalogRepositories" :key="repository.id" class="catalog-repo">
        <header>
          <span class="research-kicker"><GitBranch :size="13" />{{ repository.name }}</span>
          <strong>{{ repository.name }}</strong>
        </header>
        <div class="catalog-panels">
          <section class="catalog-panel" data-testid="catalog-registration">
            <h3>{{ t('Original registration', '原始注册数据') }}</h3>
            <dl>
              <div>
                <dt>{{ t('Name', '名称') }}</dt>
                <dd>{{ repository.name }}</dd>
              </div>
              <div>
                <dt>ssh_url</dt>
                <dd><code>{{ repository.ssh_url }}</code></dd>
              </div>
              <div>
                <dt>{{ t('Default branch', '默认分支') }}</dt>
                <dd><code>{{ repository.default_branch }}</code></dd>
              </div>
              <div>
                <dt>{{ t('Status', '状态') }}</dt>
                <dd>{{ localizedState(repository.status) }}</dd>
              </div>
              <div v-if="repository.pending_note">
                <dt>{{ t('While pending', '待验证期间') }}</dt>
                <dd>{{ repository.pending_note }}</dd>
              </div>
              <div>
                <dt>{{ t('Last verified', '上次验证') }}</dt>
                <dd>{{ dateTime(repository.last_verified_at) }}</dd>
              </div>
            </dl>
          </section>
          <section class="catalog-panel catalog-analysis" data-testid="catalog-analysis">
            <h3>{{ t('Analysis', '分析数据') }}</h3>
            <div v-if="repository.rows.length" class="table-scroll">
              <table class="data-table catalog-table">
                <thead>
                  <tr>
                    <th>{{ t('Branch', '分支') }}</th>
                    <th>Setting</th>
                    <th>{{ t('Method', '方法') }}</th>
                    <th>{{ t('Implementation', '实现') }}</th>
                    <th>metric</th>
                    <th>{{ t('Result', '结果') }}</th>
                    <th>link</th>
                    <th>hash</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="row in repository.rows" :key="row.id">
                    <td><code>{{ row.branch }}</code></td>
                    <td>{{ row.setting }}</td>
                    <td>{{ row.method }}</td>
                    <td>{{ row.implementation }}</td>
                    <td>{{ row.metric }}</td>
                    <td>{{ row.result }}</td>
                    <td><code>{{ row.link }}</code></td>
                    <td><code>{{ row.hash }}</code></td>
                  </tr>
                </tbody>
              </table>
            </div>
            <p v-else class="catalog-empty">{{ t('No extracted rows yet. Ask the Agent to record research-branch experiments into the catalog.', '还没有提取行。让 Agent 把研究分支的实验写入目录。') }}</p>
          </section>
        </div>
      </article>
    </section>

    <div v-if="loading && !workspace" class="research-loading">{{ t('Loading research workspace...', '正在加载研究工作区...') }}</div>

    <template v-else-if="study">
      <aside v-if="needsAgentHandoff" class="research-mcp-banner" role="status">
        <div>
          <span class="research-kicker"><Bot :size="15" />MCP</span>
          <strong>{{ hasActiveAgent ? t('Enable Gemcp MCP in this repository directory', '在这个仓库目录里开启 Gemcp MCP') : t('Bind an Agent, then enable MCP in this repository directory', '先绑定 Agent，再在这个仓库目录里开启 MCP') }}</strong>
          <p>{{ agentHandoffDetail }}</p>
        </div>
        <div class="research-mcp-actions">
          <button class="primary-button small-button" type="button" @click="emit('openAgents')"><Bot :size="16" />{{ hasActiveAgent ? t('Open Agent MCP', '打开 Agent MCP') : t('Bind Agent', '绑定 Agent') }}</button>
          <button class="secondary-button small-button" type="button" @click="attachOpen = true"><Clipboard :size="16" />{{ t('Attach prompt', '入图 Prompt') }}</button>
        </div>
      </aside>
      <div class="research-summary">
        <motion.article class="research-card next-card" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
          <span class="research-kicker"><Sparkles :size="15" />{{ t('Next action', '下一步') }}</span>
          <strong>{{ study.plan?.next_action ?? t('No active plan yet', '还没有活跃计划') }}</strong>
          <p>{{ study.plan?.goal ?? t('Ask the Agent to reconstruct experimental branches, hypotheses, conclusions, and evidence from the repository.', '让 Agent 从仓库重建实验分支、假设、结论和证据。') }}</p>
          <ul v-if="workspace?.next_actions?.length" class="contract-actions">
            <li v-for="action in workspace.next_actions" :key="`${action.kind}-${action.from_node_id || action.tool}`">
              <strong>{{ action.title }}</strong>
              <span>{{ action.detail }}</span>
              <button
                v-if="action.kind === 'close_run' && action.experiment_id"
                class="text-button"
                type="button"
                @click="emit('closeRun', action.experiment_id ?? '')"
              >{{ t('Close run', '结束 run') }}</button>
              <button
                v-if="action.kind === 'export_plan_sync'"
                class="text-button"
                type="button"
                @click="emit('exportPlanSync')"
              >{{ t('Export plan sync', '导出计划同步') }}</button>
            </li>
          </ul>
        </motion.article>
        <motion.article class="research-card" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
          <span class="research-kicker">{{ t('Current goal', '当前目标') }}</span>
          <strong>{{ study.plan?.goal ?? t('Waiting for the first plan', '等待首个计划') }}</strong>
          <p>{{ study.plan?.rationale || study.summary || t('Plans are superseded, not edited in place.', '计划会被新版本替代，而不是原地修改。') }}</p>
        </motion.article>
        <motion.article class="research-card" data-testid="latest-result-card" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
          <template v-if="latestResult">
            <span class="research-kicker">{{ t('Latest result', '最新结果') }}</span>
            <strong>{{ latestResult.title }}</strong>
            <p v-if="latestResult.metric_name" :title="String(latestResult.metric_value)">{{ latestResult.metric_name }} {{ formatMetric(latestResult.metric_value) }}<template v-if="metricSourceLabel(latestResult.metric_source)"> · {{ metricSourceLabel(latestResult.metric_source) }}</template></p>
            <p v-else>{{ latestResult.summary || t('A finished Experiment can be attached here without exposing argv or GPU IDs.', '完成的 Experiment 可以挂到这里，不必暴露 argv 或 GPU ID。') }}</p>
            <button v-if="latestResult.experiment_id" class="text-button" type="button" @click="emit('openExperiment', latestResult.experiment_id)">{{ t('Open evidence', '打开证据') }}</button>
          </template>
          <template v-else-if="latestEvidence">
            <span class="research-kicker" data-testid="latest-evidence">{{ t('Latest evidence', '最新证据') }}</span>
            <strong>{{ latestEvidence.title }}</strong>
            <p v-if="latestEvidence.metric_name" :title="String(latestEvidence.metric_value)">{{ latestEvidence.metric_name }} {{ formatMetric(latestEvidence.metric_value) }}<template v-if="metricSourceLabel(latestEvidence.metric_source)"> · {{ metricSourceLabel(latestEvidence.metric_source) }}</template></p>
            <p v-if="latestEvidence.summary">{{ latestEvidence.summary }}</p>
            <p>{{ t('Historical observation, not a Gemcp Experiment result.', '历史观察，不是 Gemcp Experiment 的结果。') }}</p>
          </template>
          <template v-else>
            <span class="research-kicker">{{ t('Latest result', '最新结果') }}</span>
            <strong>{{ t('No result recorded', '尚未记录结果') }}</strong>
            <p>{{ t('A finished Experiment can be attached here without exposing argv or GPU IDs.', '完成的 Experiment 可以挂到这里，不必暴露 argv 或 GPU ID。') }}</p>
          </template>
        </motion.article>
      </div>

      <div v-if="study.hypotheses?.length" class="research-hypotheses" data-testid="hypothesis-records">
        <div class="section-heading">
          <div>
            <h2>{{ t('Hypotheses', '假设') }}</h2>
            <p>{{ t('Each hypothesis lists its experiments with the recorded metric and verdict. Measured numbers come from the Experiment; asserted numbers were typed into the Graph. A failed run is never evidence for or against a claim.', '每条假设列出它的实验、记录的指标和结论。“实测”数字来自 Experiment，“声称”数字是写进 Graph 的。失败的运行既不支持也不反驳假设。') }}</p>
          </div>
          <span class="live-label">{{ study.hypotheses.length }} {{ t('hypotheses', '条假设') }}</span>
        </div>
        <article v-for="hypothesis in study.hypotheses" :key="hypothesis.id" class="hypothesis-card">
          <header>
            <span class="research-kicker">{{ t('Hypothesis', '假设') }} · {{ localizedState(hypothesis.status) }}</span>
            <strong>{{ hypothesis.title }}</strong>
            <p v-if="hypothesis.summary">{{ hypothesis.summary }}</p>
            <p v-if="hypothesis.branch" class="hypothesis-branch"><GitBranch :size="13" />{{ hypothesis.branch }}</p>
            <p v-if="verdictTally(hypothesis)" class="hypothesis-tally" data-testid="hypothesis-tally">{{ verdictTally(hypothesis) }}</p>
          </header>
          <div v-if="hypothesis.experiments.length" class="hypothesis-runs">
            <article v-for="record in hypothesis.experiments" :key="record.run_node_id" class="hypothesis-run">
              <strong>{{ record.title }}</strong>
              <dl>
                <div>
                  <dt>{{ t('Run', '运行') }}</dt>
                  <dd>{{ localizedState(record.state) }}</dd>
                </div>
                <div v-if="record.branch">
                  <dt>{{ t('Branch', '分支') }}</dt>
                  <dd><code>{{ record.branch }}</code></dd>
                </div>
                <div v-if="record.commit_sha">
                  <dt>{{ t('Commit', 'Commit') }}</dt>
                  <dd><code>{{ record.commit_sha.slice(0, 12) }}</code></dd>
                </div>
                <div v-if="record.result_title">
                  <dt>{{ t('Result', '结果') }}</dt>
                  <dd>{{ record.result_title }}</dd>
                </div>
                <div v-if="record.metric_name">
                  <dt>{{ t('Metric', '指标') }}</dt>
                  <dd :title="String(record.metric_value)">{{ record.metric_name }} {{ formatMetric(record.metric_value) }}<span v-if="metricSourceLabel(record.metric_source)" class="hypothesis-metric-source" :data-source="record.metric_source"> · {{ metricSourceLabel(record.metric_source) }}</span></dd>
                </div>
                <div v-if="verdictLabel(record.verdict)">
                  <dt>{{ t('Verdict', '结论') }}</dt>
                  <dd><span class="hypothesis-verdict" :data-verdict="record.verdict">{{ verdictLabel(record.verdict) }}</span></dd>
                </div>
                <div v-if="agentReadingLabel(record.agent_reading)">
                  <dt>{{ t('Agent reading', 'Agent 解读') }}</dt>
                  <dd>
                    <span class="hypothesis-verdict" :data-verdict="record.agent_reading" data-testid="agent-reading">{{ agentReadingLabel(record.agent_reading) }}</span>
                    <template v-if="record.agent_reading_title"> — {{ record.agent_reading_title }}</template>
                  </dd>
                </div>
                <div v-if="record.highlight_title && record.highlight_title !== record.result_title">
                  <dt>{{ t('Highlight', '亮点观察') }}</dt>
                  <dd>{{ record.highlight_title }}</dd>
                </div>
              </dl>
              <button
                v-if="record.experiment_id"
                class="text-button"
                type="button"
                @click="emit('openExperiment', record.experiment_id ?? '')"
              >{{ t('Open evidence', '打开证据') }}</button>
              <button
                v-if="record.experiment_id && !record.result_title && ['succeeded', 'failed', 'cancelled', 'timed_out', 'budget_stopped', 'provider_error'].includes(record.state)"
                class="text-button"
                type="button"
                @click="emit('closeRun', record.experiment_id ?? '')"
              >{{ t('Close run', '结束 run') }}</button>
            </article>
          </div>
          <p v-if="!hypothesis.experiments.length" class="hypothesis-empty">{{ t('No Graph experiment is bound to this hypothesis yet.', '这条假设还没有入图实验。') }}</p>
          <div v-if="historicalObservations(hypothesis.id).length" class="hypothesis-observations" data-testid="hypothesis-observations">
            <p class="hypothesis-observations-label">{{ t('Historical evidence', '历史证据') }}</p>
            <article v-for="item in historicalObservations(hypothesis.id)" :key="item.node.id" class="hypothesis-run">
              <strong>{{ item.node.title }}</strong>
              <dl>
                <div>
                  <dt>{{ t('Relation', '关系') }}</dt>
                  <dd>{{ relationLabel(item.relation) }}</dd>
                </div>
                <div v-if="item.node.metric_name">
                  <dt>{{ t('Metric', '指标') }}</dt>
                  <dd :title="String(item.node.metric_value)">{{ item.node.metric_name }} {{ formatMetric(item.node.metric_value) }}<span v-if="metricSourceLabel(item.node.metric_source)" class="hypothesis-metric-source" :data-source="item.node.metric_source"> · {{ metricSourceLabel(item.node.metric_source) }}</span></dd>
                </div>
                <div v-if="item.node.summary">
                  <dt>{{ t('Summary', '摘要') }}</dt>
                  <dd>{{ item.node.summary }}</dd>
                </div>
              </dl>
            </article>
          </div>
        </article>
      </div>

      <div v-if="study.plan?.steps?.length" class="research-steps">
        <h3>{{ t('Iteration plan', '迭代计划') }}</h3>
        <ol>
          <li v-for="(step, index) in study.plan.steps" :key="`${step.title}-${index}`">
            <strong>{{ step.title }}</strong>
            <span v-if="step.detail">{{ step.detail }}</span>
          </li>
        </ol>
      </div>

      <div class="research-graph">
        <div class="section-heading">
          <div>
            <h2>{{ t('Research Graph', '研究 Graph') }}</h2>
            <p>{{ t('The top axis is evidence time: git commit dates for historical nodes, Experiment time for Gemcp runs. It is not the moment the Agent wrote the node. The bright trail ends at the newest evidence. Green and red marks are successes and failures.', '顶轴是证据时间：历史节点用 git commit 时间，Gemcp run 用作业时间，不是 Agent 写入节点的时刻。亮的轨迹停在最新一条证据。绿是成功，红是失败。') }}</p>
          </div>
          <span class="live-label"><Network :size="13" />{{ nodes.length }} {{ t('nodes', '个节点') }} · {{ graphLayout.activeNodeIDs.length }} {{ t('on active path', '条在 active path') }}</span>
        </div>
        <ResearchGraphCanvas
          v-if="nodes.length"
          :nodes="nodes"
          :edges="edges"
          :layout="graphLayout"
          @open-experiment="emit('openExperiment', $event)"
          @select-axis="graphAxis = $event"
          @card-sizes="onCardSizes"
        />
        <div v-else class="empty-state compact-empty">
          <span class="empty-icon"><GitBranch :size="21" /></span>
          <h3>{{ t('Graph is empty', 'Graph 还是空的') }}</h3>
          <p>{{ t('The first question node is created with the Study. Later runs and results appear here.', '创建 Study 时会带上第一个问题节点；后续运行和结果会出现在这里。') }}</p>
        </div>
      </div>
      <p class="research-updated">{{ t('Updated', '更新于') }} {{ dateTime(study.updated_at) }}</p>
    </template>

    <div v-else-if="studies.length" class="empty-state research-empty">
      <span class="empty-icon"><Network :size="21" /></span>
      <h3>{{ t('Select a Study', '选择一个 Study') }}</h3>
      <p>{{ t('This Project has more than one Study. Choose one to open its plan and Graph.', '此 Project 有多个 Study。选择一个以打开计划和 Graph。') }}</p>
    </div>

    <div v-else class="empty-state research-empty">
      <span class="empty-icon"><FlaskConical :size="21" /></span>
      <h3>{{ t('Import from a research repository', '从已有研究仓库导入') }}</h3>
      <p>{{ t('Pick a registered repository or paste a GitHub HTTPS or SSH URL. That creates a Study bound to the repo and does not start a workload.', '选一个已注册仓库，或粘贴 GitHub HTTPS / SSH URL。会创建一个绑上该仓库的 Study，不会启动作业。') }}</p>
      <RepositoryReadinessPanel
        :readiness="repositoryReadiness ?? null"
        :loading="repositoryReadinessLoading"
        @verify="emit('verifyRepository')"
        @register-defaults="emit('openProjects')"
      />
      <div class="research-empty-actions">
        <button class="secondary-button" type="button" :aria-label="t('Attach prompt', '入图 Prompt')" @click="attachOpen = true"><Clipboard :size="16" />{{ t('Attach prompt', '入图 Prompt') }}</button>
        <button class="primary-button" type="button" @click="emit('createStudy')"><Plus :size="16" />{{ t('Import study', '从仓库导入') }}</button>
      </div>
    </div>
  </section>

  <WorkbenchDialog
    v-model:open="attachOpen"
    :title="t('Attach prompt', '入图 Prompt')"
    :label="t('Attach prompt', '入图 Prompt')"
    :description="t('Give this to a coding Agent that has Gemcp MCP enabled in this repository directory. It tells the Agent to reconstruct experimental branches, hypotheses, conclusions, and evidence on the Graph. It contains no Token.', '交给在这个仓库目录里开了 Gemcp MCP 的编码 Agent。它要求 Agent 把实验分支、假设、结论和证据重建到 Graph 上，不含 Token。')"
  >
    <div class="dialog-form attach-prompt-form">
      <label class="attach-prompt-label">
        <span>{{ t('Agent prompt', 'Agent Prompt') }}</span>
        <textarea class="attach-prompt" readonly rows="18" spellcheck="false" :value="attachPrompt"></textarea>
      </label>
      <button class="primary-button" type="button" @click="copyAttachPrompt">
        <Check v-if="copied" :size="16" /><Clipboard v-else :size="16" />
        {{ copied ? t('Copied', '已复制') : t('Copy for Agent', '复制给 Agent') }}
      </button>
    </div>
  </WorkbenchDialog>
</template>
