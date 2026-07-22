<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ArrowLeft, ArrowRight, Check, Clipboard, Eye, EyeOff, FlaskConical, LoaderCircle } from '@lucide/vue'
import { APIError, api, type SetupResult } from '../api'
import { useI18n } from '../i18n'
import LanguageToggle from './LanguageToggle.vue'

const emit = defineEmits<{ ready: [] }>()

const step = ref(1)
const submitting = ref(false)
const showPassword = ref(false)
const error = ref('')
const result = ref<SetupResult | null>(null)
const copied = ref(false)
const slugEdited = ref(false)
const providerMode = ref<'private' | 'public'>('private')
const { t } = useI18n()

const form = reactive({
  bootstrapToken: '',
  organizationName: '',
  ownerEmail: '',
  ownerPassword: '',
  providerName: 'AutoDL Private Cloud',
  providerBaseURL: 'https://private.autodl.com',
  providerToken: '',
  projectName: '',
  projectSlug: '',
  monthlyBudgetCNY: 100,
  maxExperimentCNY: 20,
  maxConcurrency: 1,
  maxRuntimeHours: 24,
  imageUUID: '',
  environmentName: 'default',
  profileName: 'default',
  region: 'private',
  gpuNames: 'NVIDIA GeForce RTX 3090',
  gpuNum: 1,
  cudaFrom: 118,
  cudaTo: 118,
  cpuFrom: 1,
  cpuTo: 128,
  memoryFromGB: 1,
  memoryToGB: 512,
  priceFromCNY: 0.01,
  priceToCNY: 9,
  reuseContainer: false,
})

const stepLabel = computed(() => [t('Owner', 'Owner'), t('Provider', 'Provider'), t('Project', 'Project')][step.value - 1])

function slugify(value: string) {
  return value
    .toLowerCase()
    .trim()
    .replace(/[^a-z0-9]+/g, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 64)
}

function updateProjectName() {
  if (!slugEdited.value) form.projectSlug = slugify(form.projectName)
}

function applyProviderMode() {
  if (providerMode.value === 'private') {
    form.providerName = 'AutoDL Private Cloud'
    form.providerBaseURL = 'https://private.autodl.com'
    form.region = 'private'
    form.gpuNames = 'NVIDIA GeForce RTX 3090'
    form.cudaFrom = 118
    form.cudaTo = 118
    form.priceToCNY = 9
    return
  }
  form.providerName = 'AutoDL'
  form.providerBaseURL = 'https://api.autodl.com'
  form.region = 'westDC2'
  form.gpuNames = 'RTX 4090'
  form.cudaFrom = 118
  form.cudaTo = 128
  form.priceToCNY = 3
}

function validate(current: number): string {
  if (current === 1) {
    if (form.bootstrapToken.trim().length < 32) return t('Enter the bootstrap token generated on the control server.', '请输入控制服务器生成的 bootstrap token。')
    if (!form.organizationName.trim()) return t('Organization name is required.', '必须填写组织名称。')
    if (!/^\S+@\S+\.\S+$/.test(form.ownerEmail)) return t('Enter a valid Owner email.', '请输入有效的 Owner 邮箱。')
    if (form.ownerPassword.length < 12) return t('Owner password must contain at least 12 characters.', 'Owner 密码至少需要 12 个字符。')
  }
  if (current === 2) {
    if (!form.providerName.trim() || !form.providerToken.trim()) return t('Provider name and AutoDL token are required.', '必须填写 Provider 名称和 AutoDL Token。')
    if (!form.providerBaseURL.startsWith('https://')) return t('Provider API URL must use HTTPS.', 'Provider API URL 必须使用 HTTPS。')
  }
  if (current === 3) {
    if (!form.projectName.trim() || !/^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$/.test(form.projectSlug)) {
      return t('Project name and a 3-64 character lowercase slug are required.', '必须填写 Project 名称和 3 至 64 个字符的小写 slug。')
    }
    if (form.monthlyBudgetCNY <= 0 || form.maxExperimentCNY <= 0 || form.maxExperimentCNY > form.monthlyBudgetCNY) {
      return t('Experiment cap must be positive and no greater than the monthly budget.', 'Experiment 上限必须为正数且不能超过月度预算。')
    }
    if (!form.imageUUID.trim()) return t('AutoDL image UUID is required.', '必须填写 AutoDL 镜像 UUID。')
    if (!form.region.trim() || !form.gpuNames.trim() || form.priceToCNY <= 0) return t('Resource region, GPU candidates, and price ceiling are required.', '必须填写资源区域、候选 GPU 和价格上限。')
  }
  return ''
}

