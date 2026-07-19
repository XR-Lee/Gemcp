<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
import {
  BookOpen,
  Bot,
  Check,
  Clipboard,
  Clock3,
  Download,
  ExternalLink,
  FileDown,
  KeyRound,
  Link2,
  LoaderCircle,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X,
} from '@lucide/vue'
import {
  APIError,
  api,
  type AgentEnrollment,
  type AgentEnrollmentIssue,
  type AgentScope,
  type AgentToken,
  type AgentTokenIssue,
  type AgentTokenList,
  type Project,
} from '../api'

const props = defineProps<{ active: boolean; project: Project | null }>()
const emit = defineEmits<{ unauthorized: [] }>()

const data = ref<AgentTokenList | null>(null)
const loadedProjectID = ref('')
const loading = ref(false)
const error = ref('')
const setupDialog = ref(false)
const settingUp = ref(false)
const setupError = ref('')
const setupReveal = ref<AgentEnrollmentIssue | null>(null)
const issueDialog = ref(false)
const issuing = ref(false)
const issueError = ref('')
const reveal = ref<AgentTokenIssue | null>(null)
const copied = ref('')
const guideDialog = ref(false)
const guideError = ref('')
const revokeTarget = ref<AgentToken | null>(null)
const revoking = ref(false)
const enrollmentRevokeTarget = ref<AgentEnrollment | null>(null)
const revokingEnrollment = ref(false)
const setupForm = reactive({
  label: '',
  expiration: '90',
  linkExpiration: '30',
  scopes: { read: true, submit: true, cancel: true } as Record<AgentScope, boolean>,
})
const form = reactive({
  label: '',
  expiration: '90',
  scopes: { read: true, submit: true, cancel: true } as Record<AgentScope, boolean>,
})

const activeTokens = computed(() => data.value?.tokens.filter((item) => item.status === 'active').length ?? 0)
const activeEnrollments = computed(() => data.value?.enrollments.filter((item) => item.status === 'pending' || item.status === 'claimed').length ?? 0)
const expiringTokens = computed(() => {
  const threshold = Date.now() + 30 * 24 * 60 * 60 * 1000
  return data.value?.tokens.filter((item) => item.status === 'active' && item.expires_at && new Date(item.expires_at).getTime() <= threshold).length ?? 0
})
const selectedScopes = computed(() => (Object.keys(form.scopes) as AgentScope[]).filter((scope) => form.scopes[scope]))
const selectedSetupScopes = computed(() => (Object.keys(setupForm.scopes) as AgentScope[]).filter((scope) => setupForm.scopes[scope]))
const setupAgentMessage = computed(() => setupReveal.value
  ? `Set up Gemcp for this Pi Agent using the one-time link below. Complete the automated install, tool verification, and credential storage yourself. Do not print or forward the link, and do not pass the complete link to a Web-fetch, search, or preview tool.\n\n${setupReveal.value.setup_url}`
  : '')
const configJSON = computed(() => reveal.value ? JSON.stringify(reveal.value.mcp_config, null, 2) : '')
const templateJSON = computed(() => data.value?.config_template ? JSON.stringify(data.value.config_template, null, 2) : '')
const ownerGuideURL = computed(() => guideURL('owner-mcp.md'))
const agentGuideURL = computed(() => guideURL('agent-mcp.md'))

function guideURL(filename: string) {
  const mcpURL = data.value?.mcp_url
  if (!mcpURL) return ''
  try {
    return new URL(`/docs/${filename}`, mcpURL).toString()
  } catch {
    return ''
  }
}

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

async function load() {
  const projectID = props.project?.id
  if (!projectID || loading.value) return
  loading.value = true
  error.value = ''
  try {
    const loaded = await api.agentTokens(projectID)
    loaded.enrollments ??= []
    if (props.project?.id === projectID) {
      data.value = loaded
      loadedProjectID.value = projectID
    }
  } catch (caught) {
    if (props.project?.id === projectID) handleError(caught, 'Could not load Agent access state.')
  } finally {
    loading.value = false
    if (props.active && props.project?.id && props.project.id !== projectID) void load()
  }
}

