import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useI18n } from '../i18n'
import FinanceView from './FinanceView.vue'

function response(data: unknown, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => ({ data }) }
}

const project = {
  id: 'project-id', name: 'Research', slug: 'research', status: 'active' as const, timezone: 'UTC',
  monthly_budget_milli: 100_000, max_experiment_milli: 50_000, max_concurrency: 1,
  max_runtime_seconds: 3600, timeout_extension_seconds: 600, termination_grace_seconds: 30,
}

const dashboard = {
  period: '2026-07', audit_scope: 'organization' as const,
  totals: {
    base_budget_milli: 100_000, reserved_milli: 20_000, charged_milli: 8_000,
    credits_milli: 25_000, debits_milli: 0, committed_milli: 3_000, available_milli: 97_000,
  },
  projects: [{
    id: project.id, name: project.name, status: project.status, timezone: project.timezone,
    base_budget_milli: 100_000, reserved_milli: 20_000, charged_milli: 8_000,
    credits_milli: 25_000, debits_milli: 0, committed_milli: 3_000, available_milli: 97_000,
  }],
  daily: [{ date: '2026-07-22', reserved_milli: 20_000, charged_milli: 8_000, credits_milli: 25_000, debits_milli: 0 }],
  backends: [{ backend: 'autodl_private', experiments: 1, reserved_milli: 20_000, charged_milli: 8_000 }],
  ledger: [{
    id: 'entry-id', project_id: project.id, project_name: project.name, period: '2026-07', kind: 'adjustment',
    direction: 'credit' as const, amount_milli: -25_000, balance_effect_milli: 25_000,
    description: 'Initial test allocation', created_at: '2026-07-22T12:00:00Z',
  }],
  audit: [{
    id: 'audit-id', actor_type: 'user', actor_id: 'owner-id', action: 'budget.credit_recorded',
    target_type: 'budget_entry', target_id: 'entry-id', created_at: '2026-07-22T12:00:00Z',
  }],
  generated_at: '2026-07-22T12:00:00Z',
}

afterEach(() => {
  vi.unstubAllGlobals()
  useI18n().setLocale('en')
  window.localStorage.removeItem('gemcp.locale')
})

describe('FinanceView', () => {
  it('renders analytics and records an idempotent Project credit', async () => {
    const fetchMock = vi.fn(async (input: RequestInfo | URL, options?: RequestInit) => {
      const path = String(input)
      const method = options?.method ?? 'GET'
      if (path.startsWith('/api/v1/finance?') && method === 'GET') return response(dashboard)
      if (path === '/api/v1/projects/project-id/budget-adjustments' && method === 'POST') return response({
        entry: dashboard.ledger[0], idempotent: false,
      }, 201)
      throw new Error(`unexpected request ${method} ${path}`)
    })
    vi.stubGlobal('fetch', fetchMock)
    vi.stubGlobal('crypto', { randomUUID: () => '00000000-0000-4000-8000-000000000001' })

    const wrapper = mount(FinanceView, { props: { active: true, projects: [project] } })
    await flushPromises()

    expect(wrapper.text()).toContain('Budget and ledger')
    expect(wrapper.text()).toContain('97.00')
    expect(wrapper.text()).toContain('AutoDL Private')
    expect(wrapper.text()).toContain('Initial test allocation')

    const adjustmentButton = wrapper.findAll('button').find((button) => button.text().includes('Adjust credit'))
    await adjustmentButton!.trigger('click')
    await wrapper.get('input[type="number"]').setValue('25.5')
    await wrapper.get('textarea').setValue('Approved AutoDL smoke test allocation')
    await wrapper.get('input[type="checkbox"]').setValue(true)
    await wrapper.get('form.dialog-form').trigger('submit')
    await flushPromises()

    const adjustmentCall = fetchMock.mock.calls.find(([input]) => String(input).includes('/budget-adjustments'))
    expect(JSON.parse(String(adjustmentCall?.[1]?.body))).toEqual({
      direction: 'credit', amount_milli: 25_500, reason: 'Approved AutoDL smoke test allocation',
      idempotency_key: 'budget-00000000-0000-4000-8000-000000000001',
    })
    expect(fetchMock.mock.calls.filter(([input]) => String(input).startsWith('/api/v1/finance?'))).toHaveLength(2)
    wrapper.unmount()
  })

  it('keeps the newest result when finance filters change during a request', async () => {
    let resolveFirst!: (value: ReturnType<typeof response>) => void
    const firstRequest = new Promise<ReturnType<typeof response>>((resolve) => { resolveFirst = resolve })
    const older = { ...dashboard, period: '2026-07' }
    const newer = {
      ...dashboard,
      period: '2026-06',
      totals: { ...dashboard.totals, available_milli: 88_000 },
    }
    const fetchMock = vi.fn(async () => {
      if (fetchMock.mock.calls.length === 1) return firstRequest
      return response(newer)
    })
    vi.stubGlobal('fetch', fetchMock)

    const wrapper = mount(FinanceView, { props: { active: true, projects: [project] } })
    await flushPromises()
    await wrapper.get('input[type="month"]').setValue('2026-06')
    await flushPromises()
    resolveFirst(response(older))
    await flushPromises()

    expect(wrapper.get('.finance-metrics .available strong').text()).toContain('88.00')
    expect(wrapper.get('input[type="month"]').element).toHaveProperty('value', '2026-06')
    wrapper.unmount()
  })
})
