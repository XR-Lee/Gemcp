import { readFile, unlink } from 'node:fs/promises'
import { expect, test, type Page, type Route } from '@playwright/test'

const build = { name: 'Gemcp', version: '0.15.0', commit: 'b'.repeat(40), built_at: '2026-07-16T00:00:00Z' }
const project = {
  id: 'b492cbe4-f198-4d87-bbf9-3f77d8a3ab0a', name: 'Point Models', slug: 'point-models', status: 'active',
  monthly_budget_milli: 100000, max_experiment_milli: 20000, max_concurrency: 2, max_runtime_seconds: 86400,
  timeout_extension_seconds: 3600, termination_grace_seconds: 60, timezone: 'Asia/Shanghai',
}
const repositories = [
  {
    id: '558f97ca-b648-4e7e-a329-0e03bfd58155', project_id: project.id, name: 'dynamic-point-mamba',
    ssh_url: 'git@github.com:research/dynamic-point-mamba.git', default_branch: 'main', status: 'active',
    deploy_public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFakePublicKeyForVisualTestingOnly gemcp-test',
    host_key_fingerprint: 'SHA256:trusted-test-fingerprint', last_verified_at: '2026-07-16T08:22:00Z',
  },
  {
    id: 'cd3bb6ae-fc36-47f7-8715-ce044b676515', project_id: project.id, name: 'new-baseline',
    ssh_url: 'git@github.com:research/new-baseline.git', default_branch: 'main', status: 'pending_key',
    deploy_public_key: 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAnotherFakePublicKeyForVisualTestingOnly gemcp-test',
  },
]
const experiments = [
  {
    id: 'ec29dc68-9674-4611-9d8c-542f68f5e31c', project_id: project.id, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'provisioning', desired_state: 'running',
    commit_sha: '0123456789012345678901234567890123456789', execution_mode: 'argv', argv: ['python', 'train.py', '--config', 'configs/scanobjectnn.yaml'], command: 'python train.py --config configs/scanobjectnn.yaml',
    max_runtime_seconds: 14400, reserved_cost_milli: 12250, estimated_cost_milli: 0,
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/ec29dc68/',
    artifacts: ['gemcp-launch.log', 'run.log', 'metrics.json'],
    runner_attempt_id: 'a72afbc7-df86-4aaf-a7bc-68060968ed11', runner_source_downloads: 2,
    runner_stage: 'source_extracted', runner_stage_updated_at: '2026-07-16T09:31:15Z',
    log_tail: 'epoch 3 loss=0.42\nepoch 4 loss=0.38\n', metrics: { loss: 0.38, epoch: 4 },
    execution_context: {
      agent_label: 'training-agent', agent_token_prefix: 'gmc_abcd123', proposal_id: 'proposal-live-1',
      repository_name: 'dynamic-point-mamba', repository_ssh_url: 'git@github.com:research/dynamic-point-mamba.git', requested_ref: 'main',
      backend: 'autodl_private', environment_name: 'torch-cuda11.8', image: 'base-image-1', resource_profile_name: 'one-rtx-3090', region: 'private',
      gpu_models: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, workspace_policy: 'runner_temporary', container_output_path: '/gemcp/output',
      runtime_info: { source: 'runner_observed', working_directory: '/gemcp/work/source', output_directory: '/gemcp/output', cuda_visible_devices: '0', gpu_devices: [{ index: 0, uuid: 'GPU-test-3090', name: 'NVIDIA GeForce RTX 3090' }] },
    },
    backend_observation: { kind: 'provider_resource', id: 'managed-resource-1', provider_id: 'deployment-live-1', state: 'active', status: 'running', cleanup_complete: false, updated_at: '2026-07-17T02:00:00Z' },
    timeline: [
      { at: '2026-07-16T09:30:00Z', code: 'experiment_created' },
      { at: '2026-07-16T09:31:00Z', code: 'attempt_created', detail: 'Attempt #1' },
      { at: '2026-07-16T09:31:15Z', code: 'runner_started', detail: 'Runtime paths and GPU observed' },
    ],
    created_at: '2026-07-16T09:30:00Z', updated_at: '2026-07-17T02:00:00Z',
    graph_linked: true, orphaned: false,
  },
  {
    id: '11276758-f089-49e8-b706-0aa1cd0f9ac0', project_id: project.id, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'failed', desired_state: 'running',
    commit_sha: 'abcdefabcdefabcdefabcdefabcdefabcdefabcd', command: 'python evaluate.py --checkpoint latest.pt',
    max_runtime_seconds: 3600, reserved_cost_milli: 3500, estimated_cost_milli: 2180,
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/11276758/', failure_code: 'PROCESS_EXIT',
    failure_reason: 'Process exited with status 1', created_at: '2026-07-15T05:20:00Z', updated_at: '2026-07-15T06:02:00Z',
    finished_at: '2026-07-15T06:02:00Z', exit_code: 1, metrics: { accuracy: 0.82 },
    graph_linked: false, orphaned: true,
  },
  {
    id: '33aa44bb-55cc-6677-8899-aabbccddeeff', project_id: project.id, repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id', state: 'succeeded', desired_state: 'running',
    commit_sha: 'fedcba9876543210fedcba9876543210fedcba98', execution_mode: 'argv',
    argv: ['python', 'tools/smoke.py', '--label', 'oneshot'], command: "python tools/smoke.py --label oneshot",
    max_runtime_seconds: 300, reserved_cost_milli: 3825, estimated_cost_milli: 80, budget_finalized_at: '2026-07-16T10:05:00Z',
    output_path: '/root/autodl-fs/projects/b492cbe4/experiments/33aa44bb/',
    artifacts: ['run.log'],
    log_tail: 'ok\n',
    execution_context: {
      agent_label: 'Owner', proposal_id: 'proposal-oneshot-1',
      repository_name: 'dynamic-point-mamba', repository_ssh_url: repositories[0].ssh_url, requested_ref: 'main',
      backend: 'autodl_private', environment_name: 'torch-cuda11.8', image: 'base-image-1', resource_profile_name: 'one-rtx-3090', region: 'private',
      gpu_models: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, workspace_policy: 'runner_temporary', container_output_path: '/gemcp/output',
    },
    savable_workload: true,
    saved_workload: '',
    created_at: '2026-07-16T10:00:00Z', updated_at: '2026-07-16T10:05:00Z', finished_at: '2026-07-16T10:05:00Z',
    graph_linked: true, orphaned: false,
  },
]
const agentTokens = [
  {
    id: 'agent-token-active-1', project_id: project.id, label: 'default-agent', prefix: 'gmc_abcd123',
    scopes: ['read', 'submit', 'cancel'], status: 'active', expires_at: '2026-10-15T02:00:00Z',
    last_used_at: '2026-07-17T01:58:00Z', created_at: '2026-07-16T00:00:00Z', updated_at: '2026-07-17T01:58:00Z',
  },
  {
    id: 'agent-token-revoked-1', project_id: project.id, label: 'retired-agent', prefix: 'gmc_old1234',
    scopes: ['read'], status: 'revoked', created_at: '2026-06-01T00:00:00Z', updated_at: '2026-07-01T00:00:00Z',
  },
]
const agentEnrollments = [
  {
    id: 'agent-enrollment-completed-1', project_id: project.id, label: 'pi-research-agent',
    scopes: ['read', 'submit', 'cancel'], status: 'completed', expires_at: '2026-07-17T01:00:00Z',
    token_expires_in_days: 30, claimed_at: '2026-07-17T00:31:00Z', completed_at: '2026-07-17T00:32:00Z',
    agent_token_id: agentTokens[0].id, agent_token_prefix: agentTokens[0].prefix,
    created_at: '2026-07-17T00:30:00Z', updated_at: '2026-07-17T00:32:00Z',
  },
]
const issuedEnrollment = {
  id: 'agent-enrollment-issued-1', project_id: project.id, label: 'pi-integration-agent',
  scopes: ['read', 'submit', 'cancel'], status: 'pending', expires_at: '2026-07-17T02:15:00Z',
  token_expires_in_days: 30, created_at: '2026-07-17T02:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const issuedSetupURL = 'https://gemcp.example.com/agent/setup#code=gme_setup1_test-capability'
const issuedAgentToken = 'gmc_new1234_test-secret'
const issuedAgent = {
  id: 'agent-token-issued-1', project_id: project.id, label: 'integration-agent', prefix: 'gmc_new1234',
  scopes: ['read', 'submit', 'cancel'], status: 'active', expires_at: '2026-08-16T02:00:00Z',
  created_at: '2026-07-17T02:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const mcpConfig = {
  mcpServers: {
    'gemcp-point-models': {
      type: 'http', url: 'https://gemcp.example.com/mcp',
      headers: { Authorization: `Bearer ${issuedAgentToken}` },
    },
  },
}
const agentTokenList = {
  tokens: agentTokens, enrollments: agentEnrollments, mcp_url: 'https://gemcp.example.com/mcp', config_file_name: 'gemcp-point-models-mcp.json',
  config_template: {
    mcpServers: {
      'gemcp-point-models': {
        type: 'http', url: 'https://gemcp.example.com/mcp',
        headers: { Authorization: 'Bearer ${GEMCP_AGENT_TOKEN}' },
      },
    },
  },
}
const agentReadiness = {
  project_id: project.id, project_name: project.name, status: 'waiting_compute',
  summary: 'An Agent is bound to the Project, but no Self-hosted or Cloud SSH node is visible yet.',
  generated_at: '2026-07-17T02:00:00Z',
  agents: [{
    id: agentTokens[0].id, label: agentTokens[0].label, prefix: agentTokens[0].prefix,
    scopes: agentTokens[0].scopes, status: 'active', last_used_at: agentTokens[0].last_used_at,
    can_read: true, can_submit: true, can_operate_nodes: false, bound_node_ids: [],
  }],
  compute: { ssh_cloud_enabled: true, ssh_cloud: [], self_hosted: [] },
  heartbeats: { agent_last_used_at: agentTokens[0].last_used_at, note: 'MCP last_used_at updates on every authenticated tool call.' },
  next_actions: [
    { kind: 'copy_readiness', title: 'Copy readiness prompt', detail: 'Tell the Agent to call get_project_options.' },
    { kind: 'register_node', title: 'Register a Cloud SSH host', detail: 'Have the Agent call register_ssh_cloud_node.' },
  ],
  instructions: {
    inspect_tool: 'get_project_options', monitor_tool: 'get_experiment',
    heartbeat: 'MCP last_used_at updates on every authenticated tool call.',
    binding: 'Agents are Project-scoped.',
  },
}
const attemptHistory = [{
  id: 'attempt-live-1', number: 1, state: 'running', provider_resource_id: 'deployment-live-1',
  estimated_cost_milli: 0, log_tail: 'epoch 3 loss=0.42\n', metrics: { loss: 0.42 },
  started_at: '2026-07-17T01:56:00Z', created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}]
const researchWorkspace = {
  project_id: project.id,
  studies: [{
    id: 'study-objbg-1', name: 'objbg-scan', question: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?',
    status: 'active', updated_at: '2026-07-28T18:05:00Z',
  }],
  study: {
    id: 'study-objbg-1', name: 'objbg-scan', question: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?',
    summary: 'Keep the Owner on the scientific question; attach later Experiments as evidence.',
    status: 'active', updated_at: '2026-07-28T18:05:00Z',
    plan: {
      id: 'plan-objbg-1', status: 'active', goal: 'Establish a reproducible OBJ-BG baseline.',
      next_action: 'Record the current smoke-run accuracy as the first Graph result.',
      rationale: 'The Owner should see the question before any new reservation.',
      steps: [{ title: 'Link the existing smoke Experiment', detail: 'Do not submit a new run yet.' }],
      created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:05:00Z',
    },
    nodes: [
      { id: 'node-question-1', kind: 'question', title: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?', status: 'open', created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:00:00Z' },
      { id: 'node-hypothesis-1', kind: 'hypothesis', title: 'Background noise caps accuracy', status: 'open', branch: 'autoresearch/objbg-baseline', created_at: '2026-07-28T18:02:00Z', updated_at: '2026-07-28T18:02:00Z' },
      { id: 'node-result-1', kind: 'result', title: 'OBJ-BG smoke accuracy', summary: 'The existing smoke Experiment reached 86.4 overall accuracy.', status: 'succeeded', metric_name: 'overall_accuracy', metric_value: 86.4, experiment_id: experiments[0].id, experiment_state: experiments[0].state, occurred_at: '2026-07-28T18:05:00Z', branch: 'autoresearch/objbg-baseline', created_at: '2026-07-28T18:05:00Z', updated_at: '2026-07-28T18:05:00Z' },
      { id: 'node-result-old', kind: 'result', title: 'Imported 2024 table', summary: 'A later Graph write of older evidence must not replace Latest result.', status: 'succeeded', occurred_at: '2024-03-12T00:00:00Z', created_at: '2026-07-28T18:06:00Z', updated_at: '2026-07-28T18:06:00Z' },
    ],
    edges: [
      { id: 'edge-0', from_id: 'node-question-1', to_id: 'node-hypothesis-1', relation: 'leads_to' },
      { id: 'edge-1', from_id: 'node-hypothesis-1', to_id: 'node-result-1', relation: 'produced' },
    ],
    hypotheses: [{
      id: 'node-hypothesis-1', title: 'Background noise caps accuracy', status: 'open', branch: 'autoresearch/objbg-baseline',
      experiments: [{
        run_node_id: 'node-run-1', experiment_id: experiments[0].id, title: 'OBJ-BG smoke',
        state: 'succeeded', branch: 'autoresearch/objbg-baseline', commit_sha: experiments[0].commit_sha,
        result_title: 'OBJ-BG smoke accuracy', highlight_title: 'Background noise still enters kNN',
      }],
    }],
  },
  next_actions: [{
    kind: 'record_hypothesis', tool: 'update_research_workspace', study_id: 'study-objbg-1',
    from_node_id: 'node-question-1', title: 'Record a hypothesis',
    detail: 'A paid run must start from a hypothesis or plan node, not from the question alone.',
  }],
  generated_at: '2026-07-28T18:05:00Z',
}
const datasetBindings = [{
  id: 'binding-scanobjectnn', project_id: project.id, name: 'scanobjectnn-objbg', backend: 'autodl_elastic',
  canonical_root: '/root/autodl-fs/datasets/ScanObjectNN', environment_variable: 'GEMCP_DATASET_SCANOBJECTNN_OBJBG',
  required_markers: ['main_split/train.h5'],
  sources: [{ url: 'https://huggingface.co/datasets/example/resolve/main/train.h5', relative_path: 'main_split/train.h5' }],
  status: 'active',
}]
const operationsFeed = {
  activities: [{
    id: 'activity-live-1', agent_label: 'training-agent', agent_token_prefix: 'gmc_abcd123', phase: 'monitoring',
    repository_remote: repositories[0].ssh_url, ref: 'main', proposal_id: 'proposal-live-1', experiment_id: experiments[0].id, at: '2026-07-28T18:05:00Z',
  }],
  proposals: [{
    id: 'proposal-paid-1', status: 'prepared', eligible: true, agent_label: 'training-agent', agent_token_prefix: 'gmc_abcd123',
    repository_name: repositories[0].name, requested_ref: 'autoresearch/m1-gapdelta-confirm-20260824', commit_sha: experiments[0].commit_sha,
    display_command: experiments[0].command, backend: 'autodl_elastic', environment_name: 'public-elastic', image: 'base-image-1',
    repository_access: 'public_https', repository_url: repositories[0].ssh_url, from_node_id: 'node-h1', dataset: 'scanobjectnn-objbg', workload: 'objbg-smoke',
    resource_profile_name: 'one-rtx-4090', gpu_models: ['RTX 4090'], gpu_num: 1, runtime_preset: 'train', max_runtime_seconds: 57600,
    reserved_cost_milli: 20000, checks: [{ id: 'budget', status: 'pass', summary: 'Project budget can reserve the proposal' }],
    confirmation_digest: `sha256:${'c'.repeat(64)}`, created_at: '2026-08-27T16:04:00Z', updated_at: '2026-08-27T16:04:00Z',
    expires_at: '2026-08-27T18:04:00Z',
  }, {
    id: 'proposal-live-1', status: 'submitted', eligible: true, agent_label: 'training-agent', agent_token_prefix: 'gmc_abcd123',
    repository_name: repositories[0].name, requested_ref: 'main', commit_sha: experiments[0].commit_sha,
    display_command: experiments[0].command, backend: 'autodl_private', environment_name: 'torch-cuda11.8', image: 'base-image-1',
    resource_profile_name: 'one-rtx-3090', gpu_models: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, reserved_cost_milli: 12250,
    checks: [{ id: 'source_archive', status: 'pass', summary: 'Source archive is safe' }], confirmation_digest: `sha256:${'a'.repeat(64)}`,
    experiment_id: experiments[0].id, created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:05:00Z', expires_at: '2026-07-28T18:30:00Z',
  }],
  generated_at: '2026-07-28T18:05:00Z',
}
const provider = {
  id: 'provider-id', name: 'AutoDL Private Cloud', base_url: 'https://private.autodl.com', backend: 'private',
  status: 'active', credential_configured: true, last_validated_at: '2026-07-17T02:00:00Z',
  created_at: '2026-07-16T00:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const providerDeployment = {
  uuid: 'deployment-live-1', name: 'live-job', type: 'Job', status: 'running', replica_num: 1, parallelism_num: 1,
  starting_num: 0, running_num: 1, finished_num: 0, failed_num: 0, image_uuid: 'base-image-1',
  reuse_container: true, price_estimate_milli: 1000, created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const managedProviderResource = {
  id: 'managed-resource-1', experiment_id: experiments[0].id, attempt_id: 'attempt-live-1',
  provider_id: providerDeployment.uuid, name: 'live-job', state: 'active', provider_status: 'running',
  hard_deadline_at: '2026-07-17T06:00:00Z', last_seen_at: '2026-07-17T02:00:00Z',
  created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const activeProviderContainer = {
  uuid: 'container-live-1', deployment_uuid: providerDeployment.uuid, machine_id: 'machine-safe-id', status: 'running',
  gpu_name: 'NVIDIA GeForce RTX 3090', gpu_num: 1, cpu_num: 8, memory_bytes: 34359738368,
  image_uuid: 'base-image-1', price_milli_per_hour: 1000, released: false,
  started_at: '2026-07-17T01:56:00Z', created_at: '2026-07-17T01:55:30Z',
}
const cachedProviderContainer = {
  ...activeProviderContainer, uuid: 'container-cache-1', deployment_uuid: 'deployment-old-1', status: 'in_cache', released: true,
  started_at: '2026-07-16T10:00:00Z', stopped_at: '2026-07-16T10:30:00Z', created_at: '2026-07-16T09:59:00Z',
}
const runtimeStatus = {
  scheduler_enabled: true, self_hosted_enabled: false, ssh_cloud_enabled: false, global_concurrency: 2, public_url_configured: true, public_url_https: true,
  scheduler_healthy: true, watchdog_healthy: true, notification_worker_healthy: true,
  scheduler_heartbeat: { role: 'scheduler', instance_id: 'controlplane-test', status: 'running', last_seen_at: '2026-07-17T02:00:00Z' },
  watchdog_heartbeat: { role: 'watchdog', instance_id: 'watchdog-test', status: 'running', last_seen_at: '2026-07-17T02:00:00Z' },
  notification_heartbeat: { role: 'notification', instance_id: 'notification-test', status: 'running', last_seen_at: '2026-07-17T02:00:00Z' },
  generated_at: '2026-07-17T02:00:01Z',
}
const selfHostedNode = {
  id: 'node-11111111-2222-4333-8444-555555555555', label: 'lab-gpu-01', token_prefix: 'gmn_test',
  status: 'active', observed_state: 'online', installation_id: 'installation-id', machine_fingerprint: 'a'.repeat(64),
  hostname: 'gpu-workstation', operating_system: 'linux', architecture: 'amd64', agent_version: '0.9.0', protocol_version: '1',
  capabilities: { cpu_count: 16, memory_bytes: 68719476736, gpus: [{ uuid: 'GPU-test', name: 'NVIDIA GeForce RTX 3090', memory_bytes: 25769803776 }] },
  storage: { root: '/var/lib/gemcp-node/storage', total_bytes: 1099511627776, available_bytes: 824633720832, managed_bytes: 21474836480 },
  project_ids: [project.id], last_seen_at: '2026-07-17T02:00:00Z', approved_at: '2026-07-17T01:00:00Z',
  created_at: '2026-07-17T00:30:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const discoveredA4000Node = {
  ...selfHostedNode,
  id: 'node-aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee', label: 'usb-pc', hostname: 'sage-303203', agent_version: '0.14.1',
  capabilities: {
    cpu_count: 24, memory_bytes: 33285996544, execution_modes: ['shell', 'argv'],
    gpus: [{ uuid: 'GPU-a4000-test', name: 'NVIDIA RTX A4000', memory_bytes: 17179869184 }],
  },
  storage: { root: '/var/lib/gemcp-node/storage', total_bytes: 999355760640, available_bytes: 461440319488, managed_bytes: 0 },
}
const selfHostedAssignment = {
  id: 'assignment-id', node_id: selfHostedNode.id, node_label: selfHostedNode.label, project_id: project.id,
  experiment_id: experiments[0].id, attempt_id: 'attempt-self-hosted-1', attempt_number: 1, state: 'running',
  output_ref: `experiments/${experiments[0].id}/attempts/attempt-self-hosted-1/outputs`,
  started_at: '2026-07-17T01:56:00Z', last_heartbeat_at: '2026-07-17T02:00:00Z', metrics: { loss: 0.42 },
  created_at: '2026-07-17T01:55:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const selfHostedRuntimes = {
  environments: [{ id: 'environment-self-hosted', name: 'local-3090', image: `registry.example/train@sha256:${'b'.repeat(64)}`, is_default: true }],
  resource_profiles: [{ id: 'profile-self-hosted', name: 'local-3090', gpu_names: ['NVIDIA GeForce RTX 3090'], cpu_limit: 8, memory_gb: 32, is_default: true }],
  trusted_workspaces: [],
}
const issuedNodeEnrollment = {
  id: 'node-enrollment-issued-1', label: 'second-gpu-node', status: 'pending', expires_at: '2026-07-17T02:30:00Z',
  created_at: '2026-07-17T02:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}
const issuedNodeSetupURL = 'https://gemcp.example.com/node/setup#code=gne_setup_test-capability'
const notificationSetting = {
  configured: true, enabled: true, host: 'smtp.example.com', port: 587, tls_mode: 'starttls',
  username: 'mailer@example.com', password_configured: true, from_address: 'mailer@example.com',
  recipients: ['owner@example.com'], status: 'ready', last_tested_at: '2026-07-17T01:30:00Z', updated_at: '2026-07-17T01:30:00Z',
}
const notificationDeliveries = [
  { id: 'notification-1', kind: 'watchdog_stop', severity: 'critical', subject: '[Gemcp] Watchdog enforced Provider shutdown', state: 'sent', attempts: 1, next_attempt_at: '2026-07-17T01:00:00Z', sent_at: '2026-07-17T01:00:02Z', created_at: '2026-07-17T01:00:00Z' },
  { id: 'notification-2', kind: 'timeout_extended', severity: 'warning', subject: '[Gemcp] Experiment runtime extended', state: 'pending', attempts: 0, next_attempt_at: '2026-07-17T02:01:00Z', created_at: '2026-07-17T02:00:00Z' },
]
const providerResources = {
  generated_at: '2026-07-17T02:00:00Z', provider,
  gpu_stock: [{ name: 'NVIDIA GeForce RTX 3090', idle: 2, total: 9 }],
  private_images: [],
  system_images: [
    { uuid: 'base-image-1', name: 'torch:cuda11.8-cudnn8-devel-ubuntu22.04-py310-torch2.1.2', cuda_version: '11.8', chip_corp: 'nvidia', cpu_arch: 'x86', source: 'system' },
    { uuid: 'base-image-2', name: 'miniconda:cuda12.2-cudnn8-devel-ubuntu22.04-py310', cuda_version: '12.2', chip_corp: 'nvidia', cpu_arch: 'x86', source: 'system' },
  ],
  deployments: [providerDeployment], active_containers: [activeProviderContainer], cached_containers: [cachedProviderContainer],
}
const financeDashboard = {
  period: '2026-07', audit_scope: 'organization', generated_at: '2026-07-17T02:00:00Z',
  totals: {
    base_budget_milli: 100000, reserved_milli: 15750, charged_milli: 2180,
    credits_milli: 25000, debits_milli: 0, committed_milli: -7070, available_milli: 107070,
  },
  projects: [{
    id: project.id, name: project.name, status: project.status, timezone: project.timezone,
    base_budget_milli: 100000, reserved_milli: 15750, charged_milli: 2180,
    credits_milli: 25000, debits_milli: 0, committed_milli: -7070, available_milli: 107070,
  }],
  daily: [
    { date: '2026-07-15', reserved_milli: 0, charged_milli: 2180, credits_milli: 0, debits_milli: 0 },
    { date: '2026-07-16', reserved_milli: 15750, charged_milli: 0, credits_milli: 0, debits_milli: 0 },
    { date: '2026-07-17', reserved_milli: 0, charged_milli: 0, credits_milli: 25000, debits_milli: 0 },
  ],
  backends: [{ backend: 'autodl_private', experiments: 2, reserved_milli: 15750, charged_milli: 2180 }],
  ledger: [
    {
      id: 'budget-credit-1', project_id: project.id, project_name: project.name, period: '2026-07',
      kind: 'adjustment', direction: 'credit', amount_milli: -25000, balance_effect_milli: 25000,
      description: 'Approved July AutoDL test allocation', created_at: '2026-07-17T02:00:00Z',
    },
    {
      id: 'budget-charge-1', project_id: project.id, project_name: project.name, experiment_id: experiments[1].id,
      backend: 'autodl_private', period: '2026-07', kind: 'charge', amount_milli: 2180, balance_effect_milli: -2180,
      description: 'Provider runtime cost estimate', created_at: '2026-07-15T06:02:00Z',
    },
  ],
  audit: [{
    id: 'audit-budget-1', actor_type: 'user', actor_id: 'owner-id', action: 'budget.credit_recorded',
    target_type: 'budget_entry', target_id: 'budget-credit-1', created_at: '2026-07-17T02:00:00Z',
  }],
}
const diagnosticOptions = {
  suites: [
    { id: 'gpu_connectivity', name: 'GPU connectivity', description: 'Verify GPU visibility and driver access.', runtime_seconds: 180 },
    { id: 'pytorch_cuda', name: 'PyTorch CUDA compute', description: 'Run a CUDA tensor operation through PyTorch.', runtime_seconds: 300 },
  ],
  repositories: [{ id: repositories[0].id, name: repositories[0].name, default_branch: 'main' }],
  environments: [{ id: 'environment-id', name: 'torch-cuda11.8', backend: 'autodl_private', image_uuid: 'base-image-1', status: 'approved' }],
  resource_profiles: [{ id: 'profile-id', name: 'one-rtx-3090', backend: 'autodl_private', gpu_names: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, price_to_milli: 1000, status: 'active' }],
}
const diagnosticPreflight = {
  eligible: true, requires_confirmation: true, generated_at: '2026-07-17T02:05:00Z',
  confirmation_digest: `sha256:${'d'.repeat(64)}`,
  checks: [
    { id: 'scheduler', status: 'pass', summary: 'Scheduler is healthy', detail: 'Global concurrency is 2.' },
    { id: 'watchdog', status: 'pass', summary: 'Watchdog is healthy' },
    { id: 'source_archive', status: 'pass', summary: 'Commit archive is readable and safe', detail: '495 entries, 966 compressed bytes, 1,024 payload bytes.' },
    { id: 'gpu_capacity', status: 'pass', summary: 'Selected AutoDL GPU capacity is available', detail: 'NVIDIA GeForce RTX 3090: 2 idle' },
    { id: 'budget', status: 'pass', summary: 'Project budget can reserve the diagnostic' },
  ],
  proposal: {
    backend: 'autodl_private', suite: 'gpu_connectivity', repository_id: repositories[0].id,
    environment_id: 'environment-id', resource_profile_id: 'profile-id',
    commit_sha: '0123456789012345678901234567890123456789', command: 'nvidia-smi --query-gpu=name,uuid --format=csv,noheader',
    image_uuid: 'base-image-1', gpu_models: ['NVIDIA GeForce RTX 3090'], gpu_num: 1,
    runtime_seconds: 180, termination_grace_seconds: 30, reserved_cost_milli: 234, billable: true,
    region: 'private', cuda_from: 118, cuda_to: 118, cpu_from: 1, cpu_to: 8,
    memory_from_gb: 1, memory_to_gb: 32, price_from_milli: 500, price_to_milli: 1000, reuse_container: false,
  },
}
const completedDiagnostic = {
  id: 'diagnostic-complete-1', project_id: project.id, backend: 'autodl_private', suite: 'gpu_connectivity', requested_by: 'owner-id',
  preflight: diagnosticPreflight,
  experiment: {
    ...experiments[1], id: 'diagnostic-experiment-complete', state: 'succeeded', desired_state: 'running',
    failure_code: undefined, failure_reason: undefined, exit_code: 0, estimated_cost_milli: 80,
    metrics: { diagnostic_passed: true, gpu_count: 1, expected_gpu_count: 1 },
    runner_source_downloads: 1, runner_stage: 'started', runner_stage_updated_at: '2026-07-17T02:06:10Z',
    created_at: '2026-07-17T02:05:00Z', updated_at: '2026-07-17T02:07:00Z', finished_at: '2026-07-17T02:07:00Z',
  },
  assessment: { status: 'passed', classification: 'diagnostic_passed', summary: 'GPU diagnostic completed successfully.', cleanup_complete: true },
  attempts: [{
    id: 'diagnostic-attempt-complete', number: 1, state: 'succeeded', source_downloads: 1, exit_code: 0,
    log_tail: 'NVIDIA GeForce RTX 3090, GPU-test\n', metrics: { diagnostic_passed: true, gpu_count: 1, expected_gpu_count: 1 },
    started_at: '2026-07-17T02:06:10Z', finished_at: '2026-07-17T02:06:14Z',
  }],
  backend_observation: { kind: 'autodl_private', id: 'diagnostic-resource-1', state: 'deleted', status: 'removed', stop_reason: 'experiment_terminal', finished_at: '2026-07-17T02:07:00Z' },
  timeline: [
    { at: '2026-07-17T02:05:00Z', code: 'diagnostic_submitted' },
    { at: '2026-07-17T02:06:10Z', code: 'runner.bootstrap_stage', detail: 'started' },
    { at: '2026-07-17T02:07:00Z', code: 'experiment_terminal', detail: 'succeeded' },
  ],
  created_at: '2026-07-17T02:05:00Z', updated_at: '2026-07-17T02:07:00Z',
}
const requestedImageBake = {
  id: 'image-bake-1', project_id: project.id, repository_id: repositories[0].id, name: 'torch-mamba', backend: 'autodl_pro',
  base_image_uuid: 'image-6c15b8aad2', commit_sha: '0123456789012345678901234567890123456789', recipe_path: 'requirements.gemcp.txt',
  status: 'requested', confirmation_digest: `sha256:${'e'.repeat(64)}`, requested_by: 'agent-token-active-1', requested_by_type: 'agent_token',
  proposal: {
    backend: 'autodl_pro', name: 'torch-mamba', base_image_uuid: 'image-6c15b8aad2',
    repository_id: repositories[0].id, commit_sha: '0123456789012345678901234567890123456789', recipe_path: 'requirements.gemcp.txt',
  },
  estimated_cost_milli: 0, created_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z',
}
const queuedDiagnostic = {
  ...completedDiagnostic, id: 'diagnostic-queued-1',
  experiment: {
    ...completedDiagnostic.experiment, id: 'diagnostic-experiment-queued', state: 'queued', runner_source_downloads: undefined,
    runner_stage: undefined, runner_stage_updated_at: undefined, exit_code: undefined, finished_at: undefined,
    estimated_cost_milli: 0, created_at: '2026-07-17T02:10:00Z', updated_at: '2026-07-17T02:10:00Z',
  },
  assessment: { status: 'running', classification: 'waiting_for_scheduler', summary: 'Diagnostic is queued for an execution slot.', cleanup_complete: false },
  attempts: [], backend_observation: undefined,
  timeline: [{ at: '2026-07-17T02:10:00Z', code: 'diagnostic_submitted' }],
  created_at: '2026-07-17T02:10:00Z', updated_at: '2026-07-17T02:10:00Z',
}
const cancelledDiagnostic = {
  ...queuedDiagnostic,
  experiment: { ...queuedDiagnostic.experiment, state: 'cancelled', desired_state: 'cancelled', finished_at: '2026-07-17T02:10:05Z' },
  assessment: { status: 'failed', classification: 'cancelled', summary: 'Diagnostic was cancelled.', cleanup_complete: true },
  timeline: [...queuedDiagnostic.timeline, { at: '2026-07-17T02:10:05Z', code: 'experiment_terminal', detail: 'cancelled' }],
}

async function fulfill(route: Route, data: unknown, status = 200) {
  await route.fulfill({ status, contentType: 'application/json', body: JSON.stringify(status >= 400 ? data : { data }) })
}

async function mockSetup(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: false })
    return fulfill(route, { error: { code: 'NOT_FOUND', message: 'not found' } }, 404)
  })
}

