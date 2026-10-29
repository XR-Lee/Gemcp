import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { ResearchEdge, ResearchNode } from '../api'
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
      { id: 'node-hypothesis-1', kind: 'hypothesis', title: 'Background noise caps accuracy', status: 'open', created_at: '2026-07-28T18:01:00Z', updated_at: '2026-07-28T18:01:00Z' },
      { id: 'node-result-1', kind: 'result', title: 'OBJ-BG smoke accuracy', summary: 'The existing smoke Experiment reached 86.4 overall accuracy.', status: 'succeeded', metric_name: 'overall_accuracy', metric_value: 86.4, experiment_id: 'experiment-1', experiment_state: 'succeeded', created_at: '2026-07-28T18:05:00Z', updated_at: '2026-07-28T18:05:00Z' },
    ],
    edges: [
      { id: 'edge-0', from_id: 'node-question-1', to_id: 'node-hypothesis-1', relation: 'leads_to' },
      { id: 'edge-1', from_id: 'node-hypothesis-1', to_id: 'node-result-1', relation: 'produced' },
    ],
    hypotheses: [{
      id: 'node-hypothesis-1', title: 'Background noise caps accuracy', status: 'open', branch: 'main',
      experiments: [{
        run_node_id: 'node-run-1', experiment_id: 'experiment-1', title: 'OBJ-BG smoke',
        state: 'succeeded', branch: 'main', commit_sha: '0123456789abcdef0123456789abcdef01234567',
        result_title: 'OBJ-BG smoke accuracy', highlight_title: 'Background noise still enters kNN',
      }],
    }],
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
  it('shows the measured metric, its source, and the verdict for each run', () => {
    useI18n().setLocale('en')
    const judged = structuredClone(workspace)
    judged.study.hypotheses[0].experiments = [
      {
        run_node_id: 'node-run-1', experiment_id: 'experiment-1', title: 'OBJ-BG smoke', state: 'succeeded',
        result_title: 'OBJ-BG smoke accuracy', highlight_title: 'OBJ-BG smoke accuracy',
        metric_name: 'overall_accuracy', metric_value: 0.8642000001, metric_source: 'experiment', verdict: 'supports',
      },
      {
        run_node_id: 'node-run-2', experiment_id: 'experiment-2', title: 'OBJ-BG rerun', state: 'failed',
        result_title: 'OBJ-BG rerun OOM', highlight_title: 'OBJ-BG rerun OOM', verdict: 'inconclusive',
      },
    ] as never
    const wrapper = mount(ResearchView, {
      props: { workspace: judged, loading: false, selectedStudyId: 'study-1' },
      global: { stubs: flowStubs },
    })
    const records = wrapper.get('[data-testid="hypothesis-records"]')
    expect(records.text()).toContain('overall_accuracy 0.8642 · measured')
    expect(records.text()).not.toContain('0.8642000001')
    expect(records.findAll('.hypothesis-verdict').map((item) => item.text())).toEqual(['Supports', 'Inconclusive'])
    expect(records.get('[data-testid="hypothesis-tally"]').text()).toBe('Supports 1 · Contradicts 0 · Inconclusive 1')
    // A highlight that repeats the result title is not shown twice.
    expect(records.text()).not.toContain('Highlight')
  })

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
    expect(wrapper.text()).toContain('Background noise caps accuracy')
    expect(wrapper.find('[data-testid="hypothesis-records"]').text()).toContain('main')
    expect(wrapper.find('[data-testid="hypothesis-records"]').text()).toContain('0123456789ab')
    expect(wrapper.find('[data-testid="hypothesis-records"]').text()).toContain('Background noise still enters kNN')
    expect(wrapper.text()).toContain('Record a hypothesis')
    expect(wrapper.text()).not.toContain('Close run')
    expect(wrapper.find('button[aria-label="Attach prompt"]').exists()).toBe(true)
    expect(wrapper.find('.research-mcp-banner').exists()).toBe(false)
    expect(wrapper.find('.agent-readiness').exists()).toBe(false)
  })

  it('recomputes the graph when the Owner picks a lineage axis', async () => {
    useI18n().setLocale('en')
    const dated = structuredClone(workspace)
    const datedNodes: ResearchNode[] = [
      { id: 'q', kind: 'question', title: 'Question', status: 'open', occurred_at: '2025-01-01T00:00:00Z', created_at: '2025-01-01T00:00:00Z', updated_at: '2025-01-01T00:00:00Z' },
      { id: 'h', kind: 'hypothesis', title: 'Hypothesis', status: 'open', occurred_at: '2025-06-01T12:00:00Z', created_at: '2025-06-01T12:00:00Z', updated_at: '2025-06-01T12:00:00Z' },
      { id: 'obs', kind: 'observation', title: 'Observation', status: 'open', occurred_at: '2026-03-01T08:00:00Z', created_at: '2026-03-01T08:00:00Z', updated_at: '2026-03-01T08:00:00Z' },
      { id: 'dec', kind: 'decision', title: 'Decision', status: 'open', occurred_at: '2026-03-01T14:00:00Z', created_at: '2026-03-01T14:00:00Z', updated_at: '2026-03-01T14:00:00Z' },
      { id: 'next', kind: 'hypothesis', title: 'Follow-up', status: 'open', occurred_at: '2026-03-01T20:00:00Z', created_at: '2026-03-01T20:00:00Z', updated_at: '2026-03-01T20:00:00Z' },
    ]
    dated.study.nodes = datedNodes as typeof dated.study.nodes
    const datedEdges: ResearchEdge[] = [
      { id: 'e1', from_id: 'q', to_id: 'h', relation: 'leads_to' },
      { id: 'e2', from_id: 'h', to_id: 'obs', relation: 'leads_to' },
      { id: 'e3', from_id: 'obs', to_id: 'dec', relation: 'leads_to' },
      { id: 'e4', from_id: 'dec', to_id: 'next', relation: 'leads_to' },
    ]
    dated.study.edges = datedEdges as typeof dated.study.edges
    const wrapper = mount(ResearchView, {
      props: { workspace: dated, loading: false, selectedStudyId: 'study-1' },
      global: { stubs: flowStubs },
    })
    expect(wrapper.get('[data-testid="graph-axis-time"]').attributes('aria-pressed')).toBe('true')
    await wrapper.get('[data-testid="graph-axis-lineage"]').trigger('click')
    expect(wrapper.get('[data-testid="graph-axis-lineage"]').attributes('aria-pressed')).toBe('true')
    expect(wrapper.get('[data-testid="graph-axis-time"]').attributes('aria-pressed')).toBe('false')
    await wrapper.get('[data-testid="graph-axis-time"]').trigger('click')
    expect(wrapper.get('[data-testid="graph-axis-time"]').attributes('aria-pressed')).toBe('true')
  })

  it('lets the Owner start close_run from the next-action card', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace: {
          ...workspace,
          next_actions: [{
            kind: 'close_run', tool: 'close_run', study_id: 'study-1',
            from_node_id: 'node-run-1', experiment_id: 'experiment-1',
            title: 'Close OBJ-BG smoke', detail: 'Write the result and a highlight observation.',
          }],
        },
        loading: false, selectedStudyId: 'study-1',
      },
      global: { stubs: flowStubs },
    })
    const button = wrapper.findAll('button').find((item) => item.text() === 'Close run')
    expect(button).toBeTruthy()
    await button!.trigger('click')
    expect(wrapper.emitted('closeRun')).toEqual([['experiment-1']])
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

  it('shows Original registration and Analysis for a registered DynamicPointMamba repository', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace,
        loading: false,
        selectedStudyId: 'study-1',
        repositories: [{
          id: 'repo-dpm', project_id: 'project-id', name: 'DynamicPointMamba',
          ssh_url: 'git@github.com:XR-Lee/DynamicPointMamba.git', default_branch: 'main', status: 'active',
          last_verified_at: '2026-08-21T00:00:00Z',
        }],
        catalog: {
          project_id: 'project-id',
          generated_at: '2026-08-31T00:00:00Z',
          repositories: [{
            id: 'repo-dpm', name: 'DynamicPointMamba',
            ssh_url: 'git@github.com:XR-Lee/DynamicPointMamba.git', default_branch: 'main', status: 'active',
            last_verified_at: '2026-08-21T00:00:00Z',
            rows: [{
              id: 'row-1', repository_id: 'repo-dpm',
              branch: 'autoresearch/sprint-beat-sast-20260817',
              setting: 'G2 seed-2 best-checkpoint Mean U-spec',
              method: 'U-spec unified organizer',
              implementation: 'SPRINT_G2_DECISION.md frozen analyzer',
              metric: '90.4114',
              result: 'promote_U-spec versus SAST -0.4986 pp',
              link: 'SPRINT_G2_DECISION.md',
              hash: '79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0',
              created_at: '2026-08-31T00:00:00Z', updated_at: '2026-08-31T00:00:00Z',
            }, {
              id: 'row-2', repository_id: 'repo-dpm',
              branch: 'autoresearch/learnable-membership-20260829',
              setting: 'M1M3 G-0prime objbg Coad u0',
              method: 'select_t_star',
              implementation: 'M1M3_G0PRIME_RESULTS.md',
              metric: '6.1962',
              result: 'T*=objbg Coad(u0)=6.1962 pp',
              link: 'M1M3_G0PRIME_RESULTS.md',
              hash: 'ea6b1b5e64b9fa9815bc0ac6852a3c723e03b5e5',
              created_at: '2026-08-31T00:00:00Z', updated_at: '2026-08-31T00:00:00Z',
            }],
          }],
        },
      },
      global: { stubs: flowStubs },
    })
    const panel = wrapper.get('[data-testid="repo-experiment-catalog"]')
    expect(panel.text()).toContain('Original registration')
    expect(panel.text()).toContain('Analysis')
    expect(panel.text()).not.toContain('原始注册数据')
    expect(panel.text()).not.toContain('分析数据')
    expect(panel.text()).toContain('git@github.com:XR-Lee/DynamicPointMamba.git')
    expect(panel.text()).toContain('main')
    expect(panel.text()).toContain('active')
    expect(panel.text()).toContain('Setting')
    expect(panel.text()).toContain('Method')
    expect(panel.text()).toContain('Implementation')
    expect(panel.text()).toContain('metric')
    expect(panel.text()).toContain('Result')
    expect(panel.text()).not.toContain('方法')
    expect(panel.text()).not.toContain('实现')
    expect(panel.text()).toContain('link')
    expect(panel.text()).toContain('hash')
    expect(panel.text()).toContain('G2 seed-2 best-checkpoint Mean U-spec')
    expect(panel.text()).toContain('U-spec unified organizer')
    expect(panel.text()).toContain('SPRINT_G2_DECISION.md frozen analyzer')
    expect(panel.text()).toContain('90.4114')
    expect(panel.text()).toContain('promote_U-spec versus SAST -0.4986 pp')
    expect(panel.text()).toContain('79b9a11f8e7ad9bb384ff4a5c3354b5d1adfc9c0')
    expect(panel.text()).toContain('autoresearch/learnable-membership-20260829')
    useI18n().setLocale('zh')
    await flushPromises()
    const chinese = wrapper.get('[data-testid="repo-experiment-catalog"]')
    expect(chinese.text()).toContain('原始注册数据')
    expect(chinese.text()).toContain('分析数据')
    expect(chinese.text()).toContain('方法')
    expect(chinese.text()).toContain('实现')
    expect(chinese.text()).toContain('结果')
  })

  it('shows repository readiness on an empty Research home', () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace: { project_id: 'project-id', studies: [], generated_at: '2026-09-11T00:00:00Z' },
        loading: false,
        selectedStudyId: '',
        repositoryReadiness: {
          id: 'repo-1', project_id: 'project-id', name: 'dynamic-point-mamba',
          ssh_url: 'git@github.com:research/dynamic-point-mamba.git', status: 'pending_key', access: 'ssh_deploy_key',
          default_branch: 'main', deploy_key_settings_url: 'https://github.com/research/dynamic-point-mamba/settings/keys',
          ready: false, manifest: { present: false }, defaults: {},
          blockers: [{
            kind: 'deploy_key_required', title: 'Add a read-only Deploy Key',
            detail: 'Install the key.', href: 'https://github.com/research/dynamic-point-mamba/settings/keys',
          }],
        },
      },
      global: { stubs: flowStubs },
    })
    expect(wrapper.text()).toContain('Import from a research repository')
    expect(wrapper.get('[data-testid="repository-readiness"]').text()).toContain('Add a read-only Deploy Key')
  })

  it('shows the Study route and exports a plan-sync patch without inventing a default branch', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: {
        workspace: {
          ...workspace,
          study: {
            ...workspace.study,
            route: { protocol_branch: 'research-plan', protocol_doc_path: 'research-plan/STATUS.md', code_ref_pattern: 'autoresearch/*' },
            hypotheses: [{
              id: 'node-hypothesis-1', title: 'Background noise caps accuracy', status: 'open',
              experiments: [{
                run_node_id: 'node-run-1', experiment_id: 'experiment-1', title: 'Imported without a requested ref',
                state: 'succeeded',
              }],
            }],
          },
          next_actions: [{
            kind: 'export_plan_sync', tool: 'export_research_plan_sync', study_id: 'study-1',
            title: 'Export a research-plan docs patch',
            detail: 'Dry-run a markdown amendment for the protocol branch.',
          }],
        },
        loading: false,
        selectedStudyId: 'study-1',
      },
      global: { stubs: flowStubs },
    })
    expect(wrapper.get('[data-testid="study-route"]').text()).toContain('research-plan')
    expect(wrapper.get('[data-testid="study-route"]').text()).toContain('autoresearch/*')
    expect(wrapper.get('[data-testid="hypothesis-records"]').find('.hypothesis-branch').exists()).toBe(false)
    expect(wrapper.get('[data-testid="hypothesis-records"]').text()).not.toMatch(/Branch/)
    await wrapper.get('[data-testid="export-plan-sync"]').trigger('click')
    expect(wrapper.emitted('exportPlanSync')).toEqual([[]])
  })

  it('shows historical observations when the Study has no Gemcp result', () => {
    useI18n().setLocale('en')
    const observed = structuredClone(workspace)
    observed.study.nodes = [
      { id: 'node-question-1', kind: 'question', title: 'Question', status: 'open', created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:00:00Z' },
      { id: 'node-hypothesis-1', kind: 'hypothesis', title: 'Background noise caps accuracy', status: 'open', created_at: '2026-07-28T18:01:00Z', updated_at: '2026-07-28T18:01:00Z' },
      {
        id: 'node-observation-1', kind: 'observation', title: 'Paper X reports 0.93', summary: 'Table 2, no Gemcp run',
        status: 'recorded', metric_name: 'accuracy', metric_value: 0.93, metric_source: 'asserted',
        created_at: '2026-07-28T18:02:00Z', updated_at: '2026-07-28T18:02:00Z',
      },
      {
        id: 'node-highlight-1', kind: 'observation', title: 'close_run highlight', system_written: true,
        status: 'succeeded', created_at: '2026-07-28T18:03:00Z', updated_at: '2026-07-28T18:03:00Z',
      },
    ] as never
    observed.study.edges = [
      { id: 'edge-0', from_id: 'node-question-1', to_id: 'node-hypothesis-1', relation: 'leads_to' },
      { id: 'edge-1', from_id: 'node-observation-1', to_id: 'node-hypothesis-1', relation: 'supports' },
      { id: 'edge-2', from_id: 'node-hypothesis-1', to_id: 'node-highlight-1', relation: 'leads_to' },
    ] as never
    observed.study.hypotheses = [{
      id: 'node-hypothesis-1', title: 'Background noise caps accuracy', status: 'open', branch: '', experiments: [],
    }] as never
    const wrapper = mount(ResearchView, {
      props: { workspace: observed, loading: false, selectedStudyId: 'study-1' },
      global: { stubs: flowStubs },
    })
    expect(wrapper.get('[data-testid="latest-evidence"]').text()).toContain('Latest evidence')
    expect(wrapper.get('[data-testid="latest-result-card"]').text()).toContain('Paper X reports 0.93')
    expect(wrapper.get('[data-testid="latest-result-card"]').text()).toContain('Historical observation, not a Gemcp Experiment result.')
    expect(wrapper.text()).not.toContain('No result recorded')
    const records = wrapper.get('[data-testid="hypothesis-observations"]')
    expect(records.text()).toContain('Paper X reports 0.93')
    expect(records.text()).toContain('Supports')
    expect(records.text()).toContain('asserted')
    expect(records.text()).not.toContain('close_run highlight')
    expect(wrapper.get('[data-testid="hypothesis-records"]').text()).toContain('No Graph experiment is bound to this hypothesis yet.')
  })
})
