<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
import SourceDiff from '../components/SourceDiff.vue'
import InlineQuestionComposer from '../components/InlineQuestionComposer.vue'
import UiButton from '../components/ui/UiButton.vue'
import type { DiffLine, LineRange } from '../components/diff'
import type { RuntimeClient } from './client'
import type { PinnedDiff, SourceFile } from './source'
import { questionError, QuestionAdmissionError, sameAnchor, type InitialQuestion, type QuestionAnchor, type QuestionAnswer } from './questions'

const props = defineProps<{ diff: PinnedDiff; file: SourceFile; lines: DiffLine[]; client: RuntimeClient; sourcePending: boolean; busyReason: string; markers: { answer: QuestionAnswer; anchor: QuestionAnchor }[] }>()
const emit = defineEmits<{ guard: [active: boolean]; sending: [pending: boolean]; admitted: [answer: QuestionAnswer, input: InitialQuestion]; open: [answer: QuestionAnswer, trigger: HTMLElement] }>()
const anchor = ref<QuestionAnchor | null>(null)
const draft = ref<InitialQuestion | null>(null)
const text = ref('')
const opened = ref(false)
const pending = ref(false)
const uncertain = ref(false)
const error = ref('')
const message = ref('')
const root = ref<HTMLElement>()
let controller: AbortController | undefined
let trigger: HTMLElement | null = null
let pivot = 0
const range = computed((): LineRange | null => anchor.value?.kind === 'lines' ? { side: anchor.value.side === 'old' ? 'base' : 'head', start: anchor.value.start, end: anchor.value.end } : null)
const inlineID = computed((): string | undefined => endpoint(draft.value?.anchor ?? anchor.value))
const markerLines = computed((): string[] => props.markers.map((marker): string => endpoint(marker.anchor) ?? '').filter(Boolean))
const context = computed((): string => {
  const target = draft.value?.anchor ?? anchor.value
  if (!target) return ''
  const path = target.kind === 'lines' && target.side === 'old' ? props.file.oldPath : props.file.newPath ?? props.file.oldPath
  return `${path} · ${target.kind === 'hunk' ? 'unified hunk' : `${target.side === 'old' ? 'merge base' : 'head'} L${target.start}–${target.end}`} · revision ${props.diff.revisionID.slice(0, 8)}`
})

/**
 * Locates the displayed end of an exact selection for in-flow UI insertion.
 * @param target Selection in the current file, if any.
 * @returns Last matching visible line identity.
 */
function endpoint(target: QuestionAnchor | null): string | undefined {
  if (!target) return undefined
  return props.lines.filter((line): boolean => line.hunkID === target.hunkID && (target.kind === 'hunk' || (line[target.side === 'old' ? 'base' : 'head'] ?? -1) >= target.start && (line[target.side === 'old' ? 'base' : 'head'] ?? -1) <= target.end)).at(-1)?.id
}

/**
 * Selects contiguous coordinates within one real hunk without touching native text selection.
 * @param side Displayed merge-base or head column.
 * @param line Selected source coordinate.
 * @param extend Whether to extend from the original keyboard/pointer pivot.
 */
function selectLine(side: 'base' | 'head', line: number, extend: boolean): void {
  const row = props.lines.find((item): boolean => item[side] === line)
  if (!row?.hunkID) return
  const old = anchor.value
  const canExtend = extend && old?.kind === 'lines' && old.side === (side === 'base' ? 'old' : 'new') && old.hunkID === row.hunkID
  const start = canExtend ? Math.min(pivot, line) : line
  const end = canExtend ? Math.max(pivot, line) : line
  if (extend && old && !canExtend) { message.value = 'Select a range on one side within one hunk.'; return }
  if (end - start >= 1000) { message.value = 'Select fewer than 1,000 lines.'; return }
  const target: QuestionAnchor = { kind: 'lines', fileID: props.file.id, hunkID: row.hunkID, side: side === 'base' ? 'old' : 'new', start, end }
  if (draft.value && !sameAnchor(draft.value.anchor, target)) { message.value = 'Your draft is pinned. Resume or discard it before changing the selection.'; return }
  if (!canExtend) pivot = line
  anchor.value = target
  message.value = ''
}

/**
 * Opens only an explicitly requested draft, keeping its original comparison and source.
 * @param event Ask activation used for focus recovery.
 */
async function ask(event: MouseEvent): Promise<void> {
  if (props.sourcePending || pending.value) return
  trigger = event.currentTarget as HTMLElement
  if (!draft.value && anchor.value) draft.value = { revision: { ...props.diff.revision }, anchor: { ...anchor.value }, question: '' }
  if (!draft.value) return
  opened.value = true
  emit('guard', true)
  await nextTick()
  root.value?.querySelector<HTMLTextAreaElement>('textarea')?.focus()
}

/**
 * Selects a whole counted unified hunk without guessing a file-wide anchor.
 * @param hunkID Immutable hunk identity.
 */
function selectHunk(hunkID: string): void {
  if (draft.value) { message.value = 'Resume or discard the pinned draft before changing the selection.'; return }
  anchor.value = { kind: 'hunk', fileID: props.file.id, hunkID }
  message.value = ''
}

/** Hides the composer while retaining its literal text and exact anchor. */
async function cancel(): Promise<void> {
  if (pending.value) return
  opened.value = false
  await nextTick()
  if (trigger?.isConnected) trigger.focus()
}

