const projectID = 'b492cbe4-f198-4d87-bbf9-3f77d8a3ab0a'
const experimentID = 'ec29dc68-9674-4611-9d8c-542f68f5e31c'
const empty = new URLSearchParams(window.location.search).has('empty')

const project = {
  id: projectID, name: 'Point Models', slug: 'point-models', status: 'active',
  monthly_budget_milli: 100000, max_experiment_milli: 20000, max_concurrency: 2, max_runtime_seconds: 86400,
  timeout_extension_seconds: 3600, termination_grace_seconds: 60, timezone: 'Asia/Shanghai',
}

const datasetBindingRecords: Array<{
  id: string; project_id: string; name: string; backend: string; canonical_root: string
  environment_variable: string; required_markers: string[]; sources: Array<{ url: string; relative_path: string }>
  status: string
}> = [{
  id: 'binding-scanobjectnn', project_id: projectID, name: 'scanobjectnn-objbg', backend: 'autodl_elastic',
  canonical_root: '/root/autodl-fs/datasets/ScanObjectNN', environment_variable: 'GEMCP_DATASET_SCANOBJECTNN_OBJBG',
  required_markers: ['main_split/train.h5'],
  sources: [{ url: 'https://huggingface.co/datasets/example/resolve/main/train.h5', relative_path: 'main_split/train.h5' }],
  status: 'active',
}]

