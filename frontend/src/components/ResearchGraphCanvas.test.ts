import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it } from 'vitest'
import { useI18n } from '../i18n'
import { layoutResearchGraph } from '../researchGraphLayout'
import ResearchGraphCanvas from './ResearchGraphCanvas.vue'

const nodes = [
  {
    id: 'node-question-1', kind: 'question', title: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy?',
    summary: 'Start from the published OBJ-BG protocol.', status: 'open',
    occurred_at: '2026-07-28T18:00:00Z', created_at: '2026-07-28T18:00:00Z', updated_at: '2026-07-28T18:00:00Z',
  },
  {
    id: 'node-result-1', kind: 'result', title: 'OBJ-BG smoke accuracy',
    summary: 'The existing smoke Experiment reached 86.4 overall accuracy.',
    status: 'succeeded', metric_name: 'overall_accuracy', metric_value: 86.4,
    experiment_id: 'experiment-1', experiment_state: 'succeeded',
    occurred_at: '2026-07-28T18:05:00Z', commit_sha: '0123456789ab',
    created_at: '2026-07-28T18:05:00Z', updated_at: '2026-07-28T18:05:00Z',
  },
]
const edges = [{ id: 'edge-1', from_id: 'node-question-1', to_id: 'node-result-1', relation: 'produced' }]
const layout = layoutResearchGraph({ nodes, edges })

const flowStubs = {
  VueFlow: {
    props: ['nodes'],
    emits: ['node-double-click', 'node-click', 'pane-click', 'pane-ready'],
    template: `
      <div class="vue-flow">
        <article
          v-for="node in nodes.filter((item) => item.type === 'research')"
          :key="node.id"
          class="flow-node"
          @click="$emit('node-click', { event: $event, node })"
          @dblclick="$emit('node-double-click', { event: $event, node })"
        >{{ node.data.node.title }}</article>
        <slot />
      </div>
    `,
  },
  Background: true,
  MiniMap: true,
  Controls: true,
}

afterEach(() => {
  useI18n().setLocale('en')
  document.body.style.overflow = ''
})

describe('ResearchGraphCanvas', () => {
  it('keeps the graph window capped and opens detail on double-click', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchGraphCanvas, {
      props: { nodes, edges, layout },
      attachTo: document.body,
      global: { stubs: flowStubs },
    })
    expect(wrapper.find('.graph-shell').attributes('style')).toContain('560px')
    expect(wrapper.find('.graph-axis-label').text()).toMatch(/Evidence|Lineage/)
    expect(wrapper.find('.graph-axis-label').classes()).not.toContain('is-fallback')
    expect(wrapper.find('button[aria-label="Fit graph"]').exists()).toBe(true)
    expect(layout.nodes.find((item) => item.id === 'node-result-1')?.outcome).toBe('success')
    await wrapper.get('.flow-node').trigger('dblclick')
    await flushPromises()
    expect(wrapper.find('.graph-detail').text()).toContain('Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy?')
    expect(wrapper.find('.graph-detail').text()).toContain('Start from the published OBJ-BG protocol.')
    expect(wrapper.emitted('openExperiment')).toBeUndefined()
    await wrapper.get('button[aria-label="Fullscreen graph"]').trigger('click')
    expect(document.body.querySelector('.graph-shell')?.classList.contains('is-fullscreen')).toBe(true)
    expect(document.body.style.overflow).toBe('hidden')
    wrapper.unmount()
  })

  it('opens the experiment record when double-clicking a linked node', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchGraphCanvas, {
      props: { nodes, edges, layout },
      attachTo: document.body,
      global: { stubs: flowStubs },
    })
    await wrapper.findAll('.flow-node')[1].trigger('dblclick')
    await flushPromises()
    expect(wrapper.emitted('openExperiment')).toEqual([['experiment-1']])
    expect(wrapper.find('.graph-detail').exists()).toBe(false)
    wrapper.unmount()
  })

  it('opens the experiment record after two clicks on a linked node', async () => {
    useI18n().setLocale('en')
    const wrapper = mount(ResearchGraphCanvas, {
      props: { nodes, edges, layout },
      attachTo: document.body,
      global: { stubs: flowStubs },
    })
    const linked = wrapper.findAll('.flow-node')[1]
    await linked.trigger('click')
    await linked.trigger('click')
    await flushPromises()
    expect(wrapper.emitted('openExperiment')).toEqual([['experiment-1']])
    wrapper.unmount()
  })
})
