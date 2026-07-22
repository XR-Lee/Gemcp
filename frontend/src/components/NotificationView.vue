<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Bell, CheckCircle2, CircleAlert, LoaderCircle, Mail, Send, Settings, X } from '@lucide/vue'
import { APIError, api, type NotificationDelivery, type NotificationSetting } from '../api'
import { localizedState, useI18n } from '../i18n'

const props = defineProps<{ active: boolean }>()
const emit = defineEmits<{ unauthorized: [] }>()

const setting = ref<NotificationSetting | null>(null)
const deliveries = ref<NotificationDelivery[]>([])
const loading = ref(false)
const initialized = ref(false)
const error = ref('')
const dialog = ref(false)
const saving = ref(false)
const testing = ref(false)
const formError = ref('')
const form = reactive({
  enabled: true,
  host: '',
  port: 587,
  tlsMode: 'starttls' as 'starttls' | 'tls',
  username: '',
  password: '',
  clearPassword: false,
  fromAddress: '',
  recipients: '',
})
const { languageTag, t } = useI18n()

const pending = computed(() => deliveries.value.filter((item) => ['pending', 'sending'].includes(item.state)).length)
const failed = computed(() => deliveries.value.filter((item) => item.state === 'failed').length)
const sent = computed(() => deliveries.value.filter((item) => item.state === 'sent').length)

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

async function load() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    const [loadedSetting, loadedDeliveries] = await Promise.all([api.notificationSetting(), api.notifications()])
    setting.value = loadedSetting
    deliveries.value = loadedDeliveries
    initialized.value = true
  } catch (caught) {
    handleError(caught, t('Could not load SMTP notification state.', '无法加载 SMTP 通知状态。'))
  } finally {
    loading.value = false
  }
}

function openSettings() {
  const current = setting.value
  form.enabled = current?.enabled ?? true
  form.host = current?.host ?? ''
  form.port = current?.port ?? 587
  form.tlsMode = current?.tls_mode ?? 'starttls'
  form.username = current?.username ?? ''
  form.password = ''
  form.clearPassword = false
  form.fromAddress = current?.from_address ?? ''
  form.recipients = (current?.recipients ?? []).join('\n')
  formError.value = ''
  dialog.value = true
}

function closeSettings() {
  if (saving.value) return
  form.password = ''
  formError.value = ''
  dialog.value = false
}

async function saveSettings() {
  saving.value = true
  formError.value = ''
  try {
    setting.value = await api.configureNotifications({
      enabled: form.enabled,
      host: form.host.trim(),
      port: Number(form.port),
      tls_mode: form.tlsMode,
      username: form.username.trim(),
      password: form.password,
      clear_password: form.clearPassword,
      from_address: form.fromAddress.trim(),
      recipients: form.recipients.split(/[\n,]/).map((item) => item.trim()).filter(Boolean),
    })
    form.password = ''
    dialog.value = false
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else formError.value = caught instanceof APIError ? caught.message : t('Could not save SMTP settings.', '无法保存 SMTP 设置。')
  } finally {
    form.password = ''
    saving.value = false
  }
}

async function sendTest() {
  testing.value = true
  error.value = ''
  try {
    const queued = await api.testNotification()
    deliveries.value = [queued, ...deliveries.value.filter((item) => item.id !== queued.id)]
  } catch (caught) {
    handleError(caught, t('Could not queue SMTP test.', '无法将 SMTP 测试加入队列。'))
  } finally {
    testing.value = false
  }
}

