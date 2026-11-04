<script setup lang="ts">
import { computed } from 'vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import { activeAnswer, type QuestionAnswer } from './questions'
import type { RuntimeThread, RuntimeTurn } from './conversations'

const props = defineProps<{ thread: RuntimeThread; threads: RuntimeThread[]; busyReason: string; displayedRevision?: string }>()
const emit = defineEmits<{ followup: [thread: RuntimeThread]; stop: [turn: RuntimeTurn]; observe: [turn: RuntimeTurn]; save: [turn: RuntimeTurn]; select: [threadID: string] }>()
const last = computed((): RuntimeTurn => props.thread.turns.at(-1)!)
const context = computed((): string => {
  const anchor = props.thread.input.anchor
  return `${props.thread.path} · ${anchor.kind === 'hunk' ? 'unified hunk' : `${anchor.side === 'old' ? 'merge base' : 'head'} L${anchor.start}–${anchor.end}`} · revision ${props.thread.turns[0]!.answer.revisionID.slice(0, 8)}`
})

/**
 * Describes execution independently of reading progress and stream connectivity.
 * @param answer Backend-derived public snapshot.
 * @returns Honest human-readable execution status.
 */
function status(answer: QuestionAnswer): string {
  if (answer.executionUnknown) return 'Interrupted / execution unknown — shared session remains unavailable'
  if (activeAnswer(answer) && answer.stopRequested) return 'Stop requested — awaiting confirmation'
  return { pending: 'Admitted — waiting for visible output', streaming: 'Answering', completed: 'Completed', failed: 'Failed', interrupted: 'Interrupted — confirmed' }[answer.state]
}

/**
 * Keeps backend failure categories readable without forwarding native diagnostic text.
 * @param answer Public answer snapshot.
 * @returns Actionable failure detail, or an empty string.
 */
function failure(answer: QuestionAnswer): string {
  if (answer.failureCode === 'source-unavailable') return 'The original pinned source is unavailable; no newer source was substituted.'
  if (answer.failureCode === 'answer-too-large') return 'The answer exceeded the visible-output limit. Existing text was retained.'
  if (answer.failureCode === 'execution-uncertain') return 'Execution could not be confirmed idle. No automatic retry was performed.'
  if (answer.state === 'failed') return 'The answer failed. Its transcript remains in this thread; no automatic retry was performed.'
  return ''
}

/**
 * Describes disk acknowledgement separately from model completion or unsaved drafts.
 * @param answer Current scoped public answer.
 * @returns Honest local saving feedback.
 */
function saving(answer: QuestionAnswer): string {
  if (answer.persistence === 'failed') return 'Not saved. Latest text is retained only in this runtime. Restore local storage and retry saving; no question will be resent.'
  if (answer.persistence === 'saving') return activeAnswer(answer) ? 'Question saved locally; answer will be saved when the response ends.' : 'Saving latest answer…'
  if (answer.persistence === 'saved') return answer.state === 'pending' ? 'Question saved locally; answer pending.' : 'Current answer saved locally.'
  return 'Saving status unavailable.'
}

/**
 * Sends on Enter while preserving Shift+Enter and IME composition as text input.
 * @param event Keyboard event from the follow-up textarea.
 */
function shortcut(event: KeyboardEvent): void {
  if (event.isComposing || event.keyCode === 229 || event.key !== 'Enter' || event.shiftKey) return
  event.preventDefault()
  if (!props.busyReason && !props.thread.pending && !props.thread.uncertain) emit('followup', props.thread)
}

</script>

