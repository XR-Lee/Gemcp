<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { FlaskConical, RefreshCw } from '@lucide/vue'
import { APIError, api, type BuildInfo, type User } from './api'
import ConsoleView from './components/ConsoleView.vue'
import LanguageToggle from './components/LanguageToggle.vue'
import LoginView from './components/LoginView.vue'
import SetupView from './components/SetupView.vue'
import { useI18n } from './i18n'

type Phase = 'loading' | 'setup' | 'login' | 'console' | 'unavailable'

const phase = ref<Phase>('loading')
const build = ref<BuildInfo | null>(null)
const user = ref<User | null>(null)
const startupError = ref('')
const { t } = useI18n()

async function initialize() {
  phase.value = 'loading'
  startupError.value = ''
  const buildRequest = api.build().then((value) => (build.value = value)).catch(() => undefined)
  try {
    const status = await api.setupStatus()
    await buildRequest
    if (!status.initialized) {
      phase.value = 'setup'
      return
    }
    try {
      user.value = await api.me()
      phase.value = 'console'
    } catch (caught) {
      if (caught instanceof APIError && caught.status === 401) {
        phase.value = 'login'
        return
      }
      throw caught
    }
  } catch (caught) {
    startupError.value = caught instanceof APIError ? caught.message : t('Gemcp is not reachable.', '无法连接 Gemcp。')
    phase.value = 'unavailable'
  }
}

function authenticated(value: User) {
  user.value = value
  phase.value = 'console'
}

function signedOut() {
  user.value = null
  phase.value = 'login'
}

onMounted(initialize)
</script>

<template>
  <div v-if="phase === 'loading'" class="startup-state" aria-live="polite">
    <LanguageToggle class="startup-language-toggle" />
    <span class="brand-mark"><FlaskConical :size="21" /></span>
    <strong>Gemcp</strong>
    <span>{{ t('Connecting to the control plane...', '正在连接控制平面...') }}</span>
  </div>
  <div v-else-if="phase === 'unavailable'" class="startup-state unavailable-state">
    <LanguageToggle class="startup-language-toggle" />
    <span class="brand-mark error-mark"><FlaskConical :size="21" /></span>
    <strong>{{ t('Control plane unavailable', '控制平面不可用') }}</strong>
    <span>{{ startupError }}</span>
    <button class="secondary-button" type="button" @click="initialize"><RefreshCw :size="16" />{{ t('Retry', '重试') }}</button>
  </div>
  <SetupView v-else-if="phase === 'setup'" @ready="phase = 'login'" />
  <LoginView v-else-if="phase === 'login'" :build="build" @authenticated="authenticated" />
  <ConsoleView v-else-if="user" :build="build" :user="user" @signed-out="signedOut" />
</template>