function dateTime(value?: string) {
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function label(value: string) {
  return localizedState(value)
}

watch(() => props.active, (active) => {
  if (active && !initialized.value) void load()
}, { immediate: true })
</script>

<template>
  <section class="notification-page">
    <div v-if="error" class="page-alert notification-alert" role="alert">{{ error }}<button type="button" :title="t('Dismiss', '关闭')" @click="error = ''"><X :size="16" /></button></div>

    <header class="notification-heading">
      <div>
        <p class="eyebrow">{{ t('Durable outbox', '持久化发件箱') }}</p>
        <h2>{{ t('Critical notifications', '关键通知') }}</h2>
        <p>{{ setting?.configured ? `${setting.host}:${setting.port}` : t('SMTP is not configured', '尚未配置 SMTP') }}</p>
      </div>
      <div class="notification-actions">
        <span class="state-badge" :data-state="setting?.enabled ? setting.status : 'disabled'"><span />{{ setting?.enabled ? label(setting.status ?? 'ready') : label('disabled') }}</span>
        <button class="secondary-button" type="button" :disabled="!setting?.configured || !setting.enabled || testing" @click="sendTest"><LoaderCircle v-if="testing" :size="16" class="spinning" /><Send v-else :size="16" />{{ t('Send test', '发送测试') }}</button>
        <button class="primary-button" type="button" @click="openSettings"><Settings :size="16" />{{ t('SMTP settings', 'SMTP 设置') }}</button>
      </div>
    </header>

    <div v-if="loading && !setting" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>{{ t('Loading notification state', '正在加载通知状态') }}</span></div>
    <template v-else>
      <section class="notification-metrics">
        <div><span><Bell :size="16" />{{ t('Pending', '等待中') }}</span><strong>{{ pending }}</strong><small>{{ t('Queued or sending', '排队中或正在发送') }}</small></div>
        <div><span><CheckCircle2 :size="16" />{{ t('Delivered', '已送达') }}</span><strong>{{ sent }}</strong><small>{{ t('At-least-once SMTP', 'SMTP 至少一次投递') }}</small></div>
        <div><span><CircleAlert :size="16" />{{ t('Failed', '失败') }}</span><strong>{{ failed }}</strong><small>{{ t('After five attempts', '五次尝试后') }}</small></div>
        <div><span><Mail :size="16" />{{ t('Recipients', '收件人') }}</span><strong>{{ setting?.recipients.length ?? 0 }}</strong><small>{{ setting?.password_configured ? t('Credential encrypted', '凭据已加密') : t('No SMTP credential', '无 SMTP 凭据') }}</small></div>
      </section>

      <section class="notification-connection-band">
        <div><span>{{ t('Transport', '传输') }}</span><strong>{{ setting?.tls_mode ?? t('Not set', '未设置') }}</strong></div>
        <div><span>{{ t('Sender', '发件人') }}</span><strong>{{ setting?.from_address ?? t('Not set', '未设置') }}</strong></div>
        <div><span>{{ t('Last test', '最近测试') }}</span><strong>{{ dateTime(setting?.last_tested_at) }}</strong></div>
        <div><span>{{ t('Last error', '最近错误') }}</span><strong>{{ setting?.last_error ?? t('None', '无') }}</strong></div>
      </section>

      <section class="notification-workspace">
        <div class="section-heading"><div><h2>{{ t('Delivery history', '投递历史') }}</h2><p>{{ t('Critical execution and shutdown events recorded in PostgreSQL.', '记录在 PostgreSQL 中的关键执行和关停事件。') }}</p></div><button class="icon-button" type="button" :title="t('Refresh notifications', '刷新通知')" :disabled="loading" @click="load"><LoaderCircle v-if="loading" :size="16" class="spinning" /><Bell v-else :size="16" /></button></div>
        <div v-if="deliveries.length" class="table-scroll">
          <table class="data-table notification-table">
            <thead><tr><th>{{ t('State', '状态') }}</th><th>{{ t('Severity', '严重性') }}</th><th>{{ t('Subject', '主题') }}</th><th>{{ t('Kind', '类型') }}</th><th>{{ t('Attempts', '尝试次数') }}</th><th>{{ t('Created', '创建时间') }}</th><th>{{ t('Sent', '发送时间') }}</th></tr></thead>
            <tbody><tr v-for="item in deliveries" :key="item.id"><td><span class="state-badge" :data-state="item.state"><span />{{ label(item.state) }}</span></td><td><span class="severity-chip" :data-severity="item.severity">{{ item.severity }}</span></td><td><strong>{{ item.subject }}</strong><small v-if="item.last_error">{{ item.last_error }}</small></td><td><code>{{ item.kind }}</code></td><td>{{ item.attempts }}</td><td>{{ dateTime(item.created_at) }}</td><td>{{ dateTime(item.sent_at) }}</td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Bell :size="21" /></span><h3>{{ t('No notification events', '暂无通知事件') }}</h3><p>{{ t('Critical scheduler and watchdog events will appear here.', '关键调度器和 Watchdog 事件会显示在这里。') }}</p></div>
      </section>
    </template>
  </section>

  <div v-if="dialog" class="modal-backdrop" @click.self="closeSettings">
    <section class="modal notification-modal" role="dialog" aria-modal="true" :aria-label="t('SMTP notification settings', 'SMTP 通知设置')">
      <header><div><p class="eyebrow">{{ t('Encrypted credential', '加密凭据') }}</p><h2>{{ t('SMTP settings', 'SMTP 设置') }}</h2></div><button class="icon-button" type="button" :title="t('Close SMTP settings', '关闭 SMTP 设置')" :disabled="saving" @click="closeSettings"><X :size="17" /></button></header>
      <form class="dialog-form smtp-form" @submit.prevent="saveSettings">
        <label class="toggle-row"><input v-model="form.enabled" type="checkbox" /><span>{{ t('Enable delivery', '启用投递') }}</span></label>
        <div class="form-grid"><label>SMTP {{ t('host', '主机') }}<input v-model="form.host" required maxlength="255" spellcheck="false" /></label><label>{{ t('Port', '端口') }}<input v-model.number="form.port" type="number" required min="1" max="65535" /></label></div>
        <label>TLS {{ t('mode', '模式') }}<select v-model="form.tlsMode"><option value="starttls">STARTTLS</option><option value="tls">{{ t('Implicit TLS', '隐式 TLS') }}</option></select></label>
        <label>{{ t('Username', '用户名') }}<input v-model="form.username" maxlength="320" autocomplete="username" spellcheck="false" /></label>
        <label>{{ t('Password', '密码') }}<input v-model="form.password" type="password" maxlength="4096" autocomplete="new-password" :disabled="form.clearPassword" /><small>{{ setting?.password_configured ? t('Leave blank to retain the encrypted password.', '留空以保留已加密的密码。') : t('Required when a username is set.', '设置用户名时必须填写。') }}</small></label>
        <label v-if="setting?.password_configured" class="toggle-row"><input v-model="form.clearPassword" type="checkbox" /><span>{{ t('Remove stored password', '删除已存密码') }}</span></label>
        <label>{{ t('From address', '发件地址') }}<input v-model="form.fromAddress" type="email" required maxlength="320" spellcheck="false" /></label>
        <label>{{ t('Recipients', '收件人') }}<textarea v-model="form.recipients" required rows="3" placeholder="owner@example.com" spellcheck="false" /></label>
        <div v-if="formError" class="form-error" role="alert">{{ formError }}</div>
        <button class="primary-button" type="submit" :disabled="saving"><LoaderCircle v-if="saving" :size="16" class="spinning" /><Settings v-else :size="16" />{{ t('Save settings', '保存设置') }}</button>
      </form>
    </section>
  </div>
</template>
