import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { useI18n } from '../i18n'
import type { RepositoryReadiness } from '../api'
import RepositoryReadinessPanel from './RepositoryReadinessPanel.vue'

const ready: RepositoryReadiness = {
  id: 'repo-1', project_id: 'project-id', name: 'dynamic-point-mamba',
  ssh_url: 'git@github.com:research/dynamic-point-mamba.git', status: 'active', access: 'ssh_deploy_key',
  default_branch: 'main', detected_default_branch: 'master', commit_sha: 'a'.repeat(40),
  deploy_key_settings_url: 'https://github.com/research/dynamic-point-mamba/settings/keys',
  ready: true, manifest: { present: true, workloads: ['objbg-smoke'] },
  defaults: {
    environment: { name: 'public-elastic' },
    resource_profile: { name: 'rtx4090' },
    dataset_binding: { name: 'scanobjectnn-objbg' },
  },
  blockers: [],
}

afterEach(() => {
  useI18n().setLocale('en')
})

describe('RepositoryReadinessPanel', () => {
  it('shows detected branch, workloads, and Project defaults when ready', () => {
    const wrapper = mount(RepositoryReadinessPanel, { props: { readiness: ready } })
    expect(wrapper.get('[data-testid="repository-readiness"]').text()).toContain('Ready')
    expect(wrapper.text()).toContain('master')
    expect(wrapper.text()).toContain('objbg-smoke')
    expect(wrapper.text()).toContain('public-elastic')
    expect(wrapper.text()).toContain('scanobjectnn-objbg')
    expect(wrapper.text()).toContain('Access and Project defaults are enough to prepare a first run.')
  })

  it('links a pending repository to its GitHub Deploy Key page', () => {
    const wrapper = mount(RepositoryReadinessPanel, {
      props: {
        readiness: {
          ...ready, status: 'pending_key', ready: false, detected_default_branch: undefined,
          manifest: { present: false },
          blockers: [{
            kind: 'deploy_key_required', title: 'Add a read-only Deploy Key',
            detail: 'Install the key.', href: 'https://github.com/research/dynamic-point-mamba/settings/keys',
          }],
        },
      },
    })
    expect(wrapper.text()).toContain('Blocked')
    expect(wrapper.get('a.form-note-link').attributes('href')).toBe('https://github.com/research/dynamic-point-mamba/settings/keys')
  })
})
