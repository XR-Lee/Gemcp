<script setup lang="ts">
import { Check, GitBranch, KeyRound, TriangleAlert } from '@lucide/vue'
import type { RepositoryReadiness, RepositoryReadinessBlocker } from '../api'
import { localizedState, useI18n } from '../i18n'

defineProps<{
  readiness: RepositoryReadiness | null
  loading?: boolean
}>()
const emit = defineEmits<{
  verify: []
  registerDefaults: []
}>()
const { t } = useI18n()

function workloadLabel(readiness: RepositoryReadiness) {
  if (readiness.manifest.present && readiness.manifest.workloads?.length) {
    return readiness.manifest.workloads.join(', ')
  }
  if (readiness.project_workloads?.length) {
    return t(`Project workloads: ${readiness.project_workloads.join(', ')}`, `Project 工作负载：${readiness.project_workloads.join(', ')}`)
  }
  return t('none', '无')
}

function defaultLabel(value?: { name?: string }) {
  return value?.name || t('Missing', '缺失')
}

function blockerAction(blocker: RepositoryReadinessBlocker) {
  if (blocker.kind === 'deploy_key_required' || blocker.kind === 'https_token_verify_required') return 'verify'
  if (blocker.kind === 'environment_required' || blocker.kind === 'resource_profile_required' || blocker.kind === 'dataset_binding_required' || blocker.kind === 'incompatible_defaults') {
    return 'defaults'
  }
  return ''
}

function accessLabel(access?: string) {
  if (access === 'public_https') return t('Public HTTPS', '公开 HTTPS')
  if (access === 'https_token') return t('HTTPS token', 'HTTPS 令牌')
  return t('Deploy Key', 'Deploy Key')
}

function blockerLinkLabel(blocker: RepositoryReadinessBlocker) {
  if (blocker.kind === 'https_token_verify_required') return t('Open fine-grained tokens', '打开细粒度令牌')
  return t('Open repository Deploy Keys', '打开仓库 Deploy Keys')
}
</script>

<template>
  <section v-if="loading && !readiness" class="repo-readiness" data-status="loading">
    <p class="repo-readiness-loading">{{ t('Checking repository readiness...', '正在检查仓库就绪状态...') }}</p>
  </section>
  <section v-else-if="readiness" class="repo-readiness" :data-status="readiness.ready ? 'ready' : 'blocked'" data-testid="repository-readiness">
    <header class="repo-readiness-heading">
      <div>
        <span class="research-kicker"><GitBranch :size="15" />{{ t('Repository readiness', '仓库就绪') }}</span>
        <strong>{{ readiness.ready ? t('Ready', '就绪') : t('Blocked', '受阻') }}</strong>
        <p>{{ readiness.name }} · {{ readiness.ssh_url }}</p>
      </div>
      <span class="state-badge" :data-state="readiness.status"><span />{{ localizedState(readiness.status) }}</span>
    </header>
    <p v-if="readiness.pending_note" class="repo-readiness-pending" data-testid="pending-note">{{ readiness.pending_note }}</p>
    <dl class="repo-readiness-facts">
      <div>
        <dt>{{ t('Access', '访问') }}</dt>
        <dd>{{ accessLabel(readiness.access) }}</dd>
      </div>
      <div>
        <dt>{{ t('Default branch', '默认分支') }}</dt>
        <dd><code>{{ readiness.detected_default_branch || readiness.default_branch }}</code></dd>
      </div>
      <div>
        <dt>gemcp.yaml</dt>
        <dd>{{ workloadLabel(readiness) }}</dd>
      </div>
    </dl>
    <dl class="repo-readiness-defaults">
      <div>
        <dt>{{ t('Environment', '环境') }}</dt>
        <dd>{{ defaultLabel(readiness.defaults.environment) }}</dd>
      </div>
      <div>
        <dt>{{ t('Resource Profile', '资源规格') }}</dt>
        <dd>{{ defaultLabel(readiness.defaults.resource_profile) }}</dd>
      </div>
      <div>
        <dt>{{ t('Dataset Binding', '数据集绑定') }}</dt>
        <dd>{{ defaultLabel(readiness.defaults.dataset_binding) }}</dd>
      </div>
    </dl>
    <ul v-if="readiness.blockers.length" class="repo-readiness-blockers">
      <li v-for="blocker in readiness.blockers" :key="blocker.kind">
        <TriangleAlert :size="14" />
        <div>
          <strong>{{ blocker.title }}</strong>
          <p>{{ blocker.detail }}</p>
          <a v-if="blocker.href" class="form-note-link" :href="blocker.href" target="_blank" rel="noreferrer">{{ blockerLinkLabel(blocker) }}</a>
          <button v-else-if="blockerAction(blocker) === 'verify'" class="text-button" type="button" @click="emit('verify')">{{ t('Verify repository', '验证仓库') }}</button>
          <button v-else-if="blockerAction(blocker) === 'defaults'" class="text-button" type="button" @click="emit('registerDefaults')">{{ t('Open Project defaults', '打开 Project 默认项') }}</button>
        </div>
      </li>
    </ul>
    <p v-else class="repo-readiness-ok"><Check :size="14" />{{ t('Access and Project defaults are enough to prepare a first run.', '访问和 Project 默认项已足够准备第一次运行。') }}</p>
    <p v-if="readiness.access === 'ssh_deploy_key' && readiness.deploy_key_settings_url && readiness.status !== 'active'" class="repo-readiness-key">
      <KeyRound :size="14" />
      <a class="form-note-link" :href="readiness.deploy_key_settings_url" target="_blank" rel="noreferrer">{{ t('GitHub repository → Deploy keys', 'GitHub 仓库 → Deploy keys') }}</a>
    </p>
  </section>
</template>
