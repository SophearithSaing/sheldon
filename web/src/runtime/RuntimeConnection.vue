<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import {
  fixtureEvents,
  inspectBackend,
  inspectEnvironment,
  inspectSettings,
  inspectModels,
  takeLaunchToken,
} from './client'
import EnvironmentDiagnostics from './EnvironmentDiagnostics.vue'
import SettingsPanel from '../settings/SettingsPanel.vue'
import type { EnvironmentReport } from './environment'
import type { Harness, ModelDiscovery, SettingsState } from '../settings/contracts'

const development = import.meta.env.DEV
const tokenInput = ref('')
const state = ref('Disconnected')
const pendingURL = ref('')
const heartbeats = ref(0)
const connected = ref(false)
let controller: AbortController | undefined
const environmentReport = ref<EnvironmentReport>()
const environmentLoading = ref(false)
const environmentError = ref('')
let environmentRequest: (() => Promise<EnvironmentReport>) | undefined
const settingsOpen = ref(false)
const settingsState = ref<SettingsState>()
const settingsError = ref('')
const settingsLoading = ref(false)
const models = ref<Partial<Record<Harness, ModelDiscovery>>>({})
let settingsRequest: (() => Promise<SettingsState>) | undefined
let modelsRequest: ((h: Harness) => Promise<ModelDiscovery>) | undefined

function disconnect() {
  controller?.abort()
  controller = undefined
  connected.value = false
  pendingURL.value = ''
  heartbeats.value = 0
  state.value = 'Disconnected'
  environmentRequest = undefined
  environmentReport.value = undefined
  environmentLoading.value = false
  environmentError.value = ''
  settingsOpen.value = false
  settingsState.value = undefined
  settingsError.value = ''
  settingsLoading.value = false
  models.value = {}
  settingsRequest = undefined
  modelsRequest = undefined
}

async function toggleSettings() {
  settingsOpen.value = !settingsOpen.value
  if (!settingsOpen.value || settingsState.value || settingsLoading.value || !settingsRequest)
    return
  const current = controller
  settingsLoading.value = true
  settingsError.value = ''
  try {
    const state = await settingsRequest()
    if (current === controller && !current?.signal.aborted) settingsState.value = state
  } catch {
    if (current === controller && !current?.signal.aborted)
      settingsError.value =
        'Settings metadata could not be loaded. Reconnect and try again; nothing was changed.'
  } finally {
    if (current === controller) settingsLoading.value = false
  }
}

async function loadModels(h: Harness) {
  if (!modelsRequest || models.value[h]) return
  const current = controller
  settingsError.value = ''
  try {
    const metadata = await modelsRequest(h)
    if (current === controller && !current?.signal.aborted) models.value[h] = metadata
  } catch {
    if (current === controller && !current?.signal.aborted)
      settingsError.value =
        'Native model metadata is unavailable. No model or provider was substituted.'
  }
}

async function refreshEnvironment() {
  if (!environmentRequest || environmentLoading.value) return
  const current = controller
  environmentLoading.value = true
  environmentError.value = ''
  try {
    const report = await environmentRequest()
    if (current === controller && !current?.signal.aborted) environmentReport.value = report
  } catch {
    if (current === controller && !current?.signal.aborted) {
      environmentError.value =
        'Environment checks could not finish. Retry after any active check finishes, and check the backend or external CLI installation.'
    }
  } finally {
    if (current === controller) environmentLoading.value = false
  }
}

