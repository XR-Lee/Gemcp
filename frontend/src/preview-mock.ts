const projectID = 'b492cbe4-f198-4d87-bbf9-3f77d8a3ab0a'
const experimentID = 'ec29dc68-9674-4611-9d8c-542f68f5e31c'
const empty = new URLSearchParams(window.location.search).has('empty')

const project = {
  id: projectID, name: 'Point Models', slug: 'point-models', status: 'active',
  monthly_budget_milli: 100000, max_experiment_milli: 20000, max_concurrency: 2, max_runtime_seconds: 86400,
  timeout_extension_seconds: 3600, termination_grace_seconds: 60, timezone: 'Asia/Shanghai',
}

const repositories = [
  {
    id: '558f97ca-b648-4e7e-a329-0e03bfd58155', project_id: projectID, name: 'dynamic-point-mamba',
    ssh_url: 'git@github.com:research/dynamic-point-mamba.git', default_branch: 'main', status: 'active',
    deploy_public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakePublicKeyForVisualTestingOnly gemcp-test',
    host_key_fingerprint: 'SHA256:trusted-test-fingerprint', last_verified_at: '2026-07-16T08:22:00Z',
  },
]

const experiments = [
  {
    id: experimentID, project_id: projectID, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'succeeded', desired_state: 'running',
    commit_sha: '0123456789012345678901234567890123456789', execution_mode: 'argv',
    argv: ['python', 'train.py', '--config', 'configs/scanobjectnn.yaml'],
    command: 'python train.py --config configs/scanobjectnn.yaml',
    max_runtime_seconds: 14400, reserved_cost_milli: 12250, estimated_cost_milli: 80,
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/ec29dc68/',
    runner_attempt_id: 'a72afbc7-df86-4aaf-a7bc-68060968ed11', runner_source_downloads: 1,
    runner_stage: 'started', runner_stage_updated_at: '2026-08-17T17:55:00Z',
    log_tail: 'overall_accuracy 86.4\n', metrics: { overall_accuracy: 86.4 },
    created_at: '2026-08-17T17:40:00Z', updated_at: '2026-08-17T17:55:00Z', finished_at: '2026-08-17T17:55:00Z',
    graph_linked: true, orphaned: false,
  },
  {
    id: 'aa11bb22-3344-5566-7788-99aabbccddee', project_id: projectID, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'failed', desired_state: 'running',
    commit_sha: 'fedcba9876543210fedcba9876543210fedcba98', execution_mode: 'argv',
    argv: ['python', 'train.py', '--orphan'], command: 'python train.py --orphan',
    max_runtime_seconds: 1800, reserved_cost_milli: 4200, estimated_cost_milli: 80,
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/aa11bb22/',
    created_at: '2026-08-17T16:10:00Z', updated_at: '2026-08-17T16:20:00Z', finished_at: '2026-08-17T16:20:00Z',
    graph_linked: false, orphaned: true,
  },
]

