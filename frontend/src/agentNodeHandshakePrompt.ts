export type HandshakeNodeTarget = {
  label: string
  user: string
  host: string
  port: number
  registered?: boolean
}

export type AgentNodeHandshakePromptInput = {
  locale?: 'zh' | 'en'
  projectName?: string
  setupUrl?: string
  scopes?: string[]
  sshCloudEnabled?: boolean
  operateNodes?: boolean
  node?: HandshakeNodeTarget | null
  lastError?: string
}

export function sshCommand(user: string, host: string, port: number) {
  return port === 22 ? `ssh ${user}@${host}` : `ssh -p ${port} ${user}@${host}`
}

export function agentNodeHandshakePrompt(input: AgentNodeHandshakePromptInput): string {
  const project = (input.projectName || '').trim() || (input.locale === 'zh' ? '当前 Project' : 'this Project')
  const scopes = (input.scopes?.length ? input.scopes : ['read']).join(', ')
  const node = input.node
  const ssh = node ? sshCommand(node.user, node.host, node.port) : ''
  const error = (input.lastError || '').trim()
  if (input.locale === 'zh') {
    return [
      `# Gemcp 握手`,
      ``,
      `你是在这个研究仓库目录里工作的编码 Agent。把 Gemcp MCP 只开在当前目录，不要开成全局。不要向 Owner 再要 Token。不要打印、保存或回显凭据。`,
      ...(input.sshCloudEnabled
        ? [`Gemcp 对 Cloud SSH 只做通道和观察者：收下 SSH、启动 argv、投影日志和退出码。不要装 Docker，不要锁镜像。`]
        : []),
      ``,
      `Project：${project}`,
      ``,
      `## 1. 先握手`,
      ...(input.setupUrl
        ? [
            `用下面的一次性 Setup Link 开通 Gemcp MCP。不要打印或转发，也不要交给 Web fetch、搜索或预览工具。`,
            ``,
            input.setupUrl,
            ``,
            `开通后 reload 一次 MCP。调用 get_usage_guide 和 get_project_options。`,
          ]
        : [
            `还没有 Setup Link。停下来，让 Owner 在 Gemcp 控制台 Agent 页点「注册 Agent」，把 Project Setup Prompt 发给你。不要自己发明 Token。`,
          ]),
      ``,
      `这个握手授予的 scope：${scopes}`,
      input.operateNodes
        ? `包含 operate_nodes：你可以登记 Cloud SSH 主机并轮换凭据。凭据只写不回读。`
        : `没有 operate_nodes：不要调用 register_ssh_cloud_node。已有节点从 get_project_options 读取。`,
      ``,
      `## 2. 按仓库配环境，再登记节点`,
      ...(input.sshCloudEnabled
        ? [
            `环境和数据由你按这个仓库自己配。用你自己的 SSH。不要装 gemcp-node，不要装 Docker 或 NVIDIA toolkit，不要让 Gemcp 装环境。有没有 GPU 都可以登记。`,
            `向 Owner 索取 SSH 密码或私钥。不要向 Gemcp HTTP/MCP 要凭据。`,
            ...(node
              ? [
                  ``,
                  `目标主机`,
                  `- 标签：${node.label}`,
                  `- SSH：${ssh}`,
                  `- 用户：${node.user}`,
                  ...(error ? [`- 控制面探测失败：${error}`] : []),
                  node.registered
                    ? `这台主机可能已经在 Gemcp 里。先看 get_project_options.ssh_cloud_nodes。没有再调用 register_ssh_cloud_node。`
                    : `配好后调用 register_ssh_cloud_node，传入上面的 ssh 行以及密码或私钥。探测只检查连通并钉 host key。`,
                ]
              : input.operateNodes
                ? [`配好后调用 register_ssh_cloud_node，传入 ssh 行以及 Owner 给的密码或私钥。探测只检查连通并钉 host key。`]
                : [`要登记新主机时，让 Owner 重新发带 operate_nodes 的握手 Prompt。`]),
          ]
        : [`用 get_project_options 看可用后端。不要发明 SSH 主机，也不要回退到未选中的后端。`]),
      ``,
      `## 3. 开训`,
      `调用 get_next_actions。prepare_experiment 传入 argv 和可选的远端绝对 cwd。不要传 image。仓库可选。`,
      `Project 预算只是费用上限，不是逐次授权。prepare_experiment 返回后，原样向 Owner 展示不可变提案和精确 confirmation digest，并等待 Owner 对这个 digest 明确确认；确认前绝不能调用 submit_prepared_experiment。`,
      `提交后只用 get_experiment 看 state、log_tail 和可选 metrics。不要为了看日志再 SSH。终态后 close_run。`,
      ``,
      `完成标准：MCP 已开通、节点在 get_project_options 里可见（如需），且 Owner 已确认精确 digest 的运行已提交并在监控。`,
    ].join('\n')
  }
  return [
    `# Gemcp handshake`,
    ``,
    `You are a coding Agent in this research repository directory. Enable Gemcp MCP here only, not as a global MCP. Do not ask the Owner for a Token. Never print, store, or echo credentials.`,
    ...(input.sshCloudEnabled
      ? [`For Cloud SSH, Gemcp is only a channel and observer: it stores SSH, starts argv, and projects logs and exit status. Do not install Docker or lock an image.`]
      : []),
    ``,
    `Project: ${project}`,
    ``,
    `## 1. Handshake first`,
    ...(input.setupUrl
      ? [
          `Use the one-time setup link below to enable Gemcp MCP. Do not print or forward it, and do not pass it to a Web-fetch, search, or preview tool.`,
          ``,
          input.setupUrl,
          ``,
          `Reload MCP once after setup. Call get_usage_guide and get_project_options.`,
        ]
      : [
          `There is no setup link in this prompt. Stop and ask the Owner to open Agents in the Gemcp console, click Register Agent, and send you the Project setup prompt. Do not invent a Token.`,
        ]),
    ``,
    `Scopes granted by this handshake: ${scopes}`,
    input.operateNodes
      ? `operate_nodes is included: you may register Cloud SSH hosts and rotate their credentials. Credentials are write-only.`
      : `operate_nodes is not included: do not call register_ssh_cloud_node. Read existing nodes from get_project_options.`,
    ``,
    `## 2. Prepare the host from the repository, then register it`,
    ...(input.sshCloudEnabled
      ? [
          `Set up the environment and data yourself from this repository over your own SSH. Do not install gemcp-node, Docker, or the NVIDIA toolkit. Do not ask Gemcp to install software. A GPU is optional.`,
          `Ask the Owner for the SSH password or private key. Do not request credentials from Gemcp HTTP/MCP.`,
          ...(node
            ? [
                ``,
                `Target host`,
                `- Label: ${node.label}`,
                `- SSH: ${ssh}`,
                `- User: ${node.user}`,
                ...(error ? [`- Control-plane probe failed: ${error}`] : []),
                node.registered
                  ? `This host may already be in Gemcp. Check get_project_options.ssh_cloud_nodes first. Call register_ssh_cloud_node only if it is missing.`
                  : `After the host is ready, call register_ssh_cloud_node with the ssh command above and the password or private key. Probe only checks connectivity and pins the host key.`,
              ]
            : input.operateNodes
              ? [`After the host is ready, call register_ssh_cloud_node with the ssh command and the Owner-supplied password or key. Probe only checks connectivity and pins the host key.`]
              : [`To register a new host, ask the Owner to grant operate_nodes separately. Project Agent registration itself does not bind this host.`]),
        ]
      : [`Use get_project_options to see available backends. Do not invent an SSH host, and do not fall back to another backend.`]),
    ``,
    `## 3. Start training`,
    `Call get_next_actions. prepare_experiment with argv and an optional absolute remote cwd. Omit image. Repository is optional.`,
    `The Project budget is a spending limit, not per-run approval. After prepare_experiment returns, show the Owner the immutable proposal and exact confirmation digest, then wait for the Owner to explicitly confirm that digest. Never call submit_prepared_experiment before that confirmation.`,
    `After submit, monitor only with get_experiment (state, log_tail, optional metrics). Do not SSH again for logs. Then close_run.`,
    ``,
    `Done when MCP is enabled, the node is visible in get_project_options if needed, and the run whose exact digest the Owner confirmed is submitted and being monitored.`,
  ].join('\n')
}
