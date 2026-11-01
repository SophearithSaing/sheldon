<script setup lang="ts">
import { computed, nextTick, onActivated, onDeactivated, ref, useId } from 'vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import ThreadStatus from './ThreadStatus.vue'
import { sampleSourceLabel, type SampleAnchor, type SampleThread } from './workspace'

const props = defineProps<{ thread: SampleThread; busyThread: number | null }>()
const followup = defineModel<string>('followup', { required: true })
const emit = defineEmits<{
  source: [id: number, anchor: SampleAnchor]
  understood: [id: number, value: boolean]
  preview: [id: number, state: string]
}>()
const id = useId()
const notice = ref('')
const root = ref<HTMLElement>()
const messages = ref<InstanceType<typeof ScrollRegion>>()
let scrollTop = 0
let active = false
const previewState = computed({ get: currentPreview, set: choosePreview })
const previewOptions = computed(stateOptions)
onActivated(restoreScroll)
onDeactivated(deactivate)

/**
 * Records native message scrolling only while the thread is mounted in its panel.
 * @param event Native scroll event.
 */
function rememberScroll(event: Event): void {
  if (active) scrollTop = (event.target as HTMLElement).scrollTop
}

/**
 * Restores this thread's scroll position after its cached panel is reinserted.
 */
async function restoreScroll(): Promise<void> {
  await nextTick()
  const element = messages.value?.$el as HTMLElement | undefined
  if (element) element.scrollTop = scrollTop
  active = true
}

/**
 * Ignores browser scroll resets while a cached thread is detached.
 */
function deactivate(): void {
  active = false
}

/**
 * Keeps reader-confirmation transitions reversible and keyboard focus in the action area.
 * @param value Whether the reader confirms understanding.
 */
async function understanding(value: boolean): Promise<void> {
  emit('understood', props.thread.id, value)
  await nextTick()
  root.value?.querySelector<HTMLButtonElement>('[data-understanding-action]')?.focus({ preventScroll: true })
}

/**
 * Reads this thread's explicit sample presentation state.
 * @returns Status selector value, keeping outdated source separate from understanding.
 */
function currentPreview(): string {
  return props.thread.outdated ? 'outdated' : props.thread.status
}

/**
 * Requests a presentation-only state change, never a model dispatch.
 * @param value Selected preview status.
 */
function choosePreview(value: string): void {
  emit('preview', props.thread.id, value)
}

/**
 * Keeps Understood reader-controlled and prevents overlapping investigating previews.
 * @returns Available explicit preview states.
 */
function stateOptions(): { value: string; label: string; disabled?: boolean }[] {
  return [
    { value: 'answered', label: 'Answered sample' },
    { value: 'investigating', label: 'Investigating sample', disabled: props.busyThread !== null && props.busyThread !== props.thread.id },
    { value: 'interrupted', label: 'Interrupted sample' },
    { value: 'failed', label: 'Failed sample' },
    { value: 'outdated', label: 'Outdated-context sample' },
    ...(props.thread.status === 'understood' ? [{ value: 'understood', label: 'Reader-marked Understood', disabled: true }] : []),
  ]
}

/**
 * Applies follow-up keyboard semantics without dispatching unavailable Send.
 * @param event Keyboard input in the thread-local composer.
 */
function shortcut(event: KeyboardEvent): void {
  if (event.isComposing || event.keyCode === 229 || event.key !== 'Enter' || event.shiftKey) return
  event.preventDefault()
  notice.value = 'Sending is unavailable. Your follow-up remains in this thread; nothing was sent.'
}

/**
 * Opens only pinned sample context that has not been marked outdated.
 * @param anchor Source coordinates selected by the reader.
 */
function source(anchor: SampleAnchor): void {
  if (props.thread.outdated) return
  emit('source', props.thread.id, anchor)
}
</script>

