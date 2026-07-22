<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
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
  ShieldAlert,
  Square,
  X,
} from '@lucide/vue'
import {
  APIError,
  api,
  type ProviderDeployment,
  type ProviderDeploymentDetails,
  type ManagedProviderResource,
  type ProviderImage,
  type ProviderResources,
  type ProviderSummary,
} from '../api'
import { localizedState, useI18n } from '../i18n'

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
const managedResources = ref<ManagedProviderResource[]>([])
const resourceAction = ref('')
const emergencyDialog = ref(false)
const emergencyBusy = ref(false)
const emergencyConfirmation = ref('')
const operationError = ref('')
const credentialForm = reactive({ name: 'AutoDL Private Cloud', token: '' })
const { languageTag, t } = useI18n()
const lastSuccessfulRefreshAt = ref(0)
const autoRefreshSeconds = 60
let autoRefreshTimer: number | undefined

const images = computed(() => [...(resources.value?.private_images ?? []), ...(resources.value?.system_images ?? [])])
const idleGPU = computed(() => (resources.value?.gpu_stock ?? []).reduce((total, item) => total + item.idle, 0))
const totalGPU = computed(() => (resources.value?.gpu_stock ?? []).reduce((total, item) => total + item.total, 0))
const activeDeploymentCount = computed(() => (resources.value?.deployments ?? []).filter((item) => {
  const terminalStatus = ['stopped', 'finished', 'completed', 'failed', 'shutdown'].includes(item.status.toLowerCase())
  return !terminalStatus && item.finished_num < item.replica_num
}).length)
const reusableCacheCount = computed(() => (resources.value?.cached_containers ?? []).filter((item) => item.status === 'in_cache').length)
const managedByProvider = computed(() => new Map(managedResources.value.filter((item) => item.provider_id).map((item) => [item.provider_id as string, item])))
const managedActiveCount = computed(() => managedResources.value.filter((item) => !['deleted', 'error'].includes(item.state)).length)
const selectedManagedResource = computed(() => selectedDeployment.value ? managedByProvider.value.get(selectedDeployment.value.deployment.uuid) : undefined)

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

function markRefreshSuccessful() {
  lastSuccessfulRefreshAt.value = Date.now()
}

function stopAutoRefresh() {
  if (autoRefreshTimer !== undefined) {
    window.clearTimeout(autoRefreshTimer)
    autoRefreshTimer = undefined
  }
}

function scheduleAutoRefresh() {
  stopAutoRefresh()
  if (!props.active) return
  autoRefreshTimer = window.setTimeout(async () => {
    autoRefreshTimer = undefined
    if (props.active && document.visibilityState === 'visible') {
      if (!initialized.value) await loadProvider()
      else if (summary.value?.credential_configured) await refreshProvider()
    }
    scheduleAutoRefresh()
  }, autoRefreshSeconds * 1000)
}

async function loadProvider() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  try {
    summary.value = await api.provider()
    const managedPromise = api.managedProviderResources()
    if (summary.value.credential_configured) {
      const [snapshot, managed] = await Promise.all([api.queryProvider(), managedPromise])
      resources.value = snapshot
      summary.value = snapshot.provider
      managedResources.value = managed
    } else {
      managedResources.value = await managedPromise
    }
    initialized.value = true
    markRefreshSuccessful()
  } catch (caught) {
    handleError(caught, t('Could not load Private Cloud resources.', '无法加载私有云资源。'))
  } finally {
    loading.value = false
  }
}

async function refreshProvider() {
  if (loading.value || !summary.value?.credential_configured) return
  loading.value = true
  error.value = ''
  try {
    const [snapshot, managed] = await Promise.all([api.queryProvider(), api.managedProviderResources()])
    resources.value = snapshot
    summary.value = snapshot.provider
    managedResources.value = managed
    initialized.value = true
    markRefreshSuccessful()
  } catch (caught) {
    handleError(caught, t('Private Cloud refresh failed.', '私有云刷新失败。'))
  } finally {
    loading.value = false
  }
}

