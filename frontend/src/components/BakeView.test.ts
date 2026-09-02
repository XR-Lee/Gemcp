import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ImageBake, Project } from '../api'
import BakeView from './BakeView.vue'

function response(data: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => ({ data }) }
}

function project(id: string): Project {
  return {
    id, name: 'Lab', slug: id, status: 'active', monthly_budget_milli: 100000, max_experiment_milli: 10000,
    max_concurrency: 1, max_runtime_seconds: 3600, timeout_extension_seconds: 600,
    termination_grace_seconds: 30, timezone: 'UTC',
  }
}

function requestedBake(): ImageBake {
  return {
    id: 'bake-1', project_id: 'project-b', repository_id: 'repo-1', name: 'torch-mamba', backend: 'autodl_pro',
    base_image_uuid: 'image-base12345', commit_sha: 'a'.repeat(40), recipe_path: 'requirements.gemcp.txt',
    status: 'requested', confirmation_digest: 'sha256:bake-digest', requested_by: 'token-1', requested_by_type: 'agent_token',
    proposal: {
      backend: 'autodl_pro', name: 'torch-mamba', base_image_uuid: 'image-base12345',
      repository_id: 'repo-1', commit_sha: 'a'.repeat(40), recipe_path: 'requirements.gemcp.txt',
    },
    estimated_cost_milli: 0, created_at: '2026-09-02T00:00:00Z', updated_at: '2026-09-02T00:00:00Z',
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('BakeView', () => {
  it('lists a requested bake and does not call confirm until the Owner confirms the digest', async () => {
    const bake = requestedBake()
    const fetchMock = vi.fn(async (input: RequestInfo | URL, request?: RequestInit) => {
      const path = String(input)
      const method = request?.method ?? 'GET'
      if (path.endsWith('/image-bakes/options')) {
        return response({
          project_id: 'project-b', backend: 'autodl_pro', default_recipe_path: 'requirements.gemcp.txt',
          repositories: [{ id: 'repo-1', name: 'source', default_branch: 'main' }],
          base_images: [{ uuid: 'image-base12345', name: 'torch' }], generated_at: '2026-09-02T00:00:00Z',
        })
      }
      if (path.endsWith('/image-bakes') && method === 'GET') return response({ bakes: [bake] })
      if (path.endsWith('/image-bakes/bake-1/confirm') && method === 'POST') {
        return response({ ...bake, status: 'finished', image_uuid: 'image-baked12345' })
      }
      throw new Error(`unexpected request ${method} ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(BakeView, { props: { active: true, project: project('project-b') } })
    await flushPromises()
    expect(wrapper.text()).toContain('torch-mamba')
    expect(wrapper.text()).toContain('requested')
    expect(wrapper.text()).toContain('fail-closed')
    expect(wrapper.text()).toContain('invents no image UUID')
    expect(fetchMock.mock.calls.some((call) => String(call[0]).includes('/confirm'))).toBe(false)

    await wrapper.get('tbody tr').trigger('click')
    await flushPromises()
    expect(wrapper.text()).toContain('sha256:bake-digest')
    const confirmButton = wrapper.findAll('button').find((button) => button.text().includes('Confirm and start Pro'))
    expect(confirmButton).toBeTruthy()
    expect((confirmButton!.element as HTMLButtonElement).disabled).toBe(true)

    await wrapper.get('input[type="checkbox"]').setValue(true)
    await confirmButton!.trigger('click')
    await flushPromises()
    const confirmCall = fetchMock.mock.calls.find((call) => String(call[0]).includes('/image-bakes/bake-1/confirm'))
    expect(confirmCall).toBeTruthy()
    expect(confirmCall?.[1]?.method).toBe('POST')
    expect(JSON.parse(String(confirmCall?.[1]?.body))).toEqual({ confirmation_digest: 'sha256:bake-digest' })
    wrapper.unmount()
  })
})
