import { mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useI18n } from '../i18n'
import type { ResearchNode } from '../api'
import ResearchGraphNode from './ResearchGraphNode.vue'

function node(overrides: Partial<ResearchNode> = {}): ResearchNode {
  return {
    id: 'node-question-1',
    kind: 'question',
    title: 'Can a cleaner OBJ-BG traversal raise ScanObjectNN accuracy?',
    status: 'open',
    created_at: '2026-07-28T18:00:00Z',
    updated_at: '2026-07-28T18:00:00Z',
    ...overrides,
  }
}

function mountNode(research: ResearchNode, handlers = {
  onOpenEvidence: vi.fn(),
  onOpenDetail: vi.fn(),
}) {
  const wrapper = mount(ResearchGraphNode, {
    props: {
      id: research.id,
      type: 'research',
      selected: false,
      data: {
        node: research,
        kindLabel: research.kind,
        onOpenEvidence: handlers.onOpenEvidence,
        onOpenDetail: handlers.onOpenDetail,
      },
    } as never,
    global: { stubs: { Handle: true } },
  })
  return { wrapper, ...handlers }
}

afterEach(() => {
  useI18n().setLocale('en')
})

describe('ResearchGraphNode', () => {
  it('opens node detail when the body is double-clicked', async () => {
    const { wrapper, onOpenDetail, onOpenEvidence } = mountNode(node())
    await wrapper.get('.flow-node').trigger('dblclick')
    expect(onOpenDetail).toHaveBeenCalledWith('node-question-1')
    expect(onOpenEvidence).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('opens the experiment record from the node body or the Evidence link', async () => {
    const research = node({
      id: 'node-result-1',
      kind: 'result',
      title: 'OBJ-BG smoke accuracy',
      status: 'succeeded',
      experiment_id: 'experiment-1',
      experiment_state: 'succeeded',
    })
    const { wrapper, onOpenDetail, onOpenEvidence } = mountNode(research)
    await wrapper.get('.flow-node').trigger('dblclick')
    expect(onOpenEvidence).toHaveBeenCalledWith('experiment-1')
    expect(onOpenDetail).not.toHaveBeenCalled()
    onOpenEvidence.mockClear()
    await wrapper.get('.text-button').trigger('click')
    expect(onOpenEvidence).toHaveBeenCalledWith('experiment-1')
    wrapper.unmount()
  })

  it('opens the experiment record exactly once for a physical double-click', async () => {
    const research = node({
      id: 'node-result-1',
      kind: 'result',
      title: 'OBJ-BG smoke accuracy',
      status: 'succeeded',
      experiment_id: 'experiment-1',
      experiment_state: 'succeeded',
    })
    const { wrapper, onOpenEvidence, onOpenDetail } = mountNode(research)
    // A real double-click dispatches click, click, then dblclick.
    await wrapper.get('.flow-node').trigger('click')
    await wrapper.get('.flow-node').trigger('click')
    await wrapper.get('.flow-node').trigger('dblclick')
    expect(onOpenEvidence).toHaveBeenCalledTimes(1)
    expect(onOpenEvidence).toHaveBeenCalledWith('experiment-1')
    expect(onOpenDetail).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