const environmentRecords: Array<{
  id: string; project_id: string; name: string; backend: string; image_uuid: string; is_default: boolean; status: string
}> = [{
  id: 'environment-elastic', project_id: projectID, name: 'public-elastic', backend: 'autodl_elastic',
  image_uuid: 'image-6c15b8aad2', is_default: true, status: 'approved',
}]

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
    artifacts: ['gemcp-launch.log', 'run.log', 'metrics.json', 'gemcp-result.json'],
    artifact_manifest: [
      { name: 'gemcp-launch.log', media_type: 'text/plain', available: false, availability: 'shared_storage', readable: false, unavailable_reason: 'gemcp-launch.log stays on managed shared storage.' },
      { name: 'run.log', media_type: 'text/plain', size_bytes: 21, available: true, availability: 'control_plane', readable: true, checksum: 'sha256:preview-run' },
      { name: 'metrics.json', media_type: 'application/json', size_bytes: 28, available: true, availability: 'control_plane', readable: true, checksum: 'sha256:preview-metrics' },
      { name: 'gemcp-result.json', media_type: 'application/json', size_bytes: 64, available: true, availability: 'control_plane', readable: true, checksum: 'sha256:preview-result' },
    ],
    dataset_bindings: [{
      id: 'binding-scanobjectnn', name: 'scanobjectnn-objbg', backend: 'autodl_private',
      canonical_root: '/root/autodl-fs/datasets/ScanObjectNN', environment_variable: 'GEMCP_DATASET_SCANOBJECTNN_OBJBG',
      required_markers: ['main_split/train.h5'],
    }],
    budget_finalized_at: '2026-08-17T17:55:00Z',
    runner_attempt_id: 'a72afbc7-df86-4aaf-a7bc-68060968ed11', runner_source_downloads: 1,
    runner_stage: 'started', runner_stage_updated_at: '2026-08-17T17:55:00Z',
    log_tail: 'overall_accuracy 86.4\n', metrics: { overall_accuracy: 86.4 },
    created_at: '2026-08-17T17:40:00Z', updated_at: '2026-08-17T17:55:00Z', finished_at: '2026-08-17T17:55:00Z',
    assessment: {
      status: 'passed', classification: 'succeeded', summary: 'The Experiment succeeded.', cleanup_complete: true,
    },
    runner_stages: [{ stage: 'started', at: '2026-08-17T17:55:00Z' }],
    savable_workload: true,
    saved_workload: '',
    closable_run: true,
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
      { id: 'n-q', kind: 'question', title: 'OBJ-BG 遍历能否提高 ScanObjectNN 精度？', summary: '约束：不增加 GPU 小时。', status: 'open', occurred_at: '2024-03-12T00:00:00Z', commit_sha: 'aa11bb22cc33', created_at: '2026-08-17T16:00:00Z', updated_at: '2026-08-17T16:00:00Z' },
      { id: 'n-h', kind: 'hypothesis', title: '噪声背景是精度上限', summary: '旧遍历把背景点带进局部邻域。', status: 'open', occurred_at: '2024-06-01T09:00:00Z', commit_sha: 'bb22cc33dd44', branch: 'autoresearch/objbg-baseline', created_at: '2026-08-17T16:10:00Z', updated_at: '2026-08-17T16:10:00Z' },
      { id: 'n-p', kind: 'plan', title: '先复现 baseline', summary: '同一仓库、同一 digest，只换评估脚本。', status: 'open', occurred_at: '2025-02-18T10:00:00Z', created_at: '2026-08-17T16:20:00Z', updated_at: '2026-08-17T16:20:00Z' },
      { id: 'n-run', kind: 'run', title: 'OBJ-BG smoke', summary: 'prepared Experiment，未新开 GPU。', status: 'succeeded', experiment_id: experimentID, experiment_state: 'succeeded', occurred_at: '2026-08-17T17:40:00Z', commit_sha: '0123456789012345678901234567890123456789', branch: 'autoresearch/objbg-baseline', created_at: '2026-08-17T17:40:00Z', updated_at: '2026-08-17T17:55:00Z' },
      { id: 'n-r', kind: 'result', title: 'OBJ-BG smoke accuracy', summary: '现有 smoke Experiment 达到 86.4 overall accuracy。', status: 'succeeded', metric_name: 'overall_accuracy', metric_value: 86.4, experiment_id: experimentID, experiment_state: 'succeeded', occurred_at: '2026-08-17T17:55:00Z', commit_sha: '0123456789012345678901234567890123456789', branch: 'autoresearch/objbg-baseline', created_at: '2026-08-17T18:00:00Z', updated_at: '2026-08-17T18:00:00Z' },
      { id: 'n-o', kind: 'observation', title: '背景点仍进入 kNN', summary: '失败样本里邻域仍有桌面点。', status: 'open', occurred_at: '2025-11-02T18:04:00Z', commit_sha: 'cc33dd44ee55', created_at: '2026-08-17T18:02:00Z', updated_at: '2026-08-17T18:02:00Z' },
      { id: 'n-d', kind: 'decision', title: '下一步只改遍历，不换模型', summary: '先验证假设，再谈更大的训练。', status: 'open', occurred_at: '2025-11-03T09:00:00Z', created_at: '2026-08-17T18:05:00Z', updated_at: '2026-08-17T18:05:00Z' },
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
    hypotheses: [{
      id: 'n-h', title: '噪声背景是精度上限', summary: '旧遍历把背景点带进局部邻域。', status: 'open', branch: 'autoresearch/objbg-baseline',
      experiments: [{
        run_node_id: 'n-run', experiment_id: experimentID, title: 'OBJ-BG smoke',
        state: 'succeeded', branch: 'autoresearch/objbg-baseline', commit_sha: '0123456789012345678901234567890123456789',
        result_title: 'OBJ-BG smoke accuracy', highlight_title: '背景点仍进入 kNN',
      }],
    }],
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

