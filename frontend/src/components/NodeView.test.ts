import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import NodeView from './NodeView.vue'

function response(data: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => ({ data }) }
}

const claimedEnrollment = {
  id: 'enrollment-claimed', label: 'lab-gpu-01', status: 'claimed', pairing_code: 'ABCD-1234',
  expires_at: '2026-07-21T23:00:00Z', created_at: '2026-07-21T22:00:00Z', updated_at: '2026-07-21T22:01:00Z',
}

const node = {
  id: 'node-id', label: 'lab-gpu-02', token_prefix: 'gmn_test', status: 'active', observed_state: 'online',
  installation_id: 'installation-id', machine_fingerprint: 'fingerprint', hostname: 'gpu-workstation',
  operating_system: 'linux', architecture: 'amd64', agent_version: '0.9.0', protocol_version: 'v1',
  capabilities: { gpus: [{ uuid: 'gpu-uuid', name: 'NVIDIA GeForce RTX 3090', memory_bytes: 24 * 1024 ** 3 }] },
  storage: { available_bytes: 900 * 1024 ** 3 }, project_ids: ['project-id'],
  last_seen_at: '2026-07-21T22:10:00Z', created_at: '2026-07-21T22:00:00Z', updated_at: '2026-07-21T22:10:00Z',
}
const a4000Node = {
  ...node, id: 'a4000-node-id', label: 'usb-pc', hostname: 'sage-303203', agent_version: '0.14.1',
  capabilities: {
    cpu_count: 24, memory_bytes: 31 * 1024 ** 3,
    gpus: [{ uuid: 'a4000-gpu-uuid', name: 'NVIDIA RTX A4000', memory_bytes: 16 * 1024 ** 3 }],
    execution_modes: ['shell', 'argv'],
  },
}
const build = { name: 'Gemcp', version: '0.14.2', commit: 'b'.repeat(40), built_at: '2026-07-29T08:00:00Z' }
const activeAssignment = {
  id: 'assignment-id', node_id: node.id, node_label: node.label, project_id: 'project-id',
  experiment_id: 'experiment-id', attempt_id: 'attempt-id', attempt_number: 1, state: 'running', output_ref: 'managed://output',
  created_at: '2026-07-21T22:05:00Z', updated_at: '2026-07-21T22:10:00Z',
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('NodeView enrollment operations', () => {
  it('reveals a setup link once and verifies pairing approval explicitly', async () => {
    const copy = vi.fn()
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const path = String(input)
      const method = options?.method ?? 'GET'
      if (path === '/api/v1/nodes') return response({ nodes: [node, a4000Node], enrollments: [claimedEnrollment], assignments: [activeAssignment] })
      if (path === '/api/v1/projects/project-id/self-hosted-runtimes' && method === 'GET') return response({
        environments: [{ id: 'environment-id', name: 'local-3090', image: `registry.example/train@sha256:${'a'.repeat(64)}`, is_default: true }],
        resource_profiles: [{ id: 'profile-id', name: 'local-3090', gpu_names: ['NVIDIA GeForce RTX 3090'], cpu_limit: 8, memory_gb: 32, is_default: true }],
      })
      if (path === '/api/v1/node-enrollments' && method === 'POST') return response({
        enrollment: {
          id: 'enrollment-new', label: 'new-gpu', status: 'pending', expires_at: '2026-07-21T23:30:00Z',
          created_at: '2026-07-21T22:30:00Z', updated_at: '2026-07-21T22:30:00Z',
        },
        setup_url: 'https://gemcp.example/node/setup#code=one-time-secret',
        claim_url: 'https://gemcp.example/api/v1/node-enrollments/claim',
      }, 201)
      if (path === '/api/v1/node-enrollments/enrollment-claimed/approve' && method === 'POST') return response(node)
      throw new Error(`unexpected request ${method} ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('navigator', { language: 'en-GB', clipboard: { writeText: copy } })

    const wrapper = mount(NodeView, {
      props: { active: true, build, projects: [{
        id: 'project-id', name: 'Research', slug: 'research', timezone: 'UTC', status: 'active',
        monthly_budget_milli: 100000, max_experiment_milli: 10000, max_concurrency: 1,
        max_runtime_seconds: 3600, timeout_extension_seconds: 600, termination_grace_seconds: 30,
      }] },
    })
    await flushPromises()

    expect(wrapper.text()).toContain('gpu-workstation')
    expect(wrapper.text()).toContain('NVIDIA GeForce RTX 3090')
    expect(wrapper.text()).toContain('local-3090')
    expect(wrapper.text()).not.toContain('one-time-secret')
    expect(wrapper.text()).toContain('Authorized GPU capacity is not exposed to Agents yet')
    expect(wrapper.text()).toContain('usb-pc · NVIDIA RTX A4000')

    const configureButton = wrapper.findAll('button').find((button) => button.text() === 'Configure')
    await configureButton!.trigger('click')
    const runtimeDialog = wrapper.get('.runtime-dialog')
    expect((runtimeDialog.get('input[placeholder="local-3090"]').element as HTMLInputElement).value).toBe('local-a4000')
    expect((runtimeDialog.get('input[placeholder="NVIDIA GeForce RTX 3090"]').element as HTMLInputElement).value).toBe('NVIDIA RTX A4000')
    expect((runtimeDialog.get('input[type="number"][max="1024"]').element as HTMLInputElement).value).toBe('8')
    expect((runtimeDialog.get('input[type="number"][max="4096"]').element as HTMLInputElement).value).toBe('24')
    await runtimeDialog.get('button.close-button').trigger('click')

    await wrapper.get('button[aria-label="Upgrade instructions lab-gpu-02"]').trigger('click')
    const upgradeDialog = wrapper.get('.upgrade-dialog')
    expect(upgradeDialog.text()).toContain('v0.14.2')
    expect(upgradeDialog.text()).toContain('An active Assignment is attached to this Node')
    await upgradeDialog.get('button.primary-button').trigger('click')
    const instruction = String(copy.mock.calls[0][0])
    expect(instruction).toContain("TARGET_VERSION='0.14.2'")
    expect(instruction).toContain(`TARGET_COMMIT='${'b'.repeat(40)}'`)
    expect(instruction).toContain('deploy/upgrade-gemcp-node.sh')
    expect(instruction).toContain("GEMCP_NODE_STORAGE_ROOT='/var/lib/gemcp-node/storage'")
    expect(instruction).toContain('STOP: the Owner console currently shows an active Assignment')
    expect(instruction).not.toContain('gmn_test')
    expect(instruction).not.toContain('fingerprint')
    expect(instruction).not.toContain('gpu-uuid')
    expect(instruction).not.toContain('gpu-workstation')
    await upgradeDialog.get('button.close-button').trigger('click')

    const createButton = wrapper.findAll('button').find((button) => button.text().includes('Create enrollment'))
    await createButton!.trigger('click')
    await wrapper.get('input[placeholder="lab-gpu-01"]').setValue('new-gpu')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    expect(wrapper.text()).toContain('one-time-secret')
    await wrapper.get('button[aria-label="Copy setup link"]').trigger('click')
    expect(copy).toHaveBeenCalledWith('https://gemcp.example/node/setup?lang=en#code=one-time-secret')
    const chineseButton = wrapper.findAll('button').find((button) => button.text() === '中文')
    await chineseButton!.trigger('click')
    await wrapper.get('button[aria-label="Copy setup link"]').trigger('click')
    expect(copy).toHaveBeenLastCalledWith('https://gemcp.example/node/setup?lang=zh#code=one-time-secret')
    const doneButton = wrapper.findAll('button').find((button) => button.text() === 'Done')
    await doneButton!.trigger('click')
    expect(wrapper.text()).not.toContain('one-time-secret')

    await wrapper.get('button[aria-label="Approve enrollment"]').trigger('click')
    const pairingInput = wrapper.get('input[placeholder="XXXX-XXXX"]')
    expect((pairingInput.element as HTMLInputElement).value).toBe('')
    await pairingInput.setValue('abcd-1234')
    await wrapper.get('form').trigger('submit')
    await flushPromises()

    const approveCall = fetchMock.mock.calls.find(([input]) => String(input).endsWith('/enrollment-claimed/approve'))
    expect(JSON.parse(String(approveCall?.[1]?.body))).toEqual({ pairing_code: 'ABCD-1234', project_ids: ['project-id'] })
    wrapper.unmount()
  })
})
