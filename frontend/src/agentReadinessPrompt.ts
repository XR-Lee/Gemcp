import type { AgentReadiness, RuntimeStatus } from './api'
import type { Locale } from './i18n'

export type AgentReadinessPromptInput = {
  locale?: Locale
  readiness: AgentReadiness
  runtime?: RuntimeStatus | null
}

function copy(locale: Locale | undefined, english: string, chinese: string) {
  return locale === 'zh' ? chinese : english
}

function gpuLabel(gpus?: Array<{ name: string }>) {
  const names = (gpus ?? []).map((gpu) => gpu.name.trim()).filter(Boolean)
  return names.length ? names.join(', ') : ''
}

function formatTime(value?: string) {
  if (!value) return ''
  const parsed = new Date(value)
  if (Number.isNaN(parsed.getTime())) return value
  return parsed.toISOString()
}

export function buildAgentReadinessPrompt(input: AgentReadinessPromptInput) {
  const locale = input.locale
  const readiness = input.readiness
  const runtime = input.runtime
  const lines: string[] = []
  const add = (english: string, chinese = english) => {
    lines.push(copy(locale, english, chinese))
  }

  add('# Gemcp Agent Readiness', '# Gemcp Agent Readiness')
  lines.push('')
  add(
    `You are a coding Agent for Project ${readiness.project_name}. Gemcp MCP is already enabled in this repository directory. Do not ask the Owner for a Token. Do not print credentials or SSH material.`,
    `你是 Project ${readiness.project_name} 的编码 Agent。Gemcp MCP 已经开在这个仓库目录里。不要向 Owner 再要 Token。不要打印凭据或 SSH 材料。`,
  )
  lines.push('')
  add('## Inspect first', '## 先检查')
  add(
    `Call \`${readiness.instructions.inspect_tool}\`. That response is the source of truth for available backends, node readiness, blockers, and the heartbeat contract. Then call \`${readiness.instructions.monitor_tool}\` only after a run exists.`,
    `先调用 \`${readiness.instructions.inspect_tool}\`。那份响应才是可用后端、节点就绪、blocker 和心跳约定的权威来源。只有已经有 run 时才调用 \`${readiness.instructions.monitor_tool}\`。`,
  )
  lines.push('')
  add('## Owner snapshot', '## Owner 当前看到的绑定')
  add(`Status: ${readiness.status}`, `状态：${readiness.status}`)
  add(
    'Agents are Project-scoped. They are not exclusively bound to one node. Do not invent a host, and do not fall back to AutoDL, Self-hosted, or Cloud SSH unless that backend is the selected one.',
    'Agent 属于整个 Project，不是独占绑死一台机器。不要发明主机，也不要回退到未选中的 AutoDL、Self-hosted 或 Cloud SSH。',
  )
  lines.push('')
  add('### Agents', '### Agent')
  if (!readiness.agents.length) {
    add('- None. Ask the Owner for a handshake / MCP setup link.', '- 还没有。请 Owner 发握手 / MCP Setup Link。')
  } else {
    for (const agent of readiness.agents) {
      const used = agent.last_used_at ? formatTime(agent.last_used_at) : copy(locale, 'never', '从未')
      const nodes = agent.bound_node_ids.length ? agent.bound_node_ids.join(', ') : copy(locale, 'no registered Cloud SSH host', '没有由它登记的 Cloud SSH 主机')
      add(
        `- ${agent.label} (${agent.prefix}) scopes=${agent.scopes.join(', ')} status=${agent.status} last_used_at=${used} registered_nodes=${nodes}`,
        `- ${agent.label}（${agent.prefix}）scopes=${agent.scopes.join(', ')} status=${agent.status} last_used_at=${used} 登记节点=${nodes}`,
      )
    }
  }
  lines.push('')
  add('### Compute', '### 计算节点')
  const ssh = readiness.compute.ssh_cloud ?? []
  const selfHosted = readiness.compute.self_hosted ?? []
  if (!ssh.length && !selfHosted.length) {
    add(
      readiness.compute.ssh_cloud_enabled
        ? '- No Cloud SSH or Self-hosted node is visible. If you have operate_nodes, call register_ssh_cloud_node. Otherwise ask the Owner.'
        : '- No Self-hosted node is visible. Do not invent Cloud SSH while it is disabled.',
      readiness.compute.ssh_cloud_enabled
        ? '- 还没有可见的 Cloud SSH 或 Self-hosted 节点。如果你有 operate_nodes，调用 register_ssh_cloud_node；否则请 Owner 处理。'
        : '- 还没有可见的 Self-hosted 节点。Cloud SSH 未开启时不要发明一台。',
    )
  }
  for (const node of ssh) {
    const gpus = gpuLabel(node.gpus)
    const who = node.registered_by_label || node.registered_by_kind || copy(locale, 'unknown', '未知')
    add(
      `- Cloud SSH ${node.label} ${node.user}@${node.host} ready=${node.ready} readiness=${node.readiness} bound=${node.bound_to_project} registered_by=${who}${gpus ? ` gpus=${gpus}` : ''}${node.blockers.length ? ` blockers=${node.blockers.join(',')}` : ''}`,
      `- Cloud SSH ${node.label} ${node.user}@${node.host} ready=${node.ready} readiness=${node.readiness} 已绑定=${node.bound_to_project} 登记者=${who}${gpus ? ` gpus=${gpus}` : ''}${node.blockers.length ? ` blockers=${node.blockers.join(',')}` : ''}`,
    )
  }
  for (const node of selfHosted) {
    const gpus = gpuLabel(node.gpus)
    add(
      `- Self-hosted ${node.label} ready=${node.ready} readiness=${node.readiness} observed=${node.observed_state}${gpus ? ` gpus=${gpus}` : ''}${node.blockers.length ? ` blockers=${node.blockers.join(',')}` : ''}`,
      `- Self-hosted ${node.label} ready=${node.ready} readiness=${node.readiness} observed=${node.observed_state}${gpus ? ` gpus=${gpus}` : ''}${node.blockers.length ? ` blockers=${node.blockers.join(',')}` : ''}`,
    )
  }
  lines.push('')
  add('## Heartbeats', '## 心跳')
  add(
    'MCP last_used_at updates on every authenticated tool call. Self-hosted last_seen_at is the gemcp-node heartbeat. Cloud SSH last_probed_at updates when get_project_options probes or the Owner probes. Monitor running work with get_experiment. Do not SSH for logs.',
    'MCP 的 last_used_at 会在每次已认证工具调用时更新。Self-hosted 的 last_seen_at 是 gemcp-node 心跳。Cloud SSH 的 last_probed_at 在 get_project_options 探测或 Owner 探测时更新。有 run 后只用 get_experiment 监控。不要用 SSH 看日志。',
  )
  add(
    `Owner last Agent use: ${formatTime(readiness.heartbeats.agent_last_used_at) || copy(locale, 'never', '从未')}. Cloud SSH last probe: ${formatTime(readiness.heartbeats.ssh_cloud_last_probed_at) || copy(locale, 'never', '从未')}. Self-hosted last seen: ${formatTime(readiness.heartbeats.self_hosted_last_seen_at) || copy(locale, 'never', '从未')}.`,
    `Owner 看到的 Agent 最近使用：${formatTime(readiness.heartbeats.agent_last_used_at) || '从未'}。Cloud SSH 最近探测：${formatTime(readiness.heartbeats.ssh_cloud_last_probed_at) || '从未'}。Self-hosted 最近心跳：${formatTime(readiness.heartbeats.self_hosted_last_seen_at) || '从未'}。`,
  )
  if (runtime) {
    add(
      `Control plane: scheduler_healthy=${runtime.scheduler_healthy} watchdog_healthy=${runtime.watchdog_healthy}. Those are Owner-side workers, not something you ping over SSH.`,
      `控制平面：scheduler_healthy=${runtime.scheduler_healthy} watchdog_healthy=${runtime.watchdog_healthy}。这是 Owner 侧的 worker，不是让你用 SSH 去 ping。`,
    )
  }
  add(
    'You are Project-scoped, not exclusively bound to one node.',
    '你属于整个 Project，不是独占绑死一台机器。',
  )
  lines.push('')
  add('## After inspection', '## 检查之后')
  add(
    'If a host is missing and you have operate_nodes, register it. If a blocker is runtime_configuration_required or host_key_changed, tell the Owner. Prepare work with prepare_experiment, show the Owner the immutable proposal and exact confirmation digest, and wait for explicit confirmation of that digest before submit_prepared_experiment. Monitor only with get_experiment.',
    '如果缺少主机且你有 operate_nodes，就去登记。如果 blocker 是 runtime_configuration_required 或 host_key_changed，告诉 Owner。用 prepare_experiment 准备作业，向 Owner 原样展示不可变提案和精确 confirmation digest，并等待 Owner 明确确认该 digest 后才能调用 submit_prepared_experiment。只通过 get_experiment 监控。',
  )
  return lines.join('\n')
}
