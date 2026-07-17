<script setup lang="ts">
import { ref } from 'vue'
import { Eye, EyeOff, FlaskConical, LoaderCircle, LogIn } from '@lucide/vue'
import { APIError, api, type BuildInfo, type User } from '../api'

const props = defineProps<{ build: BuildInfo | null }>()
const emit = defineEmits<{ authenticated: [user: User] }>()

const email = ref('')
const password = ref('')
const showPassword = ref(false)
const submitting = ref(false)
const error = ref('')

async function login() {
  error.value = ''
  submitting.value = true
  try {
    const user = await api.login(email.value.trim(), password.value)
    password.value = ''
    emit('authenticated', user)
  } catch (caught) {
    error.value = caught instanceof APIError ? caught.message : 'Sign in failed. Check the control-plane connection.'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-panel">
      <div class="auth-brand">
        <span class="brand-mark"><FlaskConical :size="21" /></span>
        <div><strong>Gemcp</strong><span>AutoDL control plane</span></div>
      </div>
      <div class="auth-heading">
        <p class="eyebrow">Owner access</p>
        <h1>Sign in</h1>
      </div>
      <form @submit.prevent="login">
        <label>Email<input v-model="email" type="email" autocomplete="username" required autofocus /></label>
        <label>Password<span class="password-field"><input v-model="password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" required /><button type="button" :title="showPassword ? 'Hide password' : 'Show password'" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" :size="17" /><Eye v-else :size="17" /></button></span></label>
        <div v-if="error" class="form-error" role="alert">{{ error }}</div>
        <button class="primary-button auth-submit" type="submit" :disabled="submitting">
          <LoaderCircle v-if="submitting" :size="16" class="spinning" /><LogIn v-else :size="16" />Sign in
        </button>
      </form>
      <footer><span>{{ props.build?.name ?? 'Gemcp' }} {{ props.build?.version ?? 'dev' }}</span><span>{{ props.build?.commit ?? 'unknown' }}</span></footer>
    </section>
    <aside class="auth-context" aria-label="Deployment status">
      <div class="auth-context-inner">
        <p class="eyebrow">Private operations</p>
        <h2>Project policy before provider access.</h2>
        <dl>
          <div><dt>Ingress</dt><dd>HTTPS / Cloudflare Tunnel</dd></div>
          <div><dt>Agent protocol</dt><dd>MCP Streamable HTTP</dd></div>
          <div><dt>State</dt><dd>PostgreSQL authoritative</dd></div>
        </dl>
      </div>
    </aside>
  </main>
</template>