/** Explicitly discards local draft state without claiming to cancel uncertain execution. */
async function discard(): Promise<void> {
  if (pending.value) return
  draft.value = null
  text.value = ''
  error.value = ''
  opened.value = false
  uncertain.value = false
  message.value = ''
  emit('guard', false)
  await nextTick()
  root.value?.querySelector<HTMLButtonElement>('[data-ask]')?.focus()
}

/** Sends one explicit immutable question and creates a marker only after validated admission. */
async function submit(): Promise<void> {
  if (!draft.value || pending.value || uncertain.value) return
  error.value = questionError(text.value) || props.busyReason
  if (error.value) return
  const input: InitialQuestion = { ...draft.value, question: text.value }
  const client = props.client
  const revisionID = props.diff.revisionID
  const fileID = props.file.id
  const current = new AbortController()
  controller = current
  pending.value = true
  emit('sending', true)
  try {
    const answer = await client.askQuestion(input.revision.prID, input, current.signal)
    if (current.signal.aborted || controller !== current || props.client !== client || props.diff.revisionID !== revisionID || props.file.id !== fileID) return
    emit('admitted', answer, input)
    draft.value = null
    text.value = ''
    opened.value = false
    message.value = ''
    emit('guard', false)
    await nextTick()
    if (!current.signal.aborted && controller === current && props.client === client && props.diff.revisionID === revisionID && props.file.id === fileID) root.value?.querySelector<HTMLButtonElement>(`[data-answer="${answer.answerID}"]`)?.focus({ preventScroll: true })
  } catch (failure: unknown) {
    if (current.signal.aborted || controller !== current || props.client !== client || props.diff.revisionID !== revisionID || props.file.id !== fileID) return
    uncertain.value = !(failure instanceof QuestionAdmissionError) || failure.uncertain
    error.value = failure instanceof QuestionAdmissionError ? failure.message : 'Question delivery may be uncertain. Do not resend automatically.'
  } finally {
    if (controller === current) { pending.value = false; emit('sending', false) }
  }
}

/** Prevents accidental route departure while a local draft or admission is outstanding.
 * @param event Browser navigation event.
 */
function beforeLeave(event: BeforeUnloadEvent): void {
  if (draft.value) { event.preventDefault(); event.returnValue = '' }
}
window.addEventListener('beforeunload', beforeLeave)
onUnmounted((): void => { const previous = controller; controller = undefined; previous?.abort(); window.removeEventListener('beforeunload', beforeLeave) })
watch((): string => props.file.id, (): void => { anchor.value = null; message.value = '' })
</script>

<template>
  <div ref="root" class="source-questions">
    <div class="question-selection" aria-label="Question source selection">
      <p>Select line numbers (Shift to extend), or choose a hunk. Selecting text remains native copy; only Send question starts inference.</p>
      <div class="hunk-actions"><UiButton v-for="(hunk, index) in file.hunks" :key="hunk.id" variant="quiet" :aria-pressed="anchor?.hunkID === hunk.id && anchor.kind === 'hunk'" @click="selectHunk(hunk.id)">Select hunk {{ index + 1 }}</UiButton></div>
      <p v-if="anchor">{{ context }}</p>
      <UiButton data-ask :disabled="(!anchor && !draft) || sourcePending || pending" @click="ask">{{ draft ? 'Resume draft' : 'Ask about selection' }}</UiButton>
      <UiButton v-if="draft" variant="quiet" :disabled="pending" @click="discard">Discard draft</UiButton>
      <p v-if="draft && !opened" role="status">Draft retained for the pinned selection. Resume or discard it before navigating.</p>
      <p v-if="message" role="status">{{ message }}</p>
    </div>
    <SourceDiff class="real-source" :lines="lines" :label="`Unified diff for ${file.newPath ?? file.oldPath}`" :selection="range" :selected-hunk="anchor?.kind === 'hunk' ? anchor.hunkID : undefined" :highlight="false" :insert-after="opened ? inlineID : undefined" :marked-lines="markerLines" real @select="selectLine">
      <template #inline="{ lineId }">
        <InlineQuestionComposer v-if="draft && opened && inlineID === lineId" v-model="text" :context="context" :error="error" :pending="pending" :busy="false" :unavailable-reason="uncertain ? 'Admission is uncertain. Sending again is disabled to avoid duplicate execution.' : pending ? undefined : busyReason || undefined" heading="Ask about this pinned selection" real @submit="submit" @cancel="cancel" />
        <div v-if="markers.some((marker): boolean => endpoint(marker.anchor) === lineId)" class="question-markers" aria-label="Questions at this selection">
          <UiButton v-for="marker in markers.filter((item): boolean => endpoint(item.anchor) === lineId)" :key="marker.answer.answerID" :data-answer="marker.answer.answerID" variant="quiet" @click="emit('open', marker.answer, $event.currentTarget as HTMLElement)">Question · {{ marker.anchor.kind === 'hunk' ? 'hunk' : `${marker.anchor.side === 'old' ? 'merge base' : 'head'} L${marker.anchor.start}–${marker.anchor.end}` }} · {{ marker.answer.question }}</UiButton>
        </div>
      </template>
    </SourceDiff>
  </div>
</template>

<style scoped>
.source-questions { min-width: 0; flex-shrink: 0; }
.question-selection, .question-markers { padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--color-border); font-size: var(--text-small); overflow-wrap: anywhere; }
.question-selection > * + * { margin-top: var(--space-2); }
.hunk-actions, .question-markers { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.question-markers .ui-button { white-space: normal; overflow-wrap: anywhere; text-align: left; }
</style>
