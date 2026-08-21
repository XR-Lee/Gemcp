import type { Experiment, Project, Repository, ResearchWorkspace } from './api'
import type { Locale } from './i18n'

export type ResearchAttachPromptInput = {
  locale: Locale
  project?: Project | null
  repositories?: Repository[]
  experiments?: Experiment[]
  workspace?: ResearchWorkspace | null
}

const terminalStates = new Set(['succeeded', 'failed', 'cancelled', 'timed_out', 'budget_stopped', 'provider_error'])

function shortCommit(value?: string) {
  const sha = value?.trim() ?? ''
  return sha.length > 12 ? sha.slice(0, 12) : sha
}

function isOffGraph(experiment: Experiment, hasStudy: boolean) {
  if (experiment.orphaned) return true
  if (experiment.graph_linked) return false
  return hasStudy
}

function copy(locale: Locale, english: string, chinese: string) {
  return locale === 'zh' ? chinese : english
}

export function buildResearchAttachPrompt(input: ResearchAttachPromptInput) {
  const locale = input.locale
  const project = input.project ?? null
  const repositories = input.repositories ?? []
  const experiments = input.experiments ?? []
  const workspace = input.workspace ?? null
  const study = workspace?.study
  const hasStudy = Boolean(study) || (workspace?.studies.length ?? 0) > 0
  const projectID = project?.id || workspace?.project_id || ''
  const offGraph = experiments.filter((item) => isOffGraph(item, hasStudy))
  const lines: string[] = []
  const add = (english: string, chinese = english) => {
    lines.push(copy(locale, english, chinese))
  }

  add('# Attach this repository to the Gemcp research Graph', '# 把当前仓库挂上 Gemcp 研究 Graph')
  lines.push('')
  add(
    'You are a coding Agent with Gemcp MCP already configured. Do not ask the Owner for a Token. Do not put credentials, prompts, or private reasoning into research text.',
    '你是已经配置好 Gemcp MCP 的编码 Agent。不要向 Owner 索要 Token。不要把凭据、prompt 或私有推理写入研究文本。',
  )
  lines.push('')
  add('## Goal', '## 任务')
  add(
    'Inspect the current GitHub repository, then follow the Graph contract so this repository\'s work appears on the Owner research Graph. Recording a Graph node never starts a workload.',
    '检查当前 GitHub 仓库，再按 Graph 合同把该仓库的工作挂到 Owner 可见的研究 Graph 上。记录 Graph 节点不会启动作业。',
  )
  lines.push('')
  add('## Current snapshot', '## 当前快照')
  add(
    `Project: ${project?.name || 'unknown'} (${projectID || 'unknown'})`,
    `Project：${project?.name || '未知'}（${projectID || '未知'}）`,
  )
  if (!repositories.length) {
    add('- Repositories: none registered in this Project', '- 仓库：此 Project 尚未注册仓库')
  } else {
    add('- Repositories:', '- 仓库：')
    for (const repository of repositories) {
      lines.push(`  - ${repository.name} [${repository.status}] ${repository.ssh_url} @ ${repository.default_branch}`)
    }
  }
  if (study) {
    add(`- Study: ${study.name} (${study.id})`, `- Study：${study.name}（${study.id}）`)
    add(`- Question: ${study.question}`, `- 问题：${study.question}`)
    if (study.plan?.next_action) {
      add(`- Plan next action: ${study.plan.next_action}`, `- 计划中的下一步：${study.plan.next_action}`)
    }
    if (study.nodes.length) {
      add('- Graph nodes:', '- 图上节点：')
      for (const node of study.nodes) {
        const experiment = node.experiment_id ? ` experiment=${node.experiment_id}` : ''
        lines.push(`  - ${node.kind} ${node.id} ${node.title}${experiment}`)
      }
    }
  } else if ((workspace?.studies.length ?? 0) > 1) {
    add('- Studies: choose one study_id before writing the Graph', '- Studies：写入 Graph 前先选定 study_id')
    for (const item of workspace?.studies ?? []) {
      lines.push(`  - ${item.name} (${item.id}) ${item.status}`)
    }
  } else {
    add('- Study: none yet. Create one before a paid run.', '- Study：还没有。付费 run 之前先创建。')
  }
  if (workspace?.next_actions?.length) {
    add('- Legal next actions:', '- 合法下一步：')
    for (const action of workspace.next_actions) {
      const from = action.from_node_id ? ` from_node_id=${action.from_node_id}` : ''
      const experiment = action.experiment_id ? ` experiment_id=${action.experiment_id}` : ''
      lines.push(`  - ${action.kind} → ${action.tool}${from}${experiment}`)
    }
  } else {
    add(
      '- Legal next actions: call get_next_actions after the snapshot is current.',
      '- 合法下一步：快照更新后调用 get_next_actions。',
    )
  }
  if (offGraph.length) {
    add('- Off-graph Experiments (attach these first):', '- 未入图 Experiment（先挂这些）：')
    for (const experiment of offGraph) {
      const repo = repositories.find((item) => item.id === experiment.repository_id)
      lines.push(
        `  - ${experiment.id} state=${experiment.state} commit=${shortCommit(experiment.commit_sha) || 'unknown'} repo=${repo?.name || experiment.execution_context?.repository_name || 'unknown'}`,
      )
    }
  } else if (experiments.length && hasStudy) {
    add('- Off-graph Experiments: none', '- 未入图 Experiment：无')
  }

  lines.push('')
  add('## Standards', '## 必须遵守的标准')
  add('1. Call `get_research_workspace` and `get_next_actions` first. Only take a returned next action.', '1. 先调用 `get_research_workspace` 和 `get_next_actions`。只做返回的下一步。')
  add(
    '2. If no repository is `active`, use `list_repository_registrations`. Register or verify with `configure` if needed. Show the Deploy public key to a human. Never ask for a GitHub password.',
    '2. 若没有 `active` 仓库，先 `list_repository_registrations`。需要时用 `configure` 注册或验证。把 Deploy 公钥交给人去加。不要索要 GitHub 密码。',
  )
  add(
    '3. If there is no Study, call `update_research_workspace` to create one from the repository purpose. Do not invent training flags.',
    '3. 若还没有 Study，用 `update_research_workspace` 按仓库目的创建。不要编造训练 flag。',
  )
  add(
    '4. If there is no hypothesis or plan node, inspect the local repository, then record those nodes. A paid run cannot start from the question node alone. Use the question node as `from_node_id` when recording the first hypothesis.',
    '4. 若还没有 hypothesis 或 plan 节点，先检查本地仓库再记录这些节点。付费 run 不能只从 question 出发。记录第一条 hypothesis 时，`from_node_id` 用 question 节点。',
  )
  add(
    '5. To attach an existing Experiment, call `update_research_workspace` with `kind=run`, that `experiment_id`, a hypothesis or plan `from_node_id`, and `relation=leads_to`. After the Experiment is terminal, `close_run` is the only way to write its result.',
    '5. 挂已有 Experiment：调用 `update_research_workspace`，`kind=run`，带上该 `experiment_id`、hypothesis 或 plan 的 `from_node_id`，以及 `relation=leads_to`。Experiment 终态后，只能用 `close_run` 写 result。',
  )
  add(
    '6. New spend uses `prepare_experiment` with `from_node_id` on a hypothesis or plan node. Show the digest and wait for Owner confirmation before `submit_prepared_experiment`.',
    '6. 新的花钱走 `prepare_experiment`，`from_node_id` 必须是 hypothesis 或 plan。把 digest 给 Owner 确认后再 `submit_prepared_experiment`。',
  )
  add(
    '7. Do not free-write a result node. Do not invent metrics. Do not use `submit_experiment` for this attach path.',
    '7. 不要自由写 result 节点。不要编造指标。入图不要走 `submit_experiment`。',
  )
  if (offGraph.some((item) => terminalStates.has(item.state))) {
    add(
      '8. At least one off-graph Experiment is already terminal. Attach the run, then `close_run` with the Owner-visible metric. Do not launch a replacement job just to draw the Graph.',
      '8. 至少有一个未入图 Experiment 已经终态。先挂 run，再用 Owner 可见的指标 `close_run`。不要为了画图再开一趟作业。',
    )
  }

  lines.push('')
  add(
    'Done when the Owner research page shows this repository on the Graph, and Evidence no longer marks those Experiments as off-graph.',
    '完成标准：Owner 研究页能看到这个仓库已在 Graph 上，证据页不再把这些 Experiment 标成未入图。',
  )
  return `${lines.join('\n')}\n`
}
