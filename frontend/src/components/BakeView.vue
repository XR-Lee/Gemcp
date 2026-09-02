<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { AlertTriangle, CheckCircle2, ChevronRight, Images, LoaderCircle, Play, Plus, RefreshCw, Square } from '@lucide/vue'
import {
  APIError,
  api,
  type ImageBake,
  type ImageBakeInput,
  type ImageBakeOptions,
  type Project,
} from '../api'
import { localizedState, useI18n } from '../i18n'

const props = defineProps<{ active: boolean; project: Project | null }>()
const emit = defineEmits<{ unauthorized: [] }>()
const { languageTag, t } = useI18n()

const options = ref<ImageBakeOptions | null>(null)
const bakes = ref<ImageBake[]>([])
const selected = ref<ImageBake | null>(null)
const loading = ref(false)
const requesting = ref(false)
const confirming = ref(false)
const confirmed = ref(false)
const error = ref('')
const form = reactive<ImageBakeInput>({
  name: '',
  backend: 'autodl_pro',
  base_image_uuid: '',
  repository_id: '',
  commit_sha: '',
  recipe_path: 'requirements.gemcp.txt',
})
let loadSequence = 0

const canRequest = computed(() => Boolean(
  props.project && form.name.trim() && form.base_image_uuid.trim() && /^(?:[0-9a-fA-F]{40}|[0-9a-fA-F]{64})$/.test(form.commit_sha.trim()),
))
const selectedRequested = computed(() => selected.value?.status === 'requested')

function handleError(caught: unknown, fallback: string) {
  if (caught instanceof APIError && caught.status === 401) {
    emit('unauthorized')
    return
  }
  error.value = caught instanceof APIError ? caught.message : fallback
}

function dateTime(value?: string) {
  if (!value) return t('Not set', '未设置')
  return new Intl.DateTimeFormat(languageTag.value, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value))
}

async function load() {
  if (!props.active || !props.project) {
    options.value = null
    bakes.value = []
    selected.value = null
    return
  }
  const sequence = ++loadSequence
  const projectID = props.project.id
  loading.value = true
  error.value = ''
  try {
    const [loadedOptions, listed] = await Promise.all([
      api.imageBakeOptions(projectID),
      api.imageBakes(projectID),
    ])
    if (sequence !== loadSequence || props.project?.id !== projectID) return
    options.value = loadedOptions
    bakes.value = listed.bakes
    if (!form.repository_id && loadedOptions.repositories.length === 1) {
      form.repository_id = loadedOptions.repositories[0].id
    }
    if (!form.recipe_path) form.recipe_path = loadedOptions.default_recipe_path
    if (selected.value) {
      const next = listed.bakes.find((item) => item.id === selected.value?.id)
      selected.value = next ?? selected.value
      confirmed.value = selected.value.status === 'requested' ? confirmed.value : false
    }
  } catch (caught) {
    if (sequence !== loadSequence || props.project?.id !== projectID) return
    handleError(caught, t('Could not load image bakes.', '无法加载镜像 Bake。'))
  } finally {
    if (sequence === loadSequence) loading.value = false
  }
}

async function requestBake() {
  if (!props.project || !canRequest.value) return
  requesting.value = true
  error.value = ''
  try {
    const created = await api.requestImageBake(props.project.id, {
      name: form.name.trim(),
      backend: 'autodl_pro',
      base_image_uuid: form.base_image_uuid.trim(),
      repository_id: form.repository_id || undefined,
      commit_sha: form.commit_sha.trim().toLowerCase(),
      recipe_path: form.recipe_path?.trim() || undefined,
    })
    selected.value = created
    confirmed.value = false
    await load()
  } catch (caught) {
    handleError(caught, t('Could not request the image bake.', '无法请求镜像 Bake。'))
  } finally {
    requesting.value = false
  }
}

async function confirmBake() {
  if (!props.project || !selected.value || selected.value.status !== 'requested' || !confirmed.value) return
  confirming.value = true
  error.value = ''
  try {
    selected.value = await api.confirmImageBake(props.project.id, selected.value.id, selected.value.confirmation_digest)
    await load()
  } catch (caught) {
    handleError(caught, t('Could not confirm the image bake.', '无法确认镜像 Bake。'))
  } finally {
    confirming.value = false
  }
}

async function cancelBake() {
  if (!props.project || !selected.value) return
  try {
    selected.value = await api.cancelImageBake(props.project.id, selected.value.id)
    await load()
  } catch (caught) {
    handleError(caught, t('Could not cancel the image bake.', '无法取消镜像 Bake。'))
  }
}

watch(() => [props.active, props.project?.id], () => { void load() }, { immediate: true })
watch(() => selected.value?.id, () => { confirmed.value = false })
</script>