const research = {
  project_id: projectID,
  generated_at: '2026-08-17T18:05:00Z',
  studies: empty ? [] : [{
    id: 'study-objbg-1', name: 'objbg-scan',
    question: '更干净的 OBJ-BG 遍历能否在不增加 GPU 小时的前提下提高 ScanObjectNN 精度？',
    status: 'active', updated_at: '2026-08-17T18:05:00Z',
  }],
  study: empty ? undefined : {
    id: 'study-objbg-1', name: 'objbg-scan',
    question: '更干净的 OBJ-BG 遍历能否在不增加 GPU 小时的前提下提高 ScanObjectNN 精度？',
    summary: 'Owner 先看科学问题；后续 Experiment 只作为证据挂到 Graph 上。',
    status: 'active', updated_at: '2026-08-17T18:05:00Z',
    plan: {
      id: 'plan-objbg-2', status: 'active',
      goal: '先建立可复现的 OBJ-BG baseline，再决定要不要换遍历。',
      next_action: '把 smoke run 的 86.4 精度记成第一条 Graph 结果。',
      rationale: '先让 Owner 看到问题和证据，再谈新的 reservation。',
      steps: [
        { title: '挂上已有 smoke Experiment', detail: '现在不要提交新的 run。' },
        { title: '对比旧遍历的失败点', detail: '只记观察，不改代码。' },
      ],
      created_at: '2026-08-17T17:00:00Z', updated_at: '2026-08-17T18:05:00Z',
    },
    nodes: [
      { id: 'n-q', kind: 'question', title: 'OBJ-BG 遍历能否提高 ScanObjectNN 精度？', summary: '约束：不增加 GPU 小时。', status: 'open', created_at: '2026-08-17T16:00:00Z', updated_at: '2026-08-17T16:00:00Z' },
      { id: 'n-h', kind: 'hypothesis', title: '噪声背景是精度上限', summary: '旧遍历把背景点带进局部邻域。', status: 'open', created_at: '2026-08-17T16:10:00Z', updated_at: '2026-08-17T16:10:00Z' },
      { id: 'n-p', kind: 'plan', title: '先复现 baseline', summary: '同一仓库、同一 digest，只换评估脚本。', status: 'open', created_at: '2026-08-17T16:20:00Z', updated_at: '2026-08-17T16:20:00Z' },
      { id: 'n-run', kind: 'run', title: 'OBJ-BG smoke', summary: 'prepared Experiment，未新开 GPU。', status: 'succeeded', experiment_id: experimentID, experiment_state: 'succeeded', created_at: '2026-08-17T17:40:00Z', updated_at: '2026-08-17T17:55:00Z' },
      { id: 'n-r', kind: 'result', title: 'OBJ-BG smoke accuracy', summary: '现有 smoke Experiment 达到 86.4 overall accuracy。', status: 'succeeded', metric_name: 'overall_accuracy', metric_value: 86.4, experiment_id: experimentID, experiment_state: 'succeeded', created_at: '2026-08-17T18:00:00Z', updated_at: '2026-08-17T18:00:00Z' },
      { id: 'n-o', kind: 'observation', title: '背景点仍进入 kNN', summary: '失败样本里邻域仍有桌面点。', status: 'open', created_at: '2026-08-17T18:02:00Z', updated_at: '2026-08-17T18:02:00Z' },
      { id: 'n-d', kind: 'decision', title: '下一步只改遍历，不换模型', summary: '先验证假设，再谈更大的训练。', status: 'open', created_at: '2026-08-17T18:05:00Z', updated_at: '2026-08-17T18:05:00Z' },
    ],
    edges: [
      { id: 'e1', from_id: 'n-q', to_id: 'n-h', relation: 'leads_to' },
      { id: 'e2', from_id: 'n-h', to_id: 'n-p', relation: 'leads_to' },
      { id: 'e3', from_id: 'n-p', to_id: 'n-run', relation: 'leads_to' },
      { id: 'e4', from_id: 'n-run', to_id: 'n-r', relation: 'produced' },
      { id: 'e5', from_id: 'n-r', to_id: 'n-h', relation: 'supports' },
      { id: 'e6', from_id: 'n-h', to_id: 'n-o', relation: 'leads_to' },
      { id: 'e7', from_id: 'n-o', to_id: 'n-d', relation: 'leads_to' },
    ],
  },
  next_actions: empty ? [] : [{
    kind: 'record_decision', tool: 'update_research_workspace', study_id: 'study-objbg-1',
    from_node_id: 'n-r', title: 'Record a decision from OBJ-BG smoke accuracy',
    detail: 'Say whether the result supports the hypothesis before preparing another run.',
  }],
}

const node = {
  id: 'node-11111111-2222-4333-8444-555555555555', label: 'lab-gpu-01', token_prefix: 'gmn_test',
  status: 'active', observed_state: 'online', installation_id: 'installation-id', machine_fingerprint: 'a'.repeat(64),
  hostname: 'gpu-workstation', operating_system: 'linux',
  architecture: 'amd64', agent_version: '0.16.1', protocol_version: '1',
  capabilities: { cpu_count: 16, memory_bytes: 68719476736, gpus: [{ uuid: 'GPU-test', name: 'NVIDIA GeForce RTX 3090', memory_bytes: 25769803776 }] },
  storage: { root: '/var/lib/gemcp-node/storage', total_bytes: 1099511627776, available_bytes: 824633720832, managed_bytes: 21474836480 },
  project_ids: [projectID], last_seen_at: '2026-08-17T18:00:00Z', approved_at: '2026-08-17T12:00:00Z',
  created_at: '2026-08-17T11:00:00Z', updated_at: '2026-08-17T18:00:00Z',
}

