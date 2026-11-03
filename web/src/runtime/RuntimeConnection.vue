<script setup lang="ts">
import { onMounted, onUnmounted, ref, watch } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import { inspectBackend, readRegisteredPR, takeLaunchToken, runtimeClient, type RuntimeClient, type ReviewState } from './client'

const props = defineProps<{ reviewId?: string; protectQuestions?: boolean }>()

const development = import.meta.env.DEV
const tokenInput = ref('')
const state = ref('Disconnected')
const connected = ref(false)
const disconnectDialog = ref<HTMLDialogElement>()
const connectionPanel = ref<HTMLElement>()
const expanded = defineModel<boolean>('expanded', { default: true })
const emit = defineEmits<{ state: [value: string]; review: [value: ReviewState]; connection: [value: RuntimeClient | null] }>()
let controller: AbortController | undefined

/**
 * Aborts any in-flight connection and resets the UI to a disconnected state.
 */
function disconnect(): void {
  expanded.value = true
  controller?.abort()
  controller = undefined
  connected.value = false
  emit('connection', null)
  emit('review', { status: 'disconnected' })
  state.value = 'Disconnected'
}

/** Requires an explicit decision before clearing a question workspace's page-memory state. */
function requestDisconnect(): void {
  if (props.protectQuestions) disconnectDialog.value?.showModal()
  else disconnect()
}

/** Keeps drafts, observers and the verified connection intact after cancelling disconnect. */
function keepConnected(): void {
  disconnectDialog.value?.close()
  connectionPanel.value?.querySelector<HTMLButtonElement>('[data-disconnect]')?.focus()
}

/** Clears browser state only after explicit consent; it never claims to cancel accepted execution. */
function confirmDisconnect(): void {
  disconnectDialog.value?.close()
  disconnect()
}

/**
 * Verifies the backend and reads only this tab's registered PR when routed.
 * @param token Bearer token for the local runtime. Defaults to the current input value.
 */
async function connect(token = tokenInput.value): Promise<void> {
  disconnect()
  tokenInput.value = ''
  if (!/^[A-Za-z0-9_-]{43}$/.test(token)) {
    state.value = 'Enter the API token from the private instance record.'
    return
  }
  const current = new AbortController()
  const requestedID = props.reviewId
  controller = current
  state.value = 'Connecting'
  try {
    await inspectBackend(token, current.signal)
    if (current.signal.aborted) return
    if (requestedID) {
      emit('review', { status: 'loading' })
      const pr = await readRegisteredPR(token, current.signal, requestedID)
      if (current.signal.aborted || props.reviewId !== requestedID) return
      emit('review', pr ? { status: 'registered', pr } : { status: 'not-found' })
    }
    connected.value = true
    emit('connection', runtimeClient(token, current.signal))
    state.value = 'Backend connected'
    expanded.value = false
  } catch {
    if (current.signal.aborted) return
    current.abort()
    connected.value = false
    expanded.value = true
    state.value = 'Connection failed or closed. Check the backend and API token, then reconnect.'
    emit('review', { status: 'failed' })
  } finally {
    if (current === controller) {
      controller = current.signal.aborted ? undefined : current
    }
    token = ''
  }
}

/**
 * Reports nonsecret connection status to the persistent header trigger.
 * @param value Public connection status text.
 */
function reportState(value: string): void {
  emit('state', value)
}

/**
 * Consumes the transient launch token when the runtime first mounts.
 */
function connectLaunch(): void {
  const token = takeLaunchToken()
  if (token) void connect(token)
}
watch(state, reportState, { immediate: true })
onMounted(connectLaunch)
onUnmounted(disconnect)
</script>

<template>
  <section
    ref="connectionPanel"
    v-show="expanded"
    id="runtime-panel"
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
      Rerun the original sheldon command to connect automatically, or enter the token from its private
      instance.lock. Tokens stay in page memory; reloading requires reconnecting.
    </p>
    <div class="runtime-status">
      <p>{{ state }}</p>
      <UiButton v-if="connected || state === 'Connecting'" data-disconnect variant="quiet" @click="requestDisconnect">
        Disconnect backend
      </UiButton>
    </div>
    <dialog ref="disconnectDialog" class="disconnect-dialog" aria-labelledby="disconnect-title" @cancel.prevent="keepConnected">
      <h2 id="disconnect-title">Disconnect this question workspace?</h2>
      <p>This discards page-memory source, transcripts and drafts. It does not stop accepted execution. Use Stop and wait for confirmation if you need to interrupt an answer.</p>
      <div><UiButton autofocus @click="keepConnected">Keep connected</UiButton><UiButton @click="confirmDisconnect">Discard page state and disconnect</UiButton></div>
    </dialog>
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
.disconnect-dialog { max-width: min(36rem, calc(100vw - 2rem)); padding: var(--space-5); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); color: var(--color-text); }
.disconnect-dialog::backdrop { background: rgb(0 0 0 / 55%); }
.disconnect-dialog p { margin-block: var(--space-4); line-height: 1.6; }
.disconnect-dialog > div { display: flex; flex-wrap: wrap; gap: var(--space-3); }
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
