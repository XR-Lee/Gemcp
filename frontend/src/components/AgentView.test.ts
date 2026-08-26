import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import AgentView from './AgentView.vue'

function response(data: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => ({ data }) }
}

const project = {
  id: 'project-id', name: 'Point Models', slug: 'point-models', timezone: 'UTC', status: 'active' as const,
  monthly_budget_milli: 100000, max_experiment_milli: 10000, max_concurrency: 1,
  max_runtime_seconds: 3600, timeout_extension_seconds: 600, termination_grace_seconds: 30,
}

afterEach(() => {
  vi.unstubAllGlobals()
})

describe('AgentView Project registration', () => {
  it('registers an Agent without loading or binding Cloud SSH compute', async () => {
    const copy = vi.fn()
    vi.stubGlobal('navigator', { language: 'en-GB', clipboard: { writeText: copy } })
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const path = String(input)
      const method = options?.method ?? 'GET'
      if (path === `/api/v1/projects/${project.id}/agent-readiness` && method === 'GET') {
        return response({
          project_id: project.id, project_name: project.name, status: 'waiting_compute',
          summary: 'An Agent is bound to the Project, but no Self-hosted or Cloud SSH node is visible yet.',
          generated_at: '2026-08-24T18:00:00Z',
          agents: [{
            id: 'agent-existing', label: 'existing-agent', prefix: 'gmc_existing', scopes: ['read'], status: 'active',
            can_read: true, can_submit: false, can_operate_nodes: false, bound_node_ids: [],
          }],
          compute: { ssh_cloud_enabled: true, ssh_cloud: [], self_hosted: [] },
          heartbeats: { note: 'MCP last_used_at updates on every authenticated tool call.' },
          next_actions: [{ kind: 'copy_readiness', title: 'Copy readiness prompt', detail: 'Tell the Agent.' }],
          instructions: { inspect_tool: 'get_project_options', monitor_tool: 'get_experiment', heartbeat: 'Monitor with get_experiment.', binding: 'Agents are Project-scoped.' },
        })
      }
      if (path === `/api/v1/projects/${project.id}/agent-tokens` && method === 'GET') {
        return response({
          tokens: [],
          enrollments: [],
          mcp_url: 'http://127.0.0.1:18080/mcp',
          config_file_name: 'gemcp-point-models-mcp.json',
          config_template: { mcpServers: { gemcp: { type: 'http', url: 'http://127.0.0.1:18080/mcp', headers: { Authorization: 'Bearer ${GEMCP_AGENT_TOKEN}' } } } },
        })
      }
      if (path === `/api/v1/projects/${project.id}/agent-enrollments` && method === 'POST') {
        expect(JSON.parse(String(options?.body))).toMatchObject({ setup_expires_in_minutes: 240 })
        return response({
          enrollment: {
            id: 'enroll-1', project_id: project.id, label: 'train-agent',
            scopes: ['read', 'submit', 'cancel'], status: 'pending',
            expires_at: '2026-08-25T01:00:00Z', token_expires_in_days: 90,
            created_at: '2026-08-25T00:30:00Z', updated_at: '2026-08-25T00:30:00Z',
          },
          setup_url: 'http://127.0.0.1:18080/agent/setup#code=one-time',
        }, 201)
      }
      throw new Error(`unexpected request ${method} ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    const wrapper = mount(AgentView, {
      props: { active: true, project },
    })
    await flushPromises()
    expect(wrapper.find('.agent-readiness').text()).toContain('Agent registered')
    expect(wrapper.find('.agent-readiness').text()).toContain('independent of compute')
    await wrapper.findAll('button').find((button) => button.text().includes('Register Agent'))!.trigger('click')
    expect(wrapper.text()).toContain('Project-scoped enrollment')
    expect(wrapper.text()).toContain('does not bind a GPU, node, or Provider')
    expect(wrapper.get('input[type="checkbox"]').element).toBeTruthy()
    const operate = wrapper.findAll('input[type="checkbox"]').find((input) => input.element.parentElement?.textContent?.includes('Operate nodes'))
    expect((operate!.element as HTMLInputElement).checked).toBe(false)
    const linkValidity = wrapper.findAll('label').find((label) => label.text().includes('Link validity'))!.get('select')
    expect((linkValidity.element as HTMLSelectElement).value).toBe('240')
    await wrapper.get('input[placeholder="research-agent"]').setValue('train-agent')
    await wrapper.get('form.agent-token-form').trigger('submit')
    await flushPromises()
    expect(wrapper.text()).toContain('Give the Agent this Project setup prompt')
    expect(wrapper.text()).toContain('http://127.0.0.1:18080/agent/setup#code=one-time')
    const prompt = wrapper.get('textarea.attach-prompt').element as HTMLTextAreaElement
    expect(prompt.value).toContain('http://127.0.0.1:18080/agent/setup#code=one-time')
    expect(prompt.value).toContain('does not bind a GPU, node, or Provider')
    expect(prompt.value).toContain('Project budget is a spending limit, not per-run approval')
    expect(prompt.value).toContain('wait for the Owner to explicitly confirm that digest')
    expect(prompt.value).not.toContain('register_ssh_cloud_node')
    expect(prompt.value).not.toContain('BEGIN OPENSSH')
    await wrapper.findAll('button').find((button) => button.text().includes('Copy Project setup prompt'))!.trigger('click')
    expect(copy).toHaveBeenCalled()
    const copied = String(copy.mock.calls[0]?.[0])
    expect(copied).toContain('http://127.0.0.1:18080/agent/setup#code=one-time')
    expect(copied).toContain('does not bind a GPU, node, or Provider')
    expect(copied).not.toContain('register_ssh_cloud_node')
    expect(fetchMock.mock.calls.some(([input]) => String(input) === '/api/v1/ssh-cloud-nodes')).toBe(false)
    wrapper.unmount()
  })
})
