<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import { inspectBackend, takeLaunchToken } from './client'

const development = import.meta.env.DEV
const tokenInput = ref('')
const state = ref('Disconnected')
const pendingURL = ref('')
const connected = ref(false)
let controller: AbortController | undefined

function disconnect(): void {
  controller?.abort()
  controller = undefined
  connected.value = false
  pendingURL.value = ''
  state.value = 'Disconnected'
}

async function connect(token = tokenInput.value): Promise<void> {
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
    state.value = 'Backend connected'
  } catch {
    if (current.signal.aborted) return
    current.abort()
    connected.value = false
    state.value = 'Connection failed or closed. Check the backend and API token, then reconnect.'
  } finally {
    if (current === controller) {
      controller = current.signal.aborted ? undefined : current
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
      <p class="muted small">REST connectivity only — not an agent connection.</p>
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
      Run sheldon again to connect automatically, or enter the token from its private
      instance.lock. Tokens stay in page memory; reloading requires reconnecting.
    </p>
    <div class="runtime-status">
      <p role="status">{{ state }}</p>
      <UiButton v-if="connected || state === 'Connecting'" variant="quiet" @click="disconnect">
        Disconnect backend
      </UiButton>
    </div>
    <p v-if="pendingURL" class="muted small">
      Pending open request: {{ pendingURL }} — not loaded.
    </p>
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
