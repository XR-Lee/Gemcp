import { describe, expect, it } from 'vitest'
import { buildAgentReadinessPrompt } from './agentReadinessPrompt'
import type { AgentReadiness } from './api'

const readiness: AgentReadiness = {
  project_id: 'project-id',
  project_name: 'Point Models',
  status: 'ready',
  summary: '1 Agent(s) and 1 ready compute target(s). Binding is Project-scoped, not exclusive to one node.',
  generated_at: '2026-08-24T18:00:00Z',
  agents: [{
    id: 'agent-1', label: 'train-agent', prefix: 'gmc_abcd123',
    scopes: ['read', 'submit', 'operate_nodes'], status: 'active',
    last_used_at: '2026-08-24T17:58:00Z', can_read: true, can_submit: true,
    can_operate_nodes: true, bound_node_ids: ['ssh-cloud-1'],
  }],
  compute: {
    ssh_cloud_enabled: true,
    ssh_cloud: [{
      id: 'ssh-cloud-1', label: 'cloud-4090', status: 'active', host: '203.0.113.10', user: 'ubuntu',
      gpus: [{ name: 'NVIDIA GeForce RTX 4090', memory_bytes: 24 }],
      ready: true, readiness: 'ready', blockers: [], bound_to_project: true,
      registered_by_kind: 'agent', registered_by_label: 'train-agent',
      last_probed_at: '2026-08-24T17:59:00Z', runtime_configured: true,
    }],
    self_hosted: [],
  },
  heartbeats: {
    agent_last_used_at: '2026-08-24T17:58:00Z',
    ssh_cloud_last_probed_at: '2026-08-24T17:59:00Z',
    note: 'MCP last_used_at updates on every authenticated tool call.',
  },
  next_actions: [{ kind: 'copy_readiness', title: 'Copy readiness prompt', detail: 'Tell the Agent.' }],
  instructions: {
    inspect_tool: 'get_project_options',
    monitor_tool: 'get_experiment',
    heartbeat: 'MCP last_used_at updates on every authenticated tool call. Monitor with get_experiment.',
    binding: 'Agents are Project-scoped.',
  },
}

describe('buildAgentReadinessPrompt', () => {
  it('tells the Agent to inspect options and lists the Owner binding', () => {
    const prompt = buildAgentReadinessPrompt({ locale: 'en', readiness })
    expect(prompt).toContain('get_project_options')
    expect(prompt).toContain('get_experiment')
    expect(prompt).toContain('cloud-4090')
    expect(prompt).toContain('ubuntu@203.0.113.10')
    expect(prompt).toContain('train-agent')
    expect(prompt).toContain('operate_nodes')
    expect(prompt).toContain('last_used_at')
    expect(prompt).toContain('exact confirmation digest')
    expect(prompt).toContain('wait for explicit confirmation of that digest before submit_prepared_experiment')
    expect(prompt).not.toContain('BEGIN OPENSSH')
    expect(prompt).not.toContain('gmc_secret')
  })

  it('writes the same contract in Chinese', () => {
    const prompt = buildAgentReadinessPrompt({ locale: 'zh', readiness })
    expect(prompt).toContain('先调用 `get_project_options`')
    expect(prompt).toContain('不要发明主机')
    expect(prompt).toContain('cloud-4090')
    expect(prompt).toContain('等待 Owner 明确确认该 digest 后才能调用 submit_prepared_experiment')
  })
})