async function mockLogin(page: Page) {
  await page.route('**/api/v1/**', async (route) => {
    const path = new URL(route.request().url()).pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: true })
    if (path === '/api/v1/auth/me') return fulfill(route, { error: { code: 'UNAUTHENTICATED', message: 'authentication required' } }, 401)
    return fulfill(route, { error: { code: 'NOT_FOUND', message: 'not found' } }, 404)
  })
}

async function mockConsole(page: Page, counters?: { providerQueries?: number; selfHosted?: boolean; schedulerEnabled?: boolean }) {
  let submittedDiagnostic: typeof queuedDiagnostic | typeof cancelledDiagnostic = queuedDiagnostic
  let listedImageBake: typeof requestedImageBake | (typeof requestedImageBake & { image_uuid: string; instance_uuid: string }) = requestedImageBake
  const savedWorkloads: Array<Record<string, unknown>> = []
  let oneshotDetail = { ...experiments[2] }
  await page.route('**/docs/*.md', async (route) => {
    const path = new URL(route.request().url()).pathname
    const body = path.endsWith('/agent-mcp.md')
      ? '# Gemcp MCP Agent Operating Guide\n\n## Non-negotiable rules\nCall get_project_options and get_project_cost before submission.\n'
      : '# Gemcp MCP Owner Guide\n\n## Owner onboarding checklist\nConfigure the MCP client without exposing its Agent Token.\n'
    await route.fulfill({ status: 200, contentType: 'text/markdown', headers: { 'Access-Control-Allow-Origin': '*' }, body })
  })
  await page.route('**/api/v1/**', async (route) => {
    const url = new URL(route.request().url())
    const path = url.pathname
    if (path === '/api/v1/version') return fulfill(route, build)
    if (path === '/api/v1/setup/status') return fulfill(route, { initialized: true })
    if (path === '/api/v1/auth/me') return fulfill(route, { user_id: 'owner-id', tenant_id: 'tenant-id', email: 'owner@example.com', role: 'owner' })
    if (path === '/api/v1/runtime/status') return fulfill(route, {
      ...runtimeStatus,
      scheduler_enabled: counters?.schedulerEnabled ?? runtimeStatus.scheduler_enabled,
      scheduler_healthy: counters?.schedulerEnabled === false ? false : runtimeStatus.scheduler_healthy,
      self_hosted_enabled: Boolean(counters?.selfHosted),
    })
    if (path === '/api/v1/finance' && route.request().method() === 'GET') return fulfill(route, financeDashboard)
    if (path === `/api/v1/projects/${project.id}/budget-adjustments` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toMatchObject({
        direction: 'credit', amount_milli: 50000, reason: 'Approved AutoDL integration test allocation',
      })
      expect(route.request().postDataJSON().idempotency_key).toMatch(/^budget-/)
      return fulfill(route, { entry: financeDashboard.ledger[0], idempotent: false }, 201)
    }
    if (path === '/api/v1/nodes' && counters?.selfHosted) return fulfill(route, {
      nodes: [selfHostedNode, discoveredA4000Node], enrollments: [], assignments: [selfHostedAssignment],
    })
    if (path === '/api/v1/node-enrollments' && route.request().method() === 'POST' && counters?.selfHosted) {
      expect(route.request().postDataJSON()).toEqual({ label: 'second-gpu-node', setup_expires_in_minutes: 30 })
      return fulfill(route, {
        enrollment: issuedNodeEnrollment, setup_url: issuedNodeSetupURL,
        claim_url: 'https://gemcp.example.com/api/v1/node-enrollments/claim',
      }, 201)
    }
    if (path === `/api/v1/projects/${project.id}/self-hosted-trusted-workspace` && route.request().method() === 'PUT' && counters?.selfHosted) {
      expect(route.request().postDataJSON()).toEqual({ node_id: discoveredA4000Node.id, workspace_path: '/home/campus.ncl.ac.uk/nxl51/gemcp_tmp', make_default: false })
      return fulfill(route, {
        node_id: discoveredA4000Node.id, node_label: discoveredA4000Node.label, workspace_path: '/home/campus.ncl.ac.uk/nxl51/gemcp_tmp',
        environment_id: 'workspace-environment', environment_name: 'workspace-usb-pc', resource_profile_id: 'workspace-profile',
        gpu_name: 'NVIDIA RTX A4000', cpu_limit: 22, memory_gb: 28, successful_images: [], node_ready: false,
      })
    }
    if (path === `/api/v1/projects/${project.id}/self-hosted-runtimes` && counters?.selfHosted) return fulfill(route, selfHostedRuntimes)
    if (path === '/api/v1/provider' && route.request().method() === 'GET') return fulfill(route, { providers: [provider] })
    if (path === '/api/v1/provider' && route.request().method() === 'PUT') return fulfill(route, { provider, resources: providerResources })
    if (path === '/api/v1/provider/query') {
      if (counters) counters.providerQueries += 1
      return fulfill(route, providerResources)
    }
    if (path === '/api/v1/provider/managed-resources') return fulfill(route, [managedProviderResource])
    if (path === `/api/v1/provider/deployments/${providerDeployment.uuid}/stop`) return fulfill(route, { ...managedProviderResource, stop_requested_at: '2026-07-17T02:01:00Z', stop_reason: 'owner_stop' }, 202)
    if (path === '/api/v1/provider/emergency-stop') {
      expect(route.request().postDataJSON()).toEqual({ confirmation: 'STOP' })
      return fulfill(route, { requested: 1, at: '2026-07-17T02:01:00Z' }, 202)
    }
    if (path === '/api/v1/notifications/settings') return fulfill(route, notificationSetting)
    if (path === '/api/v1/notifications') return fulfill(route, notificationDeliveries)
    if (path === '/api/v1/notifications/test') return fulfill(route, { id: 'notification-test', kind: 'smtp_test', severity: 'info', subject: 'Gemcp SMTP test', state: 'pending', attempts: 0, next_attempt_at: '2026-07-17T02:02:00Z', created_at: '2026-07-17T02:02:00Z' }, 202)
    if (path === `/api/v1/provider/deployments/${providerDeployment.uuid}`) return fulfill(route, {
      generated_at: '2026-07-17T02:00:01Z', deployment: providerDeployment,
      active_containers: [activeProviderContainer], released_containers: [],
      events: [
        { container_uuid: activeProviderContainer.uuid, status: 'running', created_at: '2026-07-17T01:56:00Z' },
        { container_uuid: activeProviderContainer.uuid, status: 'starting', created_at: '2026-07-17T01:55:30Z' },
      ],
    })
    if (path === '/api/v1/projects') return fulfill(route, [project])
    if (path === `/api/v1/projects/${project.id}` && route.request().method() === 'PATCH') {
      const body = route.request().postDataJSON()
      expect(body).toEqual({ monthly_budget_milli: 120000, max_experiment_milli: 120000 })
      return fulfill(route, { ...project, ...body })
    }
    if (path === `/api/v1/projects/${project.id}/dataset-bindings` && route.request().method() === 'GET') {
      return fulfill(route, datasetBindings)
    }
    if (path === `/api/v1/projects/${project.id}/dataset-sources` && route.request().method() === 'GET') {
      return fulfill(route, [{
        name: 'scanobjectnn-objbg', display_name: 'ScanObjectNN OBJ-BG', backend: 'autodl_elastic',
        canonical_root: '/root/autodl-fs/datasets/ScanObjectNN', required_markers: [], notes: '',
      }])
    }
    if (path === `/api/v1/projects/${project.id}/environments` && route.request().method() === 'GET') {
      return fulfill(route, [{
        id: 'environment-elastic', project_id: project.id, name: 'public-elastic', backend: 'autodl_elastic',
        image_uuid: 'image-6c15b8aad2', is_default: true, status: 'approved',
      }])
    }
    if (path === `/api/v1/projects/${project.id}/environments` && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      return fulfill(route, {
        id: 'environment-new', project_id: project.id, name: body.name, backend: body.backend || 'autodl_elastic',
        image_uuid: body.image_uuid, is_default: !!body.set_default, status: 'approved',
      }, 201)
    }
    if (path.startsWith(`/api/v1/projects/${project.id}/environments/`) && route.request().method() === 'DELETE') {
      return fulfill(route, {
        id: 'environment-elastic', project_id: project.id, name: 'public-elastic', backend: 'autodl_elastic',
        image_uuid: 'image-6c15b8aad2', is_default: false, status: 'disabled',
      })
    }
    if (path === `/api/v1/projects/${project.id}/dataset-bindings` && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      expect(body.canonical_root || body.catalog).toBeTruthy()
      return fulfill(route, {
        id: 'binding-new', project_id: project.id, name: body.name || body.catalog, backend: body.backend || 'autodl_elastic',
        canonical_root: body.canonical_root || '/root/autodl-fs/datasets/ScanObjectNN', environment_variable: 'GEMCP_DATASET_NEW_DATASET',
        required_markers: body.required_markers || [], sources: body.sources || [], status: 'active',
      }, 201)
    }
    if (path.startsWith(`/api/v1/projects/${project.id}/dataset-bindings/`) && route.request().method() === 'DELETE') {
      return fulfill(route, { ...datasetBindings[0], status: 'disabled' })
    }
    if (path === `/api/v1/projects/${project.id}/workloads` && route.request().method() === 'GET') {
      return fulfill(route, savedWorkloads)
    }
    if (path === `/api/v1/projects/${project.id}/workloads/preview` && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      expect(body.experiment_id).toBe(experiments[2].id)
      return fulfill(route, {
        name: body.name,
        manifest_yaml: `version: 1\nworkloads:\n  ${body.name}:\n    entrypoint:\n      - python\n      - tools/smoke.py\n      - --label\n      - oneshot\n    runtime_preset: smoke\n`,
      })
    }
    if (path === `/api/v1/projects/${project.id}/workloads` && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      const created = {
        id: 'workload-oneshot-1', name: body.name, manifest_yaml: `version: 1\nworkloads:\n  ${body.name}:\n    entrypoint:\n      - python\n      - tools/smoke.py\n`,
        entrypoint: ['python', 'tools/smoke.py', '--label', 'oneshot'], runtime_preset: 'smoke',
        source_experiment_id: body.experiment_id, created_at: '2026-07-16T10:06:00Z',
      }
      savedWorkloads.splice(0, savedWorkloads.length, created)
      oneshotDetail = { ...oneshotDetail, savable_workload: false, saved_workload: body.name }
      return fulfill(route, created, 201)
    }
    if (path === `/api/v1/projects/${project.id}/experiment-proposals` && route.request().method() === 'POST') {
      const body = route.request().postDataJSON()
      expect(body.argv?.[0] || body.workload || body.runtime_preset).toBeTruthy()
      return fulfill(route, {
        proposal: {
          id: 'proposal-owner-1', project_id: project.id, eligible: true, requires_confirmation: true,
          repository: {
            id: repositories[0].id, name: repositories[0].name, ssh_url: repositories[0].ssh_url,
            requested_ref: body.ref || 'main', commit_sha: experiments[0].commit_sha,
            default_branch: 'main', access: 'public_https',
          },
          execution: { mode: 'argv', argv: body.argv || ['python', 'tools/smoke.py'], display_command: 'python tools/smoke.py' },
          resource: {
            environment_name: 'public-elastic', resource_profile_name: 'one-rtx-4090', backend: 'autodl_elastic',
            image: 'base-image-1', gpu_models: ['RTX 4090'], gpu_num: 1,
          },
          runtime_preset: body.runtime_preset || 'smoke', max_runtime_seconds: 300, reserved_cost_milli: 3825,
          checks: [{ id: 'budget', status: 'pass', summary: 'Budget can reserve' }],
          confirmation_digest: `sha256:${'d'.repeat(64)}`,
          from_node_id: body.from_node_id, created_at: '2026-09-11T02:00:00Z', expires_at: '2026-09-11T04:00:00Z',
        },
      })
    }
    if (path === `/api/v1/projects/${project.id}/experiment-proposals/proposal-paid-1/submit` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual({
        confirmation_digest: operationsFeed.proposals[0].confirmation_digest, confirmed: true,
      })
      return fulfill(route, { experiment: experiments[0], idempotent: false })
    }
    if (path === `/api/v1/projects/${project.id}/diagnostics/options`) return fulfill(route, diagnosticOptions)
    if (path === `/api/v1/projects/${project.id}/diagnostics/preflight` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toMatchObject({
        backend: 'autodl_private', suite: 'gpu_connectivity', repository_id: repositories[0].id,
        environment_id: 'environment-id', resource_profile_id: 'profile-id', commit_sha: diagnosticPreflight.proposal.commit_sha,
      })
      return fulfill(route, diagnosticPreflight)
    }
    if (path === `/api/v1/projects/${project.id}/diagnostics` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toMatchObject({
        confirmed: true, confirmation_digest: diagnosticPreflight.confirmation_digest,
        backend: 'autodl_private', suite: 'gpu_connectivity',
      })
      expect(route.request().postDataJSON().idempotency_key).toMatch(/^diagnostic-/)
      submittedDiagnostic = queuedDiagnostic
      return fulfill(route, { run: queuedDiagnostic, idempotent: false }, 201)
    }
    if (path === `/api/v1/projects/${project.id}/diagnostics` && route.request().method() === 'GET') {
      return fulfill(route, { runs: [completedDiagnostic] })
    }
    if (path === `/api/v1/projects/${project.id}/diagnostics/${completedDiagnostic.id}`) return fulfill(route, completedDiagnostic)
    if (path === `/api/v1/projects/${project.id}/diagnostics/${queuedDiagnostic.id}/cancel` && route.request().method() === 'POST') {
      submittedDiagnostic = cancelledDiagnostic
      return fulfill(route, cancelledDiagnostic)
    }
    if (path === `/api/v1/projects/${project.id}/diagnostics/${queuedDiagnostic.id}`) return fulfill(route, submittedDiagnostic)
    if (path === `/api/v1/projects/${project.id}/image-bakes/options`) {
      return fulfill(route, {
        project_id: project.id, backend: 'autodl_pro', default_recipe_path: 'requirements.gemcp.txt',
        repositories: [{ id: repositories[0].id, name: repositories[0].name, default_branch: 'main' }],
        base_images: [{ uuid: 'image-6c15b8aad2', name: 'torch' }], generated_at: '2026-09-02T00:00:00Z',
      })
    }
    if (path === `/api/v1/projects/${project.id}/image-bakes` && route.request().method() === 'GET') {
      return fulfill(route, { bakes: [listedImageBake] })
    }
    if (path === `/api/v1/projects/${project.id}/image-bakes/${requestedImageBake.id}/confirm` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual({ confirmation_digest: requestedImageBake.confirmation_digest })
      listedImageBake = { ...requestedImageBake, status: 'finished', image_uuid: 'image-baked12345', instance_uuid: 'pro-instance-1' }
      return fulfill(route, listedImageBake)
    }
    if (path === `/api/v1/projects/${project.id}/agent-tokens` && route.request().method() === 'GET') return fulfill(route, agentTokenList)
    if (path === `/api/v1/projects/${project.id}/agent-readiness` && route.request().method() === 'GET') return fulfill(route, agentReadiness)
    if (path === `/api/v1/projects/${project.id}/agent-enrollments` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual({
        label: 'pi-integration-agent', scopes: ['read', 'submit', 'cancel'], expires_in_days: 30,
        never_expires: false, setup_expires_in_minutes: 15,
      })
      return fulfill(route, {
        enrollment: issuedEnrollment, setup_url: issuedSetupURL,
        installer_url: 'https://gemcp.example.com/agent/setup/install.mjs',
      }, 201)
    }
    if (path === `/api/v1/projects/${project.id}/agent-enrollments/${issuedEnrollment.id}` && route.request().method() === 'DELETE') {
      return fulfill(route, { ...issuedEnrollment, status: 'revoked', updated_at: '2026-07-17T02:01:00Z' })
    }
    if (path === `/api/v1/projects/${project.id}/agent-tokens` && route.request().method() === 'POST') {
      expect(route.request().postDataJSON()).toEqual({
        label: 'integration-agent', scopes: ['read', 'submit', 'cancel'], expires_in_days: 30, never_expires: false,
      })
      return fulfill(route, {
        token: issuedAgent, agent_token: issuedAgentToken, mcp_url: 'https://gemcp.example.com/mcp',
        mcp_config: mcpConfig, config_file_name: 'gemcp-point-models-mcp.json',
      }, 201)
    }
    if (path === `/api/v1/projects/${project.id}/agent-tokens/${agentTokens[0].id}` && route.request().method() === 'PATCH') {
      expect(route.request().postDataJSON()).toEqual({ scopes: ['read', 'submit', 'cancel', 'configure'] })
      return fulfill(route, { ...agentTokens[0], scopes: ['read', 'submit', 'cancel', 'configure'], updated_at: '2026-07-17T02:00:30Z' })
    }
    if (path === `/api/v1/projects/${project.id}/agent-tokens/${agentTokens[0].id}` && route.request().method() === 'DELETE') {
      return fulfill(route, { ...agentTokens[0], status: 'revoked', updated_at: '2026-07-17T02:01:00Z' })
    }
    if (path === '/api/v1/repositories') return fulfill(route, repositories)
    if (path === '/api/v1/experiments') return fulfill(route, experiments)
    if (path === `/api/v1/projects/${project.id}/research`) return fulfill(route, researchWorkspace)
    if (path === `/api/v1/projects/${project.id}/experiment-catalog`) {
      return fulfill(route, {
        project_id: project.id,
        generated_at: '2026-07-17T02:00:00Z',
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
            created_at: '2026-07-17T02:00:00Z', updated_at: '2026-07-17T02:00:00Z',
          }],
        }],
      })
    }
    if (path === `/api/v1/projects/${project.id}/operations`) return fulfill(route, operationsFeed)
    if (path === `/api/v1/experiments/${experiments[0].id}/attempts`) return fulfill(route, attemptHistory)
    if (path === `/api/v1/experiments/${experiments[0].id}`) return fulfill(route, experiments[0])
    if (path === `/api/v1/experiments/${experiments[2].id}`) return fulfill(route, oneshotDetail)
    if (path === `/api/v1/experiments/${experiments[2].id}/attempts`) return fulfill(route, [])
    if (path === `/api/v1/projects/${project.id}/cost`) return fulfill(route, {
      period: '2026-07', monthly_budget_milli: 100000, reserved_milli: 15750, charged_milli: 2180,
      adjustments_milli: 0, committed_milli: 17930, available_milli: 82070,
    })
    return fulfill(route, { error: { code: 'NOT_FOUND', message: 'not found' } }, 404)
  })
}