function json(data: unknown, status = 200) {
  return new Response(JSON.stringify(status >= 400 ? data : { data }), {
    status,
    headers: { 'Content-Type': 'application/json' },
  })
}

function match(url: URL, method: string): Response | null {
  const path = url.pathname
  if (path === '/api/v1/version') return json({ name: 'Gemcp', version: '0.16.1', commit: '0b68bed', built_at: '2026-08-17T18:00:00Z' })
  if (path === '/api/v1/setup/status') return json({ initialized: true })
  if (path === '/api/v1/auth/me') return json({ user_id: 'owner-id', tenant_id: 'tenant-id', email: 'owner@lab.local', role: 'owner' })
  if (path === '/api/v1/auth/logout' && method === 'POST') return json(undefined, 204)
  if (path === '/api/v1/runtime/status') {
    return json({
      scheduler_enabled: true, self_hosted_enabled: true, global_concurrency: 2, public_url_configured: true,
      scheduler_healthy: true, watchdog_healthy: true, notification_worker_healthy: true,
      generated_at: '2026-08-17T18:05:00Z',
    })
  }
  if (path === '/api/v1/projects') return json([project])
  if (path === `/api/v1/projects/${projectID}/research`) return json(research)
  if (path === `/api/v1/projects/${projectID}/operations`) {
    return json({
      activities: [{
        id: 'activity-1', agent_label: 'training-agent', agent_token_prefix: 'gmc_abcd123',
        phase: 'monitoring', experiment_id: experimentID, at: '2026-08-17T18:05:00Z',
      }],
      proposals: [],
      generated_at: '2026-08-17T18:05:00Z',
    })
  }
  if (path === '/api/v1/repositories') return json(repositories)
  if (path === '/api/v1/experiments') return json(experiments)
  if (path === `/api/v1/experiments/${experimentID}`) return json(experiments[0])
  if (path === `/api/v1/experiments/${experimentID}/attempts`) {
    return json([{
      id: 'attempt-1', number: 1, state: 'succeeded', estimated_cost_milli: 80,
      log_tail: 'overall_accuracy 86.4\n', metrics: { overall_accuracy: 86.4 },
      started_at: '2026-08-17T17:40:00Z', finished_at: '2026-08-17T17:55:00Z',
      created_at: '2026-08-17T17:40:00Z', updated_at: '2026-08-17T17:55:00Z',
    }])
  }
  if (path === `/api/v1/projects/${projectID}/diagnostics/options`) {
    return json({
      project_id: projectID, generated_at: '2026-08-17T18:00:00Z',
      repositories: [{ id: repositories[0].id, name: repositories[0].name, default_branch: 'main' }],
      environments: [{ id: 'environment-id', name: 'torch-cuda11.8', backend: 'autodl_private', image_uuid: 'base-image-1' }],
      resource_profiles: [{ id: 'profile-id', name: 'one-rtx-3090', backend: 'autodl_private', gpu_names: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, price_to_milli: 1000 }],
      suites: [
        { id: 'gpu_connectivity', runtime_seconds: 180, requires_pytorch: false, checks_cuda_compute: false },
        { id: 'pytorch_cuda', runtime_seconds: 300, requires_pytorch: true, checks_cuda_compute: true },
      ],
    })
  }
  if (path === `/api/v1/projects/${projectID}/diagnostics`) {
    return json({
      runs: [{
        id: 'diagnostic-1', project_id: projectID, backend: 'autodl_private', suite: 'gpu_connectivity',
        requested_by: 'owner-id', experiment: { id: 'diagnostic-experiment-1', state: 'succeeded' },
        assessment: { status: 'passed', classification: 'diagnostic_passed', summary: 'GPU diagnostic completed successfully.', cleanup_complete: true },
        created_at: '2026-08-17T17:00:00Z', updated_at: '2026-08-17T17:05:00Z',
      }],
    })
  }
  if (path === '/api/v1/finance') {
    return json({
      period: url.searchParams.get('period') || '2026-08', audit_scope: 'organization', generated_at: '2026-08-17T18:00:00Z',
      totals: {
        base_budget_milli: 100000, reserved_milli: 12250, charged_milli: 80,
        credits_milli: 0, debits_milli: 0, committed_milli: 12330, available_milli: 87670,
      },
      projects: [{
        id: projectID, name: project.name, status: project.status, timezone: project.timezone,
        base_budget_milli: 100000, reserved_milli: 12250, charged_milli: 80,
        credits_milli: 0, debits_milli: 0, committed_milli: 12330, available_milli: 87670,
      }],
      daily: [{ date: '2026-08-17', reserved_milli: 12250, charged_milli: 80, credits_milli: 0, debits_milli: 0 }],
      backends: [{ backend: 'autodl_private', experiments: 1, reserved_milli: 12250, charged_milli: 80 }],
      ledger: [], audit: [],
    })
  }
  if (path === `/api/v1/projects/${projectID}/agent-tokens`) {
    return json({
      tokens: [{
        id: 'agent-token-1', project_id: projectID, label: 'default-agent', prefix: 'gmc_abcd123',
        scopes: ['read', 'submit', 'cancel'], status: 'active',
        last_used_at: '2026-08-17T18:00:00Z', created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-17T18:00:00Z',
      }],
      enrollments: [],
      mcp_url: 'https://gemcp.example.com/mcp',
      config_file_name: 'gemcp-point-models-mcp.json',
      config_template: { mcpServers: { 'gemcp-point-models': { type: 'http', url: 'https://gemcp.example.com/mcp', headers: { Authorization: 'Bearer ${GEMCP_AGENT_TOKEN}' } } } },
    })
  }
  if (path === '/api/v1/nodes') {
    return json({ nodes: [node], enrollments: [], assignments: [] })
  }
  if (path === `/api/v1/projects/${projectID}/self-hosted-runtimes`) {
    return json({
      environments: [{ id: 'environment-self-hosted', name: 'local-3090', image: 'registry.example/train@sha256:abc', is_default: true }],
      resource_profiles: [{ id: 'profile-self-hosted', name: 'local-3090', gpu_names: ['NVIDIA GeForce RTX 3090'], cpu_limit: 8, memory_gb: 32, is_default: true }],
      trusted_workspaces: [],
    })
  }
  const provider = {
    id: 'provider-id', name: 'AutoDL Private Cloud', base_url: 'https://private.autodl.com',
    backend: 'private', status: 'active', credential_configured: true,
    last_validated_at: '2026-08-17T18:00:00Z', created_at: '2026-08-01T00:00:00Z', updated_at: '2026-08-17T18:00:00Z',
  }
  if (path === '/api/v1/provider') return json(provider)
  if (path === '/api/v1/provider/query' && method === 'POST') {
    return json({
      generated_at: '2026-08-17T18:00:00Z', provider,
      gpu_stock: [{ name: 'NVIDIA GeForce RTX 3090', idle: 2, total: 9 }],
      private_images: [], system_images: [], deployments: [], active_containers: [], cached_containers: [],
    })
  }
  if (path === '/api/v1/provider/managed-resources') return json([])
  if (path === '/api/v1/notifications/settings') {
    return json({
      configured: true, enabled: true, host: 'smtp.example.com', port: 587, tls_mode: 'starttls',
      username: 'mailer@example.com', password_configured: true, from_address: 'mailer@example.com',
      recipients: ['owner@lab.local'], status: 'ready', last_tested_at: '2026-08-17T12:00:00Z', updated_at: '2026-08-17T12:00:00Z',
    })
  }
  if (path === '/api/v1/notifications') return json([])
  return null
}

export function installPreviewMock() {
  const original = window.fetch.bind(window)
  window.fetch = async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(typeof input === 'string' ? input : input instanceof URL ? input.toString() : input.url, window.location.origin)
    if (!url.pathname.startsWith('/api/')) return original(input, init)
    const method = (init?.method ?? (typeof input === 'object' && 'method' in input ? input.method : 'GET') ?? 'GET').toUpperCase()
    return match(url, method) ?? json({ error: { code: 'NOT_FOUND', message: `${method} ${url.pathname}` } }, 404)
  }
}
