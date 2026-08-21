<script setup lang="ts">
import { computed, markRaw, ref } from 'vue'
import type { Edge, Node } from '@vue-flow/core'
import { Position, VueFlow } from '@vue-flow/core'
import { Background } from '@vue-flow/background'
import { Controls } from '@vue-flow/controls'
import { MiniMap } from '@vue-flow/minimap'
import { Check, Clipboard, FlaskConical, GitBranch, Network, Plus, Sparkles } from '@lucide/vue'
import { motion } from 'motion-v'
import type { Experiment, Project, Repository, ResearchNode, ResearchWorkspace } from '../api'
import { localizedState, useI18n } from '../i18n'
import { buildResearchAttachPrompt } from '../researchAttachPrompt'
import ResearchGraphNode from './ResearchGraphNode.vue'
import WorkbenchDialog from './WorkbenchDialog.vue'
import WorkbenchSelect from './WorkbenchSelect.vue'

const props = defineProps<{
  workspace: ResearchWorkspace | null
  loading: boolean
  selectedStudyId: string
  project?: Project | null
  repositories?: Repository[]
  experiments?: Experiment[]
}>()
const emit = defineEmits<{
  selectStudy: [studyID: string]
  openExperiment: [experimentID: string]
  createStudy: []
}>()
const { locale, languageTag, t } = useI18n()
const nodeTypes = { research: markRaw(ResearchGraphNode) }
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
const kindOrder = ['question', 'hypothesis', 'plan', 'run', 'result', 'observation', 'decision'] as const

const graphNodes = computed<Node[]>(() => {
  const columns = new Map<string, ResearchNode[]>()
  for (const kind of kindOrder) columns.set(kind, [])
  for (const node of nodes.value) {
    const bucket = columns.get(node.kind) ?? columns.get('decision')
    bucket?.push(node)
  }
  const result: Node[] = []
  let column = 0
  for (const kind of kindOrder) {
    const items = columns.get(kind) ?? []
    if (!items.length) continue
    items.forEach((node, row) => {
      result.push({
        id: node.id,
        type: 'research',
        position: { x: 36 + column * 280, y: 28 + row * 168 },
        data: {
          node,
          kindLabel: kindLabel(node.kind),
          onOpenEvidence: (experimentID: string) => emit('openExperiment', experimentID),
        },
        sourcePosition: Position.Right,
        targetPosition: Position.Left,
      })
    })
    column += 1
  }
  return result
})

const graphEdges = computed<Edge[]>(() => edges.value.map((edge) => ({
  id: edge.id,
  source: edge.from_id,
  target: edge.to_id,
  label: relationLabel(edge.relation),
  type: 'smoothstep',
  animated: edge.relation === 'produced' || edge.relation === 'leads_to',
})))

function relationLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    leads_to: ['leads to', '引出'],
    compares: ['compares', '对比'],
    supersedes: ['supersedes', '替代'],
    supports: ['supports', '支持'],
    contradicts: ['contradicts', '反驳'],
    produced: ['produced', '产生'],
  }
  const label = labels[value] ?? [value, value]
  return t(label[0], label[1])
}

function kindLabel(value: string) {
  const labels: Record<string, [string, string]> = {
    question: ['Question', '问题'],
    hypothesis: ['Hypothesis', '假设'],
    plan: ['Plan', '计划'],
    run: ['Run', '运行'],
    result: ['Result', '结果'],
    observation: ['Observation', '观察'],
    decision: ['Decision', '决策'],
  }
  const label = labels[value] ?? [value, value]
  return t(label[0], label[1])
}

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
        <p>{{ study?.question ?? t('The Agent can open a Study and keep the Graph current. Docker execution stays in the Lab layer.', 'Agent 可以创建 Study 并维护研究 Graph；Docker 执行仍留在 Lab 层。') }}</p>
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
      <div class="research-summary">
        <motion.article class="research-card next-card" :initial="{ y: 8 }" :animate="{ y: 0 }" :transition="{ duration: 0.22 }">
          <span class="research-kicker"><Sparkles :size="15" />{{ t('Next action', '下一步') }}</span>
          <strong>{{ study.plan?.next_action ?? t('No active plan yet', '还没有活跃计划') }}</strong>
          <p>{{ study.plan?.goal ?? t('Ask the Agent to replace the iteration plan after it inspects the repository.', '让 Agent 检查仓库后更新迭代计划。') }}</p>
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
            <p>{{ t('Hypotheses, runs, results, and decisions. Execution detail stays behind evidence links.', '假设、运行、结果和决策。执行细节留在证据链接后面。') }}</p>
          </div>
          <span class="live-label"><Network :size="13" />{{ nodes.length }} {{ t('nodes', '个节点') }}</span>
        </div>
        <div v-if="graphNodes.length" class="graph-canvas">
          <VueFlow
            :nodes="graphNodes"
            :edges="graphEdges"
            :node-types="nodeTypes"
            :min-zoom="0.4"
            :max-zoom="1.6"
            fit-view-on-init
            :nodes-draggable="false"
            :nodes-connectable="false"
            :elements-selectable="false"
            :pan-on-scroll="true"
          >
            <Background pattern-color="#d8d0c4" :gap="18" />
            <MiniMap pannable zoomable />
            <Controls />
          </VueFlow>
        </div>
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
      <h3>{{ t('Start from a research question', '从研究问题开始') }}</h3>
      <p>{{ t('The main surface is the Study, plan, and Graph. Nodes, budgets, and Provider details stay in Lab.', '主界面只展示 Study、计划和 Graph。节点、预算和 Provider 细节留在 Lab。') }}</p>
      <div class="research-empty-actions">
        <button class="secondary-button" type="button" :aria-label="t('Attach prompt', '入图 Prompt')" @click="attachOpen = true"><Clipboard :size="16" />{{ t('Attach prompt', '入图 Prompt') }}</button>
        <button class="primary-button" type="button" @click="emit('createStudy')"><Plus :size="16" />{{ t('Create the first study', '创建第一个 Study') }}</button>
      </div>
    </div>
  </section>

  <WorkbenchDialog
    v-model:open="attachOpen"
    :title="t('Attach prompt', '入图 Prompt')"
    :label="t('Attach prompt', '入图 Prompt')"
    :description="t('Give this to a coding Agent that already has Gemcp MCP. It tells the Agent how to put the current repository on the Graph. It contains no Token.', '交给已经接好 Gemcp MCP 的编码 Agent。它说明如何把当前仓库挂上 Graph，不含 Token。')"
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
