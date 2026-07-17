<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { Bell, CheckCircle2, CircleAlert, LoaderCircle, Mail, Send, Settings, X } from '@lucide/vue'
import { APIError, api, type NotificationDelivery, type NotificationSetting } from '../api'

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
    handleError(caught, 'Could not load SMTP notification state.')
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
    else formError.value = caught instanceof APIError ? caught.message : 'Could not save SMTP settings.'
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
    handleError(caught, 'Could not queue SMTP test.')
  } finally {
    testing.value = false
  }
}

function dateTime(value?: string) {
  if (!value) return 'Not set'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function label(value: string) {
  return value.replaceAll('_', ' ')
}

watch(() => props.active, (active) => {
  if (active && !initialized.value) void load()
}, { immediate: true })
</script>

<template>
  <section class="notification-page">
    <div v-if="error" class="page-alert notification-alert" role="alert">{{ error }}<button type="button" title="Dismiss" @click="error = ''"><X :size="16" /></button></div>

    <header class="notification-heading">
      <div>
        <p class="eyebrow">Durable outbox</p>
        <h2>Critical notifications</h2>
        <p>{{ setting?.configured ? `${setting.host}:${setting.port}` : 'SMTP is not configured' }}</p>
      </div>
      <div class="notification-actions">
        <span class="state-badge" :data-state="setting?.enabled ? setting.status : 'disabled'"><span />{{ setting?.enabled ? label(setting.status ?? 'ready') : 'disabled' }}</span>
        <button class="secondary-button" type="button" :disabled="!setting?.configured || !setting.enabled || testing" @click="sendTest"><LoaderCircle v-if="testing" :size="16" class="spinning" /><Send v-else :size="16" />Send test</button>
        <button class="primary-button" type="button" @click="openSettings"><Settings :size="16" />SMTP settings</button>
      </div>
    </header>

    <div v-if="loading && !setting" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>Loading notification state</span></div>
    <template v-else>
      <section class="notification-metrics">
        <div><span><Bell :size="16" />Pending</span><strong>{{ pending }}</strong><small>Queued or sending</small></div>
        <div><span><CheckCircle2 :size="16" />Delivered</span><strong>{{ sent }}</strong><small>At-least-once SMTP</small></div>
        <div><span><CircleAlert :size="16" />Failed</span><strong>{{ failed }}</strong><small>After five attempts</small></div>
        <div><span><Mail :size="16" />Recipients</span><strong>{{ setting?.recipients.length ?? 0 }}</strong><small>{{ setting?.password_configured ? 'Credential encrypted' : 'No SMTP credential' }}</small></div>
      </section>

      <section class="notification-connection-band">
        <div><span>Transport</span><strong>{{ setting?.tls_mode ?? 'Not set' }}</strong></div>
        <div><span>Sender</span><strong>{{ setting?.from_address ?? 'Not set' }}</strong></div>
        <div><span>Last test</span><strong>{{ dateTime(setting?.last_tested_at) }}</strong></div>
        <div><span>Last error</span><strong>{{ setting?.last_error ?? 'None' }}</strong></div>
      </section>

      <section class="notification-workspace">
        <div class="section-heading"><div><h2>Delivery history</h2><p>Critical execution and shutdown events recorded in PostgreSQL.</p></div><button class="icon-button" type="button" title="Refresh notifications" :disabled="loading" @click="load"><LoaderCircle v-if="loading" :size="16" class="spinning" /><Bell v-else :size="16" /></button></div>
        <div v-if="deliveries.length" class="table-scroll">
          <table class="data-table notification-table">
            <thead><tr><th>State</th><th>Severity</th><th>Subject</th><th>Kind</th><th>Attempts</th><th>Created</th><th>Sent</th></tr></thead>
            <tbody><tr v-for="item in deliveries" :key="item.id"><td><span class="state-badge" :data-state="item.state"><span />{{ label(item.state) }}</span></td><td><span class="severity-chip" :data-severity="item.severity">{{ item.severity }}</span></td><td><strong>{{ item.subject }}</strong><small v-if="item.last_error">{{ item.last_error }}</small></td><td><code>{{ item.kind }}</code></td><td>{{ item.attempts }}</td><td>{{ dateTime(item.created_at) }}</td><td>{{ dateTime(item.sent_at) }}</td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Bell :size="21" /></span><h3>No notification events</h3><p>Critical scheduler and watchdog events will appear here.</p></div>
      </section>
    </template>
  </section>

  <div v-if="dialog" class="modal-backdrop" @click.self="closeSettings">
    <section class="modal notification-modal" role="dialog" aria-modal="true" aria-label="SMTP notification settings">
      <header><div><p class="eyebrow">Encrypted credential</p><h2>SMTP settings</h2></div><button class="icon-button" type="button" title="Close SMTP settings" :disabled="saving" @click="closeSettings"><X :size="17" /></button></header>
      <form class="dialog-form smtp-form" @submit.prevent="saveSettings">
        <label class="toggle-row"><input v-model="form.enabled" type="checkbox" /><span>Enable delivery</span></label>
        <div class="form-grid"><label>SMTP host<input v-model="form.host" required maxlength="255" spellcheck="false" /></label><label>Port<input v-model.number="form.port" type="number" required min="1" max="65535" /></label></div>
        <label>TLS mode<select v-model="form.tlsMode"><option value="starttls">STARTTLS</option><option value="tls">Implicit TLS</option></select></label>
        <label>Username<input v-model="form.username" maxlength="320" autocomplete="username" spellcheck="false" /></label>
        <label>Password<input v-model="form.password" type="password" maxlength="4096" autocomplete="new-password" :disabled="form.clearPassword" /><small>{{ setting?.password_configured ? 'Leave blank to retain the encrypted password.' : 'Required when a username is set.' }}</small></label>
        <label v-if="setting?.password_configured" class="toggle-row"><input v-model="form.clearPassword" type="checkbox" /><span>Remove stored password</span></label>
        <label>From address<input v-model="form.fromAddress" type="email" required maxlength="320" spellcheck="false" /></label>
        <label>Recipients<textarea v-model="form.recipients" required rows="3" placeholder="owner@example.com" spellcheck="false" /></label>
        <div v-if="formError" class="form-error" role="alert">{{ formError }}</div>
        <button class="primary-button" type="submit" :disabled="saving"><LoaderCircle v-if="saving" :size="16" class="spinning" /><Settings v-else :size="16" />Save settings</button>
      </form>
    </section>
  </div>
</template>