<template>
  <div ref="root" class="thread-conversation">
    <div class="thread-summary"><ThreadStatus :status="thread.status" /><p>Shared agent context · This PR revision</p></div>
    <button type="button" class="thread-source" :aria-disabled="thread.outdated || undefined" :aria-describedby="thread.outdated ? `${id}-outdated` : undefined" @click="source(thread.anchor)"><span>Selected sample context</span><strong>{{ sampleSourceLabel(thread.anchor) }}</strong><span aria-hidden="true">View in diff →</span></button>
    <p v-if="thread.outdated" :id="`${id}-outdated`" class="thread-warning" role="status">Outdated context preview. The transcript remains pinned to {{ thread.anchor.revision }}; source navigation and sending are unavailable. Nothing is silently rebound to a newer revision.</p>
    <ScrollRegion ref="messages" label="Sample conversation messages" class="conversation-messages" @scroll="rememberScroll">
      <p class="transcript-disclosure">Sample transcript · Replies are fixed illustrative text, not generated for your question. No agent is connected.</p>
      <article v-for="(message, index) in thread.messages" :key="index" class="thread-message" :class="message.author === 'Agent' ? 'sample-explanation' : 'sample-user'">
        <h3><span class="message-avatar" aria-hidden="true">{{ message.author === 'You' ? 'Y' : '✧' }}</span>{{ message.author }} <span class="message-origin">· sample {{ message.author === 'You' ? 'message' : 'response' }}</span></h3>
        <p v-for="(paragraph, paragraphIndex) in message.paragraphs" :key="paragraphIndex" :class="message.author === 'You' ? 'sample-prompt' : undefined">{{ paragraph }}</p>
        <div v-if="message.citations?.length" class="thread-citations" aria-label="Illustrative source citations">
          <button v-for="(anchor, citationIndex) in message.citations" :key="citationIndex" type="button" :aria-disabled="thread.outdated || undefined" :aria-describedby="thread.outdated ? `${id}-outdated` : undefined" @click="source(anchor)">{{ sampleSourceLabel(anchor) }} ↗</button>
        </div>
      </article>
      <p v-if="thread.status === 'investigating'" class="thread-warning" role="status">Investigating preview only — no agent is running. Question {{ String(thread.id).padStart(2, '0') }} occupies the shared PR-revision session in this sample.</p>
      <p v-if="thread.status === 'failed'" class="thread-warning" role="alert">Sample response failed. No agent request ran. The thread, transcript and draft are retained; retry is unavailable.</p>
      <p v-if="thread.status === 'interrupted'" class="thread-warning" role="status">Interrupted sample response. No agent was stopped. The existing transcript and draft are retained; resuming is unavailable.</p>
      <div class="thread-actions">
        <UiButton v-if="thread.status === 'answered'" data-understanding-action variant="soft" @click="understanding(true)">Mark understood</UiButton>
        <template v-if="thread.status === 'understood'"><p>Marked understood by the sample reader. Not PR approval.</p><UiButton data-understanding-action @click="understanding(false)">Undo understood</UiButton><UiButton @click="understanding(false)">Reopen question</UiButton></template>
        <UiButton :disabled="thread.outdated" :aria-describedby="thread.outdated ? `${id}-outdated` : undefined" @click="source(thread.anchor)">View in diff</UiButton>
      </div>
      <details class="thread-preview-controls">
        <summary>Thread state previews</summary>
        <p>Changes only this sample's presentation, not its transcript or a real session. Only Mark understood represents reader confirmation.</p>
        <UiField v-model="previewState" label="Sample thread state" type="select" :options="previewOptions" />
      </details>
    </ScrollRegion>
    <form class="followup-sample" aria-label="Thread follow-up" @submit.prevent>
      <UiField v-model="followup" label="Follow up in this thread" multiline :aria-describedby="`${id}-send-unavailable`" hint="Enter to send when available · Shift+Enter for a new line. Draft stays in this page until reload." @keydown="shortcut" />
      <p :id="`${id}-send-unavailable`" class="send-reason">{{ busyThread !== null ? `Question ${String(busyThread).padStart(2, '0')} is using the shared PR-revision session in this sample. Draft stays editable; Send is unavailable.` : 'No harness is connected; sending is unavailable. This thread reuses the shared PR-revision context, not a separate native history.' }}</p>
      <div class="followup-tools"><span>Same thread · Shared context</span><UiButton type="submit" variant="primary" disabled :aria-describedby="`${id}-send-unavailable`">Send — unavailable</UiButton></div>
      <p v-if="notice" role="status">{{ notice }}</p>
    </form>
  </div>
