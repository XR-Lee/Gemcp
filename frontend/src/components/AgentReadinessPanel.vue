<script setup lang="ts">
import { computed, ref } from 'vue'
import { Activity, Bot, Check, Clipboard, Cpu, Radio, Server } from '@lucide/vue'
import type { AgentReadiness, AgentReadinessAction, RuntimeStatus } from '../api'
import { buildAgentReadinessPrompt } from '../agentReadinessPrompt'
import { localizedState, useI18n } from '../i18n'
import WorkbenchDialog from './WorkbenchDialog.vue'

const props = defineProps<{
  readiness: AgentReadiness | null
  loading?: boolean
  compact?: boolean
  runtime?: RuntimeStatus | null
}>()
const emit = defineEmits<{
  handshake: []
  openAgents: []
  openNodes: []
}>()
const { locale, languageTag, t } = useI18n()
const promptOpen = ref(false)
const copied = ref(false)

const prompt = computed(() => props.readiness ? buildAgentReadinessPrompt({
  locale: locale.value,
  readiness: props.readiness,
  runtime: props.runtime,
}) : '')

const activeAgents = computed(() => (props.readiness?.agents ?? []).filter((agent) => agent.status === 'active'))
const readyCompute = computed(() => [
  ...(props.readiness?.compute.ssh_cloud ?? []).filter((node) => node.ready),
  ...(props.readiness?.compute.self_hosted ?? []).filter((node) => node.ready),
])
const presentationStatus = computed(() => {
  if (props.readiness?.status === 'waiting_compute' && activeAgents.value.length) return 'ready'
  return props.readiness?.status
})
const summary = computed(() => {
  const readiness = props.readiness
  if (!readiness) return ''
  const agents = activeAgents.value.length
  const ready = readyCompute.value.length
  if (readiness.status === 'ready') {
    return t(
      `${agents} active Agent(s) and ${ready} ready compute target(s). Agent registration is Project-scoped and independent of compute.`,
      `${agents} 个活跃 Agent，${ready} 个可用计算目标。Agent 注册属于整个 Project，与计算资源无关。`,
    )
  }
  if (readiness.status === 'blocked') {
    return t('Agents can see compute, but every target has a readiness blocker.', 'Agent 能看到计算节点，但每个目标都有就绪 blocker。')
  }
  if (readiness.status === 'waiting_agent') {
    return t('No active Agent Token. Register an Agent so it can access this Project.', '还没有活跃 Agent Token。请注册 Agent，让它访问此 Project。')
  }
  if (readiness.status === 'waiting_compute') {
    return t('Agent registration is complete and independent of compute. No Self-hosted or Cloud SSH target is visible yet.', 'Agent 注册已完成且与计算资源无关；目前还没有可见的 Self-hosted 或 Cloud SSH 目标。')
  }
  return readiness.summary
})

const visibleCompute = computed(() => [
  ...(props.readiness?.compute.ssh_cloud ?? []).map((node) => ({
    id: node.id,
    kind: 'ssh_cloud' as const,
    label: node.label,
    detail: `${node.user}@${node.host}`,
    ready: node.ready,
    readiness: node.readiness,
    blockers: node.blockers,
    heartbeat: node.last_probed_at,
    who: node.registered_by_label || node.registered_by_kind || '',
  })),
  ...(props.readiness?.compute.self_hosted ?? []).map((node) => ({
    id: node.id,
    kind: 'self_hosted' as const,
    label: node.label,
    detail: node.observed_state,
    ready: node.ready,
    readiness: node.readiness,
    blockers: node.blockers,
    heartbeat: node.last_seen_at,
    who: '',
  })),
])

