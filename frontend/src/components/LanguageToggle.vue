<script setup lang="ts">
import { Languages } from '@lucide/vue'
import { useI18n } from '../i18n'

const { locale, t, toggleLocale } = useI18n()
</script>

<template>
  <button
    class="language-toggle"
    type="button"
    :aria-label="t('Switch to Chinese', '切换到英文')"
    :title="t('Switch to Chinese', '切换到英文')"
    @click="toggleLocale"
  >
    <Languages :size="16" />
    <span>{{ locale === 'en' ? '中文' : 'EN' }}</span>
  </button>
</template>