</template>

<style scoped>
.thread-conversation { display: flex; flex-direction: column; flex: 1; min-width: 0; min-height: 0; }
.thread-summary { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); padding: var(--space-2) var(--space-4); font-size: var(--text-small); color: var(--color-muted); }
.thread-source { display: grid; justify-items: start; gap: var(--space-1); flex-shrink: 0; width: 100%; min-height: var(--target-control); padding: var(--space-3) var(--space-6); border: 0; border-block: 1px solid var(--color-border); background: transparent; text-align: left; overflow-wrap: anywhere; font-size: var(--text-small); }
.thread-source span { color: var(--color-muted); font-size: var(--text-code-meta); }
.thread-source strong { font-weight: 500; }
.thread-source:hover { background: var(--color-surface); }
.thread-source[aria-disabled], .thread-citations [aria-disabled] { cursor: not-allowed; }
.conversation-messages { flex: 1; min-height: 10rem; padding: var(--space-5) var(--space-6); }
.transcript-disclosure { margin-bottom: var(--space-5); color: var(--color-muted); font-size: var(--text-code-meta); }
.thread-message { font-family: var(--font-prose); font-size: var(--text-prose); line-height: 1.75; overflow-wrap: anywhere; }
.thread-message + .thread-message { margin-top: var(--space-6); }
.thread-message h3 { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); margin-bottom: var(--space-3); font-size: var(--text-small); font-weight: 550; }
.message-avatar { display: inline-grid; place-items: center; width: 1.625rem; height: 1.625rem; border: 1px solid var(--color-border); border-radius: 50%; background: var(--color-surface); font-size: var(--text-small); }
.message-origin { color: var(--color-muted); font-weight: 400; }
.sample-prompt { font-size: var(--text-prompt); line-height: 1.65; }
.thread-message p + p { margin-top: var(--space-4); }
.thread-citations { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-4); }
.thread-citations button { max-width: 100%; min-height: 2rem; padding: var(--space-1) var(--space-2); border: 1px solid var(--color-border); border-radius: var(--radius-control); background: transparent; color: var(--color-muted); font: var(--text-code-meta)/1.6 var(--font-code); text-align: left; overflow-wrap: anywhere; }
.thread-citations button:hover { color: var(--color-accent); border-color: var(--color-accent-border); }
.thread-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-5); }
.thread-actions p { flex-basis: 100%; color: var(--color-muted); font-size: var(--text-small); }
.thread-warning { margin: var(--space-3) var(--space-4); color: var(--color-warning); font-size: var(--text-small); overflow-wrap: anywhere; }
.thread-preview-controls { margin-top: var(--space-6); color: var(--color-muted); font-size: var(--text-code-meta); }
.thread-preview-controls summary { min-height: var(--target-control); cursor: pointer; }
.thread-preview-controls p { margin-bottom: var(--space-3); }
.followup-sample { flex-shrink: 0; display: grid; gap: var(--space-3); padding: var(--space-4) var(--space-6); border-top: 1px solid var(--color-border); }
.send-reason, .followup-sample > p, .followup-tools > span { color: var(--color-muted); font-size: var(--text-code-meta); }
.followup-tools { display: flex; align-items: center; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); }
</style>