function openSetup() {
  setupForm.label = ''
  setupForm.expiration = '90'
  setupForm.linkExpiration = '30'
  setupForm.scopes.read = true
  setupForm.scopes.submit = true
  setupForm.scopes.cancel = true
  setupError.value = ''
  setupDialog.value = true
}

function closeSetup() {
  if (settingUp.value) return
  setupForm.label = ''
  setupError.value = ''
  setupDialog.value = false
}

async function createSetupLink() {
  if (!props.project) return
  if (!selectedSetupScopes.value.length) {
    setupError.value = 'Select at least one scope.'
    return
  }
  settingUp.value = true
  setupError.value = ''
  try {
    const neverExpires = setupForm.expiration === 'never'
    const result = await api.issueAgentEnrollment(props.project.id, {
      label: setupForm.label.trim(),
      scopes: selectedSetupScopes.value,
      ...(neverExpires ? {} : { expires_in_days: Number(setupForm.expiration) }),
      never_expires: neverExpires,
      setup_expires_in_minutes: Number(setupForm.linkExpiration),
    })
    if (data.value) {
      data.value.enrollments = [result.enrollment, ...data.value.enrollments.filter((item) => item.id !== result.enrollment.id)]
    }
    setupForm.label = ''
    setupDialog.value = false
    setupReveal.value = result
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else setupError.value = caught instanceof APIError ? caught.message : 'Could not create Pi setup link.'
  } finally {
    settingUp.value = false
  }
}

function closeSetupReveal() {
  setupReveal.value = null
  copied.value = ''
}

function openIssue() {
  form.label = ''
  form.expiration = '90'
  form.scopes.read = true
  form.scopes.submit = true
  form.scopes.cancel = true
  issueError.value = ''
  issueDialog.value = true
}

function closeIssue() {
  if (issuing.value) return
  form.label = ''
  issueError.value = ''
  issueDialog.value = false
}

async function issueToken() {
  if (!props.project) return
  if (!selectedScopes.value.length) {
    issueError.value = 'Select at least one scope.'
    return
  }
  issuing.value = true
  issueError.value = ''
  try {
    const neverExpires = form.expiration === 'never'
    const result = await api.issueAgentToken(props.project.id, {
      label: form.label.trim(),
      scopes: selectedScopes.value,
      ...(neverExpires ? {} : { expires_in_days: Number(form.expiration) }),
      never_expires: neverExpires,
    })
    if (data.value) {
      data.value.tokens = [result.token, ...data.value.tokens.filter((item) => item.id !== result.token.id)]
    }
    form.label = ''
    issueDialog.value = false
    reveal.value = result
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else issueError.value = caught instanceof APIError ? caught.message : 'Could not generate Agent token.'
  } finally {
    issuing.value = false
  }
}

function closeReveal() {
  reveal.value = null
  copied.value = ''
}

async function copy(value: string, name: string) {
  try {
    await navigator.clipboard.writeText(value)
    copied.value = name
    window.setTimeout(() => {
      if (copied.value === name) copied.value = ''
    }, 1600)
  } catch {
    error.value = 'Clipboard access was denied.'
  }
}

function openGuide() {
  guideError.value = ''
  guideDialog.value = true
}

async function copyAgentGuide() {
  guideError.value = ''
  try {
    const response = await fetch(agentGuideURL.value, { credentials: 'omit' })
    if (!response.ok) throw new Error(`Guide request failed (${response.status})`)
    await copy(await response.text(), 'guide')
  } catch (caught) {
    guideError.value = caught instanceof Error ? caught.message : 'Unable to copy the Agent guide.'
  }
}

function downloadConfig() {
  if (!reveal.value) return
  const blob = new Blob([configJSON.value + '\n'], { type: 'application/json' })
  const url = URL.createObjectURL(blob)
  const link = document.createElement('a')
  link.href = url
  link.download = reveal.value.config_file_name
  document.body.appendChild(link)
  link.click()
  link.remove()
  window.setTimeout(() => URL.revokeObjectURL(url), 0)
}

function closeEnrollmentRevoke() {
  if (!revokingEnrollment.value) enrollmentRevokeTarget.value = null
}