<template>
  <section class="diagnostic-workspace page-workspace">
    <div class="section-heading page-section-heading">
      <div><h2>{{ t('Image bake', '镜像 Bake') }}</h2><p>{{ t('Lab image factory. Confirming the digest starts AutoDL Pro. This is not a research run.', '实验室镜像工厂。确认摘要后才启动 AutoDL Pro。这不是科研 run。') }}</p></div>
      <button class="icon-button" type="button" :title="t('Refresh bakes', '刷新 Bake')" :disabled="loading" @click="load"><RefreshCw :size="17" :class="{ spinning: loading }" /></button>
    </div>

    <div v-if="error" class="page-alert" role="alert">{{ error }}</div>

    <section class="diagnostic-config" aria-labelledby="bake-config-title">
      <div class="subsection-heading"><div><h3 id="bake-config-title">{{ t('Request bake', '请求 Bake') }}</h3><p>{{ t('Zero-cost record. Owner confirmation starts Pro.', '零成本记录。Owner 确认后才启动 Pro。') }}</p></div></div>
      <form class="diagnostic-form" @submit.prevent="requestBake">
        <label>{{ t('Name', '名称') }}<input v-model="form.name" required maxlength="100" placeholder="torch-mamba" spellcheck="false" /></label>
        <label>{{ t('Repository', '仓库') }}
          <select v-model="form.repository_id">
            <option value="">{{ t('Default active repository', '默认活跃仓库') }}</option>
            <option v-for="repository in options?.repositories ?? []" :key="repository.id" :value="repository.id">{{ repository.name }} · {{ repository.default_branch }}</option>
          </select>
        </label>
        <label>{{ t('Base image UUID', '基础镜像 UUID') }}
          <input v-model="form.base_image_uuid" required maxlength="128" list="bake-base-images" placeholder="image-6c15b8aad2" spellcheck="false" />
          <datalist id="bake-base-images">
            <option v-for="image in options?.base_images ?? []" :key="image.uuid" :value="image.uuid">{{ image.name }}</option>
          </datalist>
        </label>
        <label class="commit-field">Commit SHA
          <input v-model="form.commit_sha" required minlength="40" maxlength="64" spellcheck="false" autocomplete="off" placeholder="40 or 64 hexadecimal characters" />
        </label>
        <label>{{ t('Recipe path', '配方路径') }}<input v-model="form.recipe_path" maxlength="256" placeholder="requirements.gemcp.txt" spellcheck="false" /></label>
        <button class="primary-button diagnostic-action" type="submit" :disabled="!canRequest || requesting"><LoaderCircle v-if="requesting" :size="16" class="spinning" /><Plus v-else :size="16" />{{ t('Request bake', '请求 Bake') }}</button>
      </form>
    </section>

    <section v-if="selected" class="preflight-results" aria-live="polite">
      <div class="preflight-summary" :data-eligible="selected.status === 'requested' || selected.status === 'finished'">
        <span><CheckCircle2 v-if="selected.status === 'finished'" :size="19" /><AlertTriangle v-else :size="19" /></span>
        <div>
          <strong>{{ selected.name }}</strong>
          <small>{{ localizedState(selected.status) }} · {{ t('No budget reservation', '无预算预留') }} · CNY {{ (selected.estimated_cost_milli / 1000).toFixed(3) }}</small>
        </div>
      </div>
      <dl class="proposal-grid">
        <div><dt>{{ t('Base image', '基础镜像') }}</dt><dd><code>{{ selected.base_image_uuid }}</code></dd></div>
        <div><dt>{{ t('Recipe', '配方') }}</dt><dd><code>{{ selected.recipe_path }}</code></dd></div>
        <div><dt>Commit</dt><dd><code>{{ selected.commit_sha }}</code></dd></div>
        <div v-if="selected.image_uuid"><dt>{{ t('Baked image', 'Bake 结果') }}</dt><dd><code>{{ selected.image_uuid }}</code></dd></div>
        <div v-if="selected.failure_reason" class="wide-observation"><dt>{{ t('Failure', '失败原因') }}</dt><dd>{{ selected.failure_reason }}</dd></div>
        <div class="proposal-digest"><dt>{{ t('Proposal digest', '提案摘要') }}</dt><dd><code>{{ selected.confirmation_digest }}</code></dd></div>
      </dl>
      <p v-if="selected.status === 'finished'" class="configuration-warning">{{ t('Register this image_uuid with the Project AutoDL environments form. Graph locking is separate.', '把这个 image_uuid 登记到 Project 的 AutoDL 环境表单。Graph 锁定是另一件事。') }}</p>
      <label v-if="selectedRequested" class="confirmation-row">
        <input v-model="confirmed" type="checkbox" />
        <span>{{ t('I confirm this digest and approve starting AutoDL Pro to save a new image.', '我确认此摘要，并批准启动 AutoDL Pro 以保存新镜像。') }}</span>
      </label>
      <button v-if="selectedRequested" class="primary-button" type="button" :disabled="!confirmed || confirming" @click="confirmBake">
        <LoaderCircle v-if="confirming" :size="16" class="spinning" /><Play v-else :size="16" />{{ t('Confirm and start Pro', '确认并启动 Pro') }}
      </button>
      <button v-if="selectedRequested" class="icon-button" type="button" :title="t('Cancel bake', '取消 Bake')" @click="cancelBake"><Square :size="15" /></button>
    </section>

    <section class="diagnostic-history">
      <div class="subsection-heading"><div><h3>{{ t('Bakes', 'Bake 记录') }}</h3><p>{{ bakes.length }}</p></div></div>
      <div v-if="bakes.length" class="table-scroll">
        <table class="data-table">
          <thead><tr><th>{{ t('Status', '状态') }}</th><th>{{ t('Name', '名称') }}</th><th>{{ t('Image', '镜像') }}</th><th>{{ t('Created', '创建时间') }}</th><th /></tr></thead>
          <tbody>
            <tr v-for="bake in bakes" :key="bake.id" tabindex="0" @click="selected = bake" @keydown.enter="selected = bake">
              <td><span class="state-badge" :data-state="bake.status"><span />{{ localizedState(bake.status) }}</span></td>
              <td>{{ bake.name }}</td>
              <td><code>{{ bake.image_uuid || bake.base_image_uuid }}</code></td>
              <td>{{ dateTime(bake.created_at) }}</td>
              <td class="row-arrow"><ChevronRight :size="15" /></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-else-if="!loading" class="empty-state compact-empty"><span class="empty-icon"><Images :size="21" /></span><h3>{{ t('No image bakes', '暂无镜像 Bake') }}</h3></div>
    </section>
  </section>
</template>
