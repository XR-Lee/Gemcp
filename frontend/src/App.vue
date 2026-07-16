<script setup lang="ts">
import { onMounted, ref } from 'vue'
import {
  Activity,
  Boxes,
  CircleDollarSign,
  FlaskConical,
  KeyRound,
  RefreshCw,
  Settings,
} from '@lucide/vue'

type BuildInfo = {
  name: string
  version: string
  commit: string
  built_at: string
}

const build = ref<BuildInfo | null>(null)
const loading = ref(false)
const online = ref(false)

async function refreshStatus() {
  loading.value = true
  try {
    const response = await fetch('/api/v1/version', { headers: { Accept: 'application/json' } })
    if (!response.ok) throw new Error(`HTTP ${response.status}`)
    const payload = (await response.json()) as { data: BuildInfo }
    build.value = payload.data
    online.value = true
  } catch {
    online.value = false
  } finally {
    loading.value = false
  }
}

onMounted(refreshStatus)
</script>

<template>
  <div class="app-shell">
    <aside class="sidebar">
      <div class="brand">
        <div class="brand-mark" aria-hidden="true"><FlaskConical :size="19" /></div>
        <div>
          <strong>Gemcp</strong>
          <span>AutoDL control plane</span>
        </div>
      </div>

      <nav aria-label="Primary navigation">
        <a class="nav-item active" href="#overview"><Activity :size="17" />Overview</a>
        <a class="nav-item" href="#experiments"><FlaskConical :size="17" />Experiments</a>
        <a class="nav-item" href="#projects"><Boxes :size="17" />Projects</a>
        <a class="nav-item" href="#access"><KeyRound :size="17" />Access</a>
        <a class="nav-item nav-bottom" href="#settings"><Settings :size="17" />Settings</a>
      </nav>
    </aside>

    <main>
      <header class="topbar">
        <div>
          <p class="eyebrow">Operations</p>
          <h1>Overview</h1>
        </div>
        <div class="topbar-actions">
          <span class="status" :class="online ? 'online' : 'offline'">
            <span class="status-dot" />{{ online ? 'Control plane online' : 'Control plane unavailable' }}
          </span>
          <button class="icon-button" type="button" title="Refresh status" :disabled="loading" @click="refreshStatus">
            <RefreshCw :size="17" :class="{ spinning: loading }" />
          </button>
        </div>
      </header>

      <section id="overview" class="content-band">
        <div class="metric-grid" aria-label="System metrics">
          <div class="metric">
            <span>Running</span>
            <strong>0</strong>
            <small>No active experiments</small>
          </div>
          <div class="metric">
            <span>Queued</span>
            <strong>0</strong>
            <small>Queue is clear</small>
          </div>
          <div class="metric accent-green">
            <span>Estimated spend</span>
            <strong>CNY 0.00</strong>
            <small>Current project period</small>
          </div>
          <div class="metric accent-coral">
            <span>Provider balance</span>
            <strong>Not connected</strong>
            <small>Configure AutoDL in Settings</small>
          </div>
        </div>
      </section>

      <section class="workspace">
        <div class="section-heading">
          <div>
            <h2>Recent experiments</h2>
            <p>Agent-submitted jobs will appear here.</p>
          </div>
          <span class="version-chip">{{ build?.version ?? 'dev' }}</span>
        </div>

        <div class="empty-state">
          <div class="empty-icon"><FlaskConical :size="22" /></div>
          <h3>No experiments yet</h3>
          <p>Create the first project and Agent token, then submit through MCP.</p>
        </div>
      </section>

      <footer>
        <span>{{ build?.name ?? 'Gemcp' }} {{ build?.version ?? 'dev' }}</span>
        <span class="footer-separator">Commit {{ build?.commit ?? 'unknown' }}</span>
        <CircleDollarSign :size="14" aria-hidden="true" />
        <span>Operational estimates only</span>
      </footer>
    </main>
  </div>
</template>
