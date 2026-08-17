<script setup lang="ts">
withDefaults(defineProps<{ size?: number }>(), { size: 20 })
</script>

<template>
  <span class="brand-mark" aria-hidden="true">
    <svg :width="size" :height="size" viewBox="0 0 32 32" fill="none">
      <path class="brand-link" d="M16 8.4 8.6 22.1h14.8Z" />
      <circle class="brand-node-a" cx="16" cy="8.2" r="3.15" />
      <circle class="brand-node-b" cx="8.5" cy="22.2" r="2.7" />
      <circle class="brand-node-b" cx="23.5" cy="22.2" r="2.7" />
    </svg>
  </span>
</template>
