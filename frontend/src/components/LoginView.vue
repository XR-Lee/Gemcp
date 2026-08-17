<script setup lang="ts">
import { ref } from 'vue'
import { Eye, EyeOff, FlaskConical, LoaderCircle, LogIn } from '@lucide/vue'
import { APIError, api, type BuildInfo, type User } from '../api'
import { useI18n } from '../i18n'
import LanguageToggle from './LanguageToggle.vue'

const props = defineProps<{ build: BuildInfo | null }>()
const emit = defineEmits<{ authenticated: [user: User] }>()

const email = ref('')
const password = ref('')
const showPassword = ref(false)
const submitting = ref(false)
const error = ref('')
const { t } = useI18n()

async function login() {
  error.value = ''
  submitting.value = true
  try {
    const user = await api.login(email.value.trim(), password.value)
    password.value = ''
    emit('authenticated', user)
  } catch (caught) {
    error.value = caught instanceof APIError ? caught.message : t('Sign in failed. Check the control-plane connection.', '登录失败，请检查控制平面连接。')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="auth-page">
    <LanguageToggle class="auth-language-toggle" />
    <section class="auth-panel">
      <div class="auth-brand">
        <span class="brand-mark"><FlaskConical :size="21" /></span>
        <div><strong>Gemcp</strong><span>{{ t('Research workbench', '研究工作台') }}</span></div>
      </div>
      <div class="auth-heading">
        <p class="eyebrow">{{ t('Owner access', 'Owner 访问') }}</p>
        <h1>{{ t('Sign in', '登录') }}</h1>
      </div>
      <form @submit.prevent="login">
        <label>{{ t('Email', '邮箱') }}<input v-model="email" type="email" autocomplete="username" required autofocus /></label>
        <label>{{ t('Password', '密码') }}<span class="password-field"><input v-model="password" :type="showPassword ? 'text' : 'password'" autocomplete="current-password" required /><button type="button" :title="showPassword ? t('Hide password', '隐藏密码') : t('Show password', '显示密码')" @click="showPassword = !showPassword"><EyeOff v-if="showPassword" :size="17" /><Eye v-else :size="17" /></button></span></label>
        <div v-if="error" class="form-error" role="alert">{{ error }}</div>
        <button class="primary-button auth-submit" type="submit" :disabled="submitting">
          <LoaderCircle v-if="submitting" :size="16" class="spinning" /><LogIn v-else :size="16" />{{ t('Sign in', '登录') }}
        </button>
      </form>
      <footer><span>{{ props.build?.name ?? 'Gemcp' }} {{ props.build?.version ?? 'dev' }}</span><span>{{ props.build?.commit ?? 'unknown' }}</span></footer>
    </section>
    <aside class="auth-context" :aria-label="t('Deployment status', '部署状态')">
      <div class="auth-context-inner">
        <p class="eyebrow">{{ t('Private research', '私有研究') }}</p>
        <h2>{{ t('See the question, plan, and Graph. Lab details stay behind.', '先看问题、计划和 Graph，实验室细节留在后面。') }}</h2>
        <dl>
          <div><dt>{{ t('Ingress', '入口') }}</dt><dd>HTTPS / Cloudflare Tunnel</dd></div>
          <div><dt>{{ t('Agent protocol', 'Agent 协议') }}</dt><dd>MCP Streamable HTTP</dd></div>
          <div><dt>{{ t('State', '状态存储') }}</dt><dd>{{ t('PostgreSQL authoritative', '以 PostgreSQL 为准') }}</dd></div>
        </dl>
      </div>
    </aside>
  </main>
</template>