async function revokeEnrollment() {
  if (!props.project || !enrollmentRevokeTarget.value) return
  const target = enrollmentRevokeTarget.value
  revokingEnrollment.value = true
  error.value = ''
  try {
    const revoked = await api.revokeAgentEnrollment(props.project.id, target.id)
    if (data.value) {
      data.value.enrollments = data.value.enrollments.map((item) => item.id === revoked.id ? revoked : item)
      if (target.agent_token_id) {
        data.value.tokens = data.value.tokens.map((item) => item.id === target.agent_token_id
          ? { ...item, status: 'revoked', updated_at: new Date().toISOString() }
          : item)
      }
    }
    enrollmentRevokeTarget.value = null
  } catch (caught) {
    handleError(caught, 'Could not revoke Pi setup link.')
  } finally {
    revokingEnrollment.value = false
  }
}

function closeRevoke() {
  if (!revoking.value) revokeTarget.value = null
}

async function revokeToken() {
  if (!props.project || !revokeTarget.value) return
  const target = revokeTarget.value
  revoking.value = true
  error.value = ''
  try {
    const revoked = await api.revokeAgentToken(props.project.id, target.id)
    if (data.value) {
      data.value.tokens = data.value.tokens.map((item) => item.id === revoked.id ? revoked : item)
    }
    revokeTarget.value = null
  } catch (caught) {
    handleError(caught, 'Could not revoke Agent token.')
  } finally {
    revoking.value = false
  }
}