async function expectNoPageOverflow(page: Page) {
  const sizes = await page.evaluate(() => ({ width: document.documentElement.clientWidth, scrollWidth: document.documentElement.scrollWidth }))
  expect(sizes.scrollWidth).toBeLessThanOrEqual(sizes.width + 1)
}

test('first-run setup fits desktop and mobile', async ({ page }) => {
  await mockSetup(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Create the Owner' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-mobile.png', fullPage: true })

  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByLabel('Bootstrap token').fill('gmb_abcdefghijklmnopqrstuvwxyz123456')
  await page.getByLabel('Organization name').fill('Research Lab')
  await page.getByLabel('Owner email').fill('owner@example.com')
  await page.getByLabel('Owner password').fill('correct horse battery staple')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByLabel('Configure AutoDL now')).toBeChecked()
  await expect(page.getByLabel('AutoDL service')).toHaveValue('private')
  await expect(page.getByLabel('API base URL')).toHaveValue('https://private.autodl.com')
  await expect(page.getByLabel('API base URL')).toBeDisabled()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-provider-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-provider-mobile.png', fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.getByLabel('Configure AutoDL now').uncheck()
  await expect(page.getByLabel('AutoDL service')).toHaveCount(0)
  await expect(page.getByText('Continue without AutoDL for a Self-hosted or Cloud SSH deployment.')).toBeVisible()
  await page.getByLabel('Configure AutoDL now').check()
  await page.getByLabel('AutoDL service').selectOption('public')
  await expect(page.getByLabel('API base URL')).toHaveValue('https://api.autodl.com')
  await expect(page.getByLabel('Provider name')).toHaveValue('AutoDL Public Cloud')
  await page.getByLabel('AutoDL service').selectOption('private')
  await page.getByLabel('AutoDL API token').fill('test-provider-token')
  await page.getByRole('button', { name: 'Continue' }).click()
  await expect(page.getByRole('heading', { name: 'Configure the first project' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-setup-project-desktop.png', fullPage: true })
})

test('Owner login fits desktop and mobile', async ({ page }) => {
  await mockLogin(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Sign in' })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-login-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-login-mobile.png', fullPage: true })
})

