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
import { localizedState, useI18n } from '../i18n'

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
const scopeTarget = ref<AgentToken | null>(null)
const scopeSaving = ref(false)
const scopeError = ref('')
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
  scopes: { read: true, submit: true, cancel: true, configure: false } as Record<AgentScope, boolean>,
})
const form = reactive({
  label: '',
  expiration: '90',
  scopes: { read: true, submit: true, cancel: true, configure: false } as Record<AgentScope, boolean>,
})
const scopeForm = reactive({ read: true, submit: false, cancel: false, configure: false } as Record<AgentScope, boolean>)
const { languageTag, t } = useI18n()

const activeTokens = computed(() => data.value?.tokens.filter((item) => item.status === 'active').length ?? 0)
const activeEnrollments = computed(() => data.value?.enrollments.filter((item) => item.status === 'pending' || item.status === 'claimed').length ?? 0)
const expiringTokens = computed(() => {
  const threshold = Date.now() + 30 * 24 * 60 * 60 * 1000
  return data.value?.tokens.filter((item) => item.status === 'active' && item.expires_at && new Date(item.expires_at).getTime() <= threshold).length ?? 0
})
const selectedScopes = computed(() => (Object.keys(form.scopes) as AgentScope[]).filter((scope) => form.scopes[scope]))
const selectedSetupScopes = computed(() => (Object.keys(setupForm.scopes) as AgentScope[]).filter((scope) => setupForm.scopes[scope]))
const setupAgentMessage = computed(() => setupReveal.value
  ? `${t('Set up Gemcp MCP for this Agent using the one-time link below. It works for Pi, Codex, OpenCode, Claude Code, and Grok. Enable Gemcp only in this research repository directory, then complete tool verification and credential storage yourself. Do not print or forward the link, and do not pass the complete link to a Web-fetch, search, or preview tool.', '请使用下面的一次性链接为此 Agent 设置 Gemcp MCP。Pi、Codex、OpenCode、Claude Code 和 Grok 都能用。只把 Gemcp 开在当前研究仓库目录里，然后自行完成工具验证和凭据存储。不要打印或转发此链接，也不要将完整链接传给 Web fetch、搜索或预览工具。')}\n\n${setupReveal.value.setup_url}`
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
    if (props.project?.id === projectID) handleError(caught, t('Could not load Agent access state.', '无法加载 Agent 访问状态。'))
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
  setupForm.scopes.configure = false
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
    setupError.value = t('Select at least one scope.', '请至少选择一个 scope。')
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
    else setupError.value = caught instanceof APIError ? caught.message : t('Could not create MCP setup link.', '无法创建 MCP Setup Link。')
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
  form.scopes.configure = false
  issueError.value = ''
  issueDialog.value = true
}

function closeIssue() {
  if (issuing.value) return
  form.label = ''
  issueError.value = ''
  issueDialog.value = false
}

function openScopes(token: AgentToken) {
  scopeTarget.value = token
  for (const scope of Object.keys(scopeForm) as AgentScope[]) scopeForm[scope] = token.scopes.includes(scope)
  scopeError.value = ''
}

async function updateScopes() {
  if (!props.project || !scopeTarget.value) return
  const scopes = (Object.keys(scopeForm) as AgentScope[]).filter((scope) => scopeForm[scope])
  if (!scopes.length) {
    scopeError.value = t('Select at least one scope.', '请至少选择一个 scope。')
    return
  }
  scopeSaving.value = true
  scopeError.value = ''
  try {
    const updated = await api.updateAgentTokenScopes(props.project.id, scopeTarget.value.id, scopes)
    if (data.value) data.value.tokens = data.value.tokens.map((token) => token.id === updated.id ? updated : token)
    scopeTarget.value = null
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else scopeError.value = caught instanceof APIError ? caught.message : t('Could not update Agent scopes.', '无法更新 Agent scope。')
  } finally {
    scopeSaving.value = false
  }
}

