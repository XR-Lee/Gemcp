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

  it('issues and revokes Agent tokens only through CSRF-protected project endpoints', async () => {
    const fetchMock = vi.fn()
      .mockResolvedValueOnce({
        ok: true,
        status: 201,
        json: async () => ({ data: { token: { id: 'token-id' }, agent_token: 'gmc_secret', mcp_config: {} } }),
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
    await api.revokeAgentToken('project/id', 'token/id')

    const [issuePath, issueOptions] = fetchMock.mock.calls[0]
    const issueHeaders = issueOptions.headers as Headers
    expect(issuePath).toBe('/api/v1/projects/project%2Fid/agent-tokens')
    expect(issueOptions.method).toBe('POST')
    expect(issueHeaders.get('X-CSRF-Token')).toBe('csrf-agent-token')
    expect(JSON.parse(String(issueOptions.body))).toEqual({
      label: 'third-party', scopes: ['read', 'submit'], expires_in_days: 90, never_expires: false,
    })

    const [revokePath, revokeOptions] = fetchMock.mock.calls[1]
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
})