function match(url: URL, method: string, body?: unknown): Response | null {
  const path = url.pathname
  if (path === '/api/v1/version') return json({ name: 'Gemcp', version: '0.20.0', commit: 'preview', built_at: '2026-08-27T00:00:00Z' })
  if (path === '/api/v1/setup/status') return json({ initialized: true })
  if (path === '/api/v1/auth/me') return json({ user_id: 'owner-id', tenant_id: 'tenant-id', email: 'owner@lab.local', role: 'owner' })
  if (path === '/api/v1/auth/logout' && method === 'POST') return json(undefined, 204)
  if (path === '/api/v1/runtime/status') {
    return json({
      scheduler_enabled: true, self_hosted_enabled: true, ssh_cloud_enabled: true, global_concurrency: 2, public_url_configured: true, public_url_https: true,
      scheduler_healthy: true, watchdog_healthy: true, notification_worker_healthy: true,
      generated_at: '2026-08-17T18:05:00Z',
    })
  }
  if (path === '/api/v1/projects') return json([project])
  if (path === `/api/v1/projects/${projectID}` && method === 'PATCH') {
    const update = body && typeof body === 'object' ? body : {}
    Object.assign(project, update)
    return json({ ...project })
  }
  if (path === `/api/v1/projects/${projectID}/dataset-bindings` && method === 'GET') {
    return json(datasetBindingRecords)
  }
  if (path === `/api/v1/projects/${projectID}/dataset-sources` && method === 'GET') {
    return json([{
      name: 'scanobjectnn-objbg', display_name: 'ScanObjectNN OBJ-BG', backend: 'autodl_elastic',
      canonical_root: '/root/autodl-fs/datasets/ScanObjectNN',
      required_markers: ['main_split/training_objectdataset_augmentedrot_scale75.h5'],
      notes: 'Register with HTTPS Hugging Face resolve URLs, then prepare_experiment with runtime_preset=provision.',
    }])
  }
  if (path === `/api/v1/projects/${projectID}/environments` && method === 'GET') {
    return json(environmentRecords)
  }
  if (path === `/api/v1/projects/${projectID}/environments` && method === 'POST') {
    const input = body && typeof body === 'object' ? body as Record<string, unknown> : {}
    const created = {
      id: `environment-${environmentRecords.length + 1}`, project_id: projectID,
      name: String(input.name || 'torch-train'), backend: String(input.backend || 'autodl_elastic'),
      image_uuid: String(input.image_uuid || 'image-visible1234'), is_default: Boolean(input.set_default),
      status: 'approved',
    }
    if (created.is_default) {
      for (const record of environmentRecords) record.is_default = false
    }
    const existing = environmentRecords.find((item) => item.name === created.name)
    if (existing) {
      Object.assign(existing, created, { id: existing.id })
      return json(existing)
    }
    environmentRecords.push(created)
    return json(created, 201)
  }
  if (path.startsWith(`/api/v1/projects/${projectID}/environments/`) && method === 'DELETE') {
    const id = path.split('/').pop()
    const record = environmentRecords.find((item) => item.id === id)
    if (record) record.status = 'disabled'
    return json(record ?? environmentRecords[0])
  }
  if (path === `/api/v1/projects/${projectID}/dataset-bindings` && method === 'POST') {
    const input = body && typeof body === 'object' ? body as Record<string, unknown> : {}
    const name = String(input.name || input.catalog || 'new-dataset')
    const created = {
      id: `binding-${datasetBindingRecords.length + 1}`, project_id: projectID, name,
      backend: String(input.backend || 'autodl_elastic'),
      canonical_root: String(input.canonical_root || '/root/autodl-fs/datasets/new'),
      environment_variable: `GEMCP_DATASET_${name.replace(/[^A-Za-z0-9]+/g, '_').toUpperCase()}`,
      required_markers: Array.isArray(input.required_markers) ? input.required_markers as string[] : [],
      sources: Array.isArray(input.sources) ? input.sources as Array<{ url: string; relative_path: string }> : [],
      status: 'active',
    }
    const existing = datasetBindingRecords.find((item) => item.name === name)
    if (existing) {
      Object.assign(existing, created, { id: existing.id, environment_variable: existing.environment_variable })
      return json(existing)
    }
    datasetBindingRecords.push(created)
    return json(created, 201)
  }
  if (path.startsWith(`/api/v1/projects/${projectID}/dataset-bindings/`) && method === 'DELETE') {
    const id = path.split('/').pop()
    const record = datasetBindingRecords.find((item) => item.id === id)
    if (record) record.status = 'disabled'
    return json(record ?? {
      id: 'binding-scanobjectnn', project_id: projectID, name: 'scanobjectnn-objbg', backend: 'autodl_elastic',
      canonical_root: '/root/autodl-fs/datasets/ScanObjectNN', environment_variable: 'GEMCP_DATASET_SCANOBJECTNN_OBJBG',
      required_markers: ['main_split/train.h5'], status: 'disabled',
    })
  }
  if (path === `/api/v1/projects/${projectID}/workloads` && method === 'GET') {
    return json([])
  }
  if (path === `/api/v1/projects/${projectID}/workloads/preview` && method === 'POST') {
    const input = body && typeof body === 'object' ? body as Record<string, unknown> : {}
    const name = String(input.name || 'oneshot')
    return json({
      name,
      manifest_yaml: `version: 1\nworkloads:\n  ${name}:\n    entrypoint:\n      - python\n      - train.py\n`,
    })
  }
  if (path === `/api/v1/projects/${projectID}/workloads` && method === 'POST') {
    const input = body && typeof body === 'object' ? body as Record<string, unknown> : {}
    const name = String(input.name || 'oneshot')
    experiments[0].savable_workload = false
    experiments[0].saved_workload = name
    return json({
      id: 'workload-preview-1', name, manifest_yaml: `version: 1\nworkloads:\n  ${name}:\n    entrypoint:\n      - python\n      - train.py\n`,
      entrypoint: ['python', 'train.py'], source_experiment_id: experimentID, created_at: '2026-08-17T18:00:00Z',
    }, 201)
  }
  if (path === `/api/v1/projects/${projectID}/experiment-proposals` && method === 'POST') {
    return json({
      proposal: {
        id: 'proposal-owner-1', project_id: projectID, eligible: true, requires_confirmation: true,
        repository: {
          id: repositories[0].id, name: repositories[0].name, ssh_url: repositories[0].ssh_url,
          requested_ref: 'main', commit_sha: '0123456789012345678901234567890123456789',
          default_branch: 'main', access: 'public_https',
        },
        execution: { mode: 'argv', argv: ['python', 'tools/smoke.py'], display_command: 'python tools/smoke.py' },
        resource: {
          environment_name: 'public-elastic', resource_profile_name: 'rtx4090', backend: 'autodl_elastic',
          image: 'image-uuid', gpu_models: ['RTX 4090'], gpu_num: 1,
        },
        runtime_preset: 'smoke', max_runtime_seconds: 300, reserved_cost_milli: 3825,
        checks: [{ id: 'budget', status: 'pass', summary: 'Budget can reserve' }],
        confirmation_digest: 'sha256:' + 'cd'.repeat(32),
        from_node_id: 'n-h', created_at: '2026-09-11T02:00:00Z', expires_at: '2026-09-11T04:00:00Z',
      },
    })
  }
  if (path.includes('/experiment-proposals/') && path.endsWith('/submit') && method === 'POST') {
    return json({ experiment: experiments[0], idempotent: false })
  }
  if (path === `/api/v1/projects/${projectID}/research`) return json(research)
  if (path === `/api/v1/projects/${projectID}/experiment-catalog`) {
    return json({
      project_id: projectID,
      generated_at: '2026-08-17T18:05:00Z',
      repositories: [{
        id: repositories[0].id, name: repositories[0].name, ssh_url: repositories[0].ssh_url,
        default_branch: repositories[0].default_branch, status: repositories[0].status,
        last_verified_at: repositories[0].last_verified_at, rows: [{
          id: 'catalog-row-1', repository_id: repositories[0].id,
          branch: 'autoresearch/sprint-beat-sast-20260817',
          setting: 'G2 seed-2 best-checkpoint Mean U-spec',
          method: 'U-spec unified organizer',
          implementation: 'SPRINT_G2_DECISION.md',
          metric: '90.4114',
          result: 'promote_U-spec versus SAST -0.4986 pp',
          link: 'SPRINT_G2_DECISION.md',
          hash: '79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0',
          created_at: '2026-08-17T18:00:00Z', updated_at: '2026-08-17T18:00:00Z',
        }],
      }],
    })
  }
  if (path === `/api/v1/projects/${projectID}/operations`) {
    return json({
      activities: [{
        id: 'activity-1', agent_label: 'training-agent', agent_token_prefix: 'gmc_abcd123',
        phase: 'monitoring', experiment_id: experimentID, at: '2026-08-17T18:05:00Z',
      }],
      proposals: [{
        id: 'proposal-elastic-1', status: 'prepared', eligible: true, agent_label: 'training-agent',
        agent_token_prefix: 'gmc_abcd123', repository_name: 'dynamic-point-mamba', requested_ref: 'autoresearch/m1-gapdelta-confirm-20260824',
        commit_sha: '0123456789012345678901234567890123456789', display_command: 'python tools/smoke.py --hostname',
        backend: 'autodl_elastic', environment_name: 'public-elastic', image: 'image-uuid', resource_profile_name: 'rtx4090',
        repository_access: 'ssh_deploy_key', repository_url: repositories[0].ssh_url, workload: 'objbg-smoke', dataset: 'scanobjectnn-objbg',
        gpu_models: ['RTX 4090'], gpu_num: 1, runtime_preset: 'smoke', max_runtime_seconds: 300,
        reserved_cost_milli: 3825, checks: [{ id: 'budget', status: 'pass', summary: 'Budget can reserve' }],
        confirmation_digest: 'sha256:' + 'ab'.repeat(32), created_at: '2026-08-27T16:00:00Z',
        updated_at: '2026-08-27T16:00:00Z', expires_at: '2026-08-27T18:00:00Z',
      }],
      generated_at: '2026-08-17T18:05:00Z',
    })
  }
  if (path === '/api/v1/repositories') return json(repositories)
  if (path === '/api/v1/experiments') return json(experiments)
  if (path === `/api/v1/experiments/${experimentID}`) return json(experiments[0])
  if (path === `/api/v1/experiments/${experimentID}/artifacts/metrics.json`) {
    return json({
      experiment_id: experimentID, name: 'metrics.json', media_type: 'application/json', size_bytes: 28,
      checksum: 'sha256:preview-metrics', truncated: false, available: true, availability: 'control_plane',
      json: { overall_accuracy: 86.4 },
    })
  }
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
  if (path === `/api/v1/projects/${projectID}/image-bakes/options`) {
    return json({
      project_id: projectID, backend: 'autodl_pro', default_recipe_path: 'requirements.gemcp.txt',
      repositories: [{ id: repositories[0].id, name: repositories[0].name, default_branch: 'main' }],
      base_images: [{ uuid: 'image-6c15b8aad2', name: 'torch' }], generated_at: '2026-08-17T18:00:00Z',
    })
  }
  if (path === `/api/v1/projects/${projectID}/image-bakes`) {
    return json({
      bakes: [{
        id: 'image-bake-1', project_id: projectID, repository_id: repositories[0].id, name: 'torch-mamba', backend: 'autodl_pro',
        base_image_uuid: 'image-6c15b8aad2', commit_sha: 'a'.repeat(40), recipe_path: 'requirements.gemcp.txt',
        status: 'requested', confirmation_digest: `sha256:${'e'.repeat(64)}`, requested_by: 'owner-id', requested_by_type: 'user',
        proposal: {
          backend: 'autodl_pro', name: 'torch-mamba', base_image_uuid: 'image-6c15b8aad2',
          repository_id: repositories[0].id, commit_sha: 'a'.repeat(40), recipe_path: 'requirements.gemcp.txt',
        },
        estimated_cost_milli: 0, created_at: '2026-08-17T17:00:00Z', updated_at: '2026-08-17T17:00:00Z',
      }],
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
  if (path === `/api/v1/projects/${projectID}/agent-readiness`) {
    return json({
      project_id: projectID, project_name: project.name, status: 'ready',
      summary: '1 Agent(s) and 1 ready compute target(s). Binding is Project-scoped, not exclusive to one node.',
      generated_at: '2026-08-17T18:05:00Z',
      agents: [{
        id: 'agent-token-1', label: 'default-agent', prefix: 'gmc_abcd123',
        scopes: ['read', 'submit', 'cancel'], status: 'active', last_used_at: '2026-08-17T18:00:00Z',
        can_read: true, can_submit: true, can_operate_nodes: false, bound_node_ids: ['ssh-cloud-1'],
      }],
      compute: {
        ssh_cloud_enabled: true,
        ssh_cloud: [{
          id: 'ssh-cloud-1', label: 'cloud-4090', status: 'active', host: '203.0.113.10', user: 'ubuntu',
          gpus: [{ name: 'NVIDIA GeForce RTX 4090', memory_bytes: 25769803776 }],
          ready: true, readiness: 'ready', blockers: [], last_probed_at: '2026-08-17T18:00:00Z',
          runtime_configured: true, bound_to_project: true, registered_by_kind: 'agent', registered_by_label: 'default-agent',
        }],
        self_hosted: [{
          id: node.id, label: node.label, status: node.status, observed_state: node.observed_state,
          gpus: [{ name: 'NVIDIA GeForce RTX 3090', memory_bytes: 25769803776 }],
          ready: false, readiness: 'runtime_configuration_required', blockers: ['runtime_configuration_required'],
          last_seen_at: node.last_seen_at, runtime_configured: false,
        }],
      },
      heartbeats: {
        agent_last_used_at: '2026-08-17T18:00:00Z',
        ssh_cloud_last_probed_at: '2026-08-17T18:00:00Z',
        self_hosted_last_seen_at: node.last_seen_at,
        note: 'MCP last_used_at updates on every authenticated tool call. Monitor with get_experiment.',
      },
      next_actions: [
        { kind: 'copy_readiness', title: 'Copy readiness prompt', detail: 'Tell the Agent to call get_project_options.' },
        { kind: 'open_nodes', title: 'Open Nodes', detail: 'Finish Self-hosted runtime configuration.' },
      ],
      instructions: {
        inspect_tool: 'get_project_options', monitor_tool: 'get_experiment',
        heartbeat: 'MCP last_used_at updates on every authenticated tool call. Self-hosted last_seen_at is the gemcp-node heartbeat. Cloud SSH last_probed_at updates when get_project_options probes or the Owner probes. Monitor running work with get_experiment.',
        binding: 'Agents are Project-scoped. They are not exclusively bound to one node.',
      },
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
  if (path === `/api/v1/projects/${projectID}/agent-enrollments` && method === 'POST') {
    return json({
      enrollment: {
        id: 'enrollment-preview', project_id: projectID, label: 'preview-handshake',
        scopes: ['read', 'submit', 'cancel', 'operate_nodes'], status: 'pending',
        expires_at: '2026-08-27T01:00:00Z', token_expires_in_days: 90,
        created_at: '2026-08-26T00:30:00Z', updated_at: '2026-08-26T00:30:00Z',
      },
      setup_url: 'https://gemcp.example.com/agent/setup#code=preview-one-time',
    }, 201)
  }
  if (path === '/api/v1/nodes') {
    return json({ nodes: [node], enrollments: [], assignments: [] })
  }
  if (path === '/api/v1/ssh-cloud-nodes') {
    return json({
      experimental: true,
      warning: 'Experimental observer: Gemcp stores an encrypted SSH password or private key and opens outbound SSH.',
      enabled: true,
      nodes: [{
        id: 'ssh-cloud-1', label: 'cloud-4090', status: 'active', experimental: true,
        warning: 'Experimental observer: Gemcp stores an encrypted SSH password or private key.',
        host: '203.0.113.10', port: 22, user: 'ubuntu', auth_method: 'private_key',
        host_key_fingerprint: 'SHA256:preview-fingerprint', project_ids: [projectID],
        created_actor_type: 'agent', created_actor_id: 'preview-token',
        project_runtimes: [],
        inventory: { gpus: [{ name: 'NVIDIA GeForce RTX 4090' }] },
        created_at: '2026-08-22T10:00:00Z', updated_at: '2026-08-22T10:00:00Z',
      }],
      assignments: [],
    })
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
  if (path === '/api/v1/provider') return json({ providers: [provider] })
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
    let body: unknown
    if (typeof init?.body === 'string') {
      try {
        body = JSON.parse(init.body)
      } catch {
        // Non-JSON preview requests do not need a parsed body.
      }
    }
    return match(url, method, body) ?? json({ error: { code: 'NOT_FOUND', message: `${method} ${url.pathname}` } }, 404)
  }
}
