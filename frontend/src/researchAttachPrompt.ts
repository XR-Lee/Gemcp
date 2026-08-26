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
  const nodes = study?.nodes ?? []
  const scientificNodes = nodes.filter((node) => node.kind !== 'question')
  const hypotheses = nodes.filter((node) => node.kind === 'hypothesis')
  const observations = nodes.filter((node) => node.kind === 'observation')
  const decisions = nodes.filter((node) => node.kind === 'decision')
  const lines: string[] = []
  const add = (english: string, chinese = english) => {
    lines.push(copy(locale, english, chinese))
  }

  add('# Reconstruct this repository on the Gemcp research Graph', '# 把仓库的实验谱系画到 Gemcp 研究 Graph 上')
  lines.push('')
  add(
    'You are a coding Agent in this repository directory, with Gemcp MCP enabled for this workspace only. You may never have used Gemcp before. This Graph is a scientific lineage map for the Owner, not a git commit graph and not a four-node stub. Do not ask the Owner for a Token. Do not put credentials, prompts, or private reasoning into research text.',
    '你是在这个仓库目录里工作的编码 Agent，Gemcp MCP 只开在当前工作区，不是全局。你可能从没做过 Gemcp。这张 Graph 是给 Owner 看的科学谱系，不是 git commit 图，也不是四个节点的空壳。不要向 Owner 索要 Token。不要把凭据、prompt 或私有推理写入研究文本。',
  )
  lines.push('')
  add('## Goal', '## 任务')
  add(
    'Walk the whole local repository. Collapse its experimental branches, methods, ablations, datasets, failed turns, and reported numbers into key exploration nodes. Put the important hypotheses, conclusions, and evidence on the Owner Graph so a later Agent can see what was already tried. Recording a Graph node never starts a workload. The Owner time axis uses `occurred_at` (git committer date or Experiment time), not the moment you called the tool.',
    '通读整个本地仓库。把不同实验分支、方法、消融、数据集、失败转向和已报道数字收成关键探索节点。把重要假设、结论和证据画到 Owner 可见的 Graph 上，让后来的 Agent 看得出已经试过什么。记录 Graph 节点不会启动作业。Owner 顶轴用 `occurred_at`（git commit 时间或 Experiment 时间），不是你调用工具的时刻。',
  )
  lines.push('')
  add('## What the Graph is', '## Graph 是什么')
  add(
    'One Study has one `question` node. Everything else is lineage under that question. Use these kinds:',
    '一个 Study 只有一个 `question` 节点。其余都是这个问题下的谱系。节点种类：',
  )
  add(
    '- `hypothesis`: one distinct experimental claim, method, ablation, dataset split, or failed direction.',
    '- `hypothesis`：一条独立的实验主张、方法、消融、数据划分或失败方向。',
  )
  add(
    '- `observation`: evidence that already exists in the repo (README, paper table, log, figure caption, commit note). This is how historical results become visible. Do not invent numbers.',
    '- `observation`：仓库里已经有的证据（README、论文表、日志、图注、commit 说明）。历史结果靠它上图。不要编造数字。',
  )
  add(
    '- `decision`: a keep / drop / next-direction conclusion after evidence.',
    '- `decision`：证据之后的保留、放弃或下一步方向结论。',
  )
  add(
    '- `plan`: only the current remaining work, not a dump of all history.',
    '- `plan`：只写当前还要做的事，不要把全部历史塞进一个 plan。',
  )
  add(
    '- `run` / `result`: only Gemcp Experiments. Bind an existing Experiment as `run`. After it is terminal, `close_run` is the only way to write `result`. Never free-write a result to fake history.',
    '- `run` / `result`：只用于 Gemcp Experiment。已有 Experiment 挂成 `run`。终态后只能用 `close_run` 写 `result`。不要自由写 result 来伪造历史。',
  )
  add(
    'Legal edges: `question -leads_to-> hypothesis`; `hypothesis -leads_to-> observation|plan|run|decision`; `hypothesis -compares-> hypothesis`; `observation -leads_to-> decision|hypothesis`; `observation -supports|contradicts-> hypothesis` when that hypothesis is created after the observation. Every observation must hang off a hypothesis with `from_node_id` and `relation=leads_to`. Isolated observations are a layout bug, not a style. To attach an existing observation, pass its `id` plus `from_node_id`.',
    '合法边：`question -leads_to-> hypothesis`；`hypothesis -leads_to-> observation|plan|run|decision`；`hypothesis -compares-> hypothesis`；`observation -leads_to-> decision|hypothesis`；后建 hypothesis 时可用 `observation -supports|contradicts-> hypothesis`。每条 observation 都必须挂在一条 hypothesis 上（`from_node_id` + `relation=leads_to`）。孤立观察是错误，不是风格。给已有 observation 补边时，传它的 `id` 和 `from_node_id`。',
  )
  lines.push('')
  add('## How to inspect the repository', '## 怎么检查仓库')
  add(
    'Do not stop after README. Read papers, result tables, configs, training/eval scripts, experiment directories, named branches, ablation folders, and git history that changed methods or reported metrics. Group by scientific claim, not by every commit or file. One node per distinct claim, result, or decision. Compress near-duplicates into one observation that names the variants. For that node, set `occurred_at` from `git log -1 --format=%cI <sha>` of the evidence commit, and pass `commit_sha`. The Graph is still not a commit graph.',
    '不要只看 README。还要读论文、结果表、配置、训练/评测脚本、实验目录、命名分支、消融目录，以及改过方法或报过指标的 git 历史。按科学主张分组，不要每个 commit 或每个文件一个节点。一条独立主张、结果或结论对应一个节点。相近变体收进同一条 observation，并写清变体。给该节点设 `occurred_at`：用证据 commit 的 `git log -1 --format=%cI <sha>`，并传 `commit_sha`。Graph 仍然不是 commit 图。',
  )
  lines.push('')
  add('## Coverage bar', '## 覆盖标准')
  add(
    'A mature research repository that produced only question + one hypothesis + one plan + one extra node is incomplete. Typical first imports of a large lab repo have many hypotheses and observations, often 15 to 60 nodes, not 4. Stay under 128 nodes. Stop when another node would duplicate an existing claim, not when `get_next_actions` first mentions `prepare_experiment`.',
    '一个成熟研究仓如果只画出 question + 一条 hypothesis + 一个 plan + 再一个节点，就是没画完。大型实验室仓库首次入图通常有很多 hypothesis 和 observation，常见 15 到 60 个节点，不是 4 个。不要超过 128 个节点。停止条件是再画就会重复已有主张，而不是 `get_next_actions` 第一次提到 `prepare_experiment`。',
  )
  add(
    '`get_next_actions` is the paid-run legality list. During first import it is not a stop signal. If it asks you to prepare a run after one hypothesis, keep reconstructing the repo lineage first. Do not launch a paid job just to draw the Graph.',
    '`get_next_actions` 是付费 run 的合法动作列表。首次入图时它不是停手信号。如果它在只有一条 hypothesis 时就要你准备 run，先继续把仓库谱系画完。不要为了画图去开付费作业。',
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
    if (study.repository) {
      add(
        `- Bound repository: ${study.repository.name} [${study.repository.status}] ${study.repository.ssh_url} @ ${study.repository.default_branch}`,
        `- 已绑定仓库：${study.repository.name} [${study.repository.status}] ${study.repository.ssh_url} @ ${study.repository.default_branch}`,
      )
    }
    if (study.plan?.next_action) {
      add(`- Plan next action: ${study.plan.next_action}`, `- 计划中的下一步：${study.plan.next_action}`)
    }
    add(
      `- Graph now: ${nodes.length} nodes (${hypotheses.length} hypotheses, ${observations.length} observations, ${decisions.length} decisions).`,
      `- 图上现有：${nodes.length} 个节点（${hypotheses.length} 条 hypothesis，${observations.length} 条 observation，${decisions.length} 条 decision）。`,
    )
    if (scientificNodes.length < 8) {
      add(
        '- Coverage: thin. Keep reconstructing branches, evidence, and conclusions from the repository. Do not treat this as done.',
        '- 覆盖：过薄。继续从仓库补实验分支、证据和结论。不要当成已经完成。',
      )
    }
    if (nodes.length) {
      add('- Graph nodes:', '- 图上节点：')
      for (const node of nodes) {
        const experiment = node.experiment_id ? ` experiment=${node.experiment_id}` : ''
        const occurred = node.occurred_at ? ` occurred_at=${node.occurred_at}` : ' MISSING occurred_at'
        const commit = node.commit_sha ? ` commit=${shortCommit(node.commit_sha)}` : ''
        const summary = node.summary ? ` — ${node.summary}` : ''
        lines.push(`  - ${node.kind} ${node.id} ${node.title}${experiment}${occurred}${commit}${summary}`)
      }
    }
    const missingTime = nodes.filter((node) => !node.occurred_at)
    if (missingTime.length) {
      add(
        `- Evidence time missing on ${missingTime.length} node(s). Update each with its id, the same title, occurred_at from git committer date, and commit_sha. The Owner axis falls back to MCP write time until you do.`,
        `- 有 ${missingTime.length} 个节点没有证据时间。用节点 id、原 title、git committer 日期的 occurred_at 和 commit_sha 更新它们。补上之前，Owner 顶轴会退回 MCP 写入时间。`,
      )
    }
  } else if ((workspace?.studies.length ?? 0) > 1) {
    add('- Studies: choose one study_id before writing the Graph', '- Studies：写入 Graph 前先选定 study_id')
    for (const item of workspace?.studies ?? []) {
      lines.push(`  - ${item.name} (${item.id}) ${item.status}`)
    }
  } else {
    add('- Study: none yet. Create one from the repository purpose before writing lineage.', '- Study：还没有。先按仓库目的创建，再写谱系。')
  }
  if (workspace?.next_actions?.length) {
    add('- Legal next actions (not a stop list during first import):', '- 合法下一步（首次入图时不是停手清单）：')
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
    add('- Off-graph Gemcp Experiments (bind these after the historical lineage is on the Graph):', '- 未入图的 Gemcp Experiment（历史谱系上图后再挂这些）：')
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
  add('## Procedure', '## 步骤')
  add(
    '1. Call `get_research_workspace`. Use `get_next_actions` to learn paid-run constraints, then keep reconstructing until coverage is honest.',
    '1. 调用 `get_research_workspace`。用 `get_next_actions` 了解付费 run 约束，然后继续重建，直到覆盖如实。',
  )
  add(
    '2. If no repository is `active`, use `list_repository_registrations`. Register or verify with `configure` if needed. Show the Deploy public key to a human. Never ask for a GitHub password.',
    '2. 若没有 `active` 仓库，先 `list_repository_registrations`。需要时用 `configure` 注册或验证。把 Deploy 公钥交给人去加。不要索要 GitHub 密码。',
  )
  add(
    '3. If there is no Study, call `update_research_workspace` to create one from the repository purpose. Do not invent training flags.',
    '3. 若还没有 Study，用 `update_research_workspace` 按仓库目的创建。不要编造训练 flag。',
  )
  add(
    '4. Reconstruct the lineage with many `update_research_workspace` node writes. Start each first-wave hypothesis from the question node (`from_node_id` + `relation=leads_to`). Hang every observation off its hypothesis the same way. On every historical node set `occurred_at` and `commit_sha` from the evidence commit (`git log -1 --format=%cI <sha>`). Then add competing hypotheses (`compares`) and decisions for conclusions. Never create an unlinked observation. A paid run cannot start from the question node alone.',
    '4. 用多次 `update_research_workspace` 写节点来重建谱系。第一波 hypothesis 从 question 出发（`from_node_id` + `relation=leads_to`）。每条 observation 也用同样方式挂到对应 hypothesis 上。每个历史节点都要带证据 commit 的 `occurred_at` 和 `commit_sha`（`git log -1 --format=%cI <sha>`）。再补互相竞争的 hypothesis（`compares`）和结论的 decision。不要创建没有边的 observation。付费 run 不能只从 question 出发。',
  )
  add(
    '5. To attach an existing Gemcp Experiment, call `update_research_workspace` with `kind=run`, that `experiment_id`, a hypothesis or plan `from_node_id`, and `relation=leads_to`. After the Experiment is terminal, `close_run` is the only way to write its result.',
    '5. 挂已有 Gemcp Experiment：调用 `update_research_workspace`，`kind=run`，带上该 `experiment_id`、hypothesis 或 plan 的 `from_node_id`，以及 `relation=leads_to`。Experiment 终态后，只能用 `close_run` 写 result。',
  )
  add(
    '6. New spend uses `prepare_experiment` with `from_node_id` on a hypothesis or plan node, and only after the historical map is in place. The Project budget is a limit, not approval. Show the Owner the immutable proposal and exact confirmation digest, then wait for explicit confirmation of that digest before calling `submit_prepared_experiment`; report the submitted run in Evidence.',
    '6. 新的花钱走 `prepare_experiment`，`from_node_id` 必须是 hypothesis 或 plan，而且要等历史谱系先上图。Project 预算只是上限，不是批准。向 Owner 原样展示不可变提案和精确 confirmation digest，并等待 Owner 明确确认该 digest 后才能调用 `submit_prepared_experiment`；提交后在 Evidence 中报告运行。',
  )
  add(
    '7. Do not free-write a result node. Do not invent metrics. Do not use `submit_experiment` for this attach path.',
    '7. 不要自由写 result 节点。不要编造指标。入图不要走 `submit_experiment`。',
  )
  if (offGraph.some((item) => terminalStates.has(item.state))) {
    add(
      '8. At least one off-graph Experiment is already terminal. After the historical lineage is visible, attach the run, then `close_run` with the Owner-visible metric. Do not launch a replacement job just to draw the Graph.',
      '8. 至少有一个未入图 Experiment 已经终态。历史谱系可见之后，先挂 run，再用 Owner 可见的指标 `close_run`。不要为了画图再开一趟作业。',
    )
  }

  lines.push('')
  add(
    'Done when the Owner can follow the repository\'s competing experimental branches on the Graph, see the important hypotheses, conclusions, and evidence as nodes, and Evidence no longer marks existing Gemcp Experiments as off-graph. A four-node Graph on a large research repo is not done.',
    '完成标准：Owner 能在 Graph 上跟上这个仓库互相竞争的实验分支，看见重要的假设、结论和证据节点；证据页不再把已有 Gemcp Experiment 标成未入图。大型研究仓只画出四个节点，不算完成。',
  )
  return `${lines.join('\n')}\n`
}
