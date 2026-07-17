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
