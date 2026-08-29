import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import ProviderView from './ProviderView.vue'

function response(data: unknown, status = 200) {
  return {
    ok: status >= 200 && status < 300,
    status,
    json: async () => ({ data }),
  }
}

const provider = {
  id: 'provider-id', name: 'AutoDL Private Cloud', base_url: 'https://private.autodl.com', backend: 'private',
  status: 'active', credential_configured: true, last_validated_at: '2026-07-17T02:00:00Z',
  created_at: '2026-07-16T00:00:00Z', updated_at: '2026-07-17T02:00:00Z',
}

const snapshot = {
  generated_at: '2026-07-17T02:00:00Z', provider,
  gpu_stock: [{ name: 'NVIDIA GeForce RTX 3090', idle: 2, total: 9 }],
  private_images: [], system_images: [], deployments: [], active_containers: [], cached_containers: [], truncated: [],
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('ProviderView cache and refresh', () => {
  it('reuses a fresh snapshot across navigation and refreshes it after 60 seconds', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(String(input), 'https://gemcp.example.com').pathname
      if (path === '/api/v1/provider') return response(provider)
      if (path === '/api/v1/provider/query') return response(snapshot)
      if (path === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()

    const providerQueries = () => fetchMock.mock.calls.filter(([input]) => String(input).includes('/api/v1/provider/query')).length
    expect(providerQueries()).toBe(1)
    expect(wrapper.text()).toContain('2 / 9')
    expect(wrapper.text()).toContain('Snapshot / auto 60s')

    await wrapper.setProps({ active: false })
    await wrapper.setProps({ active: true })
    await flushPromises()
    expect(providerQueries()).toBe(1)
    expect(wrapper.text()).toContain('2 / 9')

    await vi.advanceTimersByTimeAsync(60_000)
    await flushPromises()
    expect(providerQueries()).toBe(2)
    expect(wrapper.text()).toContain('2 / 9')

    wrapper.unmount()
  })

  it('rotates a Public Cloud token against the configured official host', async () => {
    const publicProvider = {
      ...provider, name: 'AutoDL Public Cloud', base_url: 'https://api.autodl.com', backend: 'elastic',
    }
    const publicSnapshot = { ...snapshot, provider: publicProvider }
    let configureBody: Record<string, unknown> | undefined
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const path = new URL(String(input), 'https://gemcp.example.com').pathname
      if (path === '/api/v1/provider' && options?.method === 'PUT') {
        configureBody = JSON.parse(String(options.body)) as Record<string, unknown>
        return response({ provider: publicProvider, resources: publicSnapshot })
      }
      if (path === '/api/v1/provider') return response(publicProvider)
      if (path === '/api/v1/provider/query') return response(publicSnapshot)
      if (path === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()
    const rotate = wrapper.findAll('button').find((button) => button.text().includes('Rotate token'))
    expect(rotate).toBeDefined()
    await rotate!.trigger('click')
    const dialog = wrapper.get('[role="dialog"]')
    expect(dialog.get('input[disabled]').element.getAttribute('value')).toBe('https://api.autodl.com')
    await dialog.get('input[type="password"]').setValue('public-provider-token-abcdefghijklmnopqrstuvwxyz')
    await dialog.get('form').trigger('submit')
    await flushPromises()

    expect(configureBody).toMatchObject({
      name: 'AutoDL Public Cloud', base_url: 'https://api.autodl.com', token: 'public-provider-token-abcdefghijklmnopqrstuvwxyz',
    })
    wrapper.unmount()
  })
})