function next() {
  error.value = validate(step.value)
  if (!error.value && step.value < 3) step.value += 1
}

async function submit() {
  error.value = validate(3)
  if (error.value) return
  submitting.value = true
  try {
    result.value = await api.setup(
      {
        organization_name: form.organizationName.trim(),
        owner: { email: form.ownerEmail.trim(), password: form.ownerPassword },
        provider: {
          name: form.providerName.trim(),
          base_url: form.providerBaseURL.trim(),
          token: form.providerToken.trim(),
        },
        project: {
          name: form.projectName.trim(),
          slug: form.projectSlug,
          monthly_budget_milli: Math.round(form.monthlyBudgetCNY * 1000),
          max_experiment_milli: Math.round(form.maxExperimentCNY * 1000),
          max_concurrency: form.maxConcurrency,
          max_runtime_seconds: Math.round(form.maxRuntimeHours * 3600),
          timeout_extension_seconds: 3600,
          termination_grace_seconds: 60,
        },
        environment: { name: form.environmentName.trim(), image_uuid: form.imageUUID.trim() },
        resource_profile: {
          name: form.profileName.trim(),
          region: form.region.trim(),
          gpu_names: form.gpuNames.split(',').map((value) => value.trim()).filter(Boolean),
          gpu_num: form.gpuNum,
          cuda_from: form.cudaFrom,
          cuda_to: form.cudaTo,
          cpu_from: form.cpuFrom,
          cpu_to: form.cpuTo,
          memory_from_gb: form.memoryFromGB,
          memory_to_gb: form.memoryToGB,
          price_from_milli: Math.round(form.priceFromCNY * 1000),
          price_to_milli: Math.round(form.priceToCNY * 1000),
          reuse_container: form.reuseContainer,
        },
        agent_token_label: 'default-agent',
      },
      form.bootstrapToken.trim(),
    )
    form.providerToken = ''
    form.bootstrapToken = ''
    form.ownerPassword = ''
  } catch (caught) {
    error.value = caught instanceof APIError ? caught.message : t('Initialization failed. Check the control-plane logs.', '初始化失败，请检查控制平面日志。')
  } finally {
    submitting.value = false
  }
}

async function copyToken() {
  if (!result.value) return
  await navigator.clipboard.writeText(result.value.agent_token)
  copied.value = true
  window.setTimeout(() => (copied.value = false), 1800)
}
</script>

