import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'
import { useI18n } from './i18n'

function response(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => data,
  }
}

const build = { name: 'Gemcp', version: '0.7.0', commit: 'abc123', built_at: 'now' }

afterEach(() => {
  vi.unstubAllGlobals()
  useI18n().setLocale('en')
  window.localStorage.removeItem('gemcp.locale')
  document.cookie = 'gemcp_csrf=; Max-Age=0; path=/'
})

describe('App', () => {
  it('opens first-run setup when the database is not initialized', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/version')) return response({ data: build })
      if (path.endsWith('/api/v1/setup/status')) return response({ data: { initialized: false } })
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('Initial configuration')
    expect(wrapper.text()).toContain('Create the Owner')
    expect(wrapper.text()).toContain('Step 1 / 3')
  })

  it('shows Owner login when an initialized deployment has no session', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/version')) return response({ data: build })
      if (path.endsWith('/api/v1/setup/status')) return response({ data: { initialized: true } })
      if (path.endsWith('/api/v1/auth/me')) return response({ error: { code: 'UNAUTHENTICATED', message: 'authentication required' } }, 401)
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('Owner access')
    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.text()).toContain('0.7.0')
    expect(wrapper.find('input[type="password"]').exists()).toBe(true)
  })

  it('hides the Owner password field when the control plane skips it', async () => {
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/version')) return response({ data: { ...build, password_required: false } })
      if (path.endsWith('/api/v1/setup/status')) return response({ data: { initialized: true } })
      if (path.endsWith('/api/v1/auth/me')) return response({ error: { code: 'UNAUTHENTICATED', message: 'authentication required' } }, 401)
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('Sign in')
    expect(wrapper.find('input[type="password"]').exists()).toBe(false)
  })

  it('loads the operations console for an authenticated Owner', async () => {
    const project = {
      id: 'project-id', name: 'Research', slug: 'research', status: 'active', monthly_budget_milli: 100000,
      max_experiment_milli: 20000, max_concurrency: 1, max_runtime_seconds: 3600,
      timeout_extension_seconds: 3600, termination_grace_seconds: 60, timezone: 'Asia/Shanghai',
    }
    vi.stubGlobal('fetch', vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/version')) return response({ data: build })
      if (path.endsWith('/api/v1/setup/status')) return response({ data: { initialized: true } })
      if (path.endsWith('/api/v1/auth/me')) return response({ data: { user_id: 'user-id', tenant_id: 'tenant-id', email: 'owner@example.com', role: 'owner' } })
      if (path.endsWith('/api/v1/projects')) return response({ data: [project] })
      if (path.endsWith('/api/v1/runtime/status')) return response({ data: {
        scheduler_enabled: false, self_hosted_enabled: false, ssh_cloud_enabled: false, global_concurrency: 1, public_url_configured: false, public_url_https: false,
        scheduler_healthy: true, watchdog_healthy: false, notification_worker_healthy: false,
        generated_at: '2026-07-17T00:00:00Z',
      } })
      if (path.includes('/api/v1/repositories?')) return response({ data: [] })
      if (path.includes('/api/v1/experiments?')) return response({ data: [] })
      if (path.includes('/api/v1/projects/project-id/operations?')) return response({ data: { activities: [], proposals: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/research')) return response({ data: { project_id: project.id, studies: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/experiment-catalog')) return response({ data: { project_id: project.id, repositories: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/agent-tokens')) return response({ data: { tokens: [], enrollments: [], config_file_name: 'mcp.json' } })
      if (path.endsWith('/api/v1/projects/project-id/dataset-bindings')) return response({ data: [] })
      if (path.endsWith('/api/v1/projects/project-id/dataset-sources')) return response({ data: [] })
      if (path.endsWith('/api/v1/projects/project-id/environments')) return response({ data: [] })
      if (path.endsWith('/api/v1/projects/project-id/agent-readiness')) return response({ error: {
        code: 'READINESS_UNAVAILABLE', message: 'compute readiness is temporarily unavailable',
      } }, 503)
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('Research workbench')
    expect(wrapper.text()).toContain('Import from a research repository')
    expect(wrapper.text()).toContain('owner@example.com')
    expect(wrapper.text()).toContain('abc123')

    await wrapper.get('button[aria-label="Switch to Chinese"]').trigger('click')
    expect(wrapper.text()).toContain('研究工作台')
    expect(wrapper.text()).toContain('从已有研究仓库导入')
    expect(wrapper.get('button[aria-label="切换到英文"]').text()).toContain('EN')
    expect(document.documentElement.lang).toBe('zh-CN')
    expect(window.localStorage.getItem('gemcp.locale')).toBe('zh')
  })

  it('exposes the Lab image bake workspace next to Diagnostics', async () => {
    const project = {
      id: 'project-id', name: 'Research', slug: 'research', status: 'active', monthly_budget_milli: 100000,
      max_experiment_milli: 20000, max_concurrency: 1, max_runtime_seconds: 3600,
      timeout_extension_seconds: 3600, termination_grace_seconds: 60, timezone: 'Asia/Shanghai',
    }
    const bake = {
      id: 'bake-1', project_id: project.id, repository_id: 'repo-1', name: 'torch-mamba', backend: 'autodl_pro',
      base_image_uuid: 'image-base12345', commit_sha: 'a'.repeat(40), recipe_path: 'requirements.gemcp.txt',
      status: 'requested', confirmation_digest: 'sha256:bake-digest', requested_by: 'owner-id', requested_by_type: 'user',
      proposal: {
        backend: 'autodl_pro', name: 'torch-mamba', base_image_uuid: 'image-base12345',
        repository_id: 'repo-1', commit_sha: 'a'.repeat(40), recipe_path: 'requirements.gemcp.txt',
      },
      estimated_cost_milli: 0, created_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z',
    }
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = String(input)
      if (path.endsWith('/api/v1/version')) return response({ data: build })
      if (path.endsWith('/api/v1/setup/status')) return response({ data: { initialized: true } })
      if (path.endsWith('/api/v1/auth/me')) return response({ data: { user_id: 'user-id', tenant_id: 'tenant-id', email: 'owner@example.com', role: 'owner' } })
      if (path.endsWith('/api/v1/projects')) return response({ data: [project] })
      if (path.endsWith('/api/v1/runtime/status')) return response({ data: {
        scheduler_enabled: false, self_hosted_enabled: false, ssh_cloud_enabled: false, global_concurrency: 1, public_url_configured: false, public_url_https: false,
        scheduler_healthy: true, watchdog_healthy: false, notification_worker_healthy: false,
        generated_at: '2026-07-17T00:00:00Z',
      } })
      if (path.includes('/api/v1/repositories?')) return response({ data: [] })
      if (path.includes('/api/v1/experiments?')) return response({ data: [] })
      if (path.includes('/api/v1/projects/project-id/operations?')) return response({ data: { activities: [], proposals: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/research')) return response({ data: { project_id: project.id, studies: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/experiment-catalog')) return response({ data: { project_id: project.id, repositories: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/agent-tokens')) return response({ data: { tokens: [], enrollments: [], config_file_name: 'mcp.json' } })
      if (path.endsWith('/api/v1/projects/project-id/dataset-bindings')) return response({ data: [] })
      if (path.endsWith('/api/v1/projects/project-id/dataset-sources')) return response({ data: [] })
      if (path.endsWith('/api/v1/projects/project-id/environments')) return response({ data: [] })
      if (path.endsWith('/api/v1/projects/project-id/agent-readiness')) return response({ error: {
        code: 'READINESS_UNAVAILABLE', message: 'compute readiness is temporarily unavailable',
      } }, 503)
      if (path.endsWith('/api/v1/projects/project-id/image-bakes/options')) {
        return response({ data: {
          project_id: project.id, backend: 'autodl_pro', default_recipe_path: 'requirements.gemcp.txt',
          repositories: [], base_images: [], generated_at: '2026-09-02T00:00:00Z',
        } })
      }
      if (path.endsWith('/api/v1/projects/project-id/image-bakes')) return response({ data: { bakes: [bake] } })
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(App)
    await flushPromises()
    expect(wrapper.get('button[aria-label="Images"]').exists()).toBe(true)
    await wrapper.get('button[aria-label="Images"]').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('Image bake')
    expect(wrapper.text()).toContain('torch-mamba')
    expect(fetchMock.mock.calls.some((call) => String(call[0]).includes('/confirm'))).toBe(false)
  })
})
