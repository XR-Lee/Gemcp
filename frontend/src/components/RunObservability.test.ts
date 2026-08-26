import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { Attempt, Experiment, OperationsFeed } from '../api'
import { useI18n } from '../i18n'
import ExperimentDetail from './ExperimentDetail.vue'
import RunActivityPanel from './RunActivityPanel.vue'

const experiment: Experiment = {
  id: 'experiment-1234567890', project_id: 'project-id', repository_id: 'repository-id', environment_id: 'environment-id', resource_profile_id: 'profile-id',
  state: 'running', desired_state: 'running', commit_sha: 'a'.repeat(40), execution_mode: 'argv', argv: ['python', 'train.py'], command: 'python train.py',
  max_runtime_seconds: 3600, reserved_cost_milli: 1250, estimated_cost_milli: 0, output_path: '/managed/experiment',
  log_tail: 'epoch 4 loss=0.38\n', metrics: { epoch: 4, loss: 0.38 },
  execution_context: {
    agent_label: 'training-agent', proposal_id: 'proposal-id', repository_name: 'point-model', repository_ssh_url: 'git@github.com:owner/point-model.git', requested_ref: 'main',
    backend: 'autodl_private', environment_name: 'torch-cuda11.8', image: 'image-id', resource_profile_name: 'one-rtx-3090', region: 'private',
    gpu_models: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, workspace_policy: 'runner_temporary', container_output_path: '/gemcp/output',
    runtime_info: { source: 'runner_observed', working_directory: '/tmp/gemcp/source', output_directory: '/managed/experiment', cuda_visible_devices: '0', gpu_devices: [{ index: 0, uuid: 'GPU-runtime-1', name: 'NVIDIA GeForce RTX 3090' }] },
  },
  backend_observation: { kind: 'autodl_private', id: 'resource-id', state: 'active', status: 'running', cleanup_complete: false, updated_at: '2026-07-28T18:00:00Z' },
  timeline: [{ at: '2026-07-28T17:59:00Z', code: 'experiment.created' }, { at: '2026-07-28T18:00:00Z', code: 'experiment.started' }],
  created_at: '2026-07-28T17:59:00Z', updated_at: '2026-07-28T18:00:00Z', started_at: '2026-07-28T18:00:00Z',
}

const attempts: Attempt[] = [{
  id: 'attempt-id', number: 1, state: 'running', estimated_cost_milli: 0, log_tail: 'epoch 4 loss=0.38\n', metrics: { loss: 0.38 },
  last_heartbeat_at: '2026-07-28T18:00:00Z', created_at: '2026-07-28T17:59:30Z', updated_at: '2026-07-28T18:00:00Z',
}]

const feed: OperationsFeed = {
  activities: [{ id: 'activity-id', agent_label: 'training-agent', agent_token_prefix: 'gmc_test', phase: 'monitoring', experiment_id: experiment.id, at: '2026-07-28T18:00:00Z' }],
  proposals: [{
    id: 'proposal-id', status: 'submitted', eligible: true, agent_label: 'training-agent', agent_token_prefix: 'gmc_test', repository_name: 'point-model', requested_ref: 'main', commit_sha: 'a'.repeat(40),
    display_command: 'python train.py', backend: 'autodl_private', environment_name: 'torch-cuda11.8', image: 'image-id', resource_profile_name: 'one-rtx-3090',
    gpu_models: ['NVIDIA GeForce RTX 3090'], gpu_num: 1, reserved_cost_milli: 1250, checks: [{ id: 'budget', status: 'pass', summary: 'Budget available' }],
    confirmation_digest: `sha256:${'b'.repeat(64)}`, experiment_id: experiment.id, created_at: '2026-07-28T17:58:00Z', updated_at: '2026-07-28T18:00:00Z', expires_at: '2026-07-28T18:28:00Z',
  }],
  generated_at: '2026-07-28T18:00:00Z',
}

afterEach(() => {
  vi.useRealTimers()
  useI18n().setLocale('en')
})

describe('run observability', () => {
  it('shows the latest Agent phase and resolved Proposal and opens its Experiment', async () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-07-28T18:00:30Z'))
    const openExperiment = vi.fn()
    const wrapper = mount(RunActivityPanel, { props: { feed, onOpenExperiment: openExperiment } })

    expect(wrapper.text()).toContain('Monitoring run')
    expect(wrapper.text()).toContain('training-agent')
    expect(wrapper.text()).toContain('torch-cuda11.8')
    expect(wrapper.text()).toContain('1× NVIDIA GeForce RTX 3090')
    expect(wrapper.text()).toContain('CNY 1.250')

    await wrapper.get('.proposal-row').trigger('click')
    expect(openExperiment).toHaveBeenCalledWith(experiment.id)
  })

  it('requires an Owner confirmation action for a paid prepared proposal', async () => {
    const prepared: OperationsFeed = {
      ...feed,
      proposals: [{
        ...feed.proposals[0],
        id: 'proposal-paid',
        status: 'prepared',
        experiment_id: undefined,
        runtime_preset: 'train',
        max_runtime_seconds: 57600,
        reserved_cost_milli: 20000,
      }],
    }
    const confirmProposal = vi.fn()
    const wrapper = mount(RunActivityPanel, { props: { feed: prepared, onConfirmProposal: confirmProposal } })

    expect(wrapper.text()).toContain('train · 57600s')
    expect(wrapper.text()).toContain('Prepared')
    expect(wrapper.text()).toContain('CNY 20.000')
    expect(wrapper.find('input[type="checkbox"]').exists()).toBe(false)
    await wrapper.get('.proposal-confirm-button').trigger('click')
    expect(confirmProposal).toHaveBeenCalledWith(prepared.proposals[0])
    expect(wrapper.emitted('confirmProposal')?.[0]).toEqual([prepared.proposals[0]])
  })

  it('distinguishes requested configuration from observed paths and GPU binding', () => {
    const wrapper = mount(ExperimentDetail, { props: { experiment, attempts, loading: false, error: '' } })

    expect(wrapper.text()).toContain('Execution request')
    expect(wrapper.text()).toContain('Requested')
    expect(wrapper.text()).toContain('Observed')
    expect(wrapper.text()).toContain('GPU-runtime-1')
    expect(wrapper.text()).toContain('/tmp/gemcp/source')
    expect(wrapper.text()).toContain('/managed/experiment')
    expect(wrapper.text()).toContain('epoch 4 loss=0.38')
    expect(wrapper.text()).toContain('Workload started')
  })
})
