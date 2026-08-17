<script setup lang="ts">
import { X } from '@lucide/vue'
import {
  DialogClose,
  DialogContent,
  DialogDescription,
  DialogOverlay,
  DialogPortal,
  DialogRoot,
  DialogTitle,
} from 'reka-ui'
import { useI18n } from '../i18n'

const open = defineModel<boolean>('open', { default: false })
defineProps<{ title: string; description?: string; label: string }>()
const { t } = useI18n()
</script>

<template>
  <DialogRoot v-model:open="open">
    <DialogPortal>
      <DialogOverlay class="workbench-dialog-overlay" />
      <DialogContent class="workbench-dialog" :aria-label="label">
        <header>
          <div>
            <p class="eyebrow">{{ t('Research', '研究') }}</p>
            <DialogTitle>{{ title }}</DialogTitle>
            <DialogDescription v-if="description">{{ description }}</DialogDescription>
          </div>
          <DialogClose class="icon-button" :title="t('Close', '关闭')">
            <X :size="17" />
          </DialogClose>
        </header>
        <slot />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
