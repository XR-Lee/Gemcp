export type BuildInfo = {
  name: string
  version: string
  commit: string
  built_at: string
}

export type User = {
  user_id: string
  tenant_id: string
  email: string
  role: string
}

export type Project = {
  id: string
  name: string
  slug: string
  status: 'active' | 'paused' | 'archived'
  monthly_budget_milli: number
  max_experiment_milli: number
  max_concurrency: number
  max_runtime_seconds: number
  timeout_extension_seconds: number
  termination_grace_seconds: number
  timezone: string
}

export type AgentScope = 'read' | 'submit' | 'cancel' | 'configure'

export type AgentToken = {
  id: string
  project_id: string
  label: string
  prefix: string
  scopes: AgentScope[]
  status: 'active' | 'expired' | 'revoked'
  expires_at?: string
  last_used_at?: string
  created_at: string
  updated_at: string
}

export type MCPServerConfig = {
  type: 'http'
  url: string
  headers: Record<string, string>
}

export type MCPConfig = {
  mcpServers: Record<string, MCPServerConfig>
}

export type AgentEnrollment = {
  id: string
  project_id: string
  label: string
  scopes: AgentScope[]
  status: 'pending' | 'claimed' | 'completed' | 'expired' | 'revoked'
  expires_at: string
  token_expires_in_days?: number
  claimed_at?: string
  completed_at?: string
  agent_token_id?: string
  agent_token_prefix?: string
  created_at: string
  updated_at: string
}

export type AgentTokenList = {
  tokens: AgentToken[]
  enrollments: AgentEnrollment[]
  mcp_url?: string
  config_template?: MCPConfig
  config_file_name: string
  truncated?: boolean
  enrollments_truncated?: boolean
}

export type AgentEnrollmentIssue = {
  enrollment: AgentEnrollment
  setup_url: string
  installer_url: string
}

export type AgentTokenIssue = {
  token: AgentToken
  agent_token: string
  mcp_url: string
  mcp_config: MCPConfig
  config_file_name: string
}

export type Repository = {
  id: string
  project_id: string
  name: string
  ssh_url: string
  default_branch: string
  status: 'pending_key' | 'active' | 'disabled' | 'error'
  deploy_public_key?: string
  host_key_fingerprint?: string
  last_verified_at?: string
}

export type Experiment = {
  id: string
  project_id: string
  repository_id: string
  environment_id: string
  resource_profile_id: string
  state: string
  desired_state: string
  commit_sha: string
  execution_mode?: 'shell' | 'argv'
  argv?: string[]
  command: string
  max_runtime_seconds: number
  reserved_cost_milli: number
  estimated_cost_milli: number
  output_path: string
  provider_resource_id?: string
  provider_status?: string
  exit_code?: number
  failure_code?: string
  failure_reason?: string
  runner_attempt_id?: string
  runner_source_downloads?: number
  runner_stage?: string
  runner_stage_updated_at?: string
  runner_error_type?: string
  log_tail?: string
  metrics?: Record<string, unknown>
  execution_context: ExecutionContext
  backend_observation?: BackendObservation
  timeline?: RunTimelineEvent[]
  created_at: string
  updated_at: string
  started_at?: string
  deadline_at?: string
  finished_at?: string
  cancel_requested_at?: string
}

export type Attempt = {
  id: string
  number: number
  state: string
  provider_resource_id?: string
  retry_reason?: string
  failure_code?: string
  failure_reason?: string
  started_at?: string
  finished_at?: string
  estimated_cost_milli: number
  exit_code?: number
  log_tail?: string
  metrics?: Record<string, unknown>
  last_heartbeat_at?: string
  created_at: string
  updated_at: string
}

