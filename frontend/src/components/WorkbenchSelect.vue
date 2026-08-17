<script setup lang="ts">
import { Check, ChevronDown } from '@lucide/vue'
import {
  SelectContent,
  SelectItem,
  SelectItemIndicator,
  SelectItemText,
  SelectPortal,
  SelectRoot,
  SelectTrigger,
  SelectValue,
  SelectViewport,
} from 'reka-ui'

const model = defineModel<string>({ default: '' })
defineProps<{
  options: Array<{ value: string; label: string }>
  placeholder?: string
}>()
</script>

<template>
  <SelectRoot v-model="model">
    <SelectTrigger class="workbench-select-trigger">
      <SelectValue :placeholder="placeholder" />
      <ChevronDown :size="14" />
    </SelectTrigger>
    <SelectPortal>
      <SelectContent class="workbench-select-content" position="popper" :side-offset="6">
        <SelectViewport>
          <SelectItem v-for="option in options" :key="option.value" class="workbench-select-item" :value="option.value">
            <SelectItemText>{{ option.label }}</SelectItemText>
            <SelectItemIndicator><Check :size="13" /></SelectItemIndicator>
          </SelectItem>
        </SelectViewport>
      </SelectContent>
    </SelectPortal>
  </SelectRoot>
</template>
