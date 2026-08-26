import { describe, expect, it } from 'vitest'
import { projectAgentSetupPrompt } from './projectAgentSetupPrompt'

describe('projectAgentSetupPrompt', () => {
  it('registers the Agent to a Project without binding compute', () => {
    const prompt = projectAgentSetupPrompt({
      locale: 'en',
      projectName: 'Point Models',
      setupUrl: 'https://gemcp.example/agent/setup#code=one-time',
      scopes: ['read', 'submit', 'cancel'],
    })

    expect(prompt).toContain('Project Point Models')
    expect(prompt).toContain('https://gemcp.example/agent/setup#code=one-time')
    expect(prompt).toContain('does not bind a GPU, node, or Provider')
    expect(prompt).toContain('Project budget is a spending limit, not per-run approval')
    expect(prompt).toContain('let setup write them to secure configuration')
    expect(prompt).toContain('wait for the Owner to explicitly confirm that digest')
    expect(prompt).toContain('Never call `submit_prepared_experiment` before that confirmation')
    expect(prompt).not.toContain('without another per-run Owner approval')
    expect(prompt).not.toContain('register_ssh_cloud_node')
    expect(prompt).not.toContain('BEGIN OPENSSH')
  })

  it('states the same compute-independent boundary in Chinese', () => {
    const prompt = projectAgentSetupPrompt({
      locale: 'zh',
      projectName: 'Point Models',
      setupUrl: 'https://gemcp.example/agent/setup#code=one-time',
      scopes: ['read'],
    })

    expect(prompt).toContain('只把你注册到 Project Point Models')
    expect(prompt).toContain('不绑定 GPU、节点或 Provider')
    expect(prompt).toContain('Project 预算是费用上限，不是逐次授权')
    expect(prompt).toContain('由 Setup 流程写入安全配置')
    expect(prompt).toContain('等待 Owner 对这个 digest 明确确认')
    expect(prompt).toContain('确认前绝不能调用 `submit_prepared_experiment`')
    expect(prompt).not.toContain('无需逐次 Owner 确认')
  })
})