function dateTime(value?: string) {
  if (!value) return t('Never', '从未')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function statusLabel(status: string) {
  if (status === 'waiting_compute' && activeAgents.value.length) return t('Agent registered', 'Agent 已注册')
  if (status === 'ready') return t('Ready', '就绪')
  if (status === 'blocked') return t('Blocked', '受阻')
  if (status === 'waiting_agent') return t('Waiting for Agent', '等待 Agent')
  if (status === 'waiting_compute') return t('Waiting for compute', '等待计算节点')
  return localizedState(status)
}

async function copyPrompt() {
  await navigator.clipboard.writeText(prompt.value)
  copied.value = true
  window.setTimeout(() => {
    copied.value = false
  }, 1600)
}

function actionTitle(action: AgentReadinessAction) {
  if (action.kind === 'copy_readiness') return t('Copy readiness prompt', '复制 Readiness Prompt')
  if (action.kind === 'handshake') return t('Register Agent', '注册 Agent')
  if (action.kind === 'grant_operate_nodes') return t('Grant operate_nodes', '授予 operate_nodes')
  if (action.kind === 'register_node') return t('Register a Cloud SSH host', '登记 Cloud SSH 主机')
  if (action.kind === 'open_nodes') return t('Open Nodes', '打开节点')
  return action.title
}

function actionDetail(action: AgentReadinessAction) {
  if (action.kind === 'copy_readiness') return t('Tell the Agent to call get_project_options, list the current Project binding, and explain heartbeats.', '告诉 Agent 调用 get_project_options，列出当前 Project 绑定，并说明心跳。')
  if (action.kind === 'handshake') return t('Issue a Project-scoped MCP setup link. Registration does not select a GPU or node.', '发一条 Project 范围的 MCP Setup Link；注册不会选择 GPU 或节点。')
  if (action.kind === 'grant_operate_nodes') return t('An Agent needs operate_nodes to register a Cloud SSH host. This scope is off by default.', 'Agent 需要 operate_nodes 才能登记 Cloud SSH 主机。该 scope 默认关闭。')
  if (action.kind === 'register_node') return t('Have the Agent call register_ssh_cloud_node, or register the host on the Nodes page.', '让 Agent 调用 register_ssh_cloud_node，或在节点页登记主机。')
  if (action.kind === 'open_nodes') return t('Probe, revoke, or finish runtime configuration. Do not ask the Agent to SSH for a workaround.', '去探测、吊销或补齐运行时配置。不要让 Agent 用 SSH 绕过。')
  return action.detail
}

function runAction(action: AgentReadinessAction) {
  if (action.kind === 'copy_readiness') {
    promptOpen.value = true
    return
  }
  if (action.kind === 'handshake' || action.kind === 'grant_operate_nodes') {
    emit('handshake')
    emit('openAgents')
    return
  }
  if (action.kind === 'register_node' || action.kind === 'open_nodes') {
    emit('openNodes')
  }
}
</script>

<template>
  <section v-if="loading && !readiness" class="agent-readiness" :class="{ compact }" :data-status="compact ? 'loading' : undefined">
    <p class="agent-readiness-loading">{{ t('Loading Agent Readiness...', '正在加载 Agent Readiness...') }}</p>
  </section>
  <section v-else-if="readiness" class="agent-readiness" :class="{ compact }" :data-status="presentationStatus">
    <header class="agent-readiness-heading">
      <div>
        <span class="research-kicker"><Activity :size="15" />{{ t('Agent Readiness', 'Agent Readiness') }}</span>
        <strong>{{ statusLabel(readiness.status) }}</strong>
        <p>{{ summary }}</p>
      </div>
      <div class="agent-readiness-actions">
        <button class="secondary-button small-button" type="button" @click="promptOpen = true">
          <Clipboard :size="16" />{{ t('Copy readiness', '复制 Readiness') }}
        </button>
        <button
          v-if="readiness.status === 'waiting_agent' || compact"
          class="primary-button small-button"
          type="button"
          @click="emit('handshake'); emit('openAgents')"
        >
          <Bot :size="16" />{{ readiness.status === 'waiting_agent' ? t('Register Agent', '注册 Agent') : t('Open Agents', '打开 Agents') }}
        </button>
      </div>
    </header>

    <dl class="agent-readiness-facts">
      <div>
        <dt><Bot :size="14" />{{ t('Agents', 'Agent') }}</dt>
        <dd>{{ activeAgents.length }} {{ t('active', '活跃') }}</dd>
        <small>{{ t('Last used', '最近使用') }} {{ dateTime(readiness.heartbeats.agent_last_used_at) }}</small>
      </div>
      <div>
        <dt><Cpu :size="14" />{{ t('Compute', '计算节点') }}</dt>
        <dd>{{ readyCompute.length }}/{{ visibleCompute.length }} {{ t('ready', '就绪') }}</dd>
        <small v-if="readiness.heartbeats.ssh_cloud_last_probed_at">{{ t('SSH probe', 'SSH 探测') }} {{ dateTime(readiness.heartbeats.ssh_cloud_last_probed_at) }}</small>
        <small v-else-if="readiness.heartbeats.self_hosted_last_seen_at">{{ t('Node seen', '节点心跳') }} {{ dateTime(readiness.heartbeats.self_hosted_last_seen_at) }}</small>
        <small v-else>{{ t('No compute heartbeat yet', '还没有计算节点心跳') }}</small>
      </div>
      <div v-if="!compact && runtime">
        <dt><Radio :size="14" />{{ t('Control plane', '控制平面') }}</dt>
        <dd>{{ runtime.scheduler_healthy && runtime.watchdog_healthy ? t('Healthy', '健康') : t('Check workers', '检查 worker') }}</dd>
        <small>scheduler {{ runtime.scheduler_healthy ? t('ok', '正常') : t('down', '异常') }} · watchdog {{ runtime.watchdog_healthy ? t('ok', '正常') : t('down', '异常') }}</small>
      </div>
    </dl>

    <template v-if="!compact">
      <div v-if="readiness.agents.length" class="table-scroll">
        <table class="data-table agent-readiness-table">
          <thead>
            <tr>
              <th>{{ t('Agent', 'Agent') }}</th>
              <th>Scopes</th>
              <th>{{ t('Last used', '最近使用') }}</th>
              <th>{{ t('Optional node registrations', '可选节点登记') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="agent in readiness.agents" :key="agent.id">
              <td>
                <strong>{{ agent.label }}</strong>
                <code>{{ agent.prefix }}</code>
                <span class="state-badge" :data-state="agent.status"><span />{{ localizedState(agent.status) }}</span>
              </td>
              <td><div class="scope-list"><span v-for="scope in agent.scopes" :key="scope">{{ scope }}</span></div></td>
              <td>{{ dateTime(agent.last_used_at) }}</td>
              <td>{{ agent.bound_node_ids.length ? agent.bound_node_ids.length : t('None', '无') }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <ul v-if="visibleCompute.length" class="agent-readiness-nodes">
        <li v-for="node in visibleCompute" :key="node.id" :data-ready="node.ready">
          <span class="research-kicker"><Server :size="13" />{{ node.kind === 'ssh_cloud' ? 'Cloud SSH' : t('Self-hosted', 'Self-hosted') }}</span>
          <strong>{{ node.label }}</strong>
          <p>{{ node.detail }} · {{ node.readiness }}<template v-if="node.who"> · {{ node.who }}</template></p>
          <small>{{ t('Heartbeat', '心跳') }} {{ dateTime(node.heartbeat) }}</small>
        </li>
      </ul>

      <ul class="contract-actions agent-readiness-next">
        <li v-for="action in readiness.next_actions" :key="action.kind">
          <button class="text-button" type="button" @click="runAction(action)">{{ actionTitle(action) }}</button>
          <span>{{ actionDetail(action) }}</span>
        </li>
      </ul>
    </template>

    <p v-else class="agent-readiness-compact-detail">
      {{ activeAgents.map((agent) => agent.label).join(', ') || t('No Agent', '没有 Agent') }}
      ·
      {{ visibleCompute.map((node) => node.label).join(', ') || t('No compute', '没有计算节点') }}
    </p>
  </section>

  <WorkbenchDialog
    v-model:open="promptOpen"
    :title="t('Readiness prompt', 'Readiness Prompt')"
    :label="t('Readiness prompt', 'Readiness Prompt')"
    :description="t('Give this to the connected Agent. It lists the Project binding the Owner sees and tells the Agent to call get_project_options. It contains no Token.', '交给已连接的 Agent。它列出 Owner 看到的 Project 绑定，并要求 Agent 调用 get_project_options。不含 Token。')"
  >
    <div class="dialog-form attach-prompt-form">
      <label class="attach-prompt-label">
        <span>{{ t('Agent prompt', 'Agent Prompt') }}</span>
        <textarea class="attach-prompt" readonly rows="18" spellcheck="false" :value="prompt"></textarea>
      </label>
      <button class="primary-button" type="button" @click="copyPrompt">
        <Check v-if="copied" :size="16" /><Clipboard v-else :size="16" />
        {{ copied ? t('Copied', '已复制') : t('Copy for Agent', '复制给 Agent') }}
      </button>
    </div>
  </WorkbenchDialog>
</template>
