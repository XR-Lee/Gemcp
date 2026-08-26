import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useI18n } from '../i18n'
import ResearchView from './ResearchView.vue'

const workspace = {
  project_id: 'project-id',
  studies: [{
    id: 'study-1', name: 'objbg-scan', question: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy?',
    status: 'active', updated_at: '2026-07-28T18:05:00Z',
  }],
  study: {
    id: 'study-1', name: 'objbg-scan', question: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy?',
    status: 'active', updated_at: '2026-07-28T18:05:00Z',
    plan: {
      id: 'plan-1', status: 'active', goal: 'Establish a reproducible OBJ-BG baseline.',
      next_action: 'Record the current smoke-run accuracy as the first Graph result.',
      steps: [{ title: 'Link the existing smoke Experiment' }],
      created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:05:00Z',
    },
    nodes: [
      { id: 'node-question-1', kind: 'question', title: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy?', status: 'open', created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:00:00Z' },
      { id: 'node-result-1', kind: 'result', title: 'OBJ-BG smoke accuracy', summary: 'The existing smoke Experiment reached 86.4 overall accuracy.', status: 'succeeded', metric_name: 'overall_accuracy', metric_value: 86.4, experiment_id: 'experiment-1', experiment_state: 'succeeded', created_at: '2026-07-28T18:05:00Z', updated_at: '2026-07-28T18:05:00Z' },
    ],
    edges: [{ id: 'edge-1', from_id: 'node-question-1', to_id: 'node-result-1', relation: 'produced' }],
  },
  next_actions: [{
    kind: 'record_hypothesis', tool: 'update_research_workspace', study_id: 'study-1',
    from_node_id: 'node-question-1', title: 'Record a hypothesis',
    detail: 'A paid run must start from a hypothesis or plan node, not from the question alone.',
  }],
  generated_at: '2026-07-28T18:05:00Z',
}

const flowStubs = {
  VueFlow: { template: '<div class="vue-flow"><slot /></div>' },
  Background: true,
  MiniMap: true,
  Controls: true,
}

afterEach(() => {
  vi.unstubAllGlobals()
  useI18n().setLocale('en')
})

