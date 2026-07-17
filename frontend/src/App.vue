<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { FlaskConical, RefreshCw } from '@lucide/vue'
import { APIError, api, type BuildInfo, type User } from './api'
import ConsoleView from './components/ConsoleView.vue'
import LoginView from './components/LoginView.vue'
import SetupView from './components/SetupView.vue'

type Phase = 'loading' | 'setup' | 'login' | 'console' | 'unavailable'

const phase = ref<Phase>('loading')
const build = ref<BuildInfo | null>(null)
const user = ref<User | null>(null)
const startupError = ref('')

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
    startupError.value = caught instanceof APIError ? caught.message : 'Gemcp is not reachable.'
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
    <span class="brand-mark"><FlaskConical :size="21" /></span>
    <strong>Gemcp</strong>
    <span>Connecting to the control plane...</span>
  </div>
  <div v-else-if="phase === 'unavailable'" class="startup-state unavailable-state">
    <span class="brand-mark error-mark"><FlaskConical :size="21" /></span>
    <strong>Control plane unavailable</strong>
    <span>{{ startupError }}</span>
    <button class="secondary-button" type="button" @click="initialize"><RefreshCw :size="16" />Retry</button>
  </div>
  <SetupView v-else-if="phase === 'setup'" @ready="phase = 'login'" />
  <LoginView v-else-if="phase === 'login'" :build="build" @authenticated="authenticated" />
  <ConsoleView v-else-if="user" :build="build" :user="user" @signed-out="signedOut" />
</template>
