import { describe, expect, it } from 'vitest'
import { agentNodeHandshakePrompt, sshCommand } from './agentNodeHandshakePrompt'

describe('agentNodeHandshakePrompt', () => {
  it('binds a setup link to Cloud SSH registration and prepare', () => {
    const prompt = agentNodeHandshakePrompt({
      locale: 'zh',
      projectName: 'Point Models',
      setupUrl: 'http://127.0.0.1:18080/agent/setup#code=one-time',
      scopes: ['read', 'submit', 'cancel', 'operate_nodes'],
      sshCloudEnabled: true,
      operateNodes: true,
    })
    expect(prompt).toContain('# Gemcp 握手')
    expect(prompt).toContain('http://127.0.0.1:18080/agent/setup#code=one-time')
    expect(prompt).toContain('operate_nodes')
    expect(prompt).toContain('register_ssh_cloud_node')
    expect(prompt).toContain('prepare_experiment')
    expect(prompt).toContain('get_experiment')
    expect(prompt).toContain('等待 Owner 对这个 digest 明确确认')
    expect(prompt).toContain('确认前绝不能调用 submit_prepared_experiment')
    expect(prompt).not.toContain('无需逐次 Owner 确认')
    expect(prompt).not.toContain('自己发明 Token')
    expect(prompt).not.toContain('BEGIN OPENSSH')
  })

  it('stops when the handshake has no setup link', () => {
    const prompt = agentNodeHandshakePrompt({
      locale: 'en',
      projectName: 'Point Models',
      sshCloudEnabled: true,
      operateNodes: true,
    })
    expect(prompt).toContain('Register Agent')
    expect(prompt).toContain('Project setup prompt')
    expect(prompt).toContain('Do not invent a Token')
    expect(prompt).not.toContain('/agent/setup#code=')
  })

  it('names an already registered host and still mentions register_ssh_cloud_node', () => {
    const prompt = agentNodeHandshakePrompt({
      locale: 'en',
      sshCloudEnabled: true,
      operateNodes: true,
      node: { label: 'cloud-4090', user: 'ubuntu', host: '203.0.113.10', port: 22, registered: true },
    })
    expect(prompt).toContain('ssh ubuntu@203.0.113.10')
    expect(prompt).toContain('cloud-4090')
    expect(prompt).toContain('register_ssh_cloud_node')
    expect(prompt).toContain('get_project_options.ssh_cloud_nodes')
    expect(prompt).not.toContain('--bridge=none')
  })

  it('omits Cloud SSH registration when operate_nodes is off', () => {
    const prompt = agentNodeHandshakePrompt({
      locale: 'en',
      setupUrl: 'https://gemcp.example/agent/setup#code=abc',
      scopes: ['read', 'submit', 'cancel'],
      sshCloudEnabled: true,
      operateNodes: false,
    })
    expect(prompt).toContain('do not call register_ssh_cloud_node')
    expect(prompt).toContain('operate_nodes')
    expect(prompt).toContain('prepare_experiment')
    expect(prompt).toContain('wait for the Owner to explicitly confirm that digest')
    expect(prompt).toContain('Never call submit_prepared_experiment before that confirmation')
    expect(prompt).not.toContain('no per-run Owner approval is required')
  })

  it('formats non-default SSH ports', () => {
    expect(sshCommand('root', 'connect.example.com', 47174)).toBe('ssh -p 47174 root@connect.example.com')
  })
})
