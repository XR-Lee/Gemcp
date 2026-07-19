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

export type AgentScope = 'read' | 'submit' | 'cancel'

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
  metrics?: Record<string, unknown>
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
  agentTokens: (projectID: string) =>
    request<AgentTokenList>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-tokens`),
  issueAgentToken: (projectID: string, payload: {
    label: string; scopes: AgentScope[]; expires_in_days?: number; never_expires: boolean;
  }) => request<AgentTokenIssue>(`/api/v1/projects/${encodeURIComponent(projectID)}/agent-tokens`, {
    method: 'POST', body: JSON.stringify(payload),
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
  cost: (projectID: string) => request<Cost>(`/api/v1/projects/${encodeURIComponent(projectID)}/cost`),
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