async function issueToken() {
  if (!props.project) return
  if (!selectedScopes.value.length) {
    issueError.value = t('Select at least one scope.', '请至少选择一个 scope。')
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
    else issueError.value = caught instanceof APIError ? caught.message : t('Could not generate Agent token.', '无法生成 Agent Token。')
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
    error.value = t('Clipboard access was denied.', '剪贴板访问被拒绝。')
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
    guideError.value = caught instanceof Error ? caught.message : t('Unable to copy the Agent guide.', '无法复制 Agent 指南。')
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
    handleError(caught, t('Could not revoke MCP setup link.', '无法撤销 MCP Setup Link。'))
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
    handleError(caught, t('Could not revoke Agent token.', '无法撤销 Agent Token。'))
  } finally {
    revoking.value = false
  }
}

function dateTime(value?: string) {
  if (!value) return t('Never', '从未')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function statusLabel(value: string) {
  return localizedState(value)
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
    <div v-if="error" class="page-alert agent-alert" role="alert">{{ error }}<button type="button" :title="t('Dismiss', '关闭')" @click="error = ''"><X :size="16" /></button></div>

    <header class="agent-heading">
      <div>
        <p class="eyebrow">{{ t('Project credentials', 'Project 凭据') }}</p>
        <h2>{{ t('Agent access', 'Agent 访问') }}</h2>
        <p>{{ props.project?.name ?? t('Select a project', '请选择 Project') }}</p>
        <p class="form-note">{{ t('Enable this MCP config in the research repository directory. The Token is Project-scoped; do not turn Gemcp on as a global MCP for every workspace.', '把这份 MCP 配置开在研究仓库的目录里。Token 属于整个 Project，但不要把 Gemcp 开成对所有工作区生效的全局 MCP。') }}</p>
      </div>
      <div class="agent-actions">
        <button class="secondary-button" type="button" :disabled="!agentGuideURL" @click="openGuide"><BookOpen :size="16" />{{ t('Guide', '指南') }}</button>
        <button class="secondary-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openIssue"><KeyRound :size="16" />Token</button>
        <button class="icon-button" type="button" :title="t('Refresh Agent access', '刷新 Agent 访问')" :disabled="loading || !props.project" @click="load"><RefreshCw :size="16" :class="{ spinning: loading }" /></button>
        <button class="primary-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openSetup"><Link2 :size="16" />{{ t('MCP setup link', 'MCP Setup Link') }}</button>
      </div>
    </header>

    <div v-if="loading && !data" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>{{ t('Loading Agent access', '正在加载 Agent 访问') }}</span></div>
    <template v-else>
      <section class="agent-metrics">
        <div><span><Bot :size="16" />{{ t('Active', '活跃') }}</span><strong>{{ activeTokens }}</strong><small>{{ t('Usable project credentials', '可用的 Project 凭据') }}</small></div>
        <div><span><Clock3 :size="16" />{{ t('Expiring', '即将过期') }}</span><strong>{{ expiringTokens }}</strong><small>{{ t('Within the next 30 days', '未来 30 天内') }}</small></div>
        <div><span><Link2 :size="16" />Setup Links</span><strong>{{ activeEnrollments }}</strong><small>{{ t('Pending or being installed', '等待中或正在安装') }}</small></div>
        <div><span><ShieldCheck :size="16" />{{ t('Authentication', '认证') }}</span><strong>Bearer</strong><small>{{ t('HMAC digest at rest', '静态保存 HMAC 摘要') }}</small></div>
      </section>

      <section class="agent-connection-band">
        <div><span>MCP transport</span><strong>Streamable HTTP</strong></div>
        <div><span>{{ t('Endpoint', '端点') }}</span><strong>{{ data?.mcp_url ?? t('GEMCP_PUBLIC_URL is not configured', '未配置 GEMCP_PUBLIC_URL') }}</strong></div>
        <div><span>{{ t('Template', '模板') }}</span><strong>{{ data?.config_file_name ?? t('Unavailable', '不可用') }}</strong></div>
        <div class="agent-template-action"><span>{{ t('Environment config', '环境变量配置') }}</span><button class="icon-button" type="button" :title="t('Copy environment-variable MCP template', '复制环境变量 MCP 模板')" :disabled="!templateJSON" @click="copy(templateJSON, 'template')"><Check v-if="copied === 'template'" :size="16" /><Clipboard v-else :size="16" /></button></div>
      </section>

      <section class="agent-workspace agent-setup-workspace">
        <div class="section-heading"><div><h2>{{ t('MCP Setup Links', 'MCP Setup Link') }}</h2><p>{{ data?.enrollments_truncated ? t('Latest 50 links. Setup secrets are never listed again.', '仅显示最新 50 条；Setup secret 不会再次列出。') : t('Short-lived one-time links that enroll Pi, Codex, OpenCode, Claude Code, or Grok in this research repository directory.', '短期一次性链接，给 Pi、Codex、OpenCode、Claude Code 或 Grok 在当前研究仓库目录里开通 MCP。') }}</p></div><button class="primary-button small-button" type="button" :disabled="!props.project || !data?.mcp_url" @click="openSetup"><Link2 :size="15" />{{ t('New link', '新建链接') }}</button></div>
        <div v-if="data?.enrollments.length" class="table-scroll">
          <table class="data-table agent-enrollment-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Label', '标签') }}</th><th>Scopes</th><th>{{ t('Link expires', '链接过期时间') }}</th><th>{{ t('Credential policy', '凭据策略') }}</th><th>{{ t('Installed token', '已安装 Token') }}</th><th>{{ t('Actions', '操作') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in data.enrollments" :key="item.id">
                <td><span class="state-badge" :data-state="item.status"><span />{{ statusLabel(item.status) }}</span></td>
                <td><strong>{{ item.label }}</strong></td>
                <td><div class="scope-list"><span v-for="scope in item.scopes" :key="scope">{{ scope }}</span></div></td>
                <td>{{ dateTime(item.expires_at) }}</td>
                <td>{{ item.token_expires_in_days ? `${item.token_expires_in_days} ${t('days', '天')}` : t('No expiry', '永不过期') }}</td>
                <td><code v-if="item.agent_token_prefix">{{ item.agent_token_prefix }}</code><span v-else>{{ t('Not claimed', '未认领') }}</span></td>
                <td><button class="icon-button danger-icon" type="button" :title="t('Revoke MCP setup link', '撤销 MCP Setup Link')" :disabled="item.status !== 'pending' && item.status !== 'claimed'" @click="enrollmentRevokeTarget = item"><Trash2 :size="16" /></button></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Link2 :size="21" /></span><h3>{{ t('No MCP setup links', '暂无 MCP Setup Link') }}</h3><p>{{ t('Create one link, send it to the Agent, and let Pi, Codex, OpenCode, Claude Code, or Grok finish MCP setup in this repository.', '创建一个链接并发送给 Agent，由 Pi、Codex、OpenCode、Claude Code 或 Grok 在当前仓库完成 MCP 设置。') }}</p></div>
      </section>

      <section class="agent-workspace">
        <div class="section-heading"><div><h2>{{ t('Project tokens', 'Project Token') }}</h2><p>{{ data?.truncated ? t('Latest 200 records. Secret values are never recoverable.', '仅显示最新 200 条；secret 值无法恢复。') : t('Prefixes, scopes, expiry and observed use. Secret values are never recoverable.', '显示前缀、scope、过期时间和使用记录；secret 值无法恢复。') }}</p></div></div>
        <div v-if="data?.tokens.length" class="table-scroll">
          <table class="data-table agent-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Label', '标签') }}</th><th>{{ t('Prefix', '前缀') }}</th><th>Scopes</th><th>{{ t('Expires', '过期时间') }}</th><th>{{ t('Last used', '最后使用') }}</th><th>{{ t('Created', '创建时间') }}</th><th>{{ t('Actions', '操作') }}</th></tr></thead>
            <tbody>
              <tr v-for="item in data.tokens" :key="item.id">
                <td><span class="state-badge" :data-state="item.status"><span />{{ statusLabel(item.status) }}</span></td>
                <td><strong>{{ item.label }}</strong></td>
                <td><code>{{ item.prefix }}</code></td>
                <td><div class="scope-list"><span v-for="scope in item.scopes" :key="scope">{{ scope }}</span></div></td>
                <td>{{ item.expires_at ? dateTime(item.expires_at) : t('No expiry', '永不过期') }}</td>
                <td>{{ dateTime(item.last_used_at) }}</td>
                <td>{{ dateTime(item.created_at) }}</td>
                <td><div class="row-actions"><button class="icon-button" type="button" :title="t('Edit Agent scopes', '编辑 Agent scope')" :disabled="item.status !== 'active'" @click="openScopes(item)"><ShieldCheck :size="16" /></button><button class="icon-button danger-icon" type="button" :title="t('Revoke Agent token', '撤销 Agent Token')" :disabled="item.status === 'revoked'" @click="revokeTarget = item"><Trash2 :size="16" /></button></div></td>
              </tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><KeyRound :size="21" /></span><h3>{{ t('No Agent tokens', '暂无 Agent Token') }}</h3><p>{{ t('Generate a project credential before connecting an MCP client.', '连接 MCP 客户端前请先生成 Project 凭据。') }}</p></div>
      </section>
    </template>
  </section>

  <div v-if="setupDialog" class="modal-backdrop" @click.self="closeSetup">
    <section class="modal agent-token-modal" role="dialog" aria-modal="true" :aria-label="t('Create MCP setup link', '创建 MCP Setup Link')">
      <header><div><p class="eyebrow">{{ t('Automated enrollment', '自动注册') }}</p><h2>{{ t('Create MCP setup link', '创建 MCP Setup Link') }}</h2></div><button class="icon-button" type="button" :title="t('Close setup link form', '关闭 Setup Link 表单')" :disabled="settingUp" @click="closeSetup"><X :size="17" /></button></header>
      <form class="dialog-form agent-token-form" @submit.prevent="createSetupLink">
        <label>{{ t('Agent label', 'Agent 标签') }}<input v-model="setupForm.label" required maxlength="120" autocomplete="off" placeholder="research-agent" /></label>
        <div class="form-grid two-columns">
          <label>{{ t('Credential expiration', '凭据有效期') }}<select v-model="setupForm.expiration"><option value="30">30 {{ t('days', '天') }}</option><option value="90">90 {{ t('days', '天') }}</option><option value="365">1 {{ t('year', '年') }}</option><option value="never">{{ t('No expiry', '永不过期') }}</option></select></label>
          <label>{{ t('Link validity', '链接有效期') }}<select v-model="setupForm.linkExpiration"><option value="15">15 {{ t('minutes', '分钟') }}</option><option value="30">30 {{ t('minutes', '分钟') }}</option><option value="60">1 {{ t('hour', '小时') }}</option><option value="240">4 {{ t('hours', '小时') }}</option></select></label>
        </div>
        <fieldset class="scope-fieldset"><legend>Scopes</legend><label :title="t('Required for setup verification', '设置验证所必需')"><input v-model="setupForm.scopes.read" type="checkbox" disabled /><span>Read</span></label><label><input v-model="setupForm.scopes.submit" type="checkbox" /><span>Submit</span></label><label><input v-model="setupForm.scopes.cancel" type="checkbox" /><span>Cancel</span></label><label :title="t('Register Project repositories and dataset paths under an approved workspace root', '在已批准工作区根目录下注册 Project 仓库和数据路径')"><input v-model="setupForm.scopes.configure" type="checkbox" /><span>Configure</span></label></fieldset>
        <p class="form-note">{{ t('The provisional credential is read-only. Selected write scopes activate after verification. Configure cannot authorize a new host root; Submit is not standing approval for paid work.', '临时凭据仅有读取权限，验证后才激活所选写入 scope。Configure 不能批准新的宿主根目录；Submit 也不代表对付费任务的长期批准。') }}</p>
        <div v-if="setupError" class="form-error" role="alert">{{ setupError }}</div>
        <button class="primary-button" type="submit" :disabled="settingUp"><LoaderCircle v-if="settingUp" :size="16" class="spinning" /><Link2 v-else :size="16" />{{ t('Create setup link', '创建 Setup Link') }}</button>
      </form>
    </section>
  </div>

  <div v-if="setupReveal" class="modal-backdrop" @click.self="closeSetupReveal">
    <section class="modal agent-setup-reveal-modal" role="dialog" aria-modal="true" :aria-label="t('One-time MCP setup link', '一次性 MCP Setup Link')">
      <header><div><p class="eyebrow">{{ t('Ready to hand off', '可以交接') }}</p><h2>{{ t('Send one link to the Agent', '向 Agent 发送一个链接') }}</h2></div><button class="icon-button" type="button" :title="t('Close MCP setup link', '关闭 MCP Setup Link')" @click="closeSetupReveal"><X :size="17" /></button></header>
      <div class="agent-reveal-body">
        <div class="credential-block"><span>{{ t('One-time setup link', '一次性 Setup Link') }}</span><div class="code-box"><code>{{ setupReveal.setup_url }}</code><button class="icon-button" type="button" :title="copied === 'setup-link' ? t('Copied', '已复制') : t('Copy setup link', '复制 Setup Link')" @click="copy(setupReveal.setup_url, 'setup-link')"><Check v-if="copied === 'setup-link'" :size="16" /><Clipboard v-else :size="16" /></button></div></div>
        <div class="setup-handoff-summary"><Link2 :size="18" /><div><strong>{{ t('Give the Agent only this link', '仅将此链接交给 Agent') }}</strong><p>{{ t('Pi, Codex, OpenCode, Claude Code, or Grok will enable Gemcp MCP in this research repository directory, store the credential locally, verify all tools plus project options and cost, then invalidate the link.', 'Pi、Codex、OpenCode、Claude Code 或 Grok 会在当前研究仓库目录开通 Gemcp MCP，在本地保存凭据，验证所有工具、Project 选项和成本，然后使链接失效。') }}</p></div></div>
        <dl class="setup-link-facts"><div><dt>{{ t('Link expires', '链接过期时间') }}</dt><dd>{{ dateTime(setupReveal.enrollment.expires_at) }}</dd></div><div><dt>Scopes</dt><dd>{{ setupReveal.enrollment.scopes.join(', ') }}</dd></div><div><dt>{{ t('Credential', '凭据') }}</dt><dd>{{ setupReveal.enrollment.token_expires_in_days ? `${setupReveal.enrollment.token_expires_in_days} ${t('days', '天')}` : t('No expiry', '永不过期') }}</dd></div></dl>
        <p class="form-note">{{ t('This link is not shown again. Do not open it with untrusted preview services or include it in a repository.', '此链接不会再次显示。不要使用不可信的预览服务打开，也不要将其写入仓库。') }}</p>
        <div class="agent-reveal-actions"><button class="secondary-button" type="button" @click="copy(setupAgentMessage, 'setup-message')"><Check v-if="copied === 'setup-message'" :size="16" /><Clipboard v-else :size="16" />{{ copied === 'setup-message' ? t('Copied', '已复制') : t('Copy Agent message', '复制 Agent 消息') }}</button><button class="primary-button" type="button" @click="copy(setupReveal.setup_url, 'setup-link')"><Check v-if="copied === 'setup-link'" :size="16" /><Link2 v-else :size="16" />{{ copied === 'setup-link' ? t('Copied', '已复制') : t('Copy link', '复制链接') }}</button></div>
      </div>
    </section>
  </div>

  <div v-if="guideDialog" class="modal-backdrop" @click.self="guideDialog = false">
    <section class="modal agent-guide-modal" role="dialog" aria-modal="true" :aria-label="t('Gemcp MCP onboarding guide', 'Gemcp MCP 接入指南')">
      <header><div><p class="eyebrow">{{ t('Owner onboarding', 'Owner 接入') }}</p><h2>{{ t('Connect an Agent safely', '安全连接 Agent') }}</h2></div><button class="icon-button" type="button" :title="t('Close MCP guide', '关闭 MCP 指南')" @click="guideDialog = false"><X :size="17" /></button></header>
      <div class="agent-guide-body">
        <ol class="agent-guide-steps">
          <li><span>1</span><div><strong>{{ t('Set the boundary', '设置边界') }}</strong><p>{{ t('Choose the project scopes, credential lifetime, budget policy, and whether every paid run requires explicit approval.', '选择 Project scope、凭据有效期、预算策略，以及每次付费运行是否都需要显式批准。') }}</p></div></li>
          <li><span>2</span><div><strong>{{ t('Create one setup link', '创建一个 Setup Link') }}</strong><p>{{ t('The short-lived fragment capability is shown once. No long-lived Token is exposed to the Owner or placed in the link.', '短期 fragment capability 只显示一次，不会向 Owner 暴露长期 Token，也不会把长期 Token 放入链接。') }}</p></div></li>
          <li><span>3</span><div><strong>{{ t('Send only the link', '只发送链接') }}</strong><p>{{ t('The Agent enables Gemcp MCP in this research repository directory, stores its credential, discovers all tools, and tests the guide, options and cost itself. Pi, Codex, OpenCode, Claude Code, and Grok all use the same link.', 'Agent 在当前研究仓库目录开通 Gemcp MCP、保存凭据、发现所有工具，并自行测试指南、选项和成本。Pi、Codex、OpenCode、Claude Code 和 Grok 用同一条链接。') }}</p></div></li>
          <li><span>4</span><div><strong>{{ t('Reload once', '重载一次') }}</strong><p>{{ t('After setup reports all checks passed, reload or restart the MCP client once so the bounded Gemcp tools are available.', '设置报告所有检查通过后，重载或重启一次 MCP 客户端，即可使用受限的 Gemcp 工具。') }}</p></div></li>
          <li><span>5</span><div><strong>{{ t('Approve and supervise', '批准并监督') }}</strong><p>{{ t('The Agent must still present the immutable run and worst-case reservation before paid submission, then monitor it to a terminal state.', 'Agent 在付费提交前仍必须展示不可变运行规格和最坏情况预留，随后监控至终态。') }}</p></div></li>
        </ol>
        <div class="agent-discovery-list">
          <div><span>{{ t('Tool fallback', '工具回退') }}</span><code>get_usage_guide</code></div>
          <div><span>MCP Resource</span><code>gemcp://docs/agent-guide</code></div>
          <div><span>MCP Prompt</span><code>operate_gemcp</code></div>
          <div><span>{{ t('Clients', '客户端') }}</span><strong>Pi, Claude Code, Codex, OpenCode, Grok, Cursor, VS Code</strong></div>
        </div>
        <p class="agent-guide-note"><ShieldCheck :size="17" />{{ t('The Owner guide and Agent handoff contain no credentials. Downloaded MCP configuration does contain the one-time live Token.', 'Owner 指南和 Agent handoff 不含凭据；下载的 MCP 配置包含一次性显示的有效 Token。') }}</p>
        <div v-if="guideError" class="form-error" role="alert">{{ guideError }}</div>
        <div class="agent-guide-actions">
          <a class="secondary-button" :href="ownerGuideURL" target="_blank" rel="noreferrer"><ExternalLink :size="16" />{{ t('Full Owner guide', '完整 Owner 指南') }}</a>
          <button class="secondary-button" type="button" @click="copyAgentGuide"><Check v-if="copied === 'guide'" :size="16" /><Clipboard v-else :size="16" />{{ copied === 'guide' ? t('Copied', '已复制') : t('Copy Agent guide', '复制 Agent 指南') }}</button>
          <a class="primary-button" :href="agentGuideURL" download="gemcp-agent-mcp.md"><FileDown :size="16" />{{ t('Download Agent handoff', '下载 Agent handoff') }}</a>
        </div>
      </div>
    </section>
  </div>

  <div v-if="issueDialog" class="modal-backdrop" @click.self="closeIssue">
    <section class="modal agent-token-modal" role="dialog" aria-modal="true" :aria-label="t('Generate Agent token', '生成 Agent Token')">
      <header><div><p class="eyebrow">{{ t('Project credential', 'Project 凭据') }}</p><h2>{{ t('Generate Agent token', '生成 Agent Token') }}</h2></div><button class="icon-button" type="button" :title="t('Close token form', '关闭 Token 表单')" :disabled="issuing" @click="closeIssue"><X :size="17" /></button></header>
      <form class="dialog-form agent-token-form" @submit.prevent="issueToken">
        <label>{{ t('Label', '标签') }}<input v-model="form.label" required maxlength="120" autocomplete="off" placeholder="training-agent" /></label>
        <label>{{ t('Expiration', '有效期') }}<select v-model="form.expiration"><option value="30">30 {{ t('days', '天') }}</option><option value="90">90 {{ t('days', '天') }}</option><option value="365">1 {{ t('year', '年') }}</option><option value="never">{{ t('No expiry', '永不过期') }}</option></select></label>
        <fieldset class="scope-fieldset"><legend>Scopes</legend><label><input v-model="form.scopes.read" type="checkbox" /><span>Read</span></label><label><input v-model="form.scopes.submit" type="checkbox" /><span>Submit</span></label><label><input v-model="form.scopes.cancel" type="checkbox" /><span>Cancel</span></label><label :title="t('Register Project repositories and dataset paths under an approved workspace root', '在已批准工作区根目录下注册 Project 仓库和数据路径')"><input v-model="form.scopes.configure" type="checkbox" /><span>Configure</span></label></fieldset>
        <p class="form-note">{{ t('The secret and complete MCP configuration are returned once. Lost credentials must be revoked and replaced.', 'secret 和完整 MCP 配置仅返回一次；丢失的凭据必须撤销并替换。') }}</p>
        <div v-if="issueError" class="form-error" role="alert">{{ issueError }}</div>
        <button class="primary-button" type="submit" :disabled="issuing"><LoaderCircle v-if="issuing" :size="16" class="spinning" /><KeyRound v-else :size="16" />{{ t('Generate token', '生成 Token') }}</button>
      </form>
    </section>
  </div>

  <div v-if="reveal" class="modal-backdrop" @click.self="closeReveal">
    <section class="modal agent-reveal-modal" role="dialog" aria-modal="true" :aria-label="t('Agent token and MCP configuration', 'Agent Token 和 MCP 配置')">
      <header><div><p class="eyebrow">{{ t('One-time secret', '一次性 secret') }}</p><h2>{{ t('MCP configuration ready', 'MCP 配置已就绪') }}</h2></div><button class="icon-button" type="button" :title="t('Close Agent token', '关闭 Agent Token')" @click="closeReveal"><X :size="17" /></button></header>
      <div class="agent-reveal-body">
        <div class="credential-block"><span>Agent Token</span><div class="code-box"><code>{{ reveal.agent_token }}</code><button class="icon-button" type="button" :title="copied === 'token' ? t('Copied', '已复制') : t('Copy Agent token', '复制 Agent Token')" @click="copy(reveal.agent_token, 'token')"><Check v-if="copied === 'token'" :size="16" /><Clipboard v-else :size="16" /></button></div></div>
        <div class="credential-block"><span>{{ t('MCP configuration', 'MCP 配置') }}</span><pre>{{ configJSON }}</pre></div>
        <p class="form-note">{{ t('The MCP JSON contains the live secret. The separate Agent handoff guide does not and is safe to give to the Agent after its client is configured.', 'MCP JSON 包含有效 secret；单独的 Agent handoff 指南不含 secret，可在配置客户端后安全交给 Agent。') }}</p>
        <div class="agent-reveal-actions"><button class="secondary-button" type="button" @click="copy(configJSON, 'config')"><Check v-if="copied === 'config'" :size="16" /><Clipboard v-else :size="16" />{{ copied === 'config' ? t('Copied', '已复制') : t('Copy JSON', '复制 JSON') }}</button><a class="secondary-button" :href="agentGuideURL" download="gemcp-agent-mcp.md"><FileDown :size="16" />Agent handoff</a><button class="primary-button" type="button" @click="downloadConfig"><Download :size="16" />{{ t('Download JSON', '下载 JSON') }}</button></div>
      </div>
    </section>
  </div>

  <div v-if="scopeTarget" class="modal-backdrop" @click.self="scopeTarget = null">
    <section class="modal agent-token-modal" role="dialog" aria-modal="true" :aria-label="t('Edit Agent scopes', '编辑 Agent scope')">
      <header><div><p class="eyebrow">{{ t('Existing credential', '现有凭据') }}</p><h2>{{ t('Edit Agent scopes', '编辑 Agent scope') }} · {{ scopeTarget.label }}</h2></div><button class="icon-button" type="button" :disabled="scopeSaving" :title="t('Close scope form', '关闭 scope 表单')" @click="scopeTarget = null"><X :size="17" /></button></header>
      <form class="dialog-form agent-token-form" @submit.prevent="updateScopes">
        <fieldset class="scope-fieldset"><legend>Scopes</legend><label><input v-model="scopeForm.read" type="checkbox" /><span>Read</span></label><label><input v-model="scopeForm.submit" type="checkbox" /><span>Submit</span></label><label><input v-model="scopeForm.cancel" type="checkbox" /><span>Cancel</span></label><label :title="t('Register Project repositories and dataset paths under an approved workspace root', '在已批准工作区根目录下注册 Project 仓库和数据路径')"><input v-model="scopeForm.configure" type="checkbox" /><span>Configure</span></label></fieldset>
        <p class="form-note">{{ t('Changes apply to the existing Token on its next authenticated request and are written to the audit log. Configure cannot authorize a new host root.', '变更会在现有 Token 的下一次认证请求生效并写入审计日志；Configure 不能批准新的宿主根目录。') }}</p>
        <div v-if="scopeError" class="form-error" role="alert">{{ scopeError }}</div>
        <button class="primary-button" type="submit" :disabled="scopeSaving"><LoaderCircle v-if="scopeSaving" :size="16" class="spinning" /><ShieldCheck v-else :size="16" />{{ t('Update scopes', '更新 scope') }}</button>
      </form>
    </section>
  </div>

  <div v-if="enrollmentRevokeTarget" class="modal-backdrop" @click.self="closeEnrollmentRevoke">
    <section class="modal agent-revoke-modal" role="alertdialog" aria-modal="true" :aria-label="t('Revoke MCP setup link', '撤销 MCP Setup Link')">
      <header><div><p class="eyebrow">{{ t('Enrollment revocation', '撤销注册') }}</p><h2>{{ t('Revoke', '撤销') }} {{ enrollmentRevokeTarget.label }}</h2></div><button class="icon-button" type="button" :title="t('Close setup revocation', '关闭注册撤销')" :disabled="revokingEnrollment" @click="closeEnrollmentRevoke"><X :size="17" /></button></header>
      <div class="agent-revoke-body"><p>{{ t('The setup link will stop working immediately. If it was already claimed but not completed, its provisional Agent Token is revoked too.', 'Setup Link 将立即失效；若已认领但尚未完成，其临时 Agent Token 也会被撤销。') }}</p><div class="agent-reveal-actions"><button class="secondary-button" type="button" :disabled="revokingEnrollment" @click="closeEnrollmentRevoke">{{ t('Keep link', '保留链接') }}</button><button class="danger-button" type="button" :disabled="revokingEnrollment" @click="revokeEnrollment"><LoaderCircle v-if="revokingEnrollment" :size="16" class="spinning" /><Trash2 v-else :size="16" />{{ t('Revoke link', '撤销链接') }}</button></div></div>
    </section>
  </div>

  <div v-if="revokeTarget" class="modal-backdrop" @click.self="closeRevoke">
    <section class="modal agent-revoke-modal" role="alertdialog" aria-modal="true" :aria-label="t('Revoke Agent token', '撤销 Agent Token')">
      <header><div><p class="eyebrow">{{ t('Immediate revocation', '立即撤销') }}</p><h2>{{ t('Revoke', '撤销') }} {{ revokeTarget.label }}</h2></div><button class="icon-button" type="button" :title="t('Close revocation', '关闭撤销')" :disabled="revoking" @click="closeRevoke"><X :size="17" /></button></header>
      <div class="agent-revoke-body"><p>{{ t('Requests using', '使用') }} <code>{{ revokeTarget.prefix }}</code> {{ t('will be rejected immediately. Running experiments remain managed by the control plane.', '的请求将立即被拒绝；运行中的实验仍由控制平面管理。') }}</p><div class="agent-reveal-actions"><button class="secondary-button" type="button" :disabled="revoking" @click="closeRevoke">{{ t('Keep token', '保留 Token') }}</button><button class="danger-button" type="button" :disabled="revoking" @click="revokeToken"><LoaderCircle v-if="revoking" :size="16" class="spinning" /><Trash2 v-else :size="16" />{{ t('Revoke token', '撤销 Token') }}</button></div></div>
    </section>
  </div>
</template>
