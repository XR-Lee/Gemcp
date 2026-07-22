import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Project } from '../api'
import DiagnosticsView from './DiagnosticsView.vue'

function response(data: unknown) {
  return { ok: true, status: 200, json: async () => ({ data }) }
}

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function project(id: string, name: string): Project {
  return {
    id, name, slug: id, status: 'active', monthly_budget_milli: 100000, max_experiment_milli: 10000,
    max_concurrency: 1, max_runtime_seconds: 3600, timeout_extension_seconds: 600,
    termination_grace_seconds: 30, timezone: 'UTC',
  }
}

function options(id: string, name: string) {
  return {
    project_id: id, repositories: [{ id: `${id}-repository`, name, default_branch: 'main' }],
    environments: [{ id: `${id}-environment`, name: `${name}-environment`, backend: 'autodl_private', image_uuid: 'image' }],
    resource_profiles: [{ id: `${id}-profile`, name: `${name}-profile`, backend: 'autodl_private', gpu_names: ['RTX 3090'], gpu_num: 1, price_to_milli: 1000 }],
    suites: [{ id: 'gpu_connectivity', runtime_seconds: 180, requires_pytorch: false, checks_cuda_compute: false }],
    generated_at: '2026-07-23T00:00:00Z',
  }
}

afterEach(() => vi.unstubAllGlobals())

describe('DiagnosticsView request ordering', () => {
  it('does not let an old Project response or stale preflight overwrite the active Project', async () => {
    const oldOptions = deferred<ReturnType<typeof response>>()
    const oldRuns = deferred<ReturnType<typeof response>>()
    const preflight = deferred<ReturnType<typeof response>>()
    const fetchMock = vi.fn((input: RequestInfo | URL, request?: RequestInit) => {
      const path = String(input)
      if (path.includes('/projects/project-a/diagnostics/options')) return oldOptions.promise
      if (path.includes('/projects/project-a/diagnostics?')) return oldRuns.promise
      if (path.includes('/projects/project-b/diagnostics/options')) return Promise.resolve(response(options('project-b', 'Repository B')))
      if (path.includes('/projects/project-b/diagnostics?') && (request?.method ?? 'GET') === 'GET') return Promise.resolve(response({ runs: [] }))
      if (path.endsWith('/projects/project-b/diagnostics/preflight')) return preflight.promise
      throw new Error(`unexpected request ${(request?.method ?? 'GET')} ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(DiagnosticsView, { props: { active: true, project: project('project-a', 'Project A') } })
    await Promise.resolve()
    await wrapper.setProps({ project: project('project-b', 'Project B') })
    await flushPromises()
    expect(wrapper.text()).toContain('Repository B')

    oldOptions.resolve(response(options('project-a', 'Repository A')))
    oldRuns.resolve(response({ runs: [] }))
    await flushPromises()
    expect(wrapper.text()).toContain('Repository B')
    expect(wrapper.text()).not.toContain('Repository A')

    await wrapper.get('input[placeholder="40 or 64 hexadecimal characters"]').setValue('a'.repeat(40))
    await wrapper.get('form').trigger('submit')
    await wrapper.get('input[placeholder="40 or 64 hexadecimal characters"]').setValue('b'.repeat(40))
    preflight.resolve(response({
      eligible: true, requires_confirmation: true, generated_at: '2026-07-23T00:00:00Z', checks: [],
      proposal: {
        backend: 'autodl_private', suite: 'gpu_connectivity', repository_id: 'project-b-repository',
        environment_id: 'project-b-environment', resource_profile_id: 'project-b-profile', commit_sha: 'a'.repeat(40),
        command: 'true', runtime_seconds: 180, termination_grace_seconds: 30, reserved_cost_milli: 234,
        billable: true, gpu_models: ['RTX 3090'], gpu_num: 1, image_uuid: 'image',
      },
    }))
    await flushPromises()
    expect(wrapper.text()).not.toContain('Preflight passed')
    wrapper.unmount()
  })
})