export type RuntimeGPUDevice = { index: number; uuid: string; name: string }
export type RuntimeInfo = {
  source: 'runner_observed' | 'node_binding'
  working_directory: string
  output_directory: string
  cuda_visible_devices?: string
  gpu_devices: RuntimeGPUDevice[]
}
export type ExecutionContext = {
  agent_label?: string
  agent_token_prefix?: string
  proposal_id?: string
  repository_name: string
  repository_ssh_url: string
  requested_ref?: string
  backend: string
  environment_name: string
  image: string
  resource_profile_name: string
  region: string
  gpu_models: string[]
  gpu_num: number
  workspace_policy: 'runner_temporary' | 'container_fixed'
  workspace_path?: string
  container_output_path: string
  runtime_info?: RuntimeInfo
}
export type BackendObservation = {
  kind: string
  id: string
  provider_id?: string
  state: string
  status?: string
  node_label?: string
  stop_reason?: string
  last_error?: string
  cleanup_complete: boolean
  updated_at: string
  finished_at?: string
}
export type RunTimelineEvent = { at: string; code: string; detail?: string }

export type AgentActivity = {
  id: string
  agent_label: string
  agent_token_prefix: string
  phase: string
  repository_remote?: string
  ref?: string
  proposal_id?: string
  experiment_id?: string
  at: string
}
export type ProposalActivity = {
  id: string
  status: string
  eligible: boolean
  agent_label: string
  agent_token_prefix: string
  repository_name: string
  requested_ref: string
  commit_sha: string
  display_command: string
  backend: string
  environment_name: string
  image: string
  resource_profile_name: string
  gpu_models: string[]
  gpu_num: number
  reserved_cost_milli: number
  checks: DiagnosticCheck[]
  confirmation_digest: string
  experiment_id?: string
  created_at: string
  updated_at: string
  expires_at: string
}
export type OperationsFeed = { activities: AgentActivity[]; proposals: ProposalActivity[]; generated_at: string }

export type DiagnosticBackend = 'autodl_private' | 'self_hosted'
export type DiagnosticSuite = 'gpu_connectivity' | 'pytorch_cuda'

export type DiagnosticRepositoryOption = { id: string; name: string; default_branch: string }
export type DiagnosticEnvironmentOption = { id: string; name: string; backend: DiagnosticBackend; image_uuid: string }
export type DiagnosticProfileOption = {
  id: string; name: string; backend: DiagnosticBackend; gpu_names: string[]; gpu_num: number; price_to_milli: number;
}
export type DiagnosticSuiteOption = {
  id: DiagnosticSuite; runtime_seconds: number; requires_pytorch: boolean; checks_cuda_compute: boolean;
}
export type DiagnosticOptions = {
  project_id: string
  repositories: DiagnosticRepositoryOption[]
  environments: DiagnosticEnvironmentOption[]
  resource_profiles: DiagnosticProfileOption[]
  suites: DiagnosticSuiteOption[]
  generated_at: string
}
export type DiagnosticInput = {
  backend: DiagnosticBackend
  suite: DiagnosticSuite
  repository_id: string
  environment_id: string
  resource_profile_id: string
  commit_sha: string
}
export type DiagnosticCheck = { id: string; status: 'pass' | 'warn' | 'fail'; summary: string; detail?: string }
export type DiagnosticProposal = DiagnosticInput & {
  command: string
  runtime_seconds: number
  termination_grace_seconds: number
  reserved_cost_milli: number
  billable: boolean
  gpu_models: string[]
  gpu_num: number
  image_uuid: string
  region: string
  cuda_from: number
  cuda_to: number
  cpu_from: number
  cpu_to: number
  memory_from_gb: number
  memory_to_gb: number
  price_from_milli: number
  price_to_milli: number
  reuse_container: boolean
}
export type DiagnosticPreflight = {
  eligible: boolean
  requires_confirmation: boolean
  checks: DiagnosticCheck[]
  proposal: DiagnosticProposal
  confirmation_digest: string
  generated_at: string
}
export type DiagnosticAssessment = {
  status: 'running' | 'passed' | 'failed'
  classification: string
  summary: string
  recommendations?: string[]
  cleanup_complete: boolean
}
export type DiagnosticAttempt = Attempt & { source_downloads: number }
export type DiagnosticBackendObservation = {
  kind: DiagnosticBackend
  id: string
  state: string
  status?: string
  stop_reason?: string
  last_error?: string
  stop_requested_at?: string
  finished_at?: string
}
export type DiagnosticTimelineEvent = { at: string; code: string; detail?: string }
export type DiagnosticRunSummary = {
  id: string
  project_id: string
  backend: DiagnosticBackend
  suite: DiagnosticSuite
  requested_by: string
  experiment: { id: string; state: string }
  assessment: DiagnosticAssessment
  created_at: string
  updated_at: string
}
export type DiagnosticRun = {
  id: string
  project_id: string
  backend: DiagnosticBackend
  suite: DiagnosticSuite
  requested_by: string
  preflight: DiagnosticPreflight
  experiment: Experiment
  assessment: DiagnosticAssessment
  attempts: DiagnosticAttempt[]
  backend_observation?: DiagnosticBackendObservation
  timeline: DiagnosticTimelineEvent[]
  created_at: string
  updated_at: string
}