async function refreshLive() {
  if (initialized.value) await refreshProvider()
  else await loadProvider()
  scheduleAutoRefresh()
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
    credentialError.value = t('Enter a valid AutoDL Private Cloud Developer Token.', '请输入有效的 AutoDL 私有云 Developer Token。')
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
    managedResources.value = await api.managedProviderResources()
    credentialForm.token = ''
    credentialDialog.value = false
    initialized.value = true
    markRefreshSuccessful()
    scheduleAutoRefresh()
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) {
      emit('unauthorized')
    } else {
      credentialError.value = caught instanceof APIError ? caught.message : t('Provider validation failed.', 'Provider 验证失败。')
    }
  } finally {
    credentialForm.token = ''
    credentialBusy.value = false
  }
}

function managedFor(deploymentID: string) {
  return managedByProvider.value.get(deploymentID)
}

async function requestStop(deployment: ProviderDeployment) {
  if (!managedFor(deployment.uuid) || resourceAction.value) return
  resourceAction.value = deployment.uuid
  operationError.value = ''
  try {
    const updated = await api.stopManagedDeployment(deployment.uuid)
    managedResources.value = managedResources.value.map((item) => item.id === updated.id ? updated : item)
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else operationError.value = caught instanceof APIError ? caught.message : t('Could not request deployment stop.', '无法请求停止部署。')
  } finally {
    resourceAction.value = ''
  }
}

function openEmergencyDialog() {
  emergencyConfirmation.value = ''
  operationError.value = ''
  emergencyDialog.value = true
}

async function emergencyStop() {
  if (emergencyConfirmation.value !== 'STOP' || emergencyBusy.value) return
  emergencyBusy.value = true
  operationError.value = ''
  try {
    await api.emergencyStop(emergencyConfirmation.value)
    managedResources.value = await api.managedProviderResources()
    emergencyDialog.value = false
    emergencyConfirmation.value = ''
  } catch (caught) {
    if (caught instanceof APIError && caught.status === 401) emit('unauthorized')
    else operationError.value = caught instanceof APIError ? caught.message : t('Emergency stop request failed.', 'Emergency Stop 请求失败。')
  } finally {
    emergencyBusy.value = false
  }
}

async function openDeployment(deployment: ProviderDeployment) {
  deploymentLoading.value = true
  error.value = ''
  try {
    selectedDeployment.value = await api.providerDeployment(deployment.uuid)
  } catch (caught) {
    handleError(caught, t('Could not load Provider deployment details.', '无法加载 Provider 部署详情。'))
  } finally {
    deploymentLoading.value = false
  }
}

function dateTime(value?: string) {
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
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
  return localizedState(value)
}

function tabLabel(tab: ProviderTab) {
  return {
    inventory: t('inventory', '库存'),
    images: t('images', '镜像'),
    deployments: t('deployments', '部署'),
    containers: t('containers', '容器'),
  }[tab]
}

function imageName(image: ProviderImage) {
  return image.name || image.uuid
}

function containerCount(deploymentID: string) {
  const active = resources.value?.active_containers.filter((item) => item.deployment_uuid === deploymentID).length ?? 0
  const cached = resources.value?.cached_containers.filter((item) => item.deployment_uuid === deploymentID).length ?? 0
  return active + cached
}

function refreshIfStale() {
  if (!props.active || document.visibilityState !== 'visible') return
  if (!initialized.value) {
    void loadProvider()
  } else if (summary.value?.credential_configured && Date.now() - lastSuccessfulRefreshAt.value >= autoRefreshSeconds * 1000) {
    void refreshProvider()
  }
}

function handleVisibilityChange() {
  if (!props.active) return
  if (document.visibilityState === 'visible') {
    refreshIfStale()
    scheduleAutoRefresh()
  } else {
    stopAutoRefresh()
  }
}

