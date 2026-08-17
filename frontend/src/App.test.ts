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
        scheduler_enabled: false, self_hosted_enabled: false, global_concurrency: 1, public_url_configured: false,
        scheduler_healthy: true, watchdog_healthy: false, notification_worker_healthy: false,
        generated_at: '2026-07-17T00:00:00Z',
      } })
      if (path.includes('/api/v1/repositories?')) return response({ data: [] })
      if (path.includes('/api/v1/experiments?')) return response({ data: [] })
      if (path.includes('/api/v1/projects/project-id/operations?')) return response({ data: { activities: [], proposals: [], generated_at: '2026-07-17T00:00:00Z' } })
      if (path.endsWith('/api/v1/projects/project-id/research')) return response({ data: { project_id: project.id, studies: [], generated_at: '2026-07-17T00:00:00Z' } })
      throw new Error(`unexpected request ${path}`)
    }))

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('Research workbench')
    expect(wrapper.text()).toContain('Start from a research question')
    expect(wrapper.text()).toContain('owner@example.com')
    expect(wrapper.text()).toContain('abc123')

    await wrapper.get('button[aria-label="Switch to Chinese"]').trigger('click')
    expect(wrapper.text()).toContain('研究工作台')
    expect(wrapper.text()).toContain('从研究问题开始')
    expect(wrapper.get('button[aria-label="切换到英文"]').text()).toContain('EN')
    expect(document.documentElement.lang).toBe('zh-CN')
    expect(window.localStorage.getItem('gemcp.locale')).toBe('zh')
  })
})