<template>
  <main class="setup-page">
    <header class="setup-header">
      <div class="setup-brand">
        <span class="brand-mark"><FlaskConical :size="20" /></span>
        <div><strong>Gemcp</strong><span>{{ t('Initial configuration', '初始配置') }}</span></div>
      </div>
      <div class="setup-header-actions"><span v-if="!result" class="setup-step-count">{{ t('Step', '步骤') }} {{ step }} / 3 · {{ stepLabel }}</span><LanguageToggle /></div>
    </header>

    <section v-if="result" class="token-reveal" aria-live="polite">
      <div class="success-mark"><Check :size="24" /></div>
      <p class="eyebrow">{{ t('Initialization complete', '初始化完成') }}</p>
      <h1>{{ t('Default Agent token', '默认 Agent Token') }}</h1>
      <p class="supporting-copy">{{ t("This credential is shown only in the setup response. Store it in the Agent's secret configuration.", '此凭据仅在初始化响应中显示一次，请将其保存到 Agent 的 secret 配置中。') }}</p>
      <div class="token-value">
        <code>{{ result.agent_token }}</code>
        <button class="icon-button" type="button" :title="copied ? t('Copied', '已复制') : t('Copy Agent token', '复制 Agent Token')" @click="copyToken">
          <Check v-if="copied" :size="17" /><Clipboard v-else :size="17" />
        </button>
      </div>
      <dl class="setup-result-grid">
        <div><dt>Project</dt><dd>{{ result.project_id }}</dd></div>
        <div><dt>{{ t('Token prefix', 'Token 前缀') }}</dt><dd>{{ result.agent_token_prefix }}</dd></div>
      </dl>
      <button class="primary-button" type="button" @click="emit('ready')">{{ t('Continue to sign in', '继续登录') }} <ArrowRight :size="16" /></button>
    </section>

    <template v-else>
      <div class="setup-progress" :aria-label="t('Setup progress', '设置进度')">
        <button v-for="item in 3" :key="item" type="button" :class="{ active: step === item, complete: step > item }" @click="item < step && (step = item)">
          <span><Check v-if="step > item" :size="13" /><template v-else>{{ item }}</template></span>{{ [t('Owner', 'Owner'), t('Provider', 'Provider'), t('Project', 'Project')][item - 1] }}
        </button>
      </div>

      <form class="setup-form" @submit.prevent="submit">
        <section v-if="step === 1" class="form-section">
          <div class="form-heading"><p class="eyebrow">{{ t('Access boundary', '访问边界') }}</p><h1>{{ t('Create the Owner', '创建 Owner') }}</h1></div>
          <div class="form-grid two-columns">
            <label class="full-field">Bootstrap Token<input v-model="form.bootstrapToken" type="password" autocomplete="off" spellcheck="false" /></label>
            <label>{{ t('Organization name', '组织名称') }}<input v-model="form.organizationName" autocomplete="organization" /></label>
            <label>{{ t('Owner email', 'Owner 邮箱') }}<input v-model="form.ownerEmail" type="email" autocomplete="username" /></label>
            <label class="full-field">{{ t('Owner password', 'Owner 密码') }}<span class="password-field"><input v-model="form.ownerPassword" :type="showPassword ? 'text' : 'password'" autocomplete="new-password" /><button type="button" :title="showPassword ? t('Hide password', '隐藏密码') : t('Show password', '显示密码')" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" :size="17" /><Eye v-else :size="17" /></button></span></label>
          </div>
        </section>

        <section v-else-if="step === 2" class="form-section">
          <div class="form-heading"><p class="eyebrow">{{ t('Credential custody', '凭据托管') }}</p><h1>{{ t('Connect AutoDL', '连接 AutoDL') }}</h1></div>
          <div class="form-grid two-columns">
            <label>{{ t('AutoDL service', 'AutoDL 服务') }}<select v-model="providerMode" @change="applyProviderMode"><option value="private">{{ t('Private Cloud', '私有云') }}</option><option value="public">{{ t('Public Cloud', '公共云') }}</option></select></label>
            <label>{{ t('Provider name', 'Provider 名称') }}<input v-model="form.providerName" /></label>
            <label class="full-field">API base URL<input v-model="form.providerBaseURL" type="url" spellcheck="false" /></label>
            <label class="full-field">AutoDL API Token<input v-model="form.providerToken" type="password" autocomplete="off" spellcheck="false" /></label>
          </div>
          <p class="form-note">{{ t('The token is encrypted before it is written to PostgreSQL. The selected service controls which official API host receives it.', 'Token 在写入 PostgreSQL 前会被加密；所选服务决定将其发送到哪个官方 API 主机。') }}</p>
        </section>

        <section v-else class="form-section wide-form-section">
          <div class="form-heading"><p class="eyebrow">{{ t('Default policy', '默认策略') }}</p><h1>{{ t('Configure the first project', '配置首个 Project') }}</h1></div>
          <div class="form-subsection">
            <h2>{{ t('Project limits', 'Project 限制') }}</h2>
            <div class="form-grid four-columns">
              <label class="span-two">{{ t('Project name', 'Project 名称') }}<input v-model="form.projectName" @input="updateProjectName" /></label>
              <label class="span-two">Project slug<input v-model="form.projectSlug" spellcheck="false" @input="slugEdited = true" /></label>
              <label>{{ t('Monthly budget (CNY)', '月度预算 (CNY)') }}<input v-model.number="form.monthlyBudgetCNY" type="number" min="0.001" step="0.001" /></label>
              <label>{{ t('Experiment cap (CNY)', 'Experiment 上限 (CNY)') }}<input v-model.number="form.maxExperimentCNY" type="number" min="0.001" step="0.001" /></label>
              <label>{{ t('Concurrency', '并发数') }}<input v-model.number="form.maxConcurrency" type="number" min="1" max="32" /></label>
              <label>{{ t('Max runtime (hours)', '最长运行时间（小时）') }}<input v-model.number="form.maxRuntimeHours" type="number" min="0.1" max="168" step="0.1" /></label>
            </div>
          </div>
          <div class="form-subsection">
            <h2>{{ t('Environment and resources', '环境与资源') }}</h2>
            <div class="form-grid four-columns">
              <label>{{ t('Environment name', 'Environment 名称') }}<input v-model="form.environmentName" /></label>
              <label class="span-three">AutoDL image UUID<input v-model="form.imageUUID" spellcheck="false" /></label>
              <label>{{ t('Profile name', 'Profile 名称') }}<input v-model="form.profileName" /></label>
              <label>{{ t('Region', '区域') }}<input v-model="form.region" spellcheck="false" /></label>
              <label class="span-two">{{ t('GPU candidates', '候选 GPU') }}<input v-model="form.gpuNames" placeholder="RTX 4090, RTX 5090" /></label>
              <label>{{ t('GPU count', 'GPU 数量') }}<input v-model.number="form.gpuNum" type="number" min="1" max="4" /></label>
              <label>CUDA from<input v-model.number="form.cudaFrom" type="number" min="1" /></label>
              <label>CUDA to<input v-model.number="form.cudaTo" type="number" min="1" /></label>
              <label>CPU from<input v-model.number="form.cpuFrom" type="number" min="1" /></label>
              <label>CPU to<input v-model.number="form.cpuTo" type="number" min="1" /></label>
              <label>{{ t('Memory from (GB)', '最低内存 (GB)') }}<input v-model.number="form.memoryFromGB" type="number" min="1" /></label>
              <label>{{ t('Memory to (GB)', '最高内存 (GB)') }}<input v-model.number="form.memoryToGB" type="number" min="1" /></label>
              <label>{{ t('Price floor (CNY/h)', '价格下限 (CNY/h)') }}<input v-model.number="form.priceFromCNY" type="number" min="0" step="0.001" /></label>
              <label>{{ t('Price ceiling (CNY/h)', '价格上限 (CNY/h)') }}<input v-model.number="form.priceToCNY" type="number" min="0.001" step="0.001" /></label>
              <label class="checkbox-field span-two"><input v-model="form.reuseContainer" type="checkbox" /><span>{{ t('Allow best-effort stopped-container reuse', '允许尽力复用已停止的容器') }}</span></label>
            </div>
          </div>
        </section>

        <div v-if="error" class="form-error" role="alert">{{ error }}</div>
        <footer class="setup-actions">
          <button v-if="step > 1" class="secondary-button" type="button" @click="step -= 1"><ArrowLeft :size="16" />{{ t('Back', '返回') }}</button>
          <span v-else />
          <button v-if="step < 3" class="primary-button" type="button" @click="next">{{ t('Continue', '继续') }}<ArrowRight :size="16" /></button>
          <button v-else class="primary-button" type="submit" :disabled="submitting"><LoaderCircle v-if="submitting" :size="16" class="spinning" /><Check v-else :size="16" />{{ t('Initialize Gemcp', '初始化 Gemcp') }}</button>
        </footer>
      </form>
    </template>
  </main>
</template>
