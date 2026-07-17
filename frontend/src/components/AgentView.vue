<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
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
  LoaderCircle,
  Plus,
  RefreshCw,
  ShieldCheck,
  Trash2,
  X,
} from '@lucide/vue'
import {
  APIError,
  api,
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
const issueDialog = ref(false)
const issuing = ref(false)
const issueError = ref('')
const reveal = ref<AgentTokenIssue | null>(null)
const copied = ref('')
const guideDialog = ref(false)
const guideError = ref('')
const revokeTarget = ref<AgentToken | null>(null)
const revoking = ref(false)
const form = reactive({
  label: '',
  expiration: '90',
  scopes: { read: true, submit: true, cancel: true } as Record<AgentScope, boolean>,
})

const activeTokens = computed(() => data.value?.tokens.filter((item) => item.status === 'active').length ?? 0)
const revokedTokens = computed(() => data.value?.tokens.filter((item) => item.status === 'revoked').length ?? 0)
const expiringTokens = computed(() => {
  const threshold = Date.now() + 30 * 24 * 60 * 60 * 1000
  return data.value?.tokens.filter((item) => item.status === 'active' && item.expires_at && new Date(item.expires_at).getTime() <= threshold).length ?? 0
})
const selectedScopes = computed(() => (Object.keys(form.scopes) as AgentScope[]).filter((scope) => form.scopes[scope]))
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
        <button class="secondary-button" type="button" :disabled="!agentGuideURL" @click="openGuide"><BookOpen :size="16" />MCP guide</button>
        <a class="secondary-button" :class="{ disabled: !agentGuideURL }" :href="agentGuideURL || undefined" download="gemcp-agent-mcp.md"><FileDown :size="16" />Agent handoff</a>
        <button class="icon-button" type="button" title="Refresh Agent tokens" :disabled="loading || !props.project" @click="load"><RefreshCw :size="16" :class="{ spinning: loading }" /></button>
        <button class="primary-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openIssue"><Plus :size="16" />Generate token</button>
      </div>
    </header>

    <div v-if="loading && !data" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>Loading Agent access</span></div>
    <template v-else>
      <section class="agent-metrics">
        <div><span><Bot :size="16" />Active</span><strong>{{ activeTokens }}</strong><small>Usable project credentials</small></div>
        <div><span><Clock3 :size="16" />Expiring</span><strong>{{ expiringTokens }}</strong><small>Within the next 30 days</small></div>
        <div><span><Trash2 :size="16" />Revoked</span><strong>{{ revokedTokens }}</strong><small>Rejected on every request</small></div>
        <div><span><ShieldCheck :size="16" />Authentication</span><strong>Bearer</strong><small>HMAC digest at rest</small></div>
      </section>

      <section class="agent-connection-band">
        <div><span>MCP transport</span><strong>Streamable HTTP</strong></div>
        <div><span>Endpoint</span><strong>{{ data?.mcp_url ?? 'GEMCP_PUBLIC_URL is not configured' }}</strong></div>
        <div><span>Template</span><strong>{{ data?.config_file_name ?? 'Unavailable' }}</strong></div>
        <div class="agent-template-action"><span>Environment config</span><button class="icon-button" type="button" title="Copy environment-variable MCP template" :disabled="!templateJSON" @click="copy(templateJSON, 'template')"><Check v-if="copied === 'template'" :size="16" /><Clipboard v-else :size="16" /></button></div>
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

  <div v-if="guideDialog" class="modal-backdrop" @click.self="guideDialog = false">
    <section class="modal agent-guide-modal" role="dialog" aria-modal="true" aria-label="Gemcp MCP onboarding guide">
      <header><div><p class="eyebrow">Owner onboarding</p><h2>Connect an Agent safely</h2></div><button class="icon-button" type="button" title="Close MCP guide" @click="guideDialog = false"><X :size="17" /></button></header>
      <div class="agent-guide-body">
        <ol class="agent-guide-steps">
          <li><span>1</span><div><strong>Set the boundary</strong><p>Confirm the project repository, approved GPU profile, runtime policy, budget, and whether every paid run requires explicit approval.</p></div></li>
          <li><span>2</span><div><strong>Issue minimum access</strong><p>Generate a finite-lived Token with only the required read, submit, and cancel scopes. The secret is shown once.</p></div></li>
          <li><span>3</span><div><strong>Configure the client</strong><p>Import the generated MCP JSON or place the Token in the client secret environment. Never paste it into an Agent prompt or repository.</p></div></li>
          <li><span>4</span><div><strong>Hand off instructions</strong><p>Give the Agent the separate non-secret guide. It requires option and cost preflight, a full pushed commit SHA, stable idempotency, and human approval.</p></div></li>
          <li><span>5</span><div><strong>Verify and supervise</strong><p>Start with the read-only guide, options, and cost tools. Monitor every submitted Experiment to a terminal state and revoke unexpected access immediately.</p></div></li>
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

  <div v-if="revokeTarget" class="modal-backdrop" @click.self="closeRevoke">
    <section class="modal agent-revoke-modal" role="alertdialog" aria-modal="true" aria-label="Revoke Agent token">
      <header><div><p class="eyebrow">Immediate revocation</p><h2>Revoke {{ revokeTarget.label }}</h2></div><button class="icon-button" type="button" title="Close revocation" :disabled="revoking" @click="closeRevoke"><X :size="17" /></button></header>
      <div class="agent-revoke-body"><p>Requests using <code>{{ revokeTarget.prefix }}</code> will be rejected immediately. Running experiments remain managed by the control plane.</p><div class="agent-reveal-actions"><button class="secondary-button" type="button" :disabled="revoking" @click="closeRevoke">Keep token</button><button class="danger-button" type="button" :disabled="revoking" @click="revokeToken"><LoaderCircle v-if="revoking" :size="16" class="spinning" /><Trash2 v-else :size="16" />Revoke token</button></div></div>
    </section>
  </div>
</template>
