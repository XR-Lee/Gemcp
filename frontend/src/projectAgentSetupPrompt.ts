import type { AgentScope } from './api'

export type ProjectAgentSetupPromptInput = {
  locale: 'en' | 'zh'
  projectName?: string
  setupUrl: string
  scopes: AgentScope[]
}

export function projectAgentSetupPrompt(input: ProjectAgentSetupPromptInput): string {
  const project = input.projectName?.trim() || (input.locale === 'zh' ? '当前 Project' : 'this Project')
  const scopes = input.scopes.join(', ')

  if (input.locale === 'zh') {
    return [
      '# Gemcp Project Agent 注册',
      '',
      `你是在研究仓库目录里工作的编码 Agent。下面的链接只把你注册到 Project ${project}，不绑定 GPU、节点或 Provider。`,
      'Gemcp MCP 只开在当前仓库目录，不要开成全局 MCP。不要打印、回显或转发 Setup Link 或凭据；由 Setup 流程写入安全配置。',
      '',
      '## 注册',
      `1. 打开一次性 Setup Link：${input.setupUrl}`,
      '2. 按页面完成 MCP 安装，并 reload 或重启 MCP 客户端一次。',
      '3. 调用 `get_usage_guide` 和 `get_project_options`，确认当前 Project。',
      '',
      `Scopes: ${scopes}`,
      '',
      'Project 预算是费用上限，不是逐次授权。计算后端和 GPU 由 Project 运行选项另行决定，不属于 Agent 注册。',
      '需要运行时调用 `prepare_experiment`。原样向 Owner 展示不可变提案和精确 confirmation digest，并等待 Owner 对这个 digest 明确确认；确认前绝不能调用 `submit_prepared_experiment`。提交后用 `get_experiment` 监控。',
    ].join('\n')
  }

  return [
    '# Gemcp Project Agent registration',
    '',
    `You are a coding Agent working in a research repository directory. The link below registers you only to Project ${project}; it does not bind a GPU, node, or Provider.`,
    'Enable Gemcp MCP for this repository directory only, not globally. Never print, echo, or forward the Setup Link or credentials; let setup write them to secure configuration.',
    '',
    '## Register',
    `1. Open the one-time Setup Link: ${input.setupUrl}`,
    '2. Complete MCP setup, then reload or restart the MCP client once.',
    '3. Call `get_usage_guide` and `get_project_options` to verify the current Project.',
    '',
    `Scopes: ${scopes}`,
    '',
    'The Project budget is a spending limit, not per-run approval. Compute backends and GPUs are selected separately from Project runtime options; they are not part of Agent registration.',
    'When work is ready, call `prepare_experiment`. Show the Owner the immutable proposal and exact confirmation digest, then wait for the Owner to explicitly confirm that digest. Never call `submit_prepared_experiment` before that confirmation; after submission, monitor with `get_experiment`.',
  ].join('\n')
}