watch(
  () => props.active,
  (active) => {
    if (active) {
      refreshIfStale()
      scheduleAutoRefresh()
    } else {
      stopAutoRefresh()
    }
  },
  { immediate: true },
)

onMounted(() => document.addEventListener('visibilitychange', handleVisibilityChange))
onBeforeUnmount(() => {
  stopAutoRefresh()
  document.removeEventListener('visibilitychange', handleVisibilityChange)
})
</script>

<template>
  <section class="provider-page">
    <div v-if="error || operationError" class="page-alert provider-alert" role="alert">{{ error || operationError }}<button type="button" :title="t('Dismiss', '关闭')" @click="error = ''; operationError = ''"><X :size="16" /></button></div>

    <header class="provider-heading">
      <div>
        <p class="eyebrow">{{ t('Live Provider', '实时 Provider') }}</p>
        <h2>{{ summary?.name ?? 'AutoDL Private Cloud' }}</h2>
        <p>{{ summary?.base_url ?? 'https://private.autodl.com' }}</p>
      </div>
      <div class="provider-actions">
        <span class="state-badge" :data-state="summary?.status ?? 'pending_validation'"><span />{{ stateLabel(summary?.status ?? 'not connected') }}</span>
        <button class="danger-button" type="button" :disabled="managedActiveCount === 0" @click="openEmergencyDialog"><ShieldAlert :size="16" />Emergency Stop</button>
        <button class="secondary-button" type="button" @click="openCredentialDialog"><KeyRound :size="16" />{{ t('Rotate token', '轮换 Token') }}</button>
        <button class="primary-button" type="button" :disabled="loading || (initialized && !summary?.credential_configured)" @click="refreshLive"><RefreshCw :size="16" :class="{ spinning: loading }" />{{ t('Refresh live', '刷新实时数据') }}</button>
      </div>
    </header>

    <div v-if="loading && !resources" class="provider-loading"><LoaderCircle :size="20" class="spinning" /><span>{{ t('Querying Private Cloud', '正在查询私有云') }}</span></div>

    <template v-else-if="resources">
      <section class="provider-metrics">
        <div><span><Gauge :size="16" />{{ t('GPU capacity', 'GPU 容量') }}</span><strong>{{ idleGPU }} / {{ totalGPU }}</strong><small>{{ t('Idle / total', '空闲 / 总量') }}</small></div>
        <div><span><Image :size="16" />{{ t('Images', '镜像') }}</span><strong>{{ images.length }}</strong><small>{{ resources.private_images.length }} {{ t('private', '私有') }}, {{ resources.system_images.length }} {{ t('system', '系统') }}</small></div>
        <div><span><Server :size="16" />{{ t('Deployments', '部署') }}</span><strong>{{ activeDeploymentCount }}</strong><small>{{ managedActiveCount }} {{ t('managed', '受管') }} / {{ resources.deployments.length }} Provider</small></div>
        <div><span><HardDrive :size="16" />{{ t('Reusable cache', '可复用缓存') }}</span><strong>{{ reusableCacheCount }}</strong><small>{{ resources.cached_containers.length }} {{ t('released containers', '个已释放容器') }}</small></div>
      </section>

      <section class="provider-connection-band">
        <div><span>Backend</span><strong>{{ summary?.backend }}</strong></div>
        <div><span>{{ t('Credential', '凭据') }}</span><strong>{{ summary?.credential_configured ? t('Configured', '已配置') : t('Missing', '缺失') }}</strong></div>
        <div><span>{{ t('Last validated', '最后验证') }}</span><strong>{{ dateTime(summary?.last_validated_at) }}</strong></div>
        <div><span>{{ t('Snapshot / auto', '快照 / 自动') }} {{ autoRefreshSeconds }}s</span><strong>{{ dateTime(resources.generated_at) }}</strong></div>
      </section>

      <div v-if="resources.truncated?.length" class="provider-truncation" role="status">{{ t('Showing the first 1,000 records for:', '以下类别仅显示前 1,000 条：') }} {{ resources.truncated.join(', ') }}</div>

      <div class="provider-tabs segmented-control" :aria-label="t('Provider resource view', 'Provider 资源视图')">
        <button v-for="tab in providerTabs" :key="tab" type="button" :class="{ active: activeTab === tab }" @click="activeTab = tab">{{ tabLabel(tab) }}</button>
      </div>

      <section v-if="activeTab === 'inventory'" class="provider-workspace">
        <div class="section-heading"><div><h2>{{ t('GPU inventory', 'GPU 库存') }}</h2><p>{{ t('Current schedulable capacity returned by Private Cloud.', '私有云返回的当前可调度容量。') }}</p></div><Cpu :size="18" /></div>
        <div v-if="resources.gpu_stock.length" class="table-scroll">
          <table class="data-table provider-table">
            <thead><tr><th>{{ t('GPU model', 'GPU 型号') }}</th><th>{{ t('Idle', '空闲') }}</th><th>{{ t('Total', '总量') }}</th><th>{{ t('Utilized', '已使用') }}</th><th>{{ t('Availability', '可用率') }}</th></tr></thead>
            <tbody><tr v-for="gpu in resources.gpu_stock" :key="gpu.name"><td><strong>{{ gpu.name }}</strong></td><td>{{ gpu.idle }}</td><td>{{ gpu.total }}</td><td>{{ gpu.total - gpu.idle }}</td><td><div class="capacity"><span :style="{ width: `${gpu.total ? (gpu.idle / gpu.total) * 100 : 0}%` }" /></div></td></tr></tbody>
          </table>
        </div>
      </section>

      <section v-else-if="activeTab === 'images'" class="provider-workspace">
        <div class="section-heading"><div><h2>{{ t('Images', '镜像') }}</h2><p>{{ t('Private and system images visible to the configured Token.', '已配置 Token 可见的私有镜像和系统镜像。') }}</p></div><Image :size="18" /></div>
        <div class="table-scroll">
          <table class="data-table provider-table image-table">
            <thead><tr><th>{{ t('Source', '来源') }}</th><th>{{ t('Name', '名称') }}</th><th>UUID</th><th>CUDA</th><th>{{ t('Chip', '芯片') }}</th><th>{{ t('Architecture', '架构') }}</th><th>{{ t('Status', '状态') }}</th></tr></thead>
            <tbody><tr v-for="item in images" :key="item.uuid"><td><span class="source-chip">{{ item.source }}</span></td><td><strong>{{ imageName(item) }}</strong></td><td><code>{{ item.uuid }}</code></td><td>{{ item.cuda_version || t('Not set', '未设置') }}</td><td>{{ item.chip_corp || t('Not set', '未设置') }}</td><td>{{ item.cpu_arch || t('Not set', '未设置') }}</td><td>{{ localizedState(item.status || 'available') }}</td></tr></tbody>
          </table>
        </div>
      </section>

      <section v-else-if="activeTab === 'deployments'" class="provider-workspace">
        <div class="section-heading"><div><h2>{{ t('Deployments', '部署') }}</h2><p>{{ t('Live Provider deployment state and completion counters.', '实时 Provider 部署状态和完成计数。') }}</p></div><Boxes :size="18" /></div>
        <div v-if="resources.deployments.length" class="table-scroll">
          <table class="data-table provider-table deployment-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Ownership', '归属') }}</th><th>{{ t('Name', '名称') }}</th><th>{{ t('Type', '类型') }}</th><th>{{ t('Replicas', '副本') }}</th><th>{{ t('Starting', '启动中') }}</th><th>{{ t('Running', '运行中') }}</th><th>{{ t('Finished', '已完成') }}</th><th>{{ t('Failed', '失败') }}</th><th>{{ t('Containers', '容器') }}</th><th>{{ t('Created', '创建时间') }}</th><th>{{ t('Action', '操作') }}</th></tr></thead>
            <tbody><tr v-for="deployment in resources.deployments" :key="deployment.uuid" tabindex="0" @click="openDeployment(deployment)" @keydown.enter="openDeployment(deployment)"><td><span class="state-badge" :data-state="deployment.status"><span />{{ stateLabel(deployment.status) }}</span></td><td><span class="ownership-chip" :data-managed="Boolean(managedFor(deployment.uuid))">{{ managedFor(deployment.uuid) ? t('Managed', '受管') : t('External', '外部') }}</span></td><td><strong>{{ deployment.name }}</strong><small><code>{{ deployment.uuid }}</code></small></td><td>{{ deployment.type }}</td><td>{{ deployment.replica_num }}</td><td>{{ deployment.starting_num }}</td><td>{{ deployment.running_num }}</td><td>{{ deployment.finished_num }}</td><td>{{ deployment.failed_num }}</td><td>{{ containerCount(deployment.uuid) }}</td><td>{{ dateTime(deployment.created_at) }}</td><td><button v-if="managedFor(deployment.uuid)" class="icon-button stop-button" type="button" :title="t('Stop managed deployment', '停止受管部署')" :disabled="Boolean(resourceAction) || Boolean(managedFor(deployment.uuid)?.stop_requested_at)" @click.stop="requestStop(deployment)"><LoaderCircle v-if="resourceAction === deployment.uuid" :size="15" class="spinning" /><Square v-else :size="14" /></button><span v-else class="muted-cell">{{ t('Read only', '只读') }}</span></td></tr></tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><CheckCircle2 :size="21" /></span><h3>{{ t('No deployments', '暂无部署') }}</h3><p>{{ t('The configured Provider Token returned no active deployment records.', '已配置的 Provider Token 未返回活跃部署记录。') }}</p></div>
      </section>

      <section v-else class="provider-workspace">
        <div class="section-heading"><div><h2>{{ t('Containers and cache', '容器与缓存') }}</h2><p>{{ t('Active and released containers with access credentials removed.', '已移除访问凭据的活跃和已释放容器。') }}</p></div><Database :size="18" /></div>
        <div v-if="resources.active_containers.length || resources.cached_containers.length" class="table-scroll">
          <table class="data-table provider-table container-table">
            <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Container', '容器') }}</th><th>{{ t('Deployment', '部署') }}</th><th>GPU</th><th>CPU</th><th>{{ t('Memory', '内存') }}</th><th>{{ t('Price', '价格') }}</th><th>{{ t('Machine', '主机') }}</th><th>{{ t('Started', '开始时间') }}</th><th>{{ t('Stopped', '停止时间') }}</th></tr></thead>
            <tbody>
              <tr v-for="container in [...resources.active_containers, ...resources.cached_containers]" :key="container.uuid"><td><span class="state-badge" :data-state="container.status"><span />{{ stateLabel(container.status) }}</span></td><td><code>{{ container.uuid }}</code></td><td><code>{{ container.deployment_uuid }}</code></td><td>{{ container.gpu_num }} x {{ container.gpu_name }}</td><td>{{ container.cpu_num }}</td><td>{{ bytes(container.memory_bytes) }}</td><td>{{ money(container.price_milli_per_hour) }}</td><td><code>{{ container.machine_id || t('Not set', '未设置') }}</code></td><td>{{ dateTime(container.started_at) }}</td><td>{{ dateTime(container.stopped_at) }}</td></tr>
            </tbody>
          </table>
        </div>
        <div v-else class="empty-state compact-empty"><span class="empty-icon"><Server :size="21" /></span><h3>{{ t('No containers', '暂无容器') }}</h3><p>{{ t('No active or released containers were returned.', '未返回活跃或已释放容器。') }}</p></div>
      </section>
    </template>

    <div v-else-if="summary && !summary.credential_configured" class="empty-state provider-empty"><span class="empty-icon"><KeyRound :size="21" /></span><h3>{{ t('Provider credential missing', '缺少 Provider 凭据') }}</h3><button class="primary-button" type="button" @click="openCredentialDialog"><KeyRound :size="16" />{{ t('Configure token', '配置 Token') }}</button></div>
  </section>

  <div v-if="emergencyDialog" class="modal-backdrop" @click.self="!emergencyBusy && (emergencyDialog = false)">
    <section class="modal emergency-modal" role="alertdialog" aria-modal="true" :aria-label="t('Emergency stop all managed resources', '紧急停止所有受管资源')">
      <header><div><p class="eyebrow">{{ t('Shutdown enforcement', '关停执行') }}</p><h2>Emergency Stop</h2></div><button class="icon-button" type="button" :title="t('Close emergency stop', '关闭 Emergency Stop')" :disabled="emergencyBusy" @click="emergencyDialog = false"><X :size="17" /></button></header>
      <form class="dialog-form" @submit.prevent="emergencyStop">
        <div class="emergency-warning"><ShieldAlert :size="20" /><p>{{ t('Request stop and deletion for all', '请求停止并删除全部') }} {{ managedActiveCount }} {{ t('active Gemcp-managed resources.', '个活跃的 Gemcp 受管资源。') }}</p></div>
        <label>{{ t('Type STOP to confirm', '输入 STOP 以确认') }}<input v-model="emergencyConfirmation" autocomplete="off" spellcheck="false" /></label>
        <button class="danger-button emergency-submit" type="submit" :disabled="emergencyBusy || emergencyConfirmation !== 'STOP'"><LoaderCircle v-if="emergencyBusy" :size="16" class="spinning" /><ShieldAlert v-else :size="16" />{{ t('Stop all managed resources', '停止所有受管资源') }}</button>
      </form>
    </section>
  </div>

  <div v-if="credentialDialog" class="modal-backdrop" @click.self="closeCredentialDialog">
    <section class="modal" role="dialog" aria-modal="true" :aria-label="t('Rotate Provider token', '轮换 Provider Token')">
      <header><div><p class="eyebrow">{{ t('Credential custody', '凭据托管') }}</p><h2>{{ t('Rotate Provider token', '轮换 Provider Token') }}</h2></div><button class="icon-button" type="button" :title="t('Close', '关闭')" :disabled="credentialBusy" @click="closeCredentialDialog"><X :size="17" /></button></header>
      <form class="dialog-form" @submit.prevent="configureProvider">
        <label>{{ t('Provider name', 'Provider 名称') }}<input v-model="credentialForm.name" required maxlength="120" /></label>
        <label>API base URL<input value="https://private.autodl.com" disabled /></label>
        <label>Developer Token<input v-model="credentialForm.token" type="password" required autocomplete="off" spellcheck="false" /></label>
        <p class="form-note">{{ t('The new Token is validated before replacing the encrypted credential.', '新 Token 会在替换已加密凭据前完成验证。') }}</p>
        <div v-if="credentialError" class="form-error" role="alert">{{ credentialError }}</div>
        <button class="primary-button" type="submit" :disabled="credentialBusy"><LoaderCircle v-if="credentialBusy" :size="16" class="spinning" /><KeyRound v-else :size="16" />{{ t('Validate and rotate', '验证并轮换') }}</button>
      </form>
    </section>
  </div>

  <div v-if="selectedDeployment || deploymentLoading" class="modal-backdrop" @click.self="selectedDeployment = null">
    <section class="detail-panel provider-detail" role="dialog" aria-modal="true" :aria-label="t('Provider deployment details', 'Provider 部署详情')">
      <header><div><p class="eyebrow">{{ t('Private Cloud deployment', '私有云部署') }}</p><h2>{{ selectedDeployment?.deployment.name ?? t('Loading deployment', '正在加载部署') }}</h2></div><button class="icon-button" type="button" :title="t('Close details', '关闭详情')" @click="selectedDeployment = null"><X :size="17" /></button></header>
      <div v-if="deploymentLoading" class="provider-loading"><LoaderCircle :size="20" class="spinning" /></div>
      <template v-else-if="selectedDeployment">
        <div class="detail-state"><span class="state-badge" :data-state="selectedDeployment.deployment.status"><span />{{ stateLabel(selectedDeployment.deployment.status) }}</span><span class="ownership-chip" :data-managed="Boolean(selectedManagedResource)">{{ selectedManagedResource ? t('Managed', '受管') : t('External', '外部') }}</span><code>{{ selectedDeployment.deployment.uuid }}</code><button v-if="selectedManagedResource" class="danger-button small-button" type="button" :disabled="Boolean(resourceAction) || Boolean(selectedManagedResource.stop_requested_at)" @click="requestStop(selectedDeployment.deployment)"><LoaderCircle v-if="resourceAction" :size="15" class="spinning" /><Square v-else :size="14" />{{ selectedManagedResource.stop_requested_at ? t('Stop requested', '已请求停止') : t('Stop deployment', '停止部署') }}</button></div>
        <dl class="detail-list provider-detail-list">
          <div><dt>{{ t('Type', '类型') }}</dt><dd>{{ selectedDeployment.deployment.type }}</dd></div>
          <div><dt>{{ t('Image', '镜像') }}</dt><dd><code>{{ selectedDeployment.deployment.image_uuid || t('Not set', '未设置') }}</code></dd></div>
          <div><dt>{{ t('Replicas', '副本') }}</dt><dd>{{ selectedDeployment.deployment.replica_num }} {{ t('requested', '请求') }} / {{ selectedDeployment.deployment.running_num }} {{ t('running', '运行中') }} / {{ selectedDeployment.deployment.finished_num }} {{ t('finished', '已完成') }} / {{ selectedDeployment.deployment.failed_num }} {{ t('failed', '失败') }}</dd></div>
          <div><dt>{{ t('Container reuse', '容器复用') }}</dt><dd>{{ selectedDeployment.deployment.reuse_container ? t('Enabled', '已启用') : t('Disabled', '已禁用') }}</dd></div>
          <div><dt>{{ t('Created', '创建时间') }}</dt><dd>{{ dateTime(selectedDeployment.deployment.created_at) }}</dd></div>
          <div><dt>{{ t('Snapshot', '快照') }}</dt><dd>{{ dateTime(selectedDeployment.generated_at) }}</dd></div>
        </dl>
        <div v-if="selectedDeployment.truncated?.length" class="provider-truncation">{{ t('Showing the first 1,000 records for:', '以下类别仅显示前 1,000 条：') }} {{ selectedDeployment.truncated.join(', ') }}</div>
        <section class="provider-detail-section"><h3>{{ t('Containers', '容器') }}</h3><div v-for="container in [...selectedDeployment.active_containers, ...selectedDeployment.released_containers]" :key="container.uuid" class="provider-detail-row"><span class="state-badge" :data-state="container.status"><span />{{ stateLabel(container.status) }}</span><code>{{ container.uuid }}</code><span>{{ container.gpu_num }} x {{ container.gpu_name }}</span><span>{{ money(container.price_milli_per_hour) }}</span></div><p v-if="!selectedDeployment.active_containers.length && !selectedDeployment.released_containers.length">{{ t('No containers returned.', '未返回容器。') }}</p></section>
        <section class="provider-detail-section"><h3>{{ t('Events', '事件') }}</h3><div v-for="event in selectedDeployment.events" :key="`${event.container_uuid}-${event.created_at}-${event.status}`" class="provider-event"><span class="status-dot" /><strong>{{ stateLabel(event.status) }}</strong><code>{{ event.container_uuid }}</code><time>{{ dateTime(event.created_at) }}</time></div><p v-if="!selectedDeployment.events.length">{{ t('No events returned.', '未返回事件。') }}</p></section>
      </template>
    </section>
  </div>
</template>