test('operations console and dialogs fit desktop', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Research', exact: true }).first()).toBeVisible()
  await expect(page.getByRole('heading', { name: 'objbg-scan' })).toBeVisible()
  await expect(page.getByText('Record the current smoke-run accuracy as the first Graph result.')).toBeVisible()
  await expect(page.getByText('Record a hypothesis')).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Research Graph' })).toBeVisible()
  const catalog = page.getByTestId('repo-experiment-catalog')
  await expect(catalog).toBeVisible()
  await expect(catalog).toContainText('原始注册数据')
  await expect(catalog).toContainText('分析数据')
  await expect(catalog).toContainText('git@github.com:research/dynamic-point-mamba.git')
  await expect(catalog).toContainText('Setting')
  await expect(catalog).toContainText('方法')
  await expect(catalog).toContainText('实现')
  await expect(catalog).toContainText('90.4114')
  await catalog.screenshot({ path: process.env.CATALOG_SCREENSHOT || '/tmp/repo-catalog-panel.png' })
  await expect(page.getByText('Agent Readiness')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Copy readiness', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Attach prompt' }).click()
  await expect(page.getByRole('dialog', { name: 'Attach prompt' })).toBeVisible()
  await expect(page.locator('.attach-prompt')).toHaveValue(/get_next_actions/)
  await page.getByRole('dialog', { name: 'Attach prompt' }).getByTitle('Close').click()
  await page.getByRole('button', { name: 'Attach prompt' }).click()
  await expect(page.getByRole('dialog', { name: 'Attach prompt' })).toBeVisible()
  await expect(page.locator('.attach-prompt')).toHaveValue(/get_next_actions/)
  await page.getByRole('dialog', { name: 'Attach prompt' }).getByTitle('Close').click()
  await page.getByRole('button', { name: 'New study' }).click()
  await expect(page.getByRole('dialog', { name: 'Import study' })).toBeVisible()
  await expect(page.getByText('Import from')).toBeVisible()
  await expect(page.getByPlaceholder('git@github.com:owner/repository.git')).toHaveCount(0)
  await page.getByRole('dialog', { name: 'Import study' }).getByTitle('Close').click()
  await expect(page.locator('.vue-flow')).toBeVisible()
  await expect(page.locator('.graph-shell')).toHaveCSS('max-height', '560px')
  await expect(page.getByRole('button', { name: 'Fullscreen graph' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Fit graph' })).toBeVisible()
  await expect(page.locator('.flow-node.is-success, .flow-node-outcome[data-outcome="success"]').first()).toBeVisible()
  await page.locator('.flow-node').first().dblclick()
  await expect(page.getByLabel('Node detail')).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(page.getByLabel('Node detail')).toHaveCount(0)
  // Layout can stack the hypothesis under a result; dispatch so the node handler still runs.
  await page.locator('.flow-node[data-kind="hypothesis"]').dispatchEvent('dblclick')
  await expect(page.getByLabel('Node detail')).toBeVisible()
  await expect(page.getByLabel('Node detail')).toContainText('autoresearch/objbg-baseline')
  await page.getByLabel('Node detail').screenshot({ path: process.env.GRAPH_DETAIL_SCREENSHOT || '/tmp/gemcp-graph-node-branch.png' })
  await page.keyboard.press('Escape')
  await expect(page.getByLabel('Node detail')).toHaveCount(0)
  await page.locator('.flow-node.is-success').filter({ hasText: 'OBJ-BG smoke accuracy' }).dblclick()
  await expect(page.getByLabel('Experiment details')).toBeVisible()
  await page.getByTitle('Close details').click()
  await expect(page.getByLabel('Experiment details')).toHaveCount(0)
  await page.getByRole('button', { name: 'Fullscreen graph' }).click()
  await expect(page.locator('.graph-shell')).toHaveClass(/is-fullscreen/)
  await page.getByRole('button', { name: 'Exit fullscreen' }).click()
  await expect(page.getByRole('article').filter({ hasText: 'Latest result' }).getByText('OBJ-BG smoke accuracy')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-console-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Project', exact: true }).click()
  await expect(page.getByText('Budget allocation')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Save budget' })).toBeVisible()
  await expect(page.getByText('Maximum runtime (hours)')).toHaveCount(0)
  await expect(page.getByText('AutoDL dataset bindings')).toBeVisible()
  await expect(page.getByRole('cell', { name: 'scanobjectnn-objbg', exact: true })).toBeVisible()
  await expect(page.getByText('/root/autodl-fs/datasets/ScanObjectNN')).toBeVisible()
  await expect(page.getByLabel('Catalog')).toBeVisible()
  await expect(page.getByRole('option', { name: 'ScanObjectNN OBJ-BG' })).toBeAttached()
  await expect(page.getByLabel('HTTPS sources')).toBeVisible()
  await page.getByLabel('Catalog').selectOption('scanobjectnn-objbg')
  await expect(page.getByPlaceholder('scanobjectnn-objbg')).toHaveValue('scanobjectnn-objbg')
  await expect(page.getByPlaceholder('/root/autodl-fs/datasets/ScanObjectNN')).toHaveValue('/root/autodl-fs/datasets/ScanObjectNN')
  await page.getByLabel('HTTPS sources').fill('https://huggingface.co/datasets/example/resolve/main/train.h5 main_split/train.h5')
  await page.getByRole('button', { name: 'Register dataset' }).click()
  await expect(page.getByRole('button', { name: 'Register dataset' })).toBeEnabled()
  await expect(page.getByText('AutoDL environments')).toBeVisible()
  await expect(page.getByRole('cell', { name: 'public-elastic', exact: true })).toBeVisible()
  await expect(page.getByText('image-6c15b8aad2')).toBeVisible()
  await page.getByPlaceholder('torch-train').fill('torch-train')
  await page.getByPlaceholder('image-6c15b8aad2').fill('image-visible1234')
  await page.getByRole('button', { name: 'Register environment' }).click()
  await expect(page.getByRole('button', { name: 'Register environment' })).toBeEnabled()
  await page.getByLabel('Monthly budget (CNY)').fill('120')
  await page.getByRole('button', { name: 'Save budget' }).click()
  await expect(page.getByRole('button', { name: 'Save budget' })).toBeEnabled()
  await expect(page.getByRole('cell', { name: 'dynamic-point-mamba', exact: true })).toBeVisible()
  await page.getByTitle('View Deploy public key').first().click()
  await expect(page.getByRole('heading', { name: 'Deploy public key' })).toBeVisible()
  await expect(page.getByText('If you cannot add a repository Deploy Key, add this same Gemcp public key to your GitHub account SSH keys.')).toBeVisible()
  await expect(page.getByRole('link', { name: 'Settings → SSH and GPG keys' })).toHaveAttribute('href', 'https://github.com/settings/keys')
  await page.screenshot({ path: '/tmp/gemcp-repository-dialog.png', fullPage: true })
  await page.getByTitle('Close').click()

  await page.getByRole('button', { name: 'Evidence', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Prepare experiment' })).toBeVisible()
  await page.locator('.owner-prepare-form textarea').first().fill('python\ntools/smoke.py')
  await page.screenshot({ path: '/tmp/gemcp-owner-prepare-form.png', fullPage: true })
  await page.getByRole('button', { name: 'Prepare proposal' }).click()
  const ownerPrepareDialog = page.getByRole('dialog', { name: 'Confirm prepared proposal' })
  await expect(ownerPrepareDialog).toBeVisible()
  await expect(ownerPrepareDialog.getByText(`sha256:${'d'.repeat(64)}`, { exact: true })).toBeVisible()
  await page.screenshot({ path: '/tmp/gemcp-owner-prepare-confirm.png', fullPage: true })
  await ownerPrepareDialog.getByTitle('Close').click()
  await expect(page.getByText('train · 57600s')).toBeVisible()
  await expect(page.locator('.proposal-card').filter({ hasText: 'train · 57600s' }).getByText('Prepared', { exact: true })).toBeVisible()
  await page.locator('.proposal-card').filter({ hasText: 'train · 57600s' }).getByRole('button', { name: 'Confirm and start' }).click()
  const confirmationDialog = page.getByRole('dialog', { name: 'Confirm prepared proposal' })
  await expect(confirmationDialog).toBeVisible()
  await expect(confirmationDialog.getByText(operationsFeed.proposals[0].confirmation_digest, { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByText('CNY 20.000', { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByText(operationsFeed.proposals[0].display_command, { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByText('Public HTTPS', { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByText('objbg-smoke', { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByText('scanobjectnn-objbg', { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByText('node-h1', { exact: true })).toBeVisible()
  await expect(confirmationDialog.getByRole('button', { name: 'Confirm and start' })).toBeDisabled()
  await page.screenshot({ path: '/tmp/gemcp-proposal-confirmation.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await expect(confirmationDialog).toBeVisible()
  await page.screenshot({ path: '/tmp/gemcp-proposal-confirmation-mobile.png', fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await confirmationDialog.getByRole('checkbox').check()
  await confirmationDialog.getByRole('button', { name: 'Confirm and start' }).click()
  await expect(confirmationDialog).not.toBeVisible()
  await expect(page.getByText('Off-graph')).toBeVisible()
  await expect(page.getByRole('dialog', { name: 'Experiment details' })).toBeVisible()
  await expect(page.getByText('source_extracted', { exact: true })).toBeVisible()
  await expect(page.getByText('Source downloads', { exact: true })).toBeVisible()
  await expect(page.getByText('Registered artifacts', { exact: true })).toBeVisible()
  await expect(page.getByText('gemcp-launch.log, run.log, metrics.json', { exact: true })).toBeVisible()
  await expect(page.getByText('Reservation open', { exact: true })).toBeVisible()
  await expect(page.getByText('epoch 3 loss=0.42')).toBeVisible()
  await expect(page.getByText('GPU-test-3090', { exact: true })).toBeVisible()
  await expect(page.getByText('/gemcp/work/source', { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-experiment-detail.png', fullPage: true })
})

test('Owner can save a succeeded one-shot as a Project workload', async ({ page, context }) => {
  await context.grantPermissions(['clipboard-read', 'clipboard-write']).catch(() => undefined)
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Evidence', exact: true }).click()
  await page.locator('.segmented-control').getByRole('button', { name: 'Succeeded' }).click()
  await page.getByRole('row').filter({ hasText: '33aa44bb' }).click()
  const detail = page.getByRole('dialog', { name: 'Experiment details' })
  await expect(detail).toBeVisible()
  await expect(detail.getByRole('button', { name: 'Save as workload' })).toBeVisible()
  await detail.getByRole('button', { name: 'Save as workload' }).click()
  const saveDialog = page.getByRole('dialog', { name: 'Save as workload' })
  await expect(saveDialog).toBeVisible()
  await saveDialog.getByPlaceholder('oneshot').fill('oneshot-smoke')
  await expect(saveDialog.locator('.workload-yaml')).toContainText('oneshot-smoke:')
  await page.screenshot({ path: '/tmp/gemcp-save-workload.png', fullPage: true })
  await saveDialog.getByRole('button', { name: 'Save workload' }).click()
  await expect(saveDialog).not.toBeVisible()
  await expect(detail).toContainText('Saved as')
  await expect(detail).toContainText('oneshot-smoke')
  await expect(detail.getByRole('button', { name: 'Save as workload' })).toHaveCount(0)
  await detail.getByTitle('Close details').click()
  await page.getByLabel('Kind').selectOption('workload')
  await expect(page.getByText('Saved on this Project', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'oneshot-smoke', exact: true })).toBeVisible()
})

test('scheduler-disabled Evidence keeps Owner confirmation available and explains queued execution', async ({ page }) => {
  await mockConsole(page, { schedulerEnabled: false })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Evidence', exact: true }).click()

  await expect(page.getByText('Scheduler is disabled', { exact: true })).toBeVisible()
  await expect(page.getByText(/Prepared proposals can still be confirmed, but their Experiments remain queued/)).toBeVisible()
  await expect(page.getByText(/GEMCP_SCHEDULER_ENABLED=true/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Confirm and start' })).toBeVisible()
})

test('backend diagnostics preflight, execution analysis and cancellation fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Diagnostics', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Backend diagnostics' })).toBeVisible()
  await expect(page.getByRole('row').filter({ hasText: 'Passed' })).toBeVisible()

  await page.getByRole('row').filter({ hasText: 'Passed' }).click()
  await expect(page.getByText('diagnostic_passed', { exact: true })).toBeVisible()
  await expect(page.getByText('NVIDIA GeForce RTX 3090, GPU-test', { exact: false })).toBeVisible()

  await page.getByLabel('Commit SHA').fill(diagnosticPreflight.proposal.commit_sha)
  await page.getByRole('button', { name: 'Run preflight' }).click()
  await expect(page.getByText('Preflight passed', { exact: true })).toBeVisible()
  await expect(page.getByText('CNY 0.234', { exact: true })).toBeVisible()
  await expect(page.getByText(diagnosticPreflight.confirmation_digest, { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-diagnostics-preflight-desktop.png', fullPage: true })

  await page.getByRole('checkbox').check()
  await page.getByRole('button', { name: 'Start diagnostic' }).click()
  await expect(page.getByText('waiting_for_scheduler', { exact: true })).toBeVisible()
  await page.getByTitle('Cancel diagnostic').click()
  const cancelDialog = page.getByRole('alertdialog', { name: 'Cancel diagnostic' })
  await expect(cancelDialog).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-diagnostics-cancel-desktop.png', fullPage: true })
  await cancelDialog.getByRole('button', { name: 'Cancel diagnostic' }).click()
  await expect(page.locator('.assessment-band strong')).toHaveText('cancelled')

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-diagnostics-mobile.png', fullPage: true })
})

test('Lab image bake workspace lists a requested bake without starting Pro until confirm', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Images', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Image bake' })).toBeVisible()
  await expect(page.getByText('torch-mamba', { exact: true }).first()).toBeVisible()
  await page.getByRole('row').filter({ hasText: 'torch-mamba' }).click()
  await expect(page.getByText(requestedImageBake.confirmation_digest, { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Confirm and start Pro' })).toBeDisabled()
  await page.getByRole('checkbox').check()
  await page.getByRole('button', { name: 'Confirm and start Pro' }).click()
  await expect(page.getByText('image-baked12345', { exact: true }).first()).toBeVisible()
})

test('Owner finance analytics and Project budget changes fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Finance', exact: true }).click()

  await expect(page.locator('.finance-heading h1')).toHaveText('Budget and ledger')
  await expect(page.getByText('Approved July AutoDL test allocation', { exact: true })).toBeVisible()
  await expect(page.getByText('AutoDL Private', { exact: true }).first()).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-finance-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Add Project budget', exact: true }).click()
  const adjustmentDialog = page.getByRole('dialog', { name: 'Add Project budget' })
  await expect(adjustmentDialog).toBeVisible()
  await adjustmentDialog.getByLabel('Amount (CNY)').fill('50')
  await adjustmentDialog.getByLabel('Reason').fill('Approved AutoDL integration test allocation')
  await adjustmentDialog.getByRole('checkbox').check()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-finance-adjustment-desktop.png', fullPage: true })
  await adjustmentDialog.getByRole('button', { name: 'Add budget', exact: true }).click()
  await expect(adjustmentDialog).toHaveCount(0)

  await page.getByRole('button', { name: 'Audit', exact: true }).click()
  await expect(page.getByText('Budget credit recorded', { exact: true })).toBeVisible()
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-finance-mobile.png', fullPage: true })

  await page.getByRole('button', { name: 'Ledger', exact: true }).click()
  await page.getByRole('button', { name: 'Add Project budget', exact: true }).click()
  await expect(adjustmentDialog).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-finance-adjustment-mobile.png', fullPage: true })
})

test('global language toggle switches immediately and persists', async ({ page }) => {
  await mockConsole(page, { providerQueries: 0, selfHosted: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')

  await page.getByRole('button', { name: 'Switch to Chinese' }).click()
  await expect(page.getByRole('heading', { name: '研究', exact: true }).first()).toBeVisible()
  await expect(page.getByText('下一步', { exact: true })).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'zh-CN')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-console-zh-desktop.png', fullPage: true })

  await page.reload()
  await expect(page.getByRole('heading', { name: '研究', exact: true }).first()).toBeVisible()
  await expect(page.getByRole('button', { name: '切换到英文' })).toContainText('EN')

  await page.getByRole('button', { name: '节点', exact: true }).click()
  await expect(page.locator('.node-heading h1')).toHaveText('自托管节点')
  await expect(page.getByText('已授权 GPU 算力尚未向 Agent 暴露', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '升级指引 lab-gpu-01' }).click()
  const chineseUpgradeDialog = page.getByRole('dialog', { name: '升级 lab-gpu-01' })
  await expect(chineseUpgradeDialog.getByText('复制 Agent 指令', { exact: true })).toBeVisible()
  await expect(chineseUpgradeDialog.locator('.upgrade-instruction')).toContainText('目标版本：v0.15.0')
  await chineseUpgradeDialog.getByRole('button', { name: '关闭', exact: true }).first().click()
  await page.getByRole('button', { name: 'Agent', exact: true }).click()
  await expect(page.locator('.agent-heading h2')).toHaveText('Agent 访问')
  await page.getByRole('button', { name: 'Provider', exact: true }).click()
  await expect(page.getByText('实时 Provider', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '告警', exact: true }).click()
  await expect(page.locator('.notification-heading h2')).toHaveText('关键通知')
  await page.getByRole('button', { name: '财务', exact: true }).click()
  await expect(page.locator('.finance-heading h1')).toHaveText('预算与账本')
  await page.getByRole('button', { name: '诊断', exact: true }).click()
  await expect(page.getByRole('heading', { name: '后端诊断', exact: true })).toBeVisible()
  await expect(page.getByText('GPU 连通性', { exact: true }).first()).toBeVisible()
  await page.getByLabel('Commit SHA').fill(diagnosticPreflight.proposal.commit_sha)
  await page.getByRole('button', { name: '运行预检' }).click()
  await expect(page.getByText('Project 预算可预留本次诊断', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: '研究', exact: true }).click()

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-console-zh-mobile.png', fullPage: true })

  await page.getByRole('button', { name: '切换到英文' }).click()
  await expect(page.getByRole('heading', { name: 'Research', exact: true }).first()).toBeVisible()
  await expect(page.locator('html')).toHaveAttribute('lang', 'en')
})

test('Self-hosted nodes, Assignments and runtime configuration fit desktop and mobile', async ({ page }) => {
  await mockConsole(page, { providerQueries: 0, selfHosted: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Nodes', exact: true }).click()
  await expect(page.getByText('gpu-workstation', { exact: false })).toBeVisible()
  await expect(page.getByText('local-3090', { exact: true })).toBeVisible()
  await expect(page.getByText('ec29dc68-967', { exact: true })).toBeVisible()
  await expect(page.getByText('gmn_test', { exact: true })).toHaveCount(0)
  await expect(page.getByText('Authorized GPU capacity is not exposed to Agents yet', { exact: true })).toBeVisible()
  await expect(page.getByText('usb-pc · NVIDIA RTX A4000 · 16.0 GB', { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-nodes-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Enable workspace', exact: true }).click()
  const workspaceDialog = page.locator('.workspace-dialog')
  await expect(workspaceDialog.getByRole('heading', { name: 'Trusted workspace · usb-pc' })).toBeVisible()
  await expect(workspaceDialog.getByText('Hardware limits and the Self-hosted profile are generated automatically from the latest Node heartbeat.', { exact: true })).toBeVisible()
  await workspaceDialog.getByLabel('Approved host workspace').fill('/home/campus.ncl.ac.uk/nxl51/gemcp_tmp')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-workspace-dialog-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-workspace-dialog-mobile.png', fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1000 })
  await workspaceDialog.getByRole('button', { name: 'Approve workspace', exact: true }).click()
  await expect(workspaceDialog).toHaveCount(0)

  await page.getByRole('button', { name: 'Upgrade instructions lab-gpu-01' }).click()
  const upgradeDialog = page.getByRole('dialog', { name: 'Upgrade lab-gpu-01' })
  await expect(upgradeDialog.getByText('v0.15.0', { exact: true })).toBeVisible()
  await expect(upgradeDialog.getByText('An active Assignment is attached to this Node. Do not run the upgrade until it reaches a terminal state.', { exact: true })).toBeVisible()
  await expect(upgradeDialog.locator('.upgrade-instruction')).toContainText(`TARGET_COMMIT='${'b'.repeat(40)}'`)
  await expect(upgradeDialog.locator('.upgrade-instruction')).toContainText('deploy/upgrade-gemcp-node.sh')
  await expect(upgradeDialog.locator('.upgrade-instruction')).not.toContainText('gmn_test')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-node-upgrade-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-node-upgrade-mobile.png', fullPage: true })
  await upgradeDialog.getByRole('button', { name: 'Close', exact: true }).first().click()
  await page.setViewportSize({ width: 1440, height: 1000 })

  await page.getByRole('button', { name: 'Create enrollment', exact: true }).click()
  const enrollmentDialog = page.locator('form.node-dialog').filter({ hasText: 'Create enrollment' })
  await enrollmentDialog.getByLabel('Node label').fill('second-gpu-node')
  await enrollmentDialog.getByRole('button', { name: 'Create', exact: true }).click()
  const setupLinkDialog = page.getByRole('dialog', { name: 'Setup link' })
  await expect(setupLinkDialog.getByText(`${issuedNodeSetupURL.replace('#', '?lang=en#')}`, { exact: true })).toBeVisible()
  await setupLinkDialog.getByRole('button', { name: '中文', exact: true }).click()
  await expect(setupLinkDialog.getByText(`${issuedNodeSetupURL.replace('#', '?lang=zh#')}`, { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-node-setup-language-desktop.png', fullPage: true })
  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-node-setup-language-mobile.png', fullPage: true })
  await setupLinkDialog.getByRole('button', { name: 'Done', exact: true }).click()
  await page.setViewportSize({ width: 1440, height: 1000 })

  await page.getByRole('button', { name: 'Advanced runtime', exact: true }).click()
  const runtimeDialog = page.locator('.runtime-dialog')
  await expect(runtimeDialog.getByRole('heading', { name: 'Add Self-hosted runtime' })).toBeVisible()
  await expect(runtimeDialog.getByLabel('Accepted GPU models')).toHaveValue('NVIDIA GeForce RTX 3090, NVIDIA RTX A4000')
  await runtimeDialog.getByLabel('Runtime name').fill('second-node-runtime')
  await runtimeDialog.getByLabel('OCI image pinned by digest').fill(`registry.example/second@sha256:${'c'.repeat(64)}`)
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-runtime-dialog-desktop.png', fullPage: true })

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-runtime-dialog-mobile.png', fullPage: true })
  await runtimeDialog.getByRole('button', { name: 'Close' }).click()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-nodes-mobile.png', fullPage: true })
})

test('MCP setup links, MCP guidance and advanced token controls fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Agents', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Agent access', level: 1 })).toBeVisible()
  await expect(page.getByText('Agent Readiness')).toBeVisible()
  await expect(page.getByRole('button', { name: 'Copy readiness', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Register Agent', exact: true }).first()).toBeVisible()
  await expect(page.getByText('default-agent', { exact: true }).first()).toBeVisible()
  await expect(page.locator('.agent-table').getByRole('row').filter({ hasText: 'default-agent' }).getByText('gmc_abcd123', { exact: true })).toBeVisible()
  await expect(page.getByText('pi-research-agent', { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agents-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Register Agent', exact: true }).first().click()
  const setupDialog = page.getByRole('dialog', { name: 'Register Agent' })
  await expect(setupDialog).toBeVisible()
  await setupDialog.getByLabel('Agent label').fill('pi-integration-agent')
  await setupDialog.getByLabel('Credential expiration').selectOption('30')
  await setupDialog.getByLabel('Link validity').selectOption('15')
  await expect(setupDialog.getByLabel('Read')).toBeChecked()
  await expect(setupDialog.getByLabel('Submit')).toBeChecked()
  await expect(setupDialog.getByLabel('Cancel')).toBeChecked()
  await setupDialog.getByRole('button', { name: 'Create setup link' }).click()

  const setupReveal = page.getByRole('dialog', { name: 'One-time MCP setup link' })
  await expect(setupReveal).toBeVisible()
  await expect(setupReveal.getByText(issuedSetupURL, { exact: true })).toBeVisible()
  await expect(setupReveal.getByText('Give the Agent this Project setup prompt', { exact: true })).toBeVisible()
  await expect(setupReveal.getByText('Project setup prompt', { exact: true })).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-setup-link.png', fullPage: true })
  await setupReveal.getByTitle('Close MCP setup link').click()
  await expect(page.getByText(issuedSetupURL, { exact: true })).toHaveCount(0)
  const setupRow = page.getByRole('row').filter({ hasText: 'pi-integration-agent' })
  await expect(setupRow).toBeVisible()
  await setupRow.getByTitle('Revoke MCP setup link').click()
  const setupRevokeDialog = page.getByRole('alertdialog', { name: 'Revoke MCP setup link' })
  await expect(setupRevokeDialog).toBeVisible()
  await setupRevokeDialog.getByRole('button', { name: 'Revoke link' }).click()
  await expect(setupRow.getByText('revoked', { exact: true })).toBeVisible()

  await page.getByRole('button', { name: 'Guide', exact: true }).click()
  const guideDialog = page.getByRole('dialog', { name: 'Gemcp MCP onboarding guide' })
  await expect(guideDialog).toBeVisible()
  await expect(guideDialog.getByText('get_usage_guide', { exact: true })).toBeVisible()
  await expect(guideDialog.getByText('gemcp://docs/agent-guide', { exact: true })).toBeVisible()
  await expect(guideDialog.getByRole('link', { name: 'Download Agent handoff' })).toHaveAttribute('href', 'https://gemcp.example.com/docs/agent-mcp.md')
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-guide-desktop.png', fullPage: true })
  await guideDialog.getByTitle('Close MCP guide').click()

  await page.getByRole('button', { name: 'Token', exact: true }).click()
  const issueDialog = page.getByRole('dialog', { name: 'Generate Agent token' })
  await expect(issueDialog).toBeVisible()
  await issueDialog.getByLabel('Label').fill('integration-agent')
  await issueDialog.getByLabel('Expiration').selectOption('30')
  await expect(issueDialog.getByLabel('Read')).toBeChecked()
  await expect(issueDialog.getByLabel('Submit')).toBeChecked()
  await expect(issueDialog.getByLabel('Cancel')).toBeChecked()
  await issueDialog.getByRole('button', { name: 'Generate token' }).click()

  const revealDialog = page.getByRole('dialog', { name: 'Agent token and MCP configuration' })
  await expect(revealDialog).toBeVisible()
  await expect(revealDialog.getByText(issuedAgentToken, { exact: true })).toBeVisible()
  const downloadPromise = page.waitForEvent('download')
  await revealDialog.getByRole('button', { name: 'Download JSON' }).click()
  const download = await downloadPromise
  expect(download.suggestedFilename()).toBe('gemcp-point-models-mcp.json')
  const downloadPath = '/tmp/gemcp-exported-mcp.json'
  await download.saveAs(downloadPath)
  const exported = JSON.parse(await readFile(downloadPath, 'utf8'))
  expect(exported).toEqual(mcpConfig)
  await unlink(downloadPath)
  await page.screenshot({ path: '/tmp/gemcp-agent-token-reveal.png', fullPage: true })
  await revealDialog.getByTitle('Close Agent token').click()
  await expect(page.getByText(issuedAgentToken, { exact: true })).toHaveCount(0)

  const defaultRow = page.getByRole('row').filter({ hasText: 'default-agent' })
  await defaultRow.getByTitle('Edit Agent scopes').click()
  const scopeDialog = page.getByRole('dialog', { name: 'Edit Agent scopes' })
  await expect(scopeDialog).toBeVisible()
  await expect(scopeDialog.getByLabel('Configure')).not.toBeChecked()
  await scopeDialog.getByLabel('Configure').check()
  await scopeDialog.getByRole('button', { name: 'Update scopes' }).click()
  await expect(defaultRow.getByText('configure', { exact: true })).toBeVisible()
  await defaultRow.getByTitle('Revoke Agent token').click()
  const revokeDialog = page.getByRole('alertdialog', { name: 'Revoke Agent token' })
  await expect(revokeDialog).toBeVisible()
  await revokeDialog.getByRole('button', { name: 'Revoke token' }).click()
  await expect(defaultRow.getByText('revoked', { exact: true })).toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.getByRole('button', { name: 'Guide', exact: true }).click()
  await expect(guideDialog).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-guide-mobile.png', fullPage: true })
  await guideDialog.getByTitle('Close MCP guide').click()
  await page.getByRole('button', { name: 'Register Agent', exact: true }).first().click()
  await expect(setupDialog).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-agent-setup-form-mobile.png', fullPage: true })
  await setupDialog.getByTitle('Close setup link form').click()
  await page.screenshot({ path: '/tmp/gemcp-agents-mobile.png', fullPage: true })
})

test('live Provider resources and details fit desktop and mobile', async ({ page }) => {
  const counters = { providerQueries: 0 }
  await mockConsole(page, counters)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Provider', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'AutoDL resources' })).toBeVisible()
  await expect(page.getByText('2 / 9')).toBeVisible()
  await expect(page.getByText('NVIDIA GeForce RTX 3090')).toBeVisible()
  await expect.poll(() => counters.providerQueries).toBe(1)
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-provider-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Research', exact: true }).click()
  await page.getByRole('button', { name: 'Provider', exact: true }).click()
  await expect(page.getByText('NVIDIA GeForce RTX 3090')).toBeVisible()
  expect(counters.providerQueries).toBe(1)

  await page.getByRole('button', { name: 'images', exact: true }).click()
  await expect(page.getByText('torch:cuda11.8-cudnn8-devel-ubuntu22.04-py310-torch2.1.2')).toBeVisible()
  await page.getByRole('button', { name: 'deployments', exact: true }).click()
  await page.getByText('live-job', { exact: true }).click()
  await expect(page.getByRole('dialog', { name: 'Provider deployment details' })).toBeVisible()
  await expect(page.getByText('container-live-1').first()).toBeVisible()
  await expect(page.getByText('Managed').last()).toBeVisible()
  await page.getByRole('button', { name: 'Stop deployment' }).click()
  await expect(page.getByRole('button', { name: 'Stop requested' })).toBeDisabled()
  await page.screenshot({ path: '/tmp/gemcp-provider-deployment.png', fullPage: true })
  await page.getByTitle('Close details').click()

  await page.getByRole('button', { name: 'Emergency stop' }).click()
  await expect(page.getByRole('alertdialog', { name: 'Emergency stop all managed resources' })).toBeVisible()
  await page.getByLabel('Type STOP to confirm').fill('STOP')
  await page.screenshot({ path: '/tmp/gemcp-provider-emergency-stop.png', fullPage: true })
  await page.getByRole('button', { name: 'Stop all managed resources' }).click()

  await page.getByRole('button', { name: 'Rotate token' }).click()
  await expect(page.getByRole('dialog', { name: 'Rotate Provider token' })).toBeVisible()
  await page.screenshot({ path: '/tmp/gemcp-provider-token-dialog.png', fullPage: true })
  await page.getByLabel('Developer Token').fill('preview-token-that-must-be-cleared-on-close')
  await page.getByTitle('Close').click()
  await page.getByRole('button', { name: 'Rotate token' }).click()
  await expect(page.getByLabel('Developer Token')).toHaveValue('')
  await page.getByTitle('Close').click()

  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: 'inventory', exact: true }).click()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-provider-mobile.png', fullPage: true })
})

test('durable notification outbox and SMTP settings fit desktop and mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 1440, height: 1000 })
  await page.goto('/')
  await page.getByRole('button', { name: 'Alerts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Notifications', exact: true })).toBeVisible()
  await expect(page.getByText('[Gemcp] Watchdog enforced Provider shutdown')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-notifications-desktop.png', fullPage: true })

  await page.getByRole('button', { name: 'Send test' }).click()
  await expect(page.getByText('Gemcp SMTP test')).toBeVisible()
  await page.getByRole('button', { name: 'SMTP settings' }).click()
  await expect(page.getByRole('dialog', { name: 'SMTP notification settings' })).toBeVisible()
  await expect(page.locator('input[type="password"]')).toHaveValue('')
  await page.screenshot({ path: '/tmp/gemcp-smtp-settings.png', fullPage: true })
  await page.getByTitle('Close SMTP settings').click()

  await page.setViewportSize({ width: 390, height: 844 })
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-notifications-mobile.png', fullPage: true })
})

test('operations console uses bottom navigation on mobile', async ({ page }) => {
  await mockConsole(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/')
  await expect(page.getByRole('heading', { name: 'Research', exact: true }).first()).toBeVisible()
  await expectNoPageOverflow(page)
  const navigation = page.getByRole('navigation', { name: 'Primary navigation' })
  await expect(navigation).toBeVisible()
  const box = await navigation.boundingBox()
  expect(box?.y ?? 0).toBeGreaterThan(780)
  await page.screenshot({ path: '/tmp/gemcp-console-mobile.png', fullPage: true })
  await navigation.getByRole('button', { name: 'Evidence', exact: true }).click()
  await page.getByText('ec29dc68').click()
  await expect(page.getByRole('dialog', { name: 'Experiment details' })).toBeVisible()
  await expect(page.getByText('epoch 3 loss=0.42')).toBeVisible()
  await expectNoPageOverflow(page)
  await page.screenshot({ path: '/tmp/gemcp-experiment-detail-mobile.png', fullPage: true })
})
