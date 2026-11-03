<script setup lang="ts">
import { onUnmounted, ref, watch } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import { reviewID, type RuntimeClient } from './client'

const props = defineProps<{ client: RuntimeClient | null }>()
const dialog = ref<HTMLDialogElement>()
const url = ref('')
const pending = ref(false)
const error = ref('')
const separate = Boolean(reviewID(location.pathname))
let controller: AbortController | undefined

/** Opens the real registration dialog without clearing a previous entry. */
function open(): void { dialog.value?.showModal() }

/** Cancels an in-flight registration without assuming its server-side delivery. */
function cancel(): void {
  controller?.abort()
  pending.value = false
  dialog.value?.close()
}

/** Registers once, retaining the entered URL on validation or network failure. */
async function submit(): Promise<void> {
  if (!props.client || pending.value) return
  const raw = url.value.trim()
  if (!/^https:\/\/github\.com\/[^/\s]+\/[^/\s]+\/pull\/[1-9][0-9]*(?:[/?#].*)?$/.test(raw)) {
    error.value = 'Enter a GitHub pull request URL, not credentials.'
    return
  }
  const current = new AbortController()
  controller = current
  pending.value = true
  error.value = ''
  let opened = false
  try { await props.client.openPR(raw, current.signal); opened = true; if (separate) dialog.value?.close() }
  catch { if (!current.signal.aborted) error.value = 'Cannot register this PR. Check the URL and backend, then retry. Delivery may have completed; reopening the same PR is safe.' }
  finally { if (controller === current && (!opened || separate)) pending.value = false }
}
watch((): RuntimeClient | null => props.client, cancel)
onUnmounted(cancel)
</script>

<template>
  <UiButton :disabled="!client" @click="open">Open a PR</UiButton>
  <dialog ref="dialog" class="open-pr-dialog" aria-labelledby="open-pr-title" @cancel="cancel">
    <h2 id="open-pr-title">Open a GitHub pull request</h2>
    <p>Registration saves this PR's identity. Load pinned source separately on its page. No question or inference is sent.</p>
    <p v-if="separate">Allow a separate PR tab so this pinned view stays unchanged.</p>
    <form @submit.prevent="submit">
      <UiField v-model="url" label="GitHub PR URL" :error="error" placeholder="https://github.com/owner/repository/pull/123" autofocus />
      <p v-if="pending" role="status">Registering PR…</p>
      <div class="actions"><UiButton type="submit" :disabled="pending || !client">{{ pending ? 'Registering…' : 'Register and open PR' }}</UiButton><UiButton variant="quiet" @click="cancel">Cancel</UiButton></div>
    </form>
  </dialog>
</template>

<style scoped>
.open-pr-dialog { width: min(36rem, calc(100vw - 2rem)); max-height: calc(100dvh - 2rem); overflow: auto; margin: auto; padding: var(--space-6); color: var(--color-text); background: var(--color-canvas); border: 1px solid var(--color-border-strong); border-radius: var(--radius-panel); }
.open-pr-dialog::backdrop { background: rgb(0 0 0 / 65%); }
.open-pr-dialog p, .open-pr-dialog form { margin-top: var(--space-4); }
.actions { display: flex; flex-wrap: wrap; gap: var(--space-3); margin-top: var(--space-4); }
</style>
