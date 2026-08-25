import { describe, expect, it } from 'vitest'
import { formatSSHTarget, parseSSHTarget, suggestedSSHLabel } from './sshTarget'

describe('parseSSHTarget', () => {
  it('parses a SeeTaCloud / AutoDL ssh -p command', () => {
    expect(parseSSHTarget('ssh -p 47174 root@connect.westb.seetacloud.com')).toEqual({
      user: 'root', host: 'connect.westb.seetacloud.com', port: 47174,
    })
  })

  it('parses user@host and ssh:// URLs the way VS Code does', () => {
    expect(parseSSHTarget('root@gpu.example.com')).toEqual({
      user: 'root', host: 'gpu.example.com', port: 22,
    })
    expect(parseSSHTarget('ssh://ubuntu@203.0.113.10:2202')).toEqual({
      user: 'ubuntu', host: '203.0.113.10', port: 2202,
    })
    expect(parseSSHTarget('ssh -l ubuntu -p 2222 10.0.0.8')).toEqual({
      user: 'ubuntu', host: '10.0.0.8', port: 2222,
    })
  })

  it('accepts combined flags, -o Port, and a following password line', () => {
    expect(parseSSHTarget('ssh -p47174 root@connect.westb.seetacloud.com')).toEqual({
      user: 'root', host: 'connect.westb.seetacloud.com', port: 47174,
    })
    expect(parseSSHTarget('ssh -o Port=47174 -o User=root connect.westb.seetacloud.com')).toEqual({
      user: 'root', host: 'connect.westb.seetacloud.com', port: 47174,
    })
    expect(parseSSHTarget('ssh -p 47174 root@connect.westb.seetacloud.com\n密码：cloud-instance-password')).toEqual({
      user: 'root', host: 'connect.westb.seetacloud.com', port: 47174, password: 'cloud-instance-password',
    })
  })

  it('ignores extra ssh options and prompt prefixes', () => {
    expect(parseSSHTarget('$ ssh -o StrictHostKeyChecking=no -p 47174 root@connect.westb.seetacloud.com')).toEqual({
      user: 'root', host: 'connect.westb.seetacloud.com', port: 47174,
    })
  })

  it('rejects commands without a user or destination', () => {
    expect(parseSSHTarget('ssh -p 22')).toBeNull()
    expect(parseSSHTarget('connect.westb.seetacloud.com')).toBeNull()
  })
})

describe('ssh target helpers', () => {
  it('formats and suggests a short label from the host', () => {
    expect(formatSSHTarget({ user: 'root', host: 'connect.westb.seetacloud.com', port: 47174 }))
      .toBe('root@connect.westb.seetacloud.com:47174')
    expect(suggestedSSHLabel({ host: 'connect.westb.seetacloud.com' })).toBe('connect')
  })
})
