<script setup lang="ts">
import { computed, ref } from 'vue'
import { Bot, Check, Clipboard, FlaskConical, GitBranch, Network, Plus, Sparkles } from '@lucide/vue'
import { motion } from 'motion-v'
import type { Experiment, Project, Repository, ResearchWorkspace } from '../api'
import { localizedState, useI18n } from '../i18n'
import { buildResearchAttachPrompt } from '../researchAttachPrompt'
import { layoutResearchGraph } from '../researchGraphLayout'
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
}>()
const emit = defineEmits<{
  selectStudy: [studyID: string]
  openExperiment: [experimentID: string]
  createStudy: []
  openAgents: []
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
const latestResult = computed(() => [...nodes.value].reverse().find((node) => node.kind === 'result') ?? null)
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
const graphLayout = computed(() => layoutResearchGraph({
  nodes: nodes.value,
  edges: edges.value,
  focusNodeIDs: focusNodeIDs.value,
}))
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
      </div>
      <div class="research-heading-actions">
        <WorkbenchSelect
          v-if="studies.length"
          v-model="selectedStudyModel"
          :aria-label="t('Study', 'Study')"
          :options="studyOptions"
        />
        <button class="secondary-button small-button" type="button" :aria-label="t('Attach prompt', '入图 Prompt')" @click="attachOpen = true"><Clipboard :size="16" />{{ t('Attach prompt', '入图 Prompt') }}</button>
        <button class="secondary-button small-button" type="button" @click="emit('createStudy')"><Plus :size="16" />{{ t('New study', '新建 Study') }}</button>
      </div>
    </motion.div>

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
            </li>
          </ul>
        </motion.article>
        <motion.article class="research-card" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
          <span class="research-kicker">{{ t('Current goal', '当前目标') }}</span>
          <strong>{{ study.plan?.goal ?? t('Waiting for the first plan', '等待首个计划') }}</strong>
          <p>{{ study.plan?.rationale || study.summary || t('Plans are superseded, not edited in place.', '计划会被新版本替代，而不是原地修改。') }}</p>
        </motion.article>
        <motion.article class="research-card" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
          <span class="research-kicker">{{ t('Latest result', '最新结果') }}</span>
          <strong>{{ latestResult?.title ?? t('No result recorded', '尚未记录结果') }}</strong>
          <p v-if="latestResult?.metric_name">{{ latestResult.metric_name }} {{ latestResult.metric_value }}</p>
          <p v-else>{{ latestResult?.summary || t('A finished Experiment can be attached here without exposing argv or GPU IDs.', '完成的 Experiment 可以挂到这里，不必暴露 argv 或 GPU ID。') }}</p>
          <button v-if="latestResult?.experiment_id" class="text-button" type="button" @click="emit('openExperiment', latestResult.experiment_id)">{{ t('Open evidence', '打开证据') }}</button>
        </motion.article>
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
            <p>{{ t('The top axis is exploration time. The bright trail ends at the newest record. Green and red marks are successes and failures.', '顶轴是探索时间。亮的轨迹停在最新一条记录。绿是成功，红是失败。') }}</p>
          </div>
          <span class="live-label"><Network :size="13" />{{ nodes.length }} {{ t('nodes', '个节点') }} · {{ graphLayout.activeNodeIDs.length }} {{ t('on active path', '条在 active path') }}</span>
        </div>
        <ResearchGraphCanvas
          v-if="nodes.length"
          :nodes="nodes"
          :edges="edges"
          :layout="graphLayout"
          @open-experiment="emit('openExperiment', $event)"
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
      <p>{{ t('Pick a registered repository or paste a GitHub SSH URL. That creates a Study bound to the repo and does not start a workload.', '选一个已注册仓库，或粘贴 GitHub SSH URL。会创建一个绑上该仓库的 Study，不会启动作业。') }}</p>
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