export type Cost = {
  period: string
  monthly_budget_milli: number
  reserved_milli: number
  charged_milli: number
  adjustments_milli: number
  committed_milli: number
  available_milli: number
}

export type FinanceTotals = {
  base_budget_milli: number
  reserved_milli: number
  charged_milli: number
  credits_milli: number
  debits_milli: number
  committed_milli: number
  available_milli: number
}

export type FinanceProjectSummary = FinanceTotals & {
  id: string
  name: string
  status: string
  timezone: string
}

export type FinanceDailyPoint = {
  date: string
  reserved_milli: number
  charged_milli: number
  credits_milli: number
  debits_milli: number
}

export type FinanceBackendSummary = {
  backend: string
  experiments: number
  reserved_milli: number
  charged_milli: number
}

export type FinanceLedgerEntry = {
  id: string
  project_id: string
  project_name: string
  experiment_id?: string
  backend?: string
  period: string
  kind: string
  direction?: 'credit' | 'debit'
  amount_milli: number
  balance_effect_milli: number
  description: string
  created_at: string
}

export type FinanceAuditEntry = {
  id: string
  actor_type: string
  actor_id?: string
  action: string
  target_type: string
  target_id?: string
  created_at: string
}

export type FinanceDashboard = {
  period: string
  audit_scope: 'organization'
  totals: FinanceTotals
  projects: FinanceProjectSummary[]
  daily: FinanceDailyPoint[]
  backends: FinanceBackendSummary[]
  ledger: FinanceLedgerEntry[]
  audit: FinanceAuditEntry[]
  generated_at: string
}

export type BudgetAdjustmentResult = {
  entry: FinanceLedgerEntry
  idempotent: boolean
}

export type ProviderSummary = {
  id: string
  name: string
  base_url: string
  backend: string
  status: string
  credential_configured: boolean
  last_validated_at?: string
  created_at: string
  updated_at: string
}

export type ProviderGPUStock = {
  name: string
  region?: string
  idle: number
  total: number
}

export type ProviderImage = {
  uuid: string
  name: string
  status?: string
  size_bytes?: number
  cuda_version?: string
  chip_corp?: string
  cpu_arch?: string
  source: 'private' | 'system'
}

export type ProviderDeployment = {
  uuid: string
  name: string
  type: string
  status: string
  replica_num: number
  parallelism_num: number
  starting_num: number
  running_num: number
  finished_num: number
  failed_num: number
  image_uuid?: string
  reuse_container: boolean
  price_estimate_milli: number
  created_at?: string
  updated_at?: string
  stopped_at?: string
}

export type ProviderContainer = {
  uuid: string
  deployment_uuid: string
  machine_id?: string
  data_center?: string
  status: string
  gpu_name: string
  gpu_num: number
  cpu_num: number
  memory_bytes: number
  image_uuid: string
  price_milli_per_hour: number
  released: boolean
  started_at?: string
  stopped_at?: string
  created_at?: string
  updated_at?: string
}

export type ProviderEvent = {
  container_uuid: string
  status: string
  created_at: string
}

export type ProviderResources = {
  generated_at: string
  provider: ProviderSummary
  gpu_stock: ProviderGPUStock[]
  private_images: ProviderImage[]
  system_images: ProviderImage[]
  deployments: ProviderDeployment[]
  active_containers: ProviderContainer[]
  cached_containers: ProviderContainer[]
  truncated?: string[]
}

