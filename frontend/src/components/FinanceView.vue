<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  Banknote,
  ChartNoAxesCombined,
  CircleDollarSign,
  CreditCard,
  LoaderCircle,
  Plus,
  ReceiptText,
  RefreshCw,
  ShieldCheck,
  WalletCards,
  X,
} from '@lucide/vue'
import {
  APIError,
  api,
  type FinanceDashboard,
  type FinanceLedgerEntry,
  type Project,
} from '../api'
import { useI18n } from '../i18n'

const props = defineProps<{ active: boolean; projects: Project[] }>()
const emit = defineEmits<{ unauthorized: [] }>()
const { languageTag, t } = useI18n()

const today = new Date()
const period = ref(`${today.getFullYear()}-${String(today.getMonth() + 1).padStart(2, '0')}`)
const projectFilter = ref('')
const data = ref<FinanceDashboard | null>(null)
const loading = ref(false)
const error = ref('')
const activeLog = ref<'ledger' | 'audit'>('ledger')
const adjustmentDialog = ref(false)
const adjustmentBusy = ref(false)
const adjustmentError = ref('')
const adjustmentForm = reactive({
  projectID: '',
  direction: 'credit' as 'credit' | 'debit',
  amountCNY: '',
  reason: '',
  confirmed: false,
  idempotencyKey: '',
})
let loadSequence = 0

const activeProjects = computed(() => props.projects.filter((project) => project.status !== 'archived'))
const chartMaximum = computed(() => Math.max(0, ...(data.value?.daily.flatMap((point) => [
  Math.abs(point.reserved_milli), point.charged_milli, point.credits_milli, point.debits_milli,
]) ?? [])))
const adjustmentAmountMilli = computed(() => {
  const amount = Number(adjustmentForm.amountCNY)
  if (!Number.isFinite(amount) || amount <= 0) return 0
  const milli = Math.round(amount * 1000)
  return Number.isSafeInteger(milli) ? milli : 0
})
const adjustmentDialogTitle = computed(() => adjustmentForm.direction === 'credit'
  ? t('Add Project budget', '充值 Project 预算')
  : t('Reduce Project budget', '扣减 Project 预算'))

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

