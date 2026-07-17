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

export type Cost = {
  period: string
  monthly_budget_milli: number
  reserved_milli: number
  charged_milli: number
  adjustments_milli: number
  committed_milli: number
  available_milli: number
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
  cost: (projectID: string) => request<Cost>(`/api/v1/projects/${encodeURIComponent(projectID)}/cost`),
}