export type RuntimeHeartbeat = {
  role: string
  instance_id: string
  status: string
  last_seen_at: string
  metadata?: Record<string, unknown>
}

export type RuntimeStatus = {
  scheduler_enabled: boolean
  self_hosted_enabled: boolean
  global_concurrency: number
  public_url_configured: boolean
  scheduler_healthy: boolean
  watchdog_healthy: boolean
  notification_worker_healthy: boolean
  scheduler_heartbeat?: RuntimeHeartbeat
  watchdog_heartbeat?: RuntimeHeartbeat
  notification_heartbeat?: RuntimeHeartbeat
  generated_at: string
}

export type ManagedProviderResource = {
  id: string
  experiment_id: string
  attempt_id: string
  provider_id?: string
  name: string
  state: string
  provider_status?: string
  hard_deadline_at?: string
  stop_requested_at?: string
  stop_reason?: string
  last_seen_at?: string
  last_error?: string
  created_at: string
  updated_at: string
}

export type SelfHostedGPU = {
  uuid: string
  name: string
  memory_bytes: number
}

export type SelfHostedNode = {
  id: string
  label: string
  token_prefix: string
  status: string
  observed_state: string
  installation_id: string
  machine_fingerprint: string
  hostname: string
  operating_system: string
  architecture: string
  agent_version: string
  protocol_version: string
  capabilities: { gpus?: SelfHostedGPU[]; cpu_count?: number; memory_bytes?: number; execution_modes?: Array<'shell' | 'argv'> }
  storage: { root?: string; total_bytes?: number; available_bytes?: number; managed_bytes?: number }
  project_ids: string[]
  last_seen_at?: string
  approved_at?: string
  revoked_at?: string
  created_at: string
  updated_at: string
}

export type NodeEnrollment = {
  id: string
  label: string
  status: 'pending' | 'claimed' | 'approved' | 'completed' | 'expired' | 'revoked'
  expires_at: string
  pairing_code?: string
  installation_id?: string
  machine_fingerprint?: string
  report?: Record<string, unknown>
  node_id?: string
  claimed_at?: string
  approved_at?: string
  completed_at?: string
  created_at: string
  updated_at: string
}

export type NodeList = {
  nodes: SelfHostedNode[]
  enrollments: NodeEnrollment[]
  assignments: NodeAssignment[]
  truncated?: boolean
}

export type NodeAssignment = {
  id: string
  node_id: string
  node_label: string
  project_id: string
  experiment_id: string
  attempt_id: string
  attempt_number: number
  state: string
  output_ref: string
  started_at?: string
  last_heartbeat_at?: string
  finished_at?: string
  exit_code?: number
  failure_code?: string
  created_at: string
  updated_at: string
}

export type NodeEnrollmentIssue = {
  enrollment: NodeEnrollment
  setup_url: string
  claim_url: string
}

export type SelfHostedRuntimeEnvironment = {
  id: string
  name: string
  image: string
  is_default: boolean
}

export type SelfHostedRuntimeProfile = {
  id: string
  name: string
  gpu_names: string[]
  cpu_limit: number
  memory_gb: number
  is_default: boolean
}

export type SelfHostedRuntimeList = {
  environments: SelfHostedRuntimeEnvironment[]
  resource_profiles: SelfHostedRuntimeProfile[]
  trusted_workspaces: TrustedWorkspace[]
}

export type TrustedWorkspace = {
  node_id: string
  node_label: string
  workspace_path: string
  environment_id: string
  environment_name: string
  resource_profile_id: string
  gpu_name: string
  cpu_limit: number
  memory_gb: number
  successful_images: string[]
  node_ready: boolean
}

export type SelfHostedRuntimeConfig = {
  environment: SelfHostedRuntimeEnvironment
  resource_profile: SelfHostedRuntimeProfile
}

export type ProviderDeploymentDetails = {
  generated_at: string
  deployment: ProviderDeployment
  active_containers: ProviderContainer[]
  released_containers: ProviderContainer[]
  events: ProviderEvent[]
  truncated?: string[]
}

