import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
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
  generated_at: '2026-07-28T18:05:00Z',
}

describe('ResearchView', () => {
  it('renders the study, next action, and Graph canvas', () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchView, {
      props: { workspace, loading: false, selectedStudyId: 'study-1' },
      global: {
        stubs: {
          VueFlow: { template: '<div class="vue-flow"><slot /></div>' },
          Background: true,
          MiniMap: true,
          Controls: true,
        },
      },
    })
    expect(wrapper.text()).toContain('objbg-scan')
    expect(wrapper.text()).toContain('Record the current smoke-run accuracy as the first Graph result.')
    expect(wrapper.text()).toContain('Research Graph')
    expect(wrapper.find('.graph-canvas').exists()).toBe(true)
    expect(wrapper.find('.vue-flow').exists()).toBe(true)
    expect(wrapper.text()).toContain('OBJ-BG smoke accuracy')
  })
})