async function load() {
  if (!props.active || !/^\d{4}-\d{2}$/.test(period.value)) return
  const sequence = ++loadSequence
  loading.value = true
  error.value = ''
  try {
    const dashboard = await api.finance(period.value, projectFilter.value)
    if (sequence === loadSequence) data.value = dashboard
  } catch (caught) {
    if (sequence === loadSequence) handleError(caught, t('Could not load finance analytics.', '无法加载财务分析。'))
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

function newIdempotencyKey() {
  const suffix = typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function'
    ? crypto.randomUUID()
    : `${Date.now()}-${Math.random().toString(16).slice(2)}`
  return `budget-${suffix}`
}

function openAdjustment() {
  const preferred = activeProjects.value.find((project) => project.id === projectFilter.value) ?? activeProjects.value[0]
  adjustmentForm.projectID = preferred?.id ?? ''
  adjustmentForm.direction = 'credit'
  adjustmentForm.amountCNY = ''
  adjustmentForm.reason = ''
  adjustmentForm.confirmed = false
  adjustmentForm.idempotencyKey = newIdempotencyKey()
  adjustmentError.value = ''
  adjustmentDialog.value = true
}

function closeAdjustment() {
  if (adjustmentBusy.value) return
  adjustmentDialog.value = false
  adjustmentForm.amountCNY = ''
  adjustmentForm.reason = ''
  adjustmentForm.confirmed = false
  adjustmentError.value = ''
}

async function submitAdjustment() {
  if (!adjustmentForm.projectID || !adjustmentForm.confirmed || adjustmentAmountMilli.value <= 0 || adjustmentForm.reason.trim().length < 3) {
    adjustmentError.value = t('Select a Project, enter a positive amount and reason, then confirm the ledger entry.', '请选择 Project，输入正数金额和原因，然后确认账本记录。')
    return
  }
  adjustmentBusy.value = true
  adjustmentError.value = ''
  const adjustedProjectID = adjustmentForm.projectID
  try {
    const result = await api.adjustBudget(adjustmentForm.projectID, {
      direction: adjustmentForm.direction,
      amount_milli: adjustmentAmountMilli.value,
      reason: adjustmentForm.reason.trim(),
      idempotency_key: adjustmentForm.idempotencyKey,
    })
    adjustmentDialog.value = false
    adjustmentForm.amountCNY = ''
    adjustmentForm.reason = ''
    adjustmentForm.confirmed = false
    const followAdjustedProject = Boolean(projectFilter.value) && projectFilter.value !== adjustedProjectID
    if (followAdjustedProject) projectFilter.value = adjustedProjectID
    if (period.value !== result.entry.period) period.value = result.entry.period
    else if (!followAdjustedProject) await load()
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else adjustmentError.value = caught instanceof APIError ? caught.message : t('Could not record the budget adjustment.', '无法记录额度调整。')
  } finally {
    adjustmentBusy.value = false
  }
}

function money(milli = 0) {
  return new Intl.NumberFormat(languageTag.value, { style: 'currency', currency: 'CNY' }).format(milli / 1000)
}

function signedMoney(milli: number) {
  const sign = milli > 0 ? '+' : milli < 0 ? '-' : ''
  return `${sign}${money(Math.abs(milli))}`
}

function dateTime(value: string) {
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function barHeight(value: number) {
  if (!value || chartMaximum.value <= 0) return '0px'
  return `${Math.max(3, Math.round((Math.abs(value) / chartMaximum.value) * 118))}px`
}

function kindLabel(entry: FinanceLedgerEntry) {
  if (entry.direction === 'credit') return t('Credit', '充值')
  if (entry.direction === 'debit') return t('Debit', '扣减')
  return {
    reservation: t('Reservation', '预留'),
    release: t('Release', '释放'),
    charge: t('Charge', '结算'),
  }[entry.kind] ?? entry.kind
}

function backendLabel(value?: string) {
  if (value === 'autodl_private') return 'AutoDL Private'
  if (value === 'autodl_elastic') return 'AutoDL Public'
  if (value === 'self_hosted') return t('Self-hosted', '自托管')
  return value || '—'
}

function actionLabel(value: string) {
  const labels: Record<string, string> = {
    'budget.credit_recorded': t('Budget credit recorded', '已记录额度充值'),
    'budget.debit_recorded': t('Budget debit recorded', '已记录额度扣减'),
    'experiment.submitted': t('Experiment submitted', '实验已提交'),
    'experiment.dispatched': t('Experiment dispatched', '实验已调度'),
    'experiment.started': t('Experiment started', '实验已开始'),
    'experiment.finalized': t('Experiment finalized', '实验已结算'),
    'experiment.cancel_requested': t('Experiment cancellation requested', '已请求取消实验'),
    'experiment.budget_stopped': t('Experiment stopped by budget', '实验因预算停止'),
    'experiment.provider_error': t('Provider error recorded', '已记录 Provider 错误'),
    'provider.resource_created': t('Provider resource created', 'Provider 资源已创建'),
    'provider.resource_stop_requested': t('Provider resource stop requested', '已请求停止 Provider 资源'),
    'provider.emergency_stop_requested': t('Emergency stop requested', '已请求 Emergency Stop'),
    'watchdog.stop_enforced': t('Watchdog stop enforced', 'Watchdog 已执行停止'),
  }
  return labels[value] ?? value.replaceAll('_', ' ')
}

watch(() => [props.active, period.value, projectFilter.value] as const, ([active]) => {
  if (active) void load()
}, { immediate: true })
</script>

<template>
  <section class="finance-page">
    <div v-if="error" class="page-alert finance-alert" role="alert">{{ error }}<button type="button" :title="t('Dismiss', '关闭')" @click="error = ''"><X :size="16" /></button></div>

    <header class="finance-heading">
      <div>
        <p class="eyebrow">{{ t('Owner finance', 'Owner 财务') }}</p>
        <h1>{{ t('Budget and ledger', '预算与账本') }}</h1>
        <p>{{ t('Internal scheduling capacity, estimated Provider charges, and immutable adjustments.', '内部调度额度、Provider 费用估算和不可变调整记录。') }}</p>
      </div>
      <div class="finance-actions">
        <label class="compact-field"><span>{{ t('Period', '周期') }}</span><input v-model="period" type="month" /></label>
        <label class="compact-field"><span>Project</span><select v-model="projectFilter"><option value="">{{ t('All Projects', '全部 Project') }}</option><option v-for="project in projects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
        <button class="icon-button" type="button" :title="t('Refresh finance data', '刷新财务数据')" :disabled="loading" @click="load"><RefreshCw :size="16" :class="{ spinning: loading }" /></button>
        <button class="primary-button finance-budget-button" type="button" :disabled="activeProjects.length === 0" @click="openAdjustment"><Plus :size="16" />{{ t('Add Project budget', '充值 Project 预算') }}</button>
      </div>
    </header>

    <div class="finance-boundary"><ShieldCheck :size="17" /><span><strong>{{ t('Project budget ledger', 'Project 预算账本') }}</strong>{{ t("Budget added here belongs only to the selected Project and is recorded in its current billing period. The period filter only views history; this does not transfer money to a Provider account.", '这里充值的预算只属于所选 Project，并记录在其当前计费周期。周期筛选仅用于查看历史；此操作不会向 Provider 账户转账。') }}</span></div>

    <div v-if="loading && !data" class="finance-loading"><LoaderCircle :size="20" class="spinning" />{{ t('Loading finance analytics', '正在加载财务分析') }}</div>
    <template v-else-if="data">
      <section class="finance-metrics" aria-label="Finance summary">
        <div><span><WalletCards :size="16" />{{ t('Base allocation', '基础额度') }}</span><strong>{{ money(data.totals.base_budget_milli) }}</strong><small>{{ data.period }}</small></div>
        <div><span><Plus :size="16" />{{ t('Credits', '充值') }}</span><strong>{{ money(data.totals.credits_milli) }}</strong><small>{{ t('Added capacity', '增加的容量') }}</small></div>
        <div><span><CreditCard :size="16" />{{ t('Debits', '扣减') }}</span><strong>{{ money(data.totals.debits_milli) }}</strong><small>{{ t('Manual corrections', '人工修正') }}</small></div>
        <div><span><ReceiptText :size="16" />{{ t('Reserved', '预留') }}</span><strong>{{ money(data.totals.reserved_milli) }}</strong><small>{{ t('Active worst case', '活跃最坏情况') }}</small></div>
        <div><span><Banknote :size="16" />{{ t('Estimated charges', '预估结算') }}</span><strong>{{ money(data.totals.charged_milli) }}</strong><small>{{ t('Operational estimate', '运行估算') }}</small></div>
        <div class="available"><span><CircleDollarSign :size="16" />{{ t('Available', '可用额度') }}</span><strong>{{ money(data.totals.available_milli) }}</strong><small>{{ t('After credits and commitments', '计入充值与承诺后') }}</small></div>
      </section>

      <div class="finance-analysis-grid">
        <section class="finance-section trend-section">
          <div class="finance-section-heading"><div><p class="eyebrow">{{ t('Cash flow', '额度流向') }}</p><h2>{{ t('Daily activity', '每日活动') }}</h2></div><ChartNoAxesCombined :size="18" /></div>
          <div class="chart-legend"><span class="credit">{{ t('Credit', '充值') }}</span><span class="charge">{{ t('Charge', '结算') }}</span><span class="reserve">{{ t('Net reservation', '净预留') }}</span><span class="debit">{{ t('Debit', '扣减') }}</span></div>
          <div v-if="data.daily.length" class="finance-chart" aria-label="Daily finance chart">
            <div v-for="point in data.daily" :key="point.date" class="finance-chart-column" :title="`${point.date} · ${t('credit', '充值')} ${money(point.credits_milli)} · ${t('charge', '结算')} ${money(point.charged_milli)}`">
              <div class="finance-bars"><span class="credit" :style="{ height: barHeight(point.credits_milli) }" /><span class="charge" :style="{ height: barHeight(point.charged_milli) }" /><span class="reserve" :style="{ height: barHeight(point.reserved_milli) }" /><span class="debit" :style="{ height: barHeight(point.debits_milli) }" /></div>
              <small>{{ point.date.slice(8) }}</small>
            </div>
          </div>
          <div v-else class="finance-empty">{{ t('No ledger activity in this period.', '此周期暂无账本活动。') }}</div>
        </section>

        <section class="finance-section backend-section">
          <div class="finance-section-heading"><div><p class="eyebrow">Backend</p><h2>{{ t('Execution cost', '执行成本') }}</h2></div><Banknote :size="18" /></div>
          <div v-if="data.backends.length" class="backend-list">
            <div v-for="backend in data.backends" :key="backend.backend"><span><strong>{{ backendLabel(backend.backend) }}</strong><small>{{ backend.experiments }} Experiments</small></span><span><strong>{{ money(backend.charged_milli) }}</strong><small>{{ t('reserved', '预留') }} {{ money(backend.reserved_milli) }}</small></span></div>
          </div>
          <div v-else class="finance-empty">{{ t('No backend costs in this period.', '此周期暂无 backend 成本。') }}</div>
        </section>
      </div>

      <section class="finance-section project-finance-section">
        <div class="finance-section-heading"><div><p class="eyebrow">Projects</p><h2>{{ t('Capacity by Project', 'Project 额度') }}</h2></div><span>{{ data.projects.length }} Projects</span></div>
        <div class="table-scroll">
          <table class="data-table finance-project-table">
            <thead><tr><th>Project</th><th>{{ t('Base', '基础') }}</th><th>{{ t('Credits', '充值') }}</th><th>{{ t('Reserved', '预留') }}</th><th>{{ t('Charged', '结算') }}</th><th>{{ t('Debits', '扣减') }}</th><th>{{ t('Available', '可用') }}</th></tr></thead>
            <tbody><tr v-for="project in data.projects" :key="project.id"><td><strong>{{ project.name }}</strong><small>{{ project.timezone }}</small></td><td>{{ money(project.base_budget_milli) }}</td><td class="positive">{{ money(project.credits_milli) }}</td><td>{{ money(project.reserved_milli) }}</td><td>{{ money(project.charged_milli) }}</td><td>{{ money(project.debits_milli) }}</td><td><strong :class="{ negative: project.available_milli < 0 }">{{ money(project.available_milli) }}</strong></td></tr></tbody>
          </table>
        </div>
      </section>

      <section class="finance-section finance-log-section">
        <div class="finance-section-heading log-heading"><div><p class="eyebrow">{{ t('Traceability', '可追溯性') }}</p><h2>{{ activeLog === 'ledger' ? t('Budget ledger', '预算账本') : t('Finance audit', '财务审计') }}</h2></div><div class="segmented-control" role="group" :aria-label="t('Finance log view', '财务日志视图')"><button type="button" :class="{ active: activeLog === 'ledger' }" @click="activeLog = 'ledger'">{{ t('Ledger', '账本') }}</button><button type="button" :class="{ active: activeLog === 'audit' }" @click="activeLog = 'audit'">{{ t('Audit', '审计') }}</button></div></div>
        <div v-if="activeLog === 'ledger'" class="table-scroll">
          <table class="data-table finance-ledger-table">
            <thead><tr><th>{{ t('Time', '时间') }}</th><th>Project</th><th>{{ t('Entry', '条目') }}</th><th>Backend</th><th>Experiment</th><th>{{ t('Balance effect', '额度影响') }}</th><th>{{ t('Description', '说明') }}</th></tr></thead>
            <tbody>
              <tr v-for="entry in data.ledger" :key="entry.id"><td>{{ dateTime(entry.created_at) }}</td><td><strong>{{ entry.project_name }}</strong></td><td><span class="ledger-kind" :data-kind="entry.direction || entry.kind">{{ kindLabel(entry) }}</span></td><td>{{ backendLabel(entry.backend) }}</td><td><code>{{ entry.experiment_id?.slice(0, 12) ?? '—' }}</code></td><td><strong :class="entry.balance_effect_milli >= 0 ? 'positive' : 'negative'">{{ signedMoney(entry.balance_effect_milli) }}</strong></td><td>{{ entry.description }}</td></tr>
              <tr v-if="data.ledger.length === 0"><td colspan="7" class="finance-empty">{{ t('No ledger entries.', '暂无账本条目。') }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-else class="table-scroll">
          <table class="data-table finance-audit-table">
            <thead><tr><th>{{ t('Time', '时间') }}</th><th>{{ t('Action', '操作') }}</th><th>{{ t('Actor', '操作者') }}</th><th>{{ t('Target', '目标') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in data.audit" :key="item.id"><td>{{ dateTime(item.created_at) }}</td><td><strong>{{ actionLabel(item.action) }}</strong><small>{{ item.action }}</small></td><td>{{ item.actor_type }}<small>{{ item.actor_id || '—' }}</small></td><td>{{ item.target_type }}<small>{{ item.target_id || '—' }}</small></td></tr>
              <tr v-if="data.audit.length === 0"><td colspan="4" class="finance-empty">{{ t('No finance-related audit events.', '暂无财务相关审计事件。') }}</td></tr>
            </tbody>
          </table>
          <p class="audit-scope-note">{{ t('Audit events are organization-wide and intentionally omit metadata that may contain operational details.', '审计事件为组织范围，并有意省略可能包含运行细节的 metadata。') }}</p>
        </div>
      </section>
    </template>
  </section>

  <div v-if="adjustmentDialog" class="modal-backdrop" @click.self="closeAdjustment">
    <section class="modal finance-adjustment-modal" role="dialog" aria-modal="true" :aria-label="adjustmentDialogTitle">
      <header><div><p class="eyebrow">{{ t('Project budget', 'Project 预算') }}</p><h2>{{ adjustmentDialogTitle }}</h2></div><button class="icon-button" type="button" :title="t('Close', '关闭')" :disabled="adjustmentBusy" @click="closeAdjustment"><X :size="17" /></button></header>
      <form class="dialog-form" @submit.prevent="submitAdjustment">
        <div class="adjustment-warning"><ShieldCheck :size="18" /><p>{{ t('This records an internal budget change for one Project only. It does not transfer money to a Provider account.', '这里只记录一个 Project 的内部预算变更，不会向 Provider 账户转账。') }}</p></div>
        <label>Project<select v-model="adjustmentForm.projectID" required><option v-for="project in activeProjects" :key="project.id" :value="project.id">{{ project.name }}</option></select></label>
        <fieldset class="adjustment-direction"><legend>{{ t('Budget change', '预算变更') }}</legend><button type="button" :class="{ active: adjustmentForm.direction === 'credit' }" @click="adjustmentForm.direction = 'credit'">{{ t('Credit', '充值') }}</button><button type="button" :class="{ active: adjustmentForm.direction === 'debit' }" @click="adjustmentForm.direction = 'debit'">{{ t('Debit', '扣减') }}</button></fieldset>
        <label>{{ t('Amount (CNY)', '金额 (CNY)') }}<input v-model="adjustmentForm.amountCNY" type="number" min="0.001" max="1000000000" step="0.001" inputmode="decimal" required /></label>
        <label>{{ t('Reason', '原因') }}<textarea v-model="adjustmentForm.reason" rows="3" minlength="3" maxlength="255" required :placeholder="t('Example: approved August Project budget', '例如：已批准的 8 月 Project 预算')" /></label>
        <div class="adjustment-preview"><span>{{ t('Available budget change', '可用预算变更') }}</span><strong :class="adjustmentForm.direction === 'credit' ? 'positive' : 'negative'">{{ adjustmentForm.direction === 'credit' ? '+' : '-' }}{{ money(adjustmentAmountMilli) }}</strong></div>
        <label class="confirmation-row"><input v-model="adjustmentForm.confirmed" type="checkbox" /><span>{{ t('I confirm this immutable entry applies to the selected Project in its current billing period.', '我确认此不可变条目应用于所选 Project 的当前计费周期。') }}</span></label>
        <div v-if="adjustmentError" class="form-error" role="alert">{{ adjustmentError }}</div>
        <button class="primary-button" type="submit" :disabled="adjustmentBusy || !adjustmentForm.confirmed"><LoaderCircle v-if="adjustmentBusy" :size="16" class="spinning" /><Plus v-else :size="16" />{{ adjustmentForm.direction === 'credit' ? t('Add budget', '确认充值') : t('Record reduction', '确认扣减') }}</button>
      </form>
    </section>
  </div>
</template>

<style scoped>
.finance-page { width: 100%; min-width: 0; padding: 30px clamp(18px, 3vw, 42px) 48px; }
.finance-alert { margin: 0 0 16px; }
.finance-heading { display: flex; align-items: flex-end; justify-content: space-between; gap: 24px; }
.finance-heading h1 { margin: 0; font-size: 24px; line-height: 31px; }
.finance-heading > div:first-child > p:last-child { margin: 5px 0 0; color: #69736c; font-size: 13px; }
.finance-actions { display: flex; align-items: flex-end; gap: 9px; }
.compact-field { gap: 4px; }
.compact-field span { font-size: 10px; }
.compact-field input, .compact-field select { width: 145px; min-height: 34px; height: 34px; }
.compact-field select { width: 170px; }
.finance-boundary { margin-top: 22px; padding: 11px 14px; display: flex; align-items: flex-start; gap: 10px; color: #526159; background: #eef4f0; border: 1px solid #d4dfd8; border-radius: 6px; font-size: 11px; line-height: 17px; }
.finance-boundary svg { flex: 0 0 auto; margin-top: 1px; color: #277057; }
.finance-boundary strong { display: block; color: #2c3c33; }
.finance-loading { min-height: 220px; display: flex; align-items: center; justify-content: center; gap: 9px; color: #748078; font-size: 12px; }
.finance-metrics { margin-top: 20px; display: grid; grid-template-columns: repeat(6, minmax(0, 1fr)); background: #fff; border: 1px solid #dce2dd; border-radius: 7px; overflow: hidden; }
.finance-metrics > div { min-width: 0; min-height: 108px; padding: 16px; border-right: 1px solid #e3e8e4; }
.finance-metrics > div:last-child { border-right: 0; }
.finance-metrics span, .finance-metrics strong, .finance-metrics small { display: flex; align-items: center; gap: 7px; }
.finance-metrics span { color: #657169; font-size: 11px; font-weight: 650; }
.finance-metrics strong { margin-top: 13px; display: block; overflow-wrap: anywhere; font-size: 19px; line-height: 24px; }
.finance-metrics small { margin-top: 5px; color: #8a938d; font-size: 10px; }
.finance-metrics .available { box-shadow: inset 0 3px #4da478; }
.finance-analysis-grid { margin-top: 20px; display: grid; grid-template-columns: minmax(0, 1.65fr) minmax(300px, .75fr); gap: 20px; }
.finance-section { min-width: 0; margin-top: 20px; background: #fff; border: 1px solid #dce2dd; border-radius: 7px; }
.finance-analysis-grid .finance-section { margin-top: 0; }
.finance-section-heading { min-height: 64px; padding: 13px 15px; display: flex; align-items: center; justify-content: space-between; gap: 18px; border-bottom: 1px solid #e3e8e4; }
.finance-section-heading h2 { margin: 0; font-size: 15px; }
.finance-section-heading > span { color: #7a847d; font-size: 11px; }
.chart-legend { padding: 10px 15px 0; display: flex; flex-wrap: wrap; gap: 14px; color: #758078; font-size: 10px; }
.chart-legend span::before { content: ''; width: 7px; height: 7px; margin-right: 5px; display: inline-block; border-radius: 2px; }
.chart-legend .credit::before, .finance-bars .credit { background: #4ba277; }
.chart-legend .charge::before, .finance-bars .charge { background: #d56f61; }
.chart-legend .reserve::before, .finance-bars .reserve { background: #d4a348; }
.chart-legend .debit::before, .finance-bars .debit { background: #8d6ea8; }
.finance-chart { height: 174px; padding: 16px 15px 12px; display: flex; align-items: flex-end; gap: 6px; overflow-x: auto; }
.finance-chart-column { min-width: 28px; height: 144px; flex: 1 0 28px; display: grid; grid-template-rows: 124px 20px; align-items: end; }
.finance-bars { height: 124px; display: flex; align-items: flex-end; justify-content: center; gap: 2px; border-bottom: 1px solid #dfe4df; }
.finance-bars span { width: 5px; min-height: 0; border-radius: 2px 2px 0 0; }
.finance-chart-column small { padding-top: 5px; color: #89928c; font-size: 9px; text-align: center; }
.backend-list { display: grid; }
.backend-list > div { min-height: 72px; padding: 13px 15px; display: flex; align-items: center; justify-content: space-between; gap: 15px; border-bottom: 1px solid #e7ebe8; }
.backend-list > div:last-child { border-bottom: 0; }
.backend-list span { min-width: 0; display: grid; gap: 4px; }
.backend-list span:last-child { text-align: right; }
.backend-list strong { color: #29342e; font-size: 12px; }
.backend-list small { color: #838d86; font-size: 10px; }
.finance-empty { min-height: 90px; padding: 20px; color: #818b84; text-align: center; font-size: 11px; }
.finance-project-table td:first-child strong, .finance-audit-table strong { display: block; }
.finance-project-table td:first-child small, .finance-audit-table small { margin-top: 3px; display: block; color: #89928c; font-size: 9px; }
.positive { color: #247052 !important; }
.negative { color: #a43f39 !important; }
.log-heading .segmented-control { flex: 0 0 auto; }
.finance-ledger-table td:nth-child(7) { min-width: 200px; white-space: normal; }
.finance-ledger-table code { white-space: nowrap; }
.ledger-kind { padding: 4px 7px; display: inline-flex; border-radius: 4px; color: #4e5c54; background: #edf1ee; font-size: 10px; font-weight: 650; }
.ledger-kind[data-kind='credit'] { color: #1e674b; background: #e6f4ec; }
.ledger-kind[data-kind='debit'], .ledger-kind[data-kind='charge'] { color: #914039; background: #f9e9e7; }
.audit-scope-note { margin: 0; padding: 10px 15px; color: #838d86; background: #f8faf8; border-top: 1px solid #e5e9e6; font-size: 10px; }
.finance-adjustment-modal { width: min(520px, 100%); }
.adjustment-warning { padding: 11px 12px; display: flex; align-items: flex-start; gap: 9px; color: #5a665f; background: #f0f4f1; border: 1px solid #dbe3dd; border-radius: 5px; font-size: 11px; line-height: 17px; }
.adjustment-warning svg { flex: 0 0 auto; color: #277057; }
.adjustment-warning p { margin: 0; }
.adjustment-direction { margin: 0; padding: 0; display: grid; grid-template-columns: 1fr 1fr; border: 1px solid #cbd3cd; border-radius: 5px; overflow: hidden; }
.adjustment-direction legend { position: absolute; width: 1px; height: 1px; overflow: hidden; clip: rect(0 0 0 0); }
.adjustment-direction button { min-height: 38px; color: #5d6861; background: #fff; border: 0; cursor: pointer; }
.adjustment-direction button + button { border-left: 1px solid #d8dfda; }
.adjustment-direction button.active { color: #185d46; background: #e8f2ec; font-weight: 680; }
.adjustment-preview { padding: 10px 12px; display: flex; align-items: center; justify-content: space-between; background: #f8faf8; border: 1px solid #e1e6e2; border-radius: 5px; font-size: 11px; }
.adjustment-preview strong { font-size: 14px; }
.confirmation-row { min-height: 34px; flex-direction: row; align-items: flex-start; gap: 9px; line-height: 17px; }
.confirmation-row input { width: 16px; height: 16px; min-height: 0; margin: 1px 0 0; flex: 0 0 auto; accent-color: #c4621a; }
@media (max-width: 1100px) {
  .finance-heading { align-items: flex-start; flex-direction: column; }
  .finance-actions { width: 100%; flex-wrap: wrap; }
  .finance-metrics { grid-template-columns: repeat(3, minmax(0, 1fr)); }
  .finance-metrics > div:nth-child(3) { border-right: 0; }
  .finance-metrics > div:nth-child(-n + 3) { border-bottom: 1px solid #e3e8e4; }
}
@media (max-width: 760px) {
  .finance-page { padding: 21px 14px 34px; }
  .finance-actions { display: grid; grid-template-columns: 1fr 1fr auto; }
  .compact-field input, .compact-field select { width: 100%; }
  .finance-actions .primary-button { grid-column: 1 / -1; }
  .finance-metrics { grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .finance-metrics > div { border-bottom: 1px solid #e3e8e4; }
  .finance-metrics > div:nth-child(odd) { border-right: 1px solid #e3e8e4; }
  .finance-metrics > div:nth-child(even) { border-right: 0; }
  .finance-metrics > div:nth-last-child(-n + 2) { border-bottom: 0; }
  .finance-analysis-grid { grid-template-columns: 1fr; }
  .log-heading { align-items: stretch; flex-direction: column; }
  .log-heading .segmented-control { width: 100%; }
}
</style>