export type NotificationSetting = {
  configured: boolean
  enabled: boolean
  host?: string
  port?: number
  tls_mode?: 'starttls' | 'tls'
  username?: string
  password_configured: boolean
  from_address?: string
  recipients: string[]
  status?: 'ready' | 'error'
  last_tested_at?: string
  last_error?: string
  updated_at?: string
}

export type NotificationDelivery = {
  id: string
  kind: string
  severity: 'info' | 'warning' | 'critical'
  subject: string
  state: 'pending' | 'sending' | 'sent' | 'failed'
  attempts: number
  next_attempt_at: string
  last_error?: string
  sent_at?: string
  created_at: string
}

export type SetupPayload = Record<string, unknown>

export type SetupResult = {
  tenant_id: string
  owner_id: string
  provider_id: string
  project_id: string
  environment_id: string
  resource_profile_id: string
  agent_token: string
  agent_token_prefix: string
}

type Envelope<T> = { data: T }
type ErrorEnvelope = { error?: { code?: string; message?: string } }

export class APIError extends Error {
  status: number
  code: string

  constructor(status: number, code: string, message: string) {
    super(message)
    this.status = status
    this.code = code
  }
}

let csrfToken = ''

function cookie(name: string): string {
  const prefix = `${encodeURIComponent(name)}=`
  const part = document.cookie.split('; ').find((value) => value.startsWith(prefix))
  return part ? decodeURIComponent(part.slice(prefix.length)) : ''
}

export function setCSRFToken(value: string) {
  csrfToken = value
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const method = (options.method ?? 'GET').toUpperCase()
  const headers = new Headers(options.headers)
  headers.set('Accept', 'application/json')
  if (options.body) headers.set('Content-Type', 'application/json')
  if (!['GET', 'HEAD', 'OPTIONS'].includes(method)) {
    const token = csrfToken || cookie('gemcp_csrf')
    if (token) headers.set('X-CSRF-Token', token)
  }
  const response = await fetch(path, { ...options, headers, credentials: 'same-origin' })
  if (!response.ok) {
    let payload: ErrorEnvelope = {}
    try {
      payload = (await response.json()) as ErrorEnvelope
    } catch {
      // Keep a stable error when a proxy returns a non-JSON response.
    }
    throw new APIError(
      response.status,
      payload.error?.code ?? 'HTTP_ERROR',
      payload.error?.message ?? `Request failed with HTTP ${response.status}`,
    )
  }
  if (response.status === 204) return undefined as T
  const payload = (await response.json()) as Envelope<T>
  return payload.data
}

