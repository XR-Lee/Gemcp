import { afterEach, describe, expect, it, vi } from 'vitest'
import { api, setCSRFToken } from './api'

afterEach(() => {
  setCSRFToken('')
  vi.unstubAllGlobals()
})

describe('API security headers', () => {
  it('sends the bootstrap token only in the setup header', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: { agent_token: 'test' } }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await api.setup({ organization_name: 'Test' }, 'bootstrap-secret-value')

    const [, options] = fetchMock.mock.calls[0]
    const headers = options.headers as Headers
    expect(headers.get('X-Gemcp-Bootstrap-Token')).toBe('bootstrap-secret-value')
    expect(String(options.body)).not.toContain('bootstrap-secret-value')
  })

  it('sends Provider rotation only in a CSRF-protected same-origin body', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 200,
      json: async () => ({ data: { provider: { id: 'provider-id' }, resources: {} } }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-provider-token')

    await api.configureProvider({
      name: 'AutoDL Private Cloud',
      base_url: 'https://private.autodl.com',
      token: 'private-provider-secret',
    })

    const [path, options] = fetchMock.mock.calls[0]
    const headers = options.headers as Headers
    expect(path).toBe('/api/v1/provider')
    expect(options.method).toBe('PUT')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-provider-token')
    expect(headers.get('Authorization')).toBeNull()
    expect(String(path)).not.toContain('private-provider-secret')
    expect(JSON.parse(String(options.body)).token).toBe('private-provider-secret')
  })

  it('sends emergency stop through a CSRF-protected Owner endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 202,
      json: async () => ({ data: { requested: 1, at: '2026-07-17T00:00:00Z' } }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-emergency-token')

    await api.emergencyStop('STOP')

    const [path, options] = fetchMock.mock.calls[0]
    const headers = options.headers as Headers
    expect(path).toBe('/api/v1/provider/emergency-stop')
    expect(options.method).toBe('POST')
    expect(headers.get('X-CSRF-Token')).toBe('csrf-emergency-token')
    expect(JSON.parse(String(options.body))).toEqual({ confirmation: 'STOP' })
  })

  it('issues, updates, and revokes Agent tokens only through CSRF-protected project endpoints', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => ({ data: { token: { id: 'token-id' }, agent_token: 'gmc_secret', mcp_config: {} } }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { id: 'token-id', status: 'active', scopes: ['read', 'submit', 'configure'] } }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { id: 'token-id', status: 'revoked' } }),
      })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-agent-token')

    await api.issueAgentToken('project/id', {
      label: 'third-party', scopes: ['read', 'submit'], expires_in_days: 90, never_expires: false,
    })
    await api.updateAgentTokenScopes('project/id', 'token/id', ['read', 'submit', 'configure'])
    await api.revokeAgentToken('project/id', 'token/id')

    const [issuePath, issueOptions] = fetchMock.mock.calls[0]
    const issueHeaders = issueOptions.headers as Headers
    expect(issuePath).toBe('/api/v1/projects/project%2Fid/agent-tokens')
    expect(issueOptions.method).toBe('POST')
    expect(issueHeaders.get('X-CSRF-Token')).toBe('csrf-agent-token')
    expect(JSON.parse(String(issueOptions.body))).toEqual({
      label: 'third-party', scopes: ['read', 'submit'], expires_in_days: 90, never_expires: false,
    })

    const [scopePath, scopeOptions] = fetchMock.mock.calls[1]
    expect(scopePath).toBe('/api/v1/projects/project%2Fid/agent-tokens/token%2Fid')
    expect(scopeOptions.method).toBe('PATCH')
    expect(JSON.parse(String(scopeOptions.body))).toEqual({ scopes: ['read', 'submit', 'configure'] })

    const [revokePath, revokeOptions] = fetchMock.mock.calls[2]
    const revokeHeaders = revokeOptions.headers as Headers
    expect(revokePath).toBe('/api/v1/projects/project%2Fid/agent-tokens/token%2Fid')
    expect(revokeOptions.method).toBe('DELETE')
    expect(revokeHeaders.get('X-CSRF-Token')).toBe('csrf-agent-token')
  })

  it('creates and revokes one-time Agent setup links through project endpoints', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => ({ data: { enrollment: { id: 'enrollment-id' }, setup_url: 'https://gemcp.example/agent/setup#code=test' } }),
      })
      .mockResolvedValueOnce({
        ok: true,
        status: 200,
        json: async () => ({ data: { id: 'enrollment-id', status: 'revoked' } }),
      })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-setup-link')

    await api.issueAgentEnrollment('project/id', {
      label: 'pi-agent', scopes: ['read', 'submit', 'cancel'], expires_in_days: 30,
      never_expires: false, setup_expires_in_minutes: 15,
    })
    await api.revokeAgentEnrollment('project/id', 'enrollment/id')

    const [issuePath, issueOptions] = fetchMock.mock.calls[0]
    expect(issuePath).toBe('/api/v1/projects/project%2Fid/agent-enrollments')
    expect(issueOptions.method).toBe('POST')
    expect((issueOptions.headers as Headers).get('X-CSRF-Token')).toBe('csrf-setup-link')
    expect(JSON.parse(String(issueOptions.body))).toEqual({
      label: 'pi-agent', scopes: ['read', 'submit', 'cancel'], expires_in_days: 30,
      never_expires: false, setup_expires_in_minutes: 15,
    })

    const [revokePath, revokeOptions] = fetchMock.mock.calls[1]
    expect(revokePath).toBe('/api/v1/projects/project%2Fid/agent-enrollments/enrollment%2Fid')
    expect(revokeOptions.method).toBe('DELETE')
    expect((revokeOptions.headers as Headers).get('X-CSRF-Token')).toBe('csrf-setup-link')
  })

  it('protects Self-hosted node enrollment mutations with CSRF', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true, status: 201,
        json: async () => ({ data: { enrollment: { id: 'enrollment/id' }, setup_url: 'https://gemcp.example/node/setup#code=secret' } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 200,
        json: async () => ({ data: { id: 'node-id', status: 'active' } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 200,
        json: async () => ({ data: { id: 'enrollment/id', status: 'revoked' } }),
      })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-node-enrollment')

    await api.issueNodeEnrollment({ label: 'lab-gpu-01', setup_expires_in_minutes: 30 })
    await api.approveNodeEnrollment('enrollment/id', { pairing_code: 'ABCD-1234', project_ids: ['project-id'] })
    await api.revokeNodeEnrollment('enrollment/id')

    const [issuePath, issueOptions] = fetchMock.mock.calls[0]
    expect(issuePath).toBe('/api/v1/node-enrollments')
    expect(issueOptions.method).toBe('POST')
    expect((issueOptions.headers as Headers).get('X-CSRF-Token')).toBe('csrf-node-enrollment')
    expect(JSON.parse(String(issueOptions.body))).toEqual({ label: 'lab-gpu-01', setup_expires_in_minutes: 30 })

    const [approvePath, approveOptions] = fetchMock.mock.calls[1]
    expect(approvePath).toBe('/api/v1/node-enrollments/enrollment%2Fid/approve')
    expect(approveOptions.method).toBe('POST')
    expect((approveOptions.headers as Headers).get('X-CSRF-Token')).toBe('csrf-node-enrollment')
    expect(JSON.parse(String(approveOptions.body))).toEqual({ pairing_code: 'ABCD-1234', project_ids: ['project-id'] })

    const [revokePath, revokeOptions] = fetchMock.mock.calls[2]
    expect(revokePath).toBe('/api/v1/node-enrollments/enrollment%2Fid')
    expect(revokeOptions.method).toBe('DELETE')
    expect((revokeOptions.headers as Headers).get('X-CSRF-Token')).toBe('csrf-node-enrollment')
  })

  it('creates Self-hosted runtime configuration through a CSRF-protected Project endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 201,
      json: async () => ({ data: { environment: { id: 'environment-id' }, resource_profile: { id: 'profile-id' } } }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-runtime')
    const payload = {
      name: 'local-3090', image: `registry.example/train@sha256:${'a'.repeat(64)}`,
      gpu_names: ['NVIDIA GeForce RTX 3090'], cpu_limit: 8, memory_gb: 32, make_default: true,
    }

    await api.createSelfHostedRuntime('project/id', payload)

    const [path, options] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/v1/projects/project%2Fid/self-hosted-runtimes')
    expect(options.method).toBe('POST')
    expect((options.headers as Headers).get('X-CSRF-Token')).toBe('csrf-runtime')
    expect(JSON.parse(String(options.body))).toEqual(payload)
  })

  it('updates the research workspace only through a CSRF-protected project endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200, json: async () => ({ data: { project_id: 'project/id', studies: [], generated_at: '2026-07-17T00:00:00Z' } }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-research')

    await api.updateResearch('project/id', {
      study: { name: 'objbg-scan', question: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?' },
    })

    const [path, options] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/v1/projects/project%2Fid/research')
    expect(options.method).toBe('PUT')
    expect((options.headers as Headers).get('X-CSRF-Token')).toBe('csrf-research')
    expect(JSON.parse(String(options.body))).toEqual({
      study: { name: 'objbg-scan', question: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy without extra GPU hours?' },
    })
  })

  it('reads the experiment catalog through the Owner project endpoint', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200, json: async () => ({ data: { project_id: 'project/id', repositories: [], generated_at: '2026-07-17T00:00:00Z' } }),
    })
    vi.stubGlobal('fetch', fetchMock)

    await api.experimentCatalog('project/id')

    const [path] = fetchMock.mock.calls[0]
    expect(path).toBe('/api/v1/projects/project%2Fid/experiment-catalog')
  })

  it('adds the current CSRF token to state-changing Owner requests', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true,
      status: 201,
      json: async () => ({ data: { id: 'repository-id' } }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-test-token')

    await api.createRepository({ project_id: 'project-id', name: 'repo', ssh_url: 'git@github.com:owner/repo.git', default_branch: 'main' })

    const [, options] = fetchMock.mock.calls[0]
    const headers = options.headers as Headers
    expect(headers.get('X-CSRF-Token')).toBe('csrf-test-token')
  })

  it('protects Project policy, AutoDL dataset bindings, and Owner proposal confirmation with CSRF', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true, status: 200, json: async () => ({ data: { id: 'project/id', max_runtime_seconds: 57600 } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 201, json: async () => ({ data: { id: 'binding/id', status: 'active' } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 200, json: async () => ({ data: { id: 'binding/id', status: 'disabled' } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 201, json: async () => ({ data: { id: 'environment/id', status: 'approved' } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 200, json: async () => ({ data: { id: 'environment/id', status: 'disabled' } }),
      })
      .mockResolvedValueOnce({
        ok: true, status: 200, json: async () => ({ data: { experiment: { id: 'experiment-id' }, idempotent: false } }),
      })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-paid-run')

    await api.updateProject('project/id', { max_runtime_seconds: 57600, max_experiment_milli: 40000 })
    await api.createDatasetBinding('project/id', {
      name: 'scanobjectnn-objbg', catalog: 'scanobjectnn-objbg', backend: 'autodl_elastic',
      canonical_root: '/root/autodl-fs/datasets/ScanObjectNN', required_markers: ['main_split/train.h5'],
      sources: [{ url: 'https://huggingface.co/datasets/example/resolve/main/train.h5', relative_path: 'main_split/train.h5' }],
    })
    await api.removeDatasetBinding('project/id', 'binding/id')
    await api.createEnvironment('project/id', { name: 'torch-train', backend: 'autodl_elastic', image_uuid: 'image-visible1234' })
    await api.removeEnvironment('project/id', 'environment/id')
    await api.submitPreparedProposal('project/id', 'proposal/id', {
      confirmation_digest: `sha256:${'ab'.repeat(32)}`, confirmed: true,
    })

    expect(fetchMock.mock.calls.map(([path, options]) => [path, options.method])).toEqual([
      ['/api/v1/projects/project%2Fid', 'PATCH'],
      ['/api/v1/projects/project%2Fid/dataset-bindings', 'POST'],
      ['/api/v1/projects/project%2Fid/dataset-bindings/binding%2Fid', 'DELETE'],
      ['/api/v1/projects/project%2Fid/environments', 'POST'],
      ['/api/v1/projects/project%2Fid/environments/environment%2Fid', 'DELETE'],
      ['/api/v1/projects/project%2Fid/experiment-proposals/proposal%2Fid/submit', 'POST'],
    ])
    for (const [, options] of fetchMock.mock.calls) {
      expect((options.headers as Headers).get('X-CSRF-Token')).toBe('csrf-paid-run')
    }
    expect(JSON.parse(String(fetchMock.mock.calls[0][1].body))).toEqual({
      max_runtime_seconds: 57600, max_experiment_milli: 40000,
    })
    expect(JSON.parse(String(fetchMock.mock.calls[5][1].body))).toEqual({
      confirmation_digest: `sha256:${'ab'.repeat(32)}`, confirmed: true,
    })
  })

  it('protects diagnostic preflight, submission and cancellation with CSRF', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200, json: async () => ({ data: {} }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-diagnostic')
    const proposal = {
      backend: 'autodl_private' as const, suite: 'gpu_connectivity' as const,
      repository_id: 'repository/id', environment_id: 'environment/id', resource_profile_id: 'profile/id',
      commit_sha: 'a'.repeat(40),
    }

    await api.diagnosticPreflight('project/id', proposal)
    await api.submitDiagnostic('project/id', {
      ...proposal, idempotency_key: 'diagnostic-test-key', confirmation_digest: `sha256:${'b'.repeat(64)}`, confirmed: true,
    })
    await api.cancelDiagnostic('project/id', 'run/id')

    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      '/api/v1/projects/project%2Fid/diagnostics/preflight',
      '/api/v1/projects/project%2Fid/diagnostics',
      '/api/v1/projects/project%2Fid/diagnostics/run%2Fid/cancel',
    ])
    for (const [, options] of fetchMock.mock.calls) {
      expect(options.method).toBe('POST')
      expect((options.headers as Headers).get('X-CSRF-Token')).toBe('csrf-diagnostic')
    }
    expect(JSON.parse(String(fetchMock.mock.calls[1][1].body))).toMatchObject({
      confirmed: true, idempotency_key: 'diagnostic-test-key', confirmation_digest: `sha256:${'b'.repeat(64)}`,
    })
  })

  it('requests and confirms image bakes through CSRF-protected Owner routes', async () => {
    const fetchMock = vi.fn().mockResolvedValue({
      ok: true, status: 200, json: async () => ({ data: {} }),
    })
    vi.stubGlobal('fetch', fetchMock)
    setCSRFToken('csrf-image-bake')

    await api.imageBakeOptions('project/id')
    await api.imageBakes('project/id')
    await api.requestImageBake('project/id', {
      name: 'torch-mamba', base_image_uuid: 'image-base12345', commit_sha: 'a'.repeat(40),
    })
    await api.confirmImageBake('project/id', 'bake/id', 'sha256:digest')
    await api.cancelImageBake('project/id', 'bake/id')

    expect(fetchMock.mock.calls.map(([path]) => path)).toEqual([
      '/api/v1/projects/project%2Fid/image-bakes/options',
      '/api/v1/projects/project%2Fid/image-bakes',
      '/api/v1/projects/project%2Fid/image-bakes',
      '/api/v1/projects/project%2Fid/image-bakes/bake%2Fid/confirm',
      '/api/v1/projects/project%2Fid/image-bakes/bake%2Fid/cancel',
    ])
    expect(fetchMock.mock.calls[2][1].method).toBe('POST')
    expect((fetchMock.mock.calls[2][1].headers as Headers).get('X-CSRF-Token')).toBe('csrf-image-bake')
    expect(JSON.parse(String(fetchMock.mock.calls[3][1].body))).toEqual({ confirmation_digest: 'sha256:digest' })
    expect((fetchMock.mock.calls[3][1].headers as Headers).get('X-CSRF-Token')).toBe('csrf-image-bake')
  })
})
