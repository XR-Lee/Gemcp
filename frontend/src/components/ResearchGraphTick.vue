<script setup lang="ts">
import type { NodeProps } from '@vue-flow/core'

defineProps<NodeProps<{ label: string }>>()
</script>

<template>
  <div class="graph-tick" aria-hidden="true">{{ data.label }}</div>
</template>