export const api = {
  build: () => request<BuildInfo>('/api/v1/version'),
  setupStatus: () => request<{ initialized: boolean }>('/api/v1/setup/status'),
  setup: (payload: SetupPayload, bootstrapToken: string) =>
    request<SetupResult>('/api/v1/setup', {
      method: 'POST',
      headers: { 'X-Gemcp-Bootstrap-Token': bootstrapToken },
      body: JSON.stringify(payload),
    }),
  me: () => request<User>('/api/v1/auth/me'),
  login: async (email: string, password: string) => {
    const result = await request<{ user: User; csrf_token: string; expires_at: string }>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    })
    setCSRFToken(result.csrf_token)
    return result.user
  },
  logout: async () => {
    await request<void>('/api/v1/auth/logout', { method: 'POST' })
    setCSRFToken('')
  },
  projects: () => request<Project[]>('/api/v1/projects'),
  nodes: () => request<NodeList>('/api/v1/nodes'),
  issueNodeEnrollment: (payload: { label: string; setup_expires_in_minutes: number }) =>
    request<NodeEnrollmentIssue>('/api/v1/node-enrollments', { method: 'POST', body: JSON.stringify(payload) }),
  approveNodeEnrollment: (enrollmentID: string, payload: { pairing_code: string; project_ids: string[] }) =>
    request<SelfHostedNode>(`/api/v1/node-enrollments/${encodeURIComponent(enrollmentID)}/approve`, {
      method: 'POST', body: JSON.stringify(payload),
    }),
  revokeNodeEnrollment: (enrollmentID: string) =>
    request<NodeEnrollment>(`/api/v1/node-enrollments/${encodeURIComponent(enrollmentID)}`, { method: 'DELETE' }),
  selfHostedRuntimes: (projectID: string) =>
    request<SelfHostedRuntimeList>(`/api/v1/projects/${encodeURIComponent(projectID)}/self-hosted-runtimes`),
  createSelfHostedRuntime: (projectID: string, payload: {
    name: string; image: string; gpu_names: string[]; cpu_limit: number; memory_gb: number; make_default: boolean
  }) => request<SelfHostedRuntimeConfig>(`/api/v1/projects/${encodeURIComponent(projectID)}/self-hosted-runtimes`, {
    method: 'POST', body: JSON.stringify(payload),
  }),
  enableTrustedWorkspace: (projectID: string, payload: { node_id: string; workspace_path: string; make_default: boolean }) =>
    request<TrustedWorkspace>(`/api/v1/projects/${encodeURIComponent(projectID)}/self-hosted-trusted-workspace`, {
      method: 'PUT', body: JSON.stringify(payload),
    }),
  disableTrustedWorkspace: (projectID: string, nodeID: string) =>
    request<{ node_id: string; disabled: boolean }>(`/api/v1/projects/${encodeURIComponent(projectID)}/self-hosted-trusted-workspace/${encodeURIComponent(nodeID)}`, { method: 'DELETE' }),
  agentTokens: (projectID: string) =>
    request<AgentTokenList>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-tokens`),
  issueAgentToken: (projectID: string, payload: {
    label: string; scopes: AgentScope[]; expires_in_days?: number; never_expires: boolean;
  }) => request<AgentTokenIssue>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-tokens`, {
    method: 'POST', body: JSON.stringify(payload),
  }),
  updateAgentTokenScopes: (projectID: string, tokenID: string, scopes: AgentScope[]) =>
    request<AgentToken>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-tokens/${encodeURIComponent(tokenID)}`, {
      method: 'PATCH', body: JSON.stringify({ scopes }),
    }),
  issueAgentEnrollment: (projectID: string, payload: {
    label: string; scopes: AgentScope[]; expires_in_days?: number; never_expires: boolean;
    setup_expires_in_minutes: number;
  }) => request<AgentEnrollmentIssue>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-enrollments`, {
    method: 'POST', body: JSON.stringify(payload),
  }),
  revokeAgentEnrollment: (projectID: string, enrollmentID: string) =>
    request<AgentEnrollment>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-enrollments/${encodeURIComponent(enrollmentID)}`, {
      method: 'DELETE',
    }),
  revokeAgentToken: (projectID: string, tokenID: string) =>
    request<AgentToken>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-tokens/${encodeURIComponent(tokenID)}`, {
      method: 'DELETE',
    }),
  repositories: (projectID: string) =>
    request<Repository[]>(`/api/v1/repositories?project_id=${encodeURIComponent(projectID)}`),
  createRepository: (payload: { project_id: string; name: string; ssh_url: string; default_branch: string }) =>
    request<Repository>('/api/v1/repositories', { method: 'POST', body: JSON.stringify(payload) }),
  verifyRepository: (repositoryID: string, hostKeyFingerprint: string) =>
    request<Repository>(`/api/v1/repositories/${encodeURIComponent(repositoryID)}/verify`, {
      method: 'POST',
      body: JSON.stringify({ host_key_fingerprint: hostKeyFingerprint }),
    }),
  experiments: (projectID: string, states: string[] = []) => {
    const params = new URLSearchParams({ project_id: projectID, limit: '100' })
    states.forEach((state) => params.append('state', state))
    return request<Experiment[]>(`/api/v1/experiments?${params}`)
  },
  experiment: (projectID: string, experimentID: string) =>
    request<Experiment>(
      `/api/v1/experiments/${encodeURIComponent(experimentID)}?project_id=${encodeURIComponent(projectID)}`,
    ),
  attempts: (projectID: string, experimentID: string) =>
    request<Attempt[]>(
      `/api/v1/experiments/${encodeURIComponent(experimentID)}/attempts?project_id=${encodeURIComponent(projectID)}`,
    ),
  operations: (projectID: string) =>
    request<OperationsFeed>(`/api/v1/projects/${encodeURIComponent(projectID)}/operations?limit=50`),
  cost: (projectID: string) => request<Cost>(`/api/v1/projects/${encodeURIComponent(projectID)}/cost`),
  diagnosticOptions: (projectID: string) =>
    request<DiagnosticOptions>(`/api/v1/projects/${encodeURIComponent(projectID)}/diagnostics/options`),
  diagnosticPreflight: (projectID: string, payload: DiagnosticInput) =>
    request<DiagnosticPreflight>(`/api/v1/projects/${encodeURIComponent(projectID)}/diagnostics/preflight`, {
      method: 'POST', body: JSON.stringify(payload),
    }),
  submitDiagnostic: (projectID: string, payload: DiagnosticInput & { idempotency_key: string; confirmation_digest: string; confirmed: boolean }) =>
    request<{ run: DiagnosticRun; idempotent: boolean }>(`/api/v1/projects/${encodeURIComponent(projectID)}/diagnostics`, {
      method: 'POST', body: JSON.stringify(payload),
    }),
  diagnostics: (projectID: string) =>
    request<{ runs: DiagnosticRunSummary[] }>(`/api/v1/projects/${encodeURIComponent(projectID)}/diagnostics?limit=50`),
  diagnostic: (projectID: string, runID: string) =>
    request<DiagnosticRun>(`/api/v1/projects/${encodeURIComponent(projectID)}/diagnostics/${encodeURIComponent(runID)}`),
  cancelDiagnostic: (projectID: string, runID: string) =>
    request<DiagnosticRun>(`/api/v1/projects/${encodeURIComponent(projectID)}/diagnostics/${encodeURIComponent(runID)}/cancel`, { method: 'POST' }),
  finance: (period: string, projectID = '') => {
    const params = new URLSearchParams({ period })
    if (projectID) params.set('project_id', projectID)
    return request<FinanceDashboard>(`/api/v1/finance?${params}`)
  },
  adjustBudget: (projectID: string, payload: {
    direction: 'credit' | 'debit'; amount_milli: number; reason: string; idempotency_key: string;
  }) => request<BudgetAdjustmentResult>(`/api/v1/projects/${encodeURIComponent(projectID)}/budget-adjustments`, {
    method: 'POST', body: JSON.stringify(payload),
  }),
  provider: () => request<ProviderSummary>('/api/v1/provider'),
  queryProvider: () => request<ProviderResources>('/api/v1/provider/query', { method: 'POST' }),
  configureProvider: (payload: { name: string; base_url: string; token: string }) =>
    request<{ provider: ProviderSummary; resources: ProviderResources }>('/api/v1/provider', {
      method: 'PUT',
      body: JSON.stringify(payload),
    }),
  providerDeployment: (deploymentID: string) =>
    request<ProviderDeploymentDetails>(`/api/v1/provider/deployments/${encodeURIComponent(deploymentID)}`),
  runtimeStatus: () => request<RuntimeStatus>('/api/v1/runtime/status'),
  managedProviderResources: () => request<ManagedProviderResource[]>('/api/v1/provider/managed-resources'),
  stopManagedDeployment: (deploymentID: string) =>
    request<ManagedProviderResource>(`/api/v1/provider/deployments/${encodeURIComponent(deploymentID)}/stop`, { method: 'POST' }),
  emergencyStop: (confirmation: string) => request<{ requested: number; at: string }>('/api/v1/provider/emergency-stop', { method: 'POST', body: JSON.stringify({ confirmation }) }),
  notificationSetting: () => request<NotificationSetting>('/api/v1/notifications/settings'),
  configureNotifications: (payload: {
    enabled: boolean; host: string; port: number; tls_mode: 'starttls' | 'tls'; username: string;
    password: string; clear_password: boolean; from_address: string; recipients: string[];
  }) => request<NotificationSetting>('/api/v1/notifications/settings', { method: 'PUT', body: JSON.stringify(payload) }),
  testNotification: () => request<NotificationDelivery>('/api/v1/notifications/test', { method: 'POST' }),
  notifications: () => request<NotificationDelivery[]>('/api/v1/notifications?limit=100'),
}