describe('ResearchView', () => {
  it('renders the study, next action, and Graph canvas', () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: { workspace, loading: false, selectedStudyId: 'study-1' },
      global: { stubs: flowStubs },
    })
    expect(wrapper.text()).toContain('objbg-scan')
    expect(wrapper.text()).toContain('Record the current smoke-run accuracy as the first Graph result.')
    expect(wrapper.text()).toContain('Research Graph')
    expect(wrapper.find('.graph-canvas').exists()).toBe(true)
    expect(wrapper.find('.graph-shell').exists()).toBe(true)
    expect(wrapper.find('.vue-flow').exists()).toBe(true)
    expect(wrapper.find('button[aria-label="Fullscreen graph"]').exists()).toBe(true)
    expect(wrapper.text()).toContain('evidence time')
    expect(wrapper.text()).toContain('OBJ-BG smoke accuracy')
    expect(wrapper.text()).toContain('Record a hypothesis')
    expect(wrapper.find('button[aria-label="Attach prompt"]').exists()).toBe(true)
    expect(wrapper.find('.research-mcp-banner').exists()).toBe(false)
    expect(wrapper.find('.agent-readiness').exists()).toBe(false)
  })

  it('shows compact Agent Readiness on the research home', () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace,
        loading: false,
        selectedStudyId: 'study-1',
        readiness: {
          project_id: 'project-id', project_name: 'Point Models', status: 'ready',
          summary: '1 Agent(s) and 1 ready compute target(s). Binding is Project-scoped, not exclusive to one node.',
          generated_at: '2026-08-24T18:00:00Z',
          agents: [{
            id: 'agent-1', label: 'train-agent', prefix: 'gmc_abcd123',
            scopes: ['read', 'submit'], status: 'active', last_used_at: '2026-08-24T17:58:00Z',
            can_read: true, can_submit: true, can_operate_nodes: false, bound_node_ids: [],
          }],
          compute: {
            ssh_cloud_enabled: true,
            ssh_cloud: [{
              id: 'ssh-1', label: 'cloud-4090', status: 'active', host: '203.0.113.10', user: 'ubuntu',
              gpus: [{ name: 'NVIDIA GeForce RTX 4090', memory_bytes: 1 }],
              ready: true, readiness: 'ready', blockers: [], bound_to_project: true, runtime_configured: true,
            }],
            self_hosted: [],
          },
          heartbeats: { agent_last_used_at: '2026-08-24T17:58:00Z', note: 'MCP last_used_at updates on every authenticated tool call.' },
          next_actions: [{ kind: 'copy_readiness', title: 'Copy readiness prompt', detail: 'Tell the Agent.' }],
          instructions: {
            inspect_tool: 'get_project_options', monitor_tool: 'get_experiment',
            heartbeat: 'MCP last_used_at updates on every authenticated tool call.',
            binding: 'Agents are Project-scoped.',
          },
        },
      },
      global: { stubs: flowStubs },
    })
    expect(wrapper.find('.agent-readiness').text()).toContain('Ready')
    expect(wrapper.text()).toContain('cloud-4090')
    expect(wrapper.findAll('button').some((button) => button.text().includes('Copy readiness'))).toBe(true)
  })

  it('reminds the Owner to bind an Agent over MCP after a Study is registered', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace: {
          ...workspace,
          study: {
            ...workspace.study,
            plan: undefined,
            repository: {
              id: 'repo-1', name: 'DynamicPointMamba',
              ssh_url: 'git@github.com:XR-Lee/DynamicPointMamba.git', default_branch: 'main', status: 'active',
            },
          },
        },
        loading: false,
        selectedStudyId: 'study-1',
        hasActiveAgent: false,
      },
      global: { stubs: flowStubs },
    })
    expect(wrapper.find('.research-mcp-banner').text()).toContain('Bind an Agent, then enable MCP in this repository directory')
    expect(wrapper.text()).toContain('DynamicPointMamba')
    expect(wrapper.text()).toContain('in that directory, not as a global MCP')
    await wrapper.get('.research-mcp-actions .primary-button').trigger('click')
    expect(wrapper.emitted('openAgents')).toEqual([[]])
  })

  it('shows a bound research repository on the study heading', () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace: {
          ...workspace,
          study: {
            ...workspace.study,
            repository: {
              id: 'repo-1', name: 'dynamic-point-mamba',
              ssh_url: 'git@github.com:research/dynamic-point-mamba.git', default_branch: 'main', status: 'active',
            },
          },
        },
        loading: false,
        selectedStudyId: 'study-1',
      },
      global: { stubs: flowStubs },
    })
    expect(wrapper.find('.research-repo').text()).toContain('dynamic-point-mamba')
    expect(wrapper.text()).toContain('git@github.com:research/dynamic-point-mamba.git')
  })

  it('copies an attach prompt that names the current repository and Graph contract', async () => {
    const writeText = vi.fn(async () => undefined)
    vi.stubGlobal('navigator', { language: 'en-GB', clipboard: { writeText } })
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace,
        loading: false,
        selectedStudyId: 'study-1',
        project: {
          id: 'project-id', name: 'Point Models', slug: 'point-models', status: 'active',
          monthly_budget_milli: 1, max_experiment_milli: 1, max_concurrency: 1,
          max_runtime_seconds: 1, timeout_extension_seconds: 1, termination_grace_seconds: 1, timezone: 'UTC',
        },
        repositories: [{
          id: 'repo-1', project_id: 'project-id', name: 'dynamic-point-mamba',
          ssh_url: 'git@github.com:research/dynamic-point-mamba.git', default_branch: 'main', status: 'active',
        }],
      },
      attachTo: document.body,
      global: { stubs: flowStubs },
    })
    await wrapper.get('button[aria-label="Attach prompt"]').trigger('click')
    await flushPromises()
    expect(document.body.textContent).toContain('reconstruct experimental branches, hypotheses, conclusions, and evidence')
    const prompt = document.body.querySelector('.attach-prompt') as HTMLTextAreaElement
    expect(prompt.value).toContain('git@github.com:research/dynamic-point-mamba.git')
    expect(prompt.value).toContain('get_next_actions')
    await document.body.querySelector<HTMLButtonElement>('.attach-prompt-form .primary-button')?.click()
    expect(writeText).toHaveBeenCalledWith(prompt.value)
  })
})
