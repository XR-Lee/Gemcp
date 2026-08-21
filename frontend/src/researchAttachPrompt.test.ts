import { describe, expect, it } from 'vitest'
import type { Experiment, Project, Repository, ResearchWorkspace } from './api'
import { buildResearchAttachPrompt } from './researchAttachPrompt'

const project: Project = {
  id: 'project-id', name: 'Point Models', slug: 'point-models', status: 'active',
  monthly_budget_milli: 100000, max_experiment_milli: 20000, max_concurrency: 1,
  max_runtime_seconds: 3600, timeout_extension_seconds: 3600, termination_grace_seconds: 60,
  timezone: 'UTC',
}

const repositories: Repository[] = [{
  id: 'repo-1', project_id: 'project-id', name: 'dynamic-point-mamba',
  ssh_url: 'git@github.com:research/dynamic-point-mamba.git', default_branch: 'main', status: 'active',
}]

const workspace: ResearchWorkspace = {
  project_id: 'project-id',
  studies: [{
    id: 'study-1', name: 'objbg-scan', question: 'Can a cleaner traversal raise accuracy?',
    status: 'active', updated_at: '2026-08-21T00:00:00Z',
  }],
  study: {
    id: 'study-1', name: 'objbg-scan', question: 'Can a cleaner traversal raise accuracy?',
    status: 'active', updated_at: '2026-08-21T00:00:00Z',
    plan: {
      id: 'plan-1', status: 'active', goal: 'Baseline first',
      next_action: 'Attach the existing smoke run',
      steps: [], created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:00Z',
    },
    nodes: [
      { id: 'node-q', kind: 'question', title: 'Question', status: 'open', created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:00Z' },
      { id: 'node-h', kind: 'hypothesis', title: 'Noise is the cap', status: 'open', created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:00Z' },
    ],
    edges: [],
  },
  next_actions: [{
    kind: 'prepare_experiment', tool: 'prepare_experiment', study_id: 'study-1',
    from_node_id: 'node-h', title: 'Prepare a run', detail: 'Bind from_node_id',
  }],
  generated_at: '2026-08-21T00:00:00Z',
}

const orphan: Experiment = {
  id: 'experiment-orphan', project_id: 'project-id', repository_id: 'repo-1',
  environment_id: 'env', resource_profile_id: 'profile', state: 'succeeded', desired_state: 'running',
  commit_sha: '0123456789abcdef0123456789abcdef01234567', command: 'python tools/smoke.py',
  max_runtime_seconds: 600, reserved_cost_milli: 1000, estimated_cost_milli: 80,
  output_path: '/outputs', created_at: '2026-08-21T00:00:00Z', updated_at: '2026-08-21T00:00:00Z',
  execution_context: {
    repository_name: 'dynamic-point-mamba', repository_ssh_url: repositories[0].ssh_url,
    backend: 'self_hosted', environment_name: 'default', image: 'img', resource_profile_name: 'gpu',
    region: 'local', gpu_models: [], gpu_num: 1, workspace_policy: 'container_fixed',
    container_output_path: '/outputs',
  },
  graph_linked: false, orphaned: true,
}

describe('buildResearchAttachPrompt', () => {
  it('snapshots the current repository, study, next action, and off-graph experiment', () => {
    const prompt = buildResearchAttachPrompt({
      locale: 'en', project, repositories, experiments: [orphan], workspace,
    })
    expect(prompt).toContain('Attach this repository to the Gemcp research Graph')
    expect(prompt).toContain('git@github.com:research/dynamic-point-mamba.git')
    expect(prompt).toContain('Study: objbg-scan (study-1)')
    expect(prompt).toContain('prepare_experiment → prepare_experiment from_node_id=node-h')
    expect(prompt).toContain('experiment-orphan')
    expect(prompt).toContain('close_run')
    expect(prompt).not.toContain('gmc_')
    expect(prompt).not.toContain('BEGIN ')
  })

  it('tells the agent to create a Study when the Graph is empty', () => {
    const prompt = buildResearchAttachPrompt({
      locale: 'zh',
      project,
      repositories,
      experiments: [],
      workspace: { project_id: 'project-id', studies: [], generated_at: '2026-08-21T00:00:00Z' },
    })
    expect(prompt).toContain('把当前仓库挂上 Gemcp 研究 Graph')
    expect(prompt).toContain('Study：还没有')
    expect(prompt).toContain('update_research_workspace')
    expect(prompt).toContain('get_next_actions')
  })
})
