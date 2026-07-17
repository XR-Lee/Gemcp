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
})
