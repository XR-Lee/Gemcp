import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import App from './App.vue'

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('App', () => {
  it('shows the build version when the control plane responds', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: true,
      json: async () => ({
        data: { name: 'Gemcp', version: '0.1.0', commit: 'abc123', built_at: 'now' }
      })
    }))

    const wrapper = mount(App)
    await flushPromises()

    expect(wrapper.text()).toContain('Control plane online')
    expect(wrapper.text()).toContain('0.1.0')
    expect(wrapper.text()).toContain('abc123')
  })
})