function dateTime(value?: string) {
  if (!value) return 'Never'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function statusLabel(value: string) {
  return value.replaceAll('_', ' ')
}

let enrollmentRefreshTimer: number | undefined
onMounted(() => {
  enrollmentRefreshTimer = window.setInterval(() => {
    if (props.active && activeEnrollments.value > 0 && !loading.value && document.visibilityState === 'visible') {
      void load()
    }
  }, 15_000)
})
onUnmounted(() => {
  if (enrollmentRefreshTimer !== undefined) window.clearInterval(enrollmentRefreshTimer)
})

watch(() => [props.active, props.project?.id] as const, ([active, projectID]) => {
  if (!active || !projectID) return
  if (loadedProjectID.value !== projectID) {
    data.value = null
    void load()
  }
}, { immediate: true })
</script>

<template>
  <section class="agent-page">
    <div v-if="error" class="page-alert agent-alert" role="alert">{{ error }}<button type="button" title="Dismiss" @click="error = ''"><X :size="16" /></button></div>

    <header class="agent-heading">
      <div>
        <p class="eyebrow">Project credentials</p>
        <h2>Agent access</h2>
        <p>{{ props.project?.name ?? 'Select a project' }}</p>
      </div>
      <div class="agent-actions">
        <button class="secondary-button" type="button" :disabled="!agentGuideURL" @click="openGuide"><BookOpen :size="16" />Guide</button>
        <button class="secondary-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openIssue"><KeyRound :size="16" />Token</button>
        <button class="icon-button" type="button" title="Refresh Agent access" :disabled="loading || !props.project" @click="load"><RefreshCw :size="16" :class="{ spinning: loading }" /></button>
        <button class="primary-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openSetup"><Link2 :size="16" />Pi setup link</button>
      </div>
    </header>

    <div v-if="loading && !data" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>Loading Agent access</span></div>
    <template v-else>
      <section class="agent-metrics">
        <div><span><Bot :size="16" />Active</span><strong>{{ activeTokens }}</strong><small>Usable project credentials</small></div>
        <div><span><Clock3 :size="16" />Expiring</span><strong>{{ expiringTokens }}</strong><small>Within the next 30 days</small></div>
        <div><span><Link2 :size="16" />Setup links</span><strong>{{ activeEnrollments }}</strong><small>Pending or being installed</small></div>
        <div><span><ShieldCheck :size="16" />Authentication</span><strong>Bearer</strong><small>HMAC digest at rest</small></div>
      </section>

      <section class="agent-connection-band">
        <div><span>MCP transport</span><strong>Streamable HTTP</strong></div>
        <div><span>Endpoint</span><strong>{{ data?.mcp_url ?? 'GEMCP_PUBLIC_URL is not configured' }}</strong></div>
        <div><span>Template</span><strong>{{ data?.config_file_name ?? 'Unavailable' }}</strong></div>
        <div class="agent-template-action"><span>Environment config</span><button class="icon-button" type="button" title="Copy environment-variable MCP template" :disabled="!templateJSON" @click="copy(templateJSON, 'template')"><Check v-if="copied === 'template'" :size="16" /><Clipboard v-else :size="16" /></button></div>
      </section>

      <section class="agent-workspace agent-setup-workspace">
        <div class="section-heading"><div><h2>Pi setup links</h2><p>{{ data?.enrollments_truncated ? 'Latest 50 links. Setup secrets are never listed again.' : 'Short-lived one-time links that install, verify and store Pi authorization.' }}</p></div><button class="primary-button small-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openSetup"><Link2 :size="15" />New link</button></div>
        <div v-if="data?.enrollments.length" class="table-scroll">
          <table class="data-table agent-enrollment-table">
            <thead><tr><th>Status</th><th>Label</th><th>Scopes</th><th>Link expires</th><th>Credential policy</th><th>Installed token</th><th>Actions</th></tr></thead>
            <tbody>
              <tr v-for="item in data.enrollments" :key="item.id">
                <td><span class="state-badge" :data-state="item.status"><span />{{ statusLabel(item.status) }}</span></td>
                <td><strong>{{ item.label }}</strong></td>
                <td><div class="scope-list"><span v-for="scope in item.scopes" :key="scope">{{ scope }}</span></div></td>
                <td>{{ dateTime(item.expires_at) }}</td>
                <td>{{ item.token_expires_in_days ? `${item.token_expires_in_days} days` : 'No expiry' }}</td>
                <td><code v-if="item.agent_token_prefix">{{ item.agent_token_prefix }}</code><span v-else>Not claimed</span></td>
                <td><button class="icon-button danger-icon" type="button" title="Revoke Pi setup link" :disabled="item.status !== 'pending' && item.status !== 'claimed'" @click="enrollmentRevokeTarget = item"><Trash2 :size="16" /></button></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Link2 :size="21" /></span><h3>No Pi setup links</h3><p>Create one link, send it to the Agent, and let the Agent finish setup.</p></div>
      </section>

      <section class="agent-workspace">
        <div class="section-heading"><div><h2>Project tokens</h2><p>{{ data?.truncated ? 'Latest 200 records. Secret values are never recoverable.' : 'Prefixes, scopes, expiry and observed use. Secret values are never recoverable.' }}</p></div></div>
        <div v-if="data?.tokens.length" class="table-scroll">
          <table class="data-table agent-table">
            <thead><tr><th>Status</th><th>Label</th><th>Prefix</th><th>Scopes</th><th>Expires</th><th>Last used</th><th>Created</th><th>Actions</th></tr></thead>
            <tbody>
              <tr v-for="item in data.tokens" :key="item.id">
                <td><span class="state-badge" :data-state="item.status"><span />{{ statusLabel(item.status) }}</span></td>
                <td><strong>{{ item.label }}</strong></td>
                <td><code>{{ item.prefix }}</code></td>
                <td><div class="scope-list"><span v-for="scope in item.scopes" :key="scope">{{ scope }}</span></div></td>
                <td>{{ item.expires_at ? dateTime(item.expires_at) : 'No expiry' }}</td>
                <td>{{ dateTime(item.last_used_at) }}</td>
                <td>{{ dateTime(item.created_at) }}</td>
                <td><button class="icon-button danger-icon" type="button" title="Revoke Agent token" :disabled="item.status === 'revoked'" @click="revokeTarget = item"><Trash2 :size="16" /></button></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><KeyRound :size="21" /></span><h3>No Agent tokens</h3><p>Generate a project credential before connecting an MCP client.</p></div>
      </section>
    </template>
  </section>

  <div v-if="setupDialog" class="modal-backdrop" @click.self="closeSetup">
    <section class="modal agent-token-modal" role="dialog" aria-modal="true" aria-label="Create Pi setup link">
      <header><div><p class="eyebrow">Automated enrollment</p><h2>Create Pi setup link</h2></div><button class="icon-button" type="button" title="Close setup link form" :disabled="settingUp" @click="closeSetup"><X :size="17" /></button></header>
      <form class="dialog-form agent-token-form" @submit.prevent="createSetupLink">
        <label>Agent label<input v-model="setupForm.label" required maxlength="120" autocomplete="off" placeholder="pi-training-agent" /></label>
        <div class="form-grid two-columns">
          <label>Credential expiration<select v-model="setupForm.expiration"><option value="30">30 days</option><option value="90">90 days</option><option value="365">1 year</option><option value="never">No expiry</option></select></label>
          <label>Link validity<select v-model="setupForm.linkExpiration"><option value="15">15 minutes</option><option value="30">30 minutes</option><option value="60">1 hour</option><option value="240">4 hours</option></select></label>
        </div>
        <fieldset class="scope-fieldset"><legend>Scopes</legend><label title="Required for setup verification"><input v-model="setupForm.scopes.read" type="checkbox" disabled /><span>Read</span></label><label><input v-model="setupForm.scopes.submit" type="checkbox" /><span>Submit</span></label><label><input v-model="setupForm.scopes.cancel" type="checkbox" /><span>Cancel</span></label></fieldset>
        <p class="form-note">The provisional credential is read-only. Selected write scopes activate after verification; Submit is technical capability, not standing approval for paid work.</p>
        <div v-if="setupError" class="form-error" role="alert">{{ setupError }}</div>
        <button class="primary-button" type="submit" :disabled="settingUp"><LoaderCircle v-if="settingUp" :size="16" class="spinning" /><Link2 v-else :size="16" />Create setup link</button>
      </form>
    </section>
  </div>

  <div v-if="setupReveal" class="modal-backdrop" @click.self="closeSetupReveal">
    <section class="modal agent-setup-reveal-modal" role="dialog" aria-modal="true" aria-label="One-time Pi setup link">
      <header><div><p class="eyebrow">Ready to hand off</p><h2>Send one link to the Pi Agent</h2></div><button class="icon-button" type="button" title="Close Pi setup link" @click="closeSetupReveal"><X :size="17" /></button></header>
      <div class="agent-reveal-body">
        <div class="credential-block"><span>One-time setup link</span><div class="code-box"><code>{{ setupReveal.setup_url }}</code><button class="icon-button" type="button" :title="copied === 'setup-link' ? 'Copied' : 'Copy setup link'" @click="copy(setupReveal.setup_url, 'setup-link')"><Check v-if="copied === 'setup-link'" :size="16" /><Clipboard v-else :size="16" /></button></div></div>
        <div class="setup-handoff-summary"><Link2 :size="18" /><div><strong>Give the Agent only this link</strong><p>It will install the Pi MCP configuration, store the credential with mode 0600, verify all tools plus project options and cost, then invalidate the link.</p></div></div>
        <dl class="setup-link-facts"><div><dt>Link expires</dt><dd>{{ dateTime(setupReveal.enrollment.expires_at) }}</dd></div><div><dt>Scopes</dt><dd>{{ setupReveal.enrollment.scopes.join(', ') }}</dd></div><div><dt>Credential</dt><dd>{{ setupReveal.enrollment.token_expires_in_days ? `${setupReveal.enrollment.token_expires_in_days} days` : 'No expiry' }}</dd></div></dl>
        <p class="form-note">This link is not shown again. Do not open it with untrusted preview services or include it in a repository.</p>
        <div class="agent-reveal-actions"><button class="secondary-button" type="button" @click="copy(setupAgentMessage, 'setup-message')"><Check v-if="copied === 'setup-message'" :size="16" /><Clipboard v-else :size="16" />{{ copied === 'setup-message' ? 'Copied' : 'Copy Agent message' }}</button><button class="primary-button" type="button" @click="copy(setupReveal.setup_url, 'setup-link')"><Check v-if="copied === 'setup-link'" :size="16" /><Link2 v-else :size="16" />{{ copied === 'setup-link' ? 'Copied' : 'Copy link' }}</button></div>
      </div>
    </section>
  </div>

  <div v-if="guideDialog" class="modal-backdrop" @click.self="guideDialog = false">
    <section class="modal agent-guide-modal" role="dialog" aria-modal="true" aria-label="Gemcp MCP onboarding guide">
      <header><div><p class="eyebrow">Owner onboarding</p><h2>Connect an Agent safely</h2></div><button class="icon-button" type="button" title="Close MCP guide" @click="guideDialog = false"><X :size="17" /></button></header>
      <div class="agent-guide-body">
        <ol class="agent-guide-steps">
          <li><span>1</span><div><strong>Set the boundary</strong><p>Choose the project scopes, credential lifetime, budget policy, and whether every paid run requires explicit approval.</p></div></li>
          <li><span>2</span><div><strong>Create one setup link</strong><p>The short-lived fragment capability is shown once. No long-lived Token is exposed to the Owner or placed in the link.</p></div></li>
          <li><span>3</span><div><strong>Send only the link</strong><p>The Pi Agent runs the fixed installer, stores its credential, discovers all tools, and tests the guide, options and cost itself.</p></div></li>
          <li><span>4</span><div><strong>Reload once</strong><p>After setup reports all checks passed, one Pi reload makes the eight Gemcp operations available as native direct tools.</p></div></li>
          <li><span>5</span><div><strong>Approve and supervise</strong><p>The Agent must still present the immutable run and worst-case reservation before paid submission, then monitor it to a terminal state.</p></div></li>
        </ol>
        <div class="agent-discovery-list">
          <div><span>Tool fallback</span><code>get_usage_guide</code></div>
          <div><span>MCP Resource</span><code>gemcp://docs/agent-guide</code></div>
          <div><span>MCP Prompt</span><code>operate_gemcp</code></div>
          <div><span>Clients</span><strong>Claude Code, Cursor, VS Code, Codex, Streamable HTTP clients</strong></div>
        </div>
        <p class="agent-guide-note"><ShieldCheck :size="17" />The Owner guide and Agent handoff contain no credentials. Downloaded MCP configuration does contain the one-time live Token.</p>
        <div v-if="guideError" class="form-error" role="alert">{{ guideError }}</div>
        <div class="agent-guide-actions">
          <a class="secondary-button" :href="ownerGuideURL" target="_blank" rel="noreferrer"><ExternalLink :size="16" />Full Owner guide</a>
          <button class="secondary-button" type="button" @click="copyAgentGuide"><Check v-if="copied === 'guide'" :size="16" /><Clipboard v-else :size="16" />{{ copied === 'guide' ? 'Copied' : 'Copy Agent guide' }}</button>
          <a class="primary-button" :href="agentGuideURL" download="gemcp-agent-mcp.md"><FileDown :size="16" />Download Agent handoff</a>
        </div>
      </div>
    </section>
  </div>

  <div v-if="issueDialog" class="modal-backdrop" @click.self="closeIssue">
    <section class="modal agent-token-modal" role="dialog" aria-modal="true" aria-label="Generate Agent token">
      <header><div><p class="eyebrow">Project credential</p><h2>Generate Agent token</h2></div><button class="icon-button" type="button" title="Close token form" :disabled="issuing" @click="closeIssue"><X :size="17" /></button></header>
      <form class="dialog-form agent-token-form" @submit.prevent="issueToken">
        <label>Label<input v-model="form.label" required maxlength="120" autocomplete="off" placeholder="training-agent" /></label>
        <label>Expiration<select v-model="form.expiration"><option value="30">30 days</option><option value="90">90 days</option><option value="365">1 year</option><option value="never">No expiry</option></select></label>
        <fieldset class="scope-fieldset"><legend>Scopes</legend><label><input v-model="form.scopes.read" type="checkbox" /><span>Read</span></label><label><input v-model="form.scopes.submit" type="checkbox" /><span>Submit</span></label><label><input v-model="form.scopes.cancel" type="checkbox" /><span>Cancel</span></label></fieldset>
        <p class="form-note">The secret and complete MCP configuration are returned once. Lost credentials must be revoked and replaced.</p>
        <div v-if="issueError" class="form-error" role="alert">{{ issueError }}</div>
        <button class="primary-button" type="submit" :disabled="issuing"><LoaderCircle v-if="issuing" :size="16" class="spinning" /><KeyRound v-else :size="16" />Generate token</button>
      </form>
    </section>
  </div>

  <div v-if="reveal" class="modal-backdrop" @click.self="closeReveal">
    <section class="modal agent-reveal-modal" role="dialog" aria-modal="true" aria-label="Agent token and MCP configuration">
      <header><div><p class="eyebrow">One-time secret</p><h2>MCP configuration ready</h2></div><button class="icon-button" type="button" title="Close Agent token" @click="closeReveal"><X :size="17" /></button></header>
      <div class="agent-reveal-body">
        <div class="credential-block"><span>Agent token</span><div class="code-box"><code>{{ reveal.agent_token }}</code><button class="icon-button" type="button" :title="copied === 'token' ? 'Copied' : 'Copy Agent token'" @click="copy(reveal.agent_token, 'token')"><Check v-if="copied === 'token'" :size="16" /><Clipboard v-else :size="16" /></button></div></div>
        <div class="credential-block"><span>MCP configuration</span><pre>{{ configJSON }}</pre></div>
        <p class="form-note">The MCP JSON contains the live secret. The separate Agent handoff guide does not and is safe to give to the Agent after its client is configured.</p>
        <div class="agent-reveal-actions"><button class="secondary-button" type="button" @click="copy(configJSON, 'config')"><Check v-if="copied === 'config'" :size="16" /><Clipboard v-else :size="16" />{{ copied === 'config' ? 'Copied' : 'Copy JSON' }}</button><a class="secondary-button" :href="agentGuideURL" download="gemcp-agent-mcp.md"><FileDown :size="16" />Agent handoff</a><button class="primary-button" type="button" @click="downloadConfig"><Download :size="16" />Download JSON</button></div>
      </div>
    </section>
  </div>

  <div v-if="enrollmentRevokeTarget" class="modal-backdrop" @click.self="closeEnrollmentRevoke">
    <section class="modal agent-revoke-modal" role="alertdialog" aria-modal="true" aria-label="Revoke Pi setup link">
      <header><div><p class="eyebrow">Enrollment revocation</p><h2>Revoke {{ enrollmentRevokeTarget.label }}</h2></div><button class="icon-button" type="button" title="Close setup revocation" :disabled="revokingEnrollment" @click="closeEnrollmentRevoke"><X :size="17" /></button></header>
      <div class="agent-revoke-body"><p>The setup link will stop working immediately. If it was already claimed but not completed, its provisional Agent Token is revoked too.</p><div class="agent-reveal-actions"><button class="secondary-button" type="button" :disabled="revokingEnrollment" @click="closeEnrollmentRevoke">Keep link</button><button class="danger-button" type="button" :disabled="revokingEnrollment" @click="revokeEnrollment"><LoaderCircle v-if="revokingEnrollment" :size="16" class="spinning" /><Trash2 v-else :size="16" />Revoke link</button></div></div>
    </section>
  </div>

  <div v-if="revokeTarget" class="modal-backdrop" @click.self="closeRevoke">
    <section class="modal agent-revoke-modal" role="alertdialog" aria-modal="true" aria-label="Revoke Agent token">
      <header><div><p class="eyebrow">Immediate revocation</p><h2>Revoke {{ revokeTarget.label }}</h2></div><button class="icon-button" type="button" title="Close revocation" :disabled="revoking" @click="closeRevoke"><X :size="17" /></button></header>
      <div class="agent-revoke-body"><p>Requests using <code>{{ revokeTarget.prefix }}</code> will be rejected immediately. Running experiments remain managed by the control plane.</p><div class="agent-reveal-actions"><button class="secondary-button" type="button" :disabled="revoking" @click="closeRevoke">Keep token</button><button class="danger-button" type="button" :disabled="revoking" @click="revokeToken"><LoaderCircle v-if="revoking" :size="16" class="spinning" /><Trash2 v-else :size="16" />Revoke token</button></div></div>
    </section>
  </div>
</template>
