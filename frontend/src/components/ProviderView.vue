<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import {
  Boxes,
  CheckCircle2,
  Cpu,
  Database,
  Gauge,
  HardDrive,
  Image,
  KeyRound,
  LoaderCircle,
  RefreshCw,
  Server,
  X,
} from '@lucide/vue'
import {
  APIError,
  api,
  type ProviderDeployment,
  type ProviderDeploymentDetails,
  type ProviderImage,
  type ProviderResources,
  type ProviderSummary,
} from '../api'

const props = defineProps<{ active: boolean }>()
const emit = defineEmits<{ unauthorized: [] }>()

type ProviderTab = 'inventory' | 'images' | 'deployments' | 'containers'

const summary = ref<ProviderSummary | null>(null)
const resources = ref<ProviderResources | null>(null)
const loading = ref(false)
const initialized = ref(false)
const error = ref('')
const activeTab = ref<ProviderTab>('inventory')
const providerTabs: ProviderTab[] = ['inventory', 'images', 'deployments', 'containers']
const credentialDialog = ref(false)
const credentialBusy = ref(false)
const credentialError = ref('')
const selectedDeployment = ref<ProviderDeploymentDetails | null>(null)
const deploymentLoading = ref(false)
const credentialForm = reactive({ name: 'AutoDL Private Cloud', token: '' })

const images = computed(() => [...(resources.value?.private_images ?? []), ...(resources.value?.system_images ?? [])])
const idleGPU = computed(() => (resources.value?.gpu_stock ?? []).reduce((total, item) => total + item.idle, 0))
const totalGPU = computed(() => (resources.value?.gpu_stock ?? []).reduce((total, item) => total + item.total, 0))
const activeDeploymentCount = computed(() => (resources.value?.deployments ?? []).filter((item) => {
  const terminalStatus = ['stopped', 'finished', 'completed', 'failed', 'shutdown'].includes(item.status.toLowerCase())
  return !terminalStatus && item.finished_num < item.replica_num
}).length)
const reusableCacheCount = computed(() => (resources.value?.cached_containers ?? []).filter((item) => item.status === 'in_cache').length)

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

async function loadProvider() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    summary.value = await api.provider()
    if (summary.value.credential_configured) {
      resources.value = await api.queryProvider()
      summary.value = resources.value.provider
    }
    initialized.value = true
  } catch (caught) {
    handleError(caught, 'Could not load Private Cloud resources.')
  } finally {
    loading.value = false
  }
}

async function refreshLive() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    resources.value = await api.queryProvider()
    summary.value = resources.value.provider
    initialized.value = true
  } catch (caught) {
    handleError(caught, 'Private Cloud refresh failed.')
  } finally {
    loading.value = false
  }
}

function openCredentialDialog() {
  credentialForm.name = summary.value?.name || 'AutoDL Private Cloud'
  credentialForm.token = ''
  credentialError.value = ''
  credentialDialog.value = true
}

function closeCredentialDialog() {
  if (credentialBusy.value) return
  credentialForm.token = ''
  credentialError.value = ''
  credentialDialog.value = false
}

async function configureProvider() {
  if (credentialForm.token.trim().length < 32) {
    credentialError.value = 'Enter a valid AutoDL Private Cloud Developer Token.'
    return
  }
  credentialBusy.value = true
  credentialError.value = ''
  try {
    const result = await api.configureProvider({
      name: credentialForm.name.trim(),
      base_url: 'https://private.autodl.com',
      token: credentialForm.token.trim(),
    })
    summary.value = result.provider
    resources.value = result.resources
    credentialForm.token = ''
    credentialDialog.value = false
    initialized.value = true
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) {
      emit('unauthorized')
    } else {
      credentialError.value = caught instanceof APIError ? caught.message : 'Provider validation failed.'
    }
  } finally {
    credentialForm.token = ''
    credentialBusy.value = false
  }
}

async function openDeployment(deployment: ProviderDeployment) {
  deploymentLoading.value = true
  error.value = ''
  try {
    selectedDeployment.value = await api.providerDeployment(deployment.uuid)
  } catch (caught) {
    handleError(caught, 'Could not load Provider deployment details.')
  } finally {
    deploymentLoading.value = false
  }
}

