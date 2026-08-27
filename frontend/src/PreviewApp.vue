<script setup lang="ts">
import ConsoleView from './components/ConsoleView.vue'

const build = { name: 'Gemcp', version: '0.20.0', commit: 'preview', built_at: '2026-08-27T00:00:00Z' }
const user = { user_id: 'owner-id', tenant_id: 'tenant-id', email: 'owner@lab.local', role: 'owner' }
</script>

<template>
  <ConsoleView :build="build" :user="user" />
</template>