async function connect(token = tokenInput.value) {
  disconnect()
  tokenInput.value = ''
  if (!/^[A-Za-z0-9_-]{43}$/.test(token)) {
    state.value = 'Enter the API token from the private instance record.'
    return
  }
  const current = new AbortController()
  controller = current
  state.value = 'Connecting'
  try {
    pendingURL.value = await inspectBackend(token, current.signal)
    if (current.signal.aborted) return
    connected.value = true
    environmentRequest = () => inspectEnvironment(token, current.signal)
    settingsRequest = () => inspectSettings(token, current.signal)
    modelsRequest = (h) => inspectModels(token, current.signal, h)
    state.value = 'Backend connected · waiting for fixture stream'
    for await (const event of fixtureEvents(token, current.signal)) {
      if (current.signal.aborted) return
      if (event === 'heartbeat') heartbeats.value += 1
      state.value =
        event === 'connected'
          ? 'Backend connected · fixture stream connected'
          : 'Backend connected · fixture heartbeat received'
    }
  } catch {
    if (current.signal.aborted) return
    current.abort()
    connected.value = false
    state.value = 'Connection failed or closed. Check the backend and API token, then reconnect.'
  } finally {
    if (current === controller) {
      environmentRequest = undefined
      settingsRequest = undefined
      modelsRequest = undefined
    }
    token = ''
  }
}

onMounted(() => {
  const token = takeLaunchToken()
  if (token) void connect(token)
})
onUnmounted(disconnect)
</script>

<template>
  <section
    class="runtime-connection"
    :aria-label="development ? 'Development backend connection' : 'Runtime connection'"
  >
    <div class="runtime-heading">
      <h2>{{ development ? 'Development backend' : 'Local runtime' }}</h2>
      <p class="muted small">REST/SSE connectivity only — not an agent connection.</p>
    </div>
    <form v-if="!connected" class="runtime-form" @submit.prevent="connect()">
      <label for="runtime-api-token">API token</label>
      <input
        id="runtime-api-token"
        v-model="tokenInput"
        type="password"
        autocomplete="off"
        spellcheck="false"
        aria-describedby="runtime-token-hint"
      />
      <UiButton type="submit">Connect backend</UiButton>
    </form>
    <p v-if="!connected" id="runtime-token-hint" class="muted small">
      Run sheldon again to connect automatically, or enter the token from its private instance.lock.
      Tokens stay in page memory; reloading requires reconnecting.
    </p>
    <div class="runtime-status">
      <p role="status">{{ state }}</p>
      <span v-if="connected" class="small">
        Fixture heartbeats:
        <output aria-label="Fixture heartbeats" aria-live="off">{{ heartbeats }}</output>
      </span>
      <UiButton v-if="connected || state === 'Connecting'" variant="quiet" @click="disconnect">
        Disconnect backend
      </UiButton>
    </div>
    <p v-if="pendingURL" class="muted small">
      Pending open request: {{ pendingURL }} — not loaded.
    </p>
    <UiButton v-if="connected" :aria-expanded="settingsOpen" @click="toggleSettings">{{
      settingsOpen ? 'Close settings — return later' : 'Settings & Environment'
    }}</UiButton>
    <p v-if="settingsOpen && settingsLoading" role="status">Loading settings contracts…</p>
    <p v-if="settingsOpen && settingsError && !settingsState" role="alert">{{ settingsError }}</p>
    <SettingsPanel
      v-if="connected && settingsOpen && settingsState"
      :state="settingsState"
      :models="models"
      :environment="environmentReport"
      :error="settingsError"
      @models="loadModels"
    />
    <EnvironmentDiagnostics
      v-if="connected"
      :report="environmentReport"
      :loading="environmentLoading"
      :error="environmentError"
      @refresh="refreshEnvironment"
    />
  </section>
</template>

<style scoped>
.runtime-connection {
  display: grid;
  flex-shrink: 0;
  gap: var(--space-2);
  padding: var(--space-3) var(--space-6);
  border-bottom: 1px solid var(--color-border-strong);
  background: var(--color-panel);
}
.runtime-heading,
.runtime-form,
.runtime-status {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3);
}
.runtime-heading h2 {
  font-size: var(--text-body);
  font-weight: 600;
}
.runtime-form input {
  min-width: 0;
  min-height: var(--target-control);
  width: 25rem;
  max-width: 100%;
  padding: var(--space-2);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
  background: var(--color-canvas);
}
</style>
