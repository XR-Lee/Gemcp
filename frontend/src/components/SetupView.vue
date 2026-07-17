<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { ArrowLeft, ArrowRight, Check, Clipboard, Eye, EyeOff, FlaskConical, LoaderCircle } from '@lucide/vue'
import { APIError, api, type SetupResult } from '../api'

const emit = defineEmits<{ ready: [] }>()

const step = ref(1)
const submitting = ref(false)
const showPassword = ref(false)
const error = ref('')
const result = ref<SetupResult | null>(null)
const copied = ref(false)
const slugEdited = ref(false)

const form = reactive({
  bootstrapToken: '',
  organizationName: '',
  ownerEmail: '',
  ownerPassword: '',
  providerName: 'AutoDL',
  providerBaseURL: 'https://api.autodl.com',
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
  region: 'westDC2',
  gpuNames: 'RTX 4090',
  gpuNum: 1,
  cudaFrom: 118,
  cudaTo: 128,
  cpuFrom: 1,
  cpuTo: 128,
  memoryFromGB: 1,
  memoryToGB: 512,
  priceFromCNY: 0.01,
  priceToCNY: 3,
  reuseContainer: false,
})

const stepLabel = computed(() => ['Owner', 'Provider', 'Project'][step.value - 1])

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

function validate(current: number): string {
  if (current === 1) {
    if (form.bootstrapToken.trim().length < 32) return 'Enter the bootstrap token generated on the control server.'
    if (!form.organizationName.trim()) return 'Organization name is required.'
    if (!/^\S+@\S+\.\S+$/.test(form.ownerEmail)) return 'Enter a valid Owner email.'
    if (form.ownerPassword.length < 12) return 'Owner password must contain at least 12 characters.'
  }
  if (current === 2) {
    if (!form.providerName.trim() || !form.providerToken.trim()) return 'Provider name and AutoDL token are required.'
    if (!form.providerBaseURL.startsWith('https://')) return 'Provider API URL must use HTTPS.'
  }
  if (current === 3) {
    if (!form.projectName.trim() || !/^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$/.test(form.projectSlug)) {
      return 'Project name and a 3-64 character lowercase slug are required.'
    }
    if (form.monthlyBudgetCNY <= 0 || form.maxExperimentCNY <= 0 || form.maxExperimentCNY > form.monthlyBudgetCNY) {
      return 'Experiment cap must be positive and no greater than the monthly budget.'
    }
    if (!form.imageUUID.trim()) return 'AutoDL image UUID is required.'
    if (!form.region.trim() || !form.gpuNames.trim() || form.priceToCNY <= 0) return 'Resource region, GPU candidates, and price ceiling are required.'
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
    error.value = caught instanceof APIError ? caught.message : 'Initialization failed. Check the control-plane logs.'
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
        <div><strong>Gemcp</strong><span>Initial configuration</span></div>
      </div>
      <span v-if="!result" class="setup-step-count">Step {{ step }} of 3 / {{ stepLabel }}</span>
    </header>

    <section v-if="result" class="token-reveal" aria-live="polite">
      <div class="success-mark"><Check :size="24" /></div>
      <p class="eyebrow">Initialization complete</p>
      <h1>Default Agent token</h1>
      <p class="supporting-copy">This credential is shown only in the setup response. Store it in the Agent's secret configuration.</p>
      <div class="token-value">
        <code>{{ result.agent_token }}</code>
        <button class="icon-button" type="button" :title="copied ? 'Copied' : 'Copy Agent token'" @click="copyToken">
          <Check v-if="copied" :size="17" /><Clipboard v-else :size="17" />
        </button>
      </div>
      <dl class="setup-result-grid">
        <div><dt>Project</dt><dd>{{ result.project_id }}</dd></div>
        <div><dt>Token prefix</dt><dd>{{ result.agent_token_prefix }}</dd></div>
      </dl>
      <button class="primary-button" type="button" @click="emit('ready')">Continue to sign in <ArrowRight :size="16" /></button>
    </section>

    <template v-else>
      <div class="setup-progress" aria-label="Setup progress">
        <button v-for="item in 3" :key="item" type="button" :class="{ active: step === item, complete: step > item }" @click="item < step && (step = item)">
          <span><Check v-if="step > item" :size="13" /><template v-else>{{ item }}</template></span>{{ ['Owner', 'Provider', 'Project'][item - 1] }}
        </button>
      </div>

      <form class="setup-form" @submit.prevent="submit">
        <section v-if="step === 1" class="form-section">
          <div class="form-heading"><p class="eyebrow">Access boundary</p><h1>Create the Owner</h1></div>
          <div class="form-grid two-columns">
            <label class="full-field">Bootstrap token<input v-model="form.bootstrapToken" type="password" autocomplete="off" spellcheck="false" /></label>
            <label>Organization name<input v-model="form.organizationName" autocomplete="organization" /></label>
            <label>Owner email<input v-model="form.ownerEmail" type="email" autocomplete="username" /></label>
            <label class="full-field">Owner password<span class="password-field"><input v-model="form.ownerPassword" :type="showPassword ? 'text' : 'password'" autocomplete="new-password" /><button type="button" :title="showPassword ? 'Hide password' : 'Show password'" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" :size="17" /><Eye v-else :size="17" /></button></span></label>
          </div>
        </section>

        <section v-else-if="step === 2" class="form-section">
          <div class="form-heading"><p class="eyebrow">Credential custody</p><h1>Connect AutoDL</h1></div>
          <div class="form-grid two-columns">
            <label>Provider name<input v-model="form.providerName" /></label>
            <label>API base URL<input v-model="form.providerBaseURL" type="url" spellcheck="false" /></label>
            <label class="full-field">AutoDL API token<input v-model="form.providerToken" type="password" autocomplete="off" spellcheck="false" /></label>
          </div>
          <p class="form-note">The token is encrypted before it is written to PostgreSQL. Live compute remains disabled until phase-zero validation is accepted.</p>
        </section>

        <section v-else class="form-section wide-form-section">
          <div class="form-heading"><p class="eyebrow">Default policy</p><h1>Configure the first project</h1></div>
          <div class="form-subsection">
            <h2>Project limits</h2>
            <div class="form-grid four-columns">
              <label class="span-two">Project name<input v-model="form.projectName" @input="updateProjectName" /></label>
              <label class="span-two">Project slug<input v-model="form.projectSlug" spellcheck="false" @input="slugEdited = true" /></label>
              <label>Monthly budget (CNY)<input v-model.number="form.monthlyBudgetCNY" type="number" min="0.001" step="0.001" /></label>
              <label>Experiment cap (CNY)<input v-model.number="form.maxExperimentCNY" type="number" min="0.001" step="0.001" /></label>
              <label>Concurrency<input v-model.number="form.maxConcurrency" type="number" min="1" max="32" /></label>
              <label>Max runtime (hours)<input v-model.number="form.maxRuntimeHours" type="number" min="0.1" max="168" step="0.1" /></label>
            </div>
          </div>
          <div class="form-subsection">
            <h2>Environment and resources</h2>
            <div class="form-grid four-columns">
              <label>Environment name<input v-model="form.environmentName" /></label>
              <label class="span-three">AutoDL image UUID<input v-model="form.imageUUID" spellcheck="false" /></label>
              <label>Profile name<input v-model="form.profileName" /></label>
              <label>Region<input v-model="form.region" spellcheck="false" /></label>
              <label class="span-two">GPU candidates<input v-model="form.gpuNames" placeholder="RTX 4090, RTX 5090" /></label>
              <label>GPU count<input v-model.number="form.gpuNum" type="number" min="1" max="4" /></label>
              <label>CUDA from<input v-model.number="form.cudaFrom" type="number" min="1" /></label>
              <label>CUDA to<input v-model.number="form.cudaTo" type="number" min="1" /></label>
              <label>CPU from<input v-model.number="form.cpuFrom" type="number" min="1" /></label>
              <label>CPU to<input v-model.number="form.cpuTo" type="number" min="1" /></label>
              <label>Memory from (GB)<input v-model.number="form.memoryFromGB" type="number" min="1" /></label>
              <label>Memory to (GB)<input v-model.number="form.memoryToGB" type="number" min="1" /></label>
              <label>Price floor (CNY/h)<input v-model.number="form.priceFromCNY" type="number" min="0" step="0.001" /></label>
              <label>Price ceiling (CNY/h)<input v-model.number="form.priceToCNY" type="number" min="0.001" step="0.001" /></label>
              <label class="checkbox-field span-two"><input v-model="form.reuseContainer" type="checkbox" /><span>Allow best-effort stopped-container reuse</span></label>
            </div>
          </div>
        </section>

        <div v-if="error" class="form-error" role="alert">{{ error }}</div>
        <footer class="setup-actions">
          <button v-if="step > 1" class="secondary-button" type="button" @click="step -= 1"><ArrowLeft :size="16" />Back</button>
          <span v-else />
          <button v-if="step < 3" class="primary-button" type="button" @click="next">Continue<ArrowRight :size="16" /></button>
          <button v-else class="primary-button" type="submit" :disabled="submitting"><LoaderCircle v-if="submitting" :size="16" class="spinning" /><Check v-else :size="16" />Initialize Gemcp</button>
        </footer>
      </form>
    </template>
  </main>
</template>
