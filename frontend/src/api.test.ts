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