function dateTime(value?: string) {
  if (!value) return 'Not set'
  return new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

function bytes(value = 0) {
  if (!value) return '0 B'
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB']
  const index = Math.min(Math.floor(Math.log(value) / Math.log(1024)), units.length - 1)
  return `${(value / 1024 ** index).toFixed(index > 2 ? 1 : 0)} ${units[index]}`
}

function money(milli = 0) {
  return `CNY ${(milli / 1000).toFixed(3)}/h`
}

function stateLabel(value: string) {
  return value.replaceAll('_', ' ')
}

function imageName(image: ProviderImage) {
  return image.name || image.uuid
}

function containerCount(deploymentID: string) {
  const active = resources.value?.active_containers.filter((item) => item.deployment_uuid === deploymentID).length ?? 0
  const cached = resources.value?.cached_containers.filter((item) => item.deployment_uuid === deploymentID).length ?? 0
  return active + cached
}

watch(
  () => props.active,
  (active) => {
    if (active && !initialized.value) void loadProvider()
  },
  { immediate: true },
)
</script>

<template>
  <section class="provider-page">
    <div v-if="error" class="page-alert provider-alert" role="alert">{{ error }}<button type="button" title="Dismiss" @click="error = ''"><X :size="16" /></button></div>

    <header class="provider-heading">
      <div>
        <p class="eyebrow">Live Provider</p>
        <h2>{{ summary?.name ?? 'AutoDL Private Cloud' }}</h2>
        <p>{{ summary?.base_url ?? 'https://private.autodl.com' }}</p>
      </div>
      <div class="provider-actions">
        <span class="state-badge" :data-state="summary?.status ?? 'pending_validation'"><span />{{ stateLabel(summary?.status ?? 'not connected') }}</span>
        <button class="secondary-button" type="button" @click="openCredentialDialog"><KeyRound :size="16" />Rotate token</button>
        <button class="primary-button" type="button" :disabled="loading || !summary?.credential_configured" @click="refreshLive"><RefreshCw :size="16" :class="{ spinning: loading }" />Refresh live</button>
      </div>
    </header>

    <div v-if="loading && !resources" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>Querying Private Cloud</span></div>

    <template v-else-if="resources">
      <section class="provider-metrics">
        <div><span><Gauge :size="16" />GPU capacity</span><strong>{{ idleGPU }} / {{ totalGPU }}</strong><small>Idle / total</small></div>
        <div><span><Image :size="16" />Images</span><strong>{{ images.length }}</strong><small>{{ resources.private_images.length }} private, {{ resources.system_images.length }} system</small></div>
        <div><span><Server :size="16" />Deployments</span><strong>{{ activeDeploymentCount }}</strong><small>{{ resources.deployments.length }} returned by Provider</small></div>
        <div><span><HardDrive :size="16" />Reusable cache</span><strong>{{ reusableCacheCount }}</strong><small>{{ resources.cached_containers.length }} released containers</small></div>
      </section>

      <section class="provider-connection-band">
        <div><span>Backend</span><strong>{{ summary?.backend }}</strong></div>
        <div><span>Credential</span><strong>{{ summary?.credential_configured ? 'Configured' : 'Missing' }}</strong></div>
        <div><span>Last validated</span><strong>{{ dateTime(summary?.last_validated_at) }}</strong></div>
        <div><span>Snapshot</span><strong>{{ dateTime(resources.generated_at) }}</strong></div>
      </section>

      <div v-if="resources.truncated?.length" class="provider-truncation" role="status">Showing the first 1,000 records for: {{ resources.truncated.join(', ') }}</div>

      <div class="provider-tabs segmented-control" aria-label="Provider resource view">
        <button v-for="tab in providerTabs" :key="tab" type="button" :class="{ active: activeTab === tab }" @click="activeTab = tab">{{ tab }}</button>
      </div>

      <section v-if="activeTab === 'inventory'" class="provider-workspace">
        <div class="section-heading"><div><h2>GPU inventory</h2><p>Current schedulable capacity returned by Private Cloud.</p></div><Cpu :size="18" /></div>
        <div v-if="resources.gpu_stock.length" class="table-scroll">
          <table class="data-table provider-table">
            <thead><tr><th>GPU model</th><th>Idle</th><th>Total</th><th>Utilized</th><th>Availability</th></tr></thead>
            <tbody><tr v-for="gpu in resources.gpu_stock" :key="gpu.name"><td><strong>{{ gpu.name }}</strong></td><td>{{ gpu.idle }}</td><td>{{ gpu.total }}</td><td>{{ gpu.total - gpu.idle }}</td><td><div class="capacity"><span :style="{ width: `${gpu.total ? (gpu.idle / gpu.total) * 100 : 0}%` }" /></div></td></tr></tbody>
          </table>
        </div>
      </section>

      <section v-else-if="activeTab === 'images'" class="provider-workspace">
        <div class="section-heading"><div><h2>Images</h2><p>Private and system images visible to the configured Token.</p></div><Image :size="18" /></div>
        <div class="table-scroll">
          <table class="data-table provider-table image-table">
            <thead><tr><th>Source</th><th>Name</th><th>UUID</th><th>CUDA</th><th>Chip</th><th>Architecture</th><th>Status</th></tr></thead>
            <tbody><tr v-for="item in images" :key="item.uuid"><td><span class="source-chip">{{ item.source }}</span></td><td><strong>{{ imageName(item) }}</strong></td><td><code>{{ item.uuid }}</code></td><td>{{ item.cuda_version || 'Not set' }}</td><td>{{ item.chip_corp || 'Not set' }}</td><td>{{ item.cpu_arch || 'Not set' }}</td><td>{{ item.status || 'available' }}</td></tr></tbody>
          </table>
        </div>
      </section>

      <section v-else-if="activeTab === 'deployments'" class="provider-workspace">
        <div class="section-heading"><div><h2>Deployments</h2><p>Live Provider deployment state and completion counters.</p></div><Boxes :size="18" /></div>
        <div v-if="resources.deployments.length" class="table-scroll">
          <table class="data-table provider-table deployment-table">
            <thead><tr><th>Status</th><th>Name</th><th>Type</th><th>Replicas</th><th>Starting</th><th>Running</th><th>Finished</th><th>Failed</th><th>Containers</th><th>Created</th></tr></thead>
            <tbody><tr v-for="deployment in resources.deployments" :key="deployment.uuid" tabindex="0" @click="openDeployment(deployment)" @keydown.enter="openDeployment(deployment)"><td><span class="state-badge" :data-state="deployment.status"><span />{{ stateLabel(deployment.status) }}</span></td><td><strong>{{ deployment.name }}</strong><small><code>{{ deployment.uuid }}</code></small></td><td>{{ deployment.type }}</td><td>{{ deployment.replica_num }}</td><td>{{ deployment.starting_num }}</td><td>{{ deployment.running_num }}</td><td>{{ deployment.finished_num }}</td><td>{{ deployment.failed_num }}</td><td>{{ containerCount(deployment.uuid) }}</td><td>{{ dateTime(deployment.created_at) }}</td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><CheckCircle2 :size="21" /></span><h3>No deployments</h3><p>The configured Provider Token returned no active deployment records.</p></div>
      </section>

      <section v-else class="provider-workspace">
        <div class="section-heading"><div><h2>Containers and cache</h2><p>Active and released containers with access credentials removed.</p></div><Database :size="18" /></div>
        <div v-if="resources.active_containers.length || resources.cached_containers.length" class="table-scroll">
          <table class="data-table provider-table container-table">
            <thead><tr><th>Status</th><th>Container</th><th>Deployment</th><th>GPU</th><th>CPU</th><th>Memory</th><th>Price</th><th>Machine</th><th>Started</th><th>Stopped</th></tr></thead>
            <tbody>
              <tr v-for="container in [...resources.active_containers, ...resources.cached_containers]" :key="container.uuid"><td><span class="state-badge" :data-state="container.status"><span />{{ stateLabel(container.status) }}</span></td><td><code>{{ container.uuid }}</code></td><td><code>{{ container.deployment_uuid }}</code></td><td>{{ container.gpu_num }} x {{ container.gpu_name }}</td><td>{{ container.cpu_num }}</td><td>{{ bytes(container.memory_bytes) }}</td><td>{{ money(container.price_milli_per_hour) }}</td><td><code>{{ container.machine_id || 'Not set' }}</code></td><td>{{ dateTime(container.started_at) }}</td><td>{{ dateTime(container.stopped_at) }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Server :size="21" /></span><h3>No containers</h3><p>No active or released containers were returned.</p></div>
      </section>
    </template>

    <div v-else-if="summary && !summary.credential_configured" class="empty-state provider-empty"><span class="empty-icon"><KeyRound :size="21" /></span><h3>Provider credential missing</h3><button class="primary-button" type="button" @click="openCredentialDialog"><KeyRound :size="16" />Configure token</button></div>
  </section>

  <div v-if="credentialDialog" class="modal-backdrop" @click.self="closeCredentialDialog">
    <section class="modal" role="dialog" aria-modal="true" aria-label="Rotate Provider token">
      <header><div><p class="eyebrow">Credential custody</p><h2>Rotate Provider token</h2></div><button class="icon-button" type="button" title="Close" :disabled="credentialBusy" @click="closeCredentialDialog"><X :size="17" /></button></header>
      <form class="dialog-form" @submit.prevent="configureProvider">
        <label>Provider name<input v-model="credentialForm.name" required maxlength="120" /></label>
        <label>API base URL<input value="https://private.autodl.com" disabled /></label>
        <label>Developer Token<input v-model="credentialForm.token" type="password" required autocomplete="off" spellcheck="false" /></label>
        <p class="form-note">The new Token is validated before replacing the encrypted credential.</p>
        <div v-if="credentialError" class="form-error" role="alert">{{ credentialError }}</div>
        <button class="primary-button" type="submit" :disabled="credentialBusy"><LoaderCircle v-if="credentialBusy" :size="16" class="spinning" /><KeyRound v-else :size="16" />Validate and rotate</button>
      </form>
    </section>
  </div>

  <div v-if="selectedDeployment || deploymentLoading" class="modal-backdrop" @click.self="selectedDeployment = null">
    <section class="detail-panel provider-detail" role="dialog" aria-modal="true" aria-label="Provider deployment details">
      <header><div><p class="eyebrow">Private Cloud deployment</p><h2>{{ selectedDeployment?.deployment.name ?? 'Loading deployment' }}</h2></div><button class="icon-button" type="button" title="Close details" @click="selectedDeployment = null"><X :size="17" /></button></header>
      <div v-if="deploymentLoading" class="provider-loading"><LoaderCircle :size="20" class="spinning" /></div>
      <template v-else-if="selectedDeployment">
        <div class="detail-state"><span class="state-badge" :data-state="selectedDeployment.deployment.status"><span />{{ stateLabel(selectedDeployment.deployment.status) }}</span><code>{{ selectedDeployment.deployment.uuid }}</code></div>
        <dl class="detail-list provider-detail-list">
          <div><dt>Type</dt><dd>{{ selectedDeployment.deployment.type }}</dd></div>
          <div><dt>Image</dt><dd><code>{{ selectedDeployment.deployment.image_uuid || 'Not set' }}</code></dd></div>
          <div><dt>Replicas</dt><dd>{{ selectedDeployment.deployment.replica_num }} requested / {{ selectedDeployment.deployment.running_num }} running / {{ selectedDeployment.deployment.finished_num }} finished / {{ selectedDeployment.deployment.failed_num }} failed</dd></div>
          <div><dt>Container reuse</dt><dd>{{ selectedDeployment.deployment.reuse_container ? 'Enabled' : 'Disabled' }}</dd></div>
          <div><dt>Created</dt><dd>{{ dateTime(selectedDeployment.deployment.created_at) }}</dd></div>
          <div><dt>Snapshot</dt><dd>{{ dateTime(selectedDeployment.generated_at) }}</dd></div>
        </dl>
        <div v-if="selectedDeployment.truncated?.length" class="provider-truncation">Showing the first 1,000 records for: {{ selectedDeployment.truncated.join(', ') }}</div>
        <section class="provider-detail-section"><h3>Containers</h3><div v-for="container in [...selectedDeployment.active_containers, ...selectedDeployment.released_containers]" :key="container.uuid" class="provider-detail-row"><span class="state-badge" :data-state="container.status"><span />{{ stateLabel(container.status) }}</span><code>{{ container.uuid }}</code><span>{{ container.gpu_num }} x {{ container.gpu_name }}</span><span>{{ money(container.price_milli_per_hour) }}</span></div><p v-if="!selectedDeployment.active_containers.length && !selectedDeployment.released_containers.length">No containers returned.</p></section>
        <section class="provider-detail-section"><h3>Events</h3><div v-for="event in selectedDeployment.events" :key="`${event.container_uuid}-${event.created_at}-${event.status}`" class="provider-event"><span class="status-dot" /><strong>{{ stateLabel(event.status) }}</strong><code>{{ event.container_uuid }}</code><time>{{ dateTime(event.created_at) }}</time></div><p v-if="!selectedDeployment.events.length">No events returned.</p></section>
      </template>
    </section>
  </div>
</template>