<template>
  <section class="question-conversation" aria-label="Question conversation">
    <nav v-if="threads.length > 1" class="thread-switcher" aria-label="Runtime question threads">
      <UiButton v-for="(item, index) in threads" :key="item.id" variant="quiet" :aria-pressed="item.id === thread.id" @click="emit('select', item.id)">Thread {{ index + 1 }} · {{ item.input.question }}</UiButton>
    </nav>
    <div class="thread-summary">
      <p>Shared PR-revision context · One active question per revision.</p>
      <p>Runtime only conversation view. Answer saving is confirmed separately below; saved-thread reopening is not connected yet. Drafts are not saved. Reload/disconnect clears this page, not accepted execution or backend saving. Execution is bounded to five minutes including recovery.</p>
    </div>
    <div class="thread-context"><strong>Pinned context</strong><p>{{ context }}</p><p v-if="displayedRevision !== thread.turns[0]!.answer.revisionID" class="thread-warning">Older context. This thread is not rebound to the displayed source revision.</p></div>
    <ScrollRegion label="Conversation messages" class="conversation-messages">
      <article v-for="turn in thread.turns" :key="turn.answer.answerID" class="turn" :aria-label="`Answer to ${turn.answer.question}`">
        <div class="thread-message user-message"><h3><span class="message-avatar" aria-hidden="true">Y</span>You</h3><p>{{ turn.answer.question }}</p></div>
        <div class="thread-message agent-message"><h3><span class="message-avatar" aria-hidden="true">✧</span>Agent</h3><p v-if="turn.answer.text" class="answer-text">{{ turn.answer.text }}</p><p v-else class="empty-answer">{{ activeAnswer(turn.answer) ? 'No visible answer text yet.' : 'No visible answer text is available.' }}</p></div>
        <p class="execution-status" role="status">{{ status(turn.answer) }}</p>
        <p class="saving-status" role="status">{{ saving(turn.answer) }}</p>
        <UiButton v-if="turn.answer.persistence === 'failed'" :busy="turn.savePending" :disabled="turn.savePending" @click="emit('save', turn)">{{ turn.savePending ? 'Retrying save…' : 'Retry saving' }}</UiButton>
        <p v-if="turn.saveNotice" class="thread-warning" role="status">{{ turn.saveNotice }}</p>
        <p v-if="failure(turn.answer)" class="thread-warning">{{ failure(turn.answer) }}</p>
        <p v-if="turn.observation === 'connecting' && activeAnswer(turn.answer)" class="observation-notice">Connecting answer observation… Admission is not completion proof.</p>
        <p v-if="turn.observationError" class="thread-warning" role="status">{{ turn.observationError }}</p>
        <UiButton v-if="turn.observation === 'lost' || turn.answer.executionUnknown" @click="emit('observe', turn)">Reconnect answer observation</UiButton>
        <UiButton v-if="activeAnswer(turn.answer)" :busy="turn.stopPending" :disabled="turn.stopPending || turn.answer.stopRequested" @click="emit('stop', turn)">{{ turn.answer.stopRequested ? 'Stop requested' : 'Stop this answer' }}</UiButton>
        <p v-if="turn.stopNotice" class="observation-notice" role="status">{{ turn.stopNotice }}</p>
      </article>
    </ScrollRegion>
    <form class="followup" aria-label="Thread follow-up" @submit.prevent="emit('followup', thread)">
      <UiField v-model="thread.draft" label="Follow up in this thread" multiline :readonly="thread.pending" :error="thread.error" hint="Enter sends · Shift+Enter inserts a new line. Initial questions use Ctrl/Cmd+Enter." @keydown="shortcut" />
      <p v-if="busyReason" class="send-reason" role="status">{{ busyReason }}</p>
      <p v-if="thread.uncertain" class="thread-warning">Follow-up admission is uncertain. The draft is retained; sending again is disabled to avoid duplicate execution.</p>
      <p v-if="last.answer.state === 'completed' && !busyReason" class="send-reason">Continue this same thread on its original pinned source.</p>
      <UiButton type="submit" variant="primary" :busy="thread.pending" :disabled="thread.pending || thread.uncertain || !!busyReason">{{ thread.pending ? 'Sending follow-up…' : 'Send follow-up' }}</UiButton>
    </form>
  </section>
</template>

<style scoped>
.question-conversation { display: flex; flex-direction: column; flex: 1; min-width: 0; min-height: 0; }
.thread-switcher { display: flex; flex-wrap: wrap; gap: var(--space-2); padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--color-border); max-height: 10rem; overflow: auto; }
.thread-switcher .ui-button { max-width: 100%; white-space: normal; overflow-wrap: anywhere; text-align: left; }
.thread-summary, .thread-context { padding: var(--space-3) var(--space-4); color: var(--color-muted); font-size: var(--text-small); overflow-wrap: anywhere; border-bottom: 1px solid var(--color-border); }
.thread-summary p + p, .thread-context p { margin-top: var(--space-2); }
.thread-context strong { color: var(--color-text); font-weight: 550; }
.conversation-messages { flex: 1; min-height: 10rem; padding: var(--space-4); }
.turn + .turn { margin-top: var(--space-6); padding-top: var(--space-5); border-top: 1px solid var(--color-border); }
.thread-message { font: var(--text-prose)/1.75 var(--font-prose); overflow-wrap: anywhere; }
.thread-message + .thread-message { margin-top: var(--space-4); }
.thread-message h3 { display: flex; align-items: center; gap: var(--space-2); font-size: var(--text-small); font-weight: 550; margin-bottom: var(--space-3); }
.thread-message p { white-space: pre-wrap; }
.user-message p { font-size: var(--text-prompt); }
.message-avatar { display: inline-grid; place-items: center; width: 1.625rem; height: 1.625rem; border: 1px solid var(--color-border); border-radius: 50%; font-size: var(--text-small); background: var(--color-surface); }
.empty-answer, .execution-status, .saving-status, .observation-notice, .thread-warning, .send-reason { font-family: var(--font-ui); font-size: var(--text-small); color: var(--color-muted); overflow-wrap: anywhere; }
.execution-status, .saving-status, .observation-notice, .thread-warning, .turn > .ui-button { margin-top: var(--space-3); }
.execution-status { color: var(--color-accent); }
.thread-warning { color: var(--color-warning); }
.followup { display: grid; flex-shrink: 0; gap: var(--space-3); padding: var(--space-4); border-top: 1px solid var(--color-border); }
</style>
