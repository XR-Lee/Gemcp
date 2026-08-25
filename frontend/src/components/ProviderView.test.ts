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

function providerSnapshot(providerRecord = provider, wallet?: { assets: number; accumulate: number; voucher_balance: number }) {
  return {
    generated_at: '2026-07-17T02:00:00Z', provider: providerRecord,
    wallet,
    gpu_stock: [{ name: 'NVIDIA GeForce RTX 3090', idle: 2, total: 9 }],
    private_images: [], system_images: [], deployments: [], active_containers: [], cached_containers: [], truncated: [],
  }
}

const snapshot = providerSnapshot()

const publicProvider = {
  ...provider,
  name: 'AutoDL Public Cloud',
  base_url: 'https://api.autodl.com',
  backend: 'elastic',
}

const publicSnapshot = {
  ...providerSnapshot(publicProvider, { assets: 12_345, accumulate: 67_890, voucher_balance: 500 }),
  gpu_stock: [{ region: 'westDC2', name: 'RTX 4090', idle: 3, total: 8 }],
}

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe('ProviderView cache and refresh', () => {
  it('reuses a fresh snapshot across navigation and refreshes it after 60 seconds', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'https://gemcp.example.com')
      if (url.pathname === '/api/v1/provider') return response({ providers: [provider] })
      if (url.pathname === '/api/v1/provider/query') return response(snapshot)
      if (url.pathname === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${url.pathname}`)
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

  it.each([
    ['Private Cloud', provider, snapshot, 'https://private.autodl.com'],
    ['Public Cloud', publicProvider, publicSnapshot, 'https://api.autodl.com'],
  ])('rotates the %s token against its official API host', async (_label, providerRecord, providerResources, expectedBaseURL) => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const path = new URL(String(input), 'https://gemcp.example.com').pathname
      if (path === '/api/v1/provider' && options?.method === 'PUT') {
        return response({ provider: providerRecord, resources: providerResources })
      }
      if (path === '/api/v1/provider') return response({ providers: [providerRecord] })
      if (path === '/api/v1/provider/query') return response(providerResources)
      if (path === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()

    const rotateButton = wrapper.findAll('button').find((button) => button.text().includes('Rotate token'))
    expect(rotateButton).toBeDefined()
    await rotateButton!.trigger('click')
    await flushPromises()

    expect(wrapper.get('input[disabled]').element.getAttribute('value')).toBe(expectedBaseURL)
    await wrapper.get('input[type="password"]').setValue('provider-token-abcdefghijklmnopqrstuvwxyz')
    await wrapper.get('form.dialog-form').trigger('submit')
    await flushPromises()

    const rotationCall = fetchMock.mock.calls.find(([input, options]) => (
      String(input) === '/api/v1/provider' && (options as RequestInit | undefined)?.method === 'PUT'
    ))
    expect(rotationCall).toBeDefined()
    expect(JSON.parse(String((rotationCall![1] as RequestInit).body))).toMatchObject({
      name: providerRecord.name,
      base_url: expectedBaseURL,
      backend: providerRecord.backend,
      token: 'provider-token-abcdefghijklmnopqrstuvwxyz',
    })

    wrapper.unmount()
  })

  it('switches the credential target between the two official hosts', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const path = new URL(String(input), 'https://gemcp.example.com').pathname
      if (path === '/api/v1/provider' && options?.method === 'PUT') {
        return response({ provider: publicProvider, resources: publicSnapshot })
      }
      if (path === '/api/v1/provider') return response({ providers: [provider] })
      if (path === '/api/v1/provider/query') return response(snapshot)
      if (path === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()
    await wrapper.findAll('button').find((button) => button.text().includes('Rotate token'))!.trigger('click')
    await wrapper.get('select').setValue('public')

    expect(wrapper.get('input[disabled]').element.getAttribute('value')).toBe('https://api.autodl.com')
    expect((wrapper.get('input:not([disabled]):not([type="password"])').element as HTMLInputElement).value).toBe('AutoDL Public Cloud')

    await wrapper.get('input[type="password"]').setValue('provider-token-abcdefghijklmnopqrstuvwxyz')
    await wrapper.get('form.dialog-form').trigger('submit')
    await flushPromises()

    const rotationCall = fetchMock.mock.calls.find(([input, options]) => (
      String(input) === '/api/v1/provider' && (options as RequestInit | undefined)?.method === 'PUT'
    ))
    expect(JSON.parse(String((rotationCall![1] as RequestInit).body))).toMatchObject({
      name: 'AutoDL Public Cloud',
      base_url: 'https://api.autodl.com',
      backend: 'elastic',
    })
    wrapper.unmount()
  })

  it('shows wallet and regional inventory only for Public Cloud', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const path = new URL(String(input), 'https://gemcp.example.com').pathname
      if (path === '/api/v1/provider') return response({ providers: [publicProvider] })
      if (path === '/api/v1/provider/query') return response(publicSnapshot)
      if (path === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()

    expect(wrapper.text()).toContain('Wallet balance')
    expect(wrapper.text()).toContain('CNY 12.345')
    expect(wrapper.text()).toContain('Region')
    expect(wrapper.text()).toContain('westDC2')
    expect(wrapper.text()).not.toContain('Reusable cache')
    wrapper.unmount()
  })

  it('keeps Private Cloud bound while adding Public Cloud', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const url = new URL(String(input), 'https://gemcp.example.com')
      if (url.pathname === '/api/v1/provider' && options?.method === 'PUT') {
        return response({ provider: publicProvider, resources: publicSnapshot })
      }
      if (url.pathname === '/api/v1/provider') return response({ providers: [provider] })
      if (url.pathname === '/api/v1/provider/query') {
        return response(url.searchParams.get('backend') === 'elastic' ? publicSnapshot : snapshot)
      }
      if (url.pathname === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${url.pathname}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()

    expect(wrapper.text()).toContain('Add Public Cloud')
    expect(wrapper.text()).toContain('2 / 9')
    await wrapper.findAll('button').find((button) => button.text().includes('Add Public Cloud'))!.trigger('click')
    await flushPromises()
    expect(wrapper.get('[aria-label="Add Provider token"] h2').text()).toBe('Add Provider token')
    await wrapper.get('input[type="password"]').setValue('provider-token-abcdefghijklmnopqrstuvwxyz')
    await wrapper.get('form.dialog-form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('Private Cloud')
    expect(wrapper.text()).toContain('Public Cloud')
    expect(wrapper.text()).toContain('Wallet balance')
    expect(wrapper.text()).not.toContain('Add Public Cloud')
    wrapper.unmount()
  })

  it('queries the selected backend when both providers are bound', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL) => {
      const url = new URL(String(input), 'https://gemcp.example.com')
      if (url.pathname === '/api/v1/provider') return response({ providers: [provider, publicProvider] })
      if (url.pathname === '/api/v1/provider/query') {
        return response(url.searchParams.get('backend') === 'elastic' ? publicSnapshot : snapshot)
      }
      if (url.pathname === '/api/v1/provider/managed-resources') return response([])
      throw new Error(`unexpected request ${url.pathname}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(ProviderView, { props: { active: true } })
    await flushPromises()

    expect(wrapper.text()).toContain('2 / 9')
    expect(wrapper.text()).not.toContain('Add Public Cloud')
    await wrapper.findAll('button').find((button) => button.text() === 'Public Cloud')!.trigger('click')
    await flushPromises()

    const publicQuery = fetchMock.mock.calls.find(([input]) => String(input).includes('/api/v1/provider/query?backend=elastic'))
    expect(publicQuery).toBeDefined()
    expect(wrapper.text()).toContain('Wallet balance')
    expect(wrapper.text()).toContain('westDC2')
    wrapper.unmount()
  })
})
