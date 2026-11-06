<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, shallowRef, useId, watch } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import SourceDiff from '../components/SourceDiff.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import type { DiffLine } from '../components/diff'
import type { RuntimeClient } from './client'
import type { PinnedDiff, SourceLine } from './source'
import { quizActive, type QuizRead, type QuizStatus, type QuizChecked, type QuizEvidence, type QuizQuestion } from './quiz'

const props = defineProps<{ prID: string; client: RuntimeClient | null; active: boolean; diff?: PinnedDiff; drafting: boolean }>()
const emit = defineEmits<{ source: [evidence: QuizEvidence, revisionID: string] }>()
const id = useId()
const saved = shallowRef<QuizRead>()
const status = shallowRef<QuizStatus | null>(null)
const selections = ref<Record<string, string>>({})
const checks = shallowRef<Record<string, QuizChecked>>({})
const position = ref(0)
const reading = ref(false)
const sending = ref(false)
const cancelling = ref(false)
const checking = ref('')
const confirming = ref(false)
const needsRead = ref(false)
const error = ref('')
const legend = ref<HTMLElement>()
const quiz = computed((): QuizRead['quiz'] => saved.value?.quiz ?? null)
const question = computed((): QuizQuestion | undefined => quiz.value?.questions[position.value])
const checked = computed((): QuizChecked | undefined => question.value ? checks.value[question.value.questionID] : undefined)
const selected = computed((): string => question.value ? selections.value[question.value.questionID] ?? '' : '')
const working = computed((): boolean => quizActive(status.value))
const unavailable = computed((): boolean => !props.client || !saved.value || !props.diff || reading.value || sending.value || cancelling.value || !!checking.value || working.value || needsRead.value || status.value?.executionUnknown === true)
const count = computed((): number => Object.keys(checks.value).length)
const outdated = computed((): boolean => !!quiz.value && !!props.diff && quiz.value.revisionID !== props.diff.revisionID)
let epoch = 0
let readRequest: AbortController | undefined
let pollRequest: AbortController | undefined
let checkRequest: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined
const mutations = new Set<AbortController>()

/** Stops observation only; accepted app-owned work continues. */
function stopPolling(): void { clearTimeout(timer); timer = undefined; pollRequest?.abort(); pollRequest = undefined }

/** Clears connection-scoped state without submitting or cancelling generation. */
function reset(): void {
  epoch++
  stopPolling()
  readRequest?.abort()
  readRequest = undefined
  checkRequest = undefined
  for (const request of mutations) request.abort()
  mutations.clear()
  saved.value = undefined
  status.value = null
  selections.value = {}
  checks.value = {}
  position.value = 0
  reading.value = false
  sending.value = false
  cancelling.value = false
  checking.value = ''
  confirming.value = false
  needsRead.value = false
  error.value = ''
}

/**
 * Accepts nonregressing feedback only within the retained attempt.
 * @param next Scoped current feedback.
 */
function acceptStatus(next: QuizStatus): void {
  const previous = status.value
  if (previous?.generationID === next.generationID) {
    if (next.quizID !== previous.quizID || next.revisionID !== previous.revisionID) throw new Error('Changed quiz generation scope.')
    if (BigInt(next.sequence) < BigInt(previous.sequence)) return
    const unchanged = JSON.stringify(next) === JSON.stringify(previous)
    if ((next.sequence === previous.sequence || !quizActive(previous)) && !unchanged || previous.cancelRequested && !next.cancelRequested) throw new Error('Inconsistent quiz generation feedback.')
  }
  status.value = next
}

/** Schedules at most one read per second while this view observes active work. */
function schedule(): void {
  clearTimeout(timer)
  if (props.active && working.value && !needsRead.value && !pollRequest) timer = setTimeout((): void => { void poll() }, 1000)
}

/** Observes one exact attempt without resubmission or timer-driven progress. */
async function poll(): Promise<void> {
  const currentStatus = status.value
  const client = props.client
  if (!props.active || !client || !currentStatus || pollRequest || reading.value || !working.value || needsRead.value) return
  const request = new AbortController()
  const scope = epoch
  pollRequest = request
  try {
    const next = await client.readQuizGeneration(currentStatus, request.signal)
    if (request.signal.aborted || scope !== epoch || client !== props.client || status.value?.generationID !== currentStatus.generationID) return
    acceptStatus(next)
    if (status.value?.state === 'completed') await refresh()
  } catch {
    if (!request.signal.aborted && scope === epoch && client === props.client) { needsRead.value = true; error.value = 'Generation observation stopped. Accepted work may continue. Refresh saved quiz; do not resend automatically.' }
  } finally {
    if (pollRequest === request) { pollRequest = undefined; schedule() }
  }
}

/** Reads saved bytes on entry or explicit refresh without creating an attempt or starting inference. */
async function refresh(): Promise<void> {
  const client = props.client
  if (!client || !props.active || reading.value || sending.value || cancelling.value) return
  stopPolling()
  const request = new AbortController()
  const scope = epoch
  readRequest = request
  reading.value = true
  error.value = ''
  confirming.value = false
  try {
    const result = await client.readQuiz(props.prID, request.signal)
    if (request.signal.aborted || readRequest !== request || scope !== epoch || client !== props.client) return
    if (saved.value && (BigInt(result.version) < BigInt(saved.value.version) || result.version === saved.value.version && result.quiz?.quizID !== saved.value.quiz?.quizID)) throw new Error('Regressing saved quiz.')
    if (saved.value && result.quiz && quiz.value && (result.version === saved.value.version ? JSON.stringify(result.quiz) !== JSON.stringify(quiz.value) : result.quiz.quizID === quiz.value.quizID)) throw new Error('Changed immutable quiz.')
    if (!result.generation && status.value || result.generation?.state === 'completed' && result.generation.savedVersion === result.version && result.generation.quizID !== result.quiz?.quizID) throw new Error('Inconsistent saved quiz feedback.')
    if (result.generation) acceptStatus(result.generation)
    if (result.quiz?.quizID !== quiz.value?.quizID) {
      checkRequest?.abort()
      checkRequest = undefined
      checking.value = ''
      selections.value = {}
      checks.value = {}
      position.value = 0
    }
    saved.value = result
    needsRead.value = false
    if (status.value?.state === 'completed' && BigInt(status.value.savedVersion!) > BigInt(result.version)) { needsRead.value = true; error.value = 'Saving completed after this read. Refresh saved quiz to load the acknowledged set.' }
  } catch {
    if (!request.signal.aborted && scope === epoch && client === props.client) { needsRead.value = true; error.value = 'Cannot read saved quiz. Existing questions and selections were retained. Refresh explicitly.' }
  } finally {
    if (readRequest === request) { reading.value = false; readRequest = undefined; schedule() }
  }
}

/** Generates once from displayed source, keeping any previous set and page-memory answers. */
async function generate(): Promise<void> {
  const client = props.client
  const read = saved.value
  const diff = props.diff
  if (!client || !read || !diff || unavailable.value) return
  const request = new AbortController()
  const scope = epoch
  mutations.add(request)
  confirming.value = false
  sending.value = true
  error.value = ''
  stopPolling()
  try {
    const admitted = await client.generateQuiz(props.prID, diff.revision, read.version, request.signal)
    if (request.signal.aborted || scope !== epoch || client !== props.client) return
    status.value = admitted
  } catch {
    if (!request.signal.aborted && scope === epoch && client === props.client) { needsRead.value = true; error.value = 'Generation admission could not be confirmed. Read saved state before another explicit generation; do not resend automatically.' }
  } finally {
    mutations.delete(request)
    if (scope === epoch) { sending.value = false; schedule() }
  }
}

/** Requests cancellation of only the displayed generation; no saved content is removed. */
async function cancel(): Promise<void> {
  const client = props.client
  const currentStatus = status.value
  if (!client || !currentStatus || !working.value || cancelling.value || needsRead.value) return
  const request = new AbortController()
  const scope = epoch
  cancelling.value = true
  mutations.add(request)
  try {
    const result = await client.cancelQuizGeneration(currentStatus, request.signal)
    if (request.signal.aborted || scope !== epoch || client !== props.client || status.value?.generationID !== currentStatus.generationID) return
    acceptStatus(result)
  } catch {
    if (!request.signal.aborted && scope === epoch) { needsRead.value = true; error.value = 'Cancellation could not be confirmed. Generation may continue; refresh saved quiz.' }
  } finally {
    mutations.delete(request)
    if (scope === epoch) { cancelling.value = false; if (status.value?.state === 'completed') void refresh(); else schedule() }
  }
}

/**
 * Selects without checking or changing a previously checked choice.
 * @param choiceID Explicit native radio alternative.
 */
function choose(choiceID: string): void { if (question.value && !checked.value && checking.value !== question.value.questionID) selections.value[question.value.questionID] = choiceID }

/**
 * Preserves read-only radio selection while retaining native focusability.
 * @param event Keyboard activation of a frozen group.
 */
function guardRadio(event: KeyboardEvent): void { if ((checked.value || checking.value === question.value?.questionID) && [' ', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) event.preventDefault() }

/** Checks the explicit selected question once, never by inference or local key calculation. */
async function check(): Promise<void> {
  const client = props.client
  const read = saved.value
  const q = question.value
  const choiceID = selected.value
  if (!client || !read?.quiz || !q || !choiceID || checked.value || checking.value || reading.value || needsRead.value) return
  const request = new AbortController()
  const scope = epoch
  checkRequest = request
  checking.value = q.questionID
  mutations.add(request)
  error.value = ''
  try {
    const result = await client.checkQuiz(read, q.questionID, choiceID, request.signal)
    if (request.signal.aborted || checkRequest !== request || scope !== epoch || client !== props.client || saved.value?.version !== read.version || quiz.value?.quizID !== read.quiz.quizID || selections.value[q.questionID] !== choiceID) return
    checks.value = { ...checks.value, [q.questionID]: result }
  } catch {
    if (!request.signal.aborted && checkRequest === request && scope === epoch) { needsRead.value = true; error.value = 'Answer check could not be confirmed. Your selection was retained. Refresh saved quiz before checking again.' }
  } finally {
    mutations.delete(request)
    if (checkRequest === request) { checking.value = ''; checkRequest = undefined }
  }
}

/**
 * Navigates page-memory answers and focuses only an explicit active-view action.
 * @param index Question position.
 */
async function go(index: number): Promise<void> {
  if (!quiz.value || index < 0 || index >= quiz.value.questions.length) return
  const scope = epoch
  const quizID = quiz.value.quizID
  const trigger = document.activeElement
  position.value = index
  await nextTick()
  if (props.active && scope === epoch && quiz.value?.quizID === quizID && position.value === index && (document.activeElement === trigger || document.activeElement === document.body)) legend.value?.focus()
}

/**
 * Adapts retained selected quotes for the literal source renderer.
 * @param quote Checked original evidence.
 * @returns Original-side lines, never source from a newer revision.
 */
function lines(quote: QuizEvidence): DiffLine[] {
  return quote.selected.map((line: SourceLine): DiffLine => ({ id: line.id, hunkID: quote.anchor.hunkID, base: line.oldLine ?? null, head: line.newLine ?? null, kind: line.sign === '+' ? 'added' : line.sign === '-' ? 'removed' : 'context', text: line.text, noNewline: line.noNewline }))
}

/**
 * Enables navigation only when the displayed original revision still contains the quoted range.
 * @param quote Retained evidence.
 * @returns Whether source navigation is currently supported.
 */
function navigable(quote: QuizEvidence): boolean {
  if (!quiz.value || props.drafting || props.diff?.revisionID !== quiz.value.revisionID) return false
  const hunk = props.diff.files.find((file): boolean => file.id === quote.anchor.fileID)?.hunks.find((h): boolean => h.id === quote.anchor.hunkID)
  return !!hunk && quote.selected.every((line: SourceLine): boolean => hunk.lines.some((actual: SourceLine): boolean => actual.id === line.id && actual.text === line.text && actual.oldLine === line.oldLine && actual.newLine === line.newLine))
}

watch((): readonly [string, RuntimeClient | null] => [props.prID, props.client], (): void => { reset(); if (props.active) void refresh() }, { flush: 'sync' })
watch((): boolean => props.active, (active: boolean): void => {
  if (active) void refresh()
  else {
    confirming.value = false
    stopPolling()
    readRequest?.abort()
    readRequest = undefined
    reading.value = false
  }
}, { immediate: true, flush: 'sync' })
onUnmounted(reset)
</script>

<template>
  <section id="real-quiz" class="real-quiz" aria-label="PR quiz">
    <ScrollRegion label="Quiz questions and generation" class="quiz-main">
      <header class="quiz-heading"><h2 tabindex="-1">PR quiz</h2><div class="quiz-actions"><UiButton :disabled="reading || sending || cancelling || !client" @click="refresh">Refresh saved quiz</UiButton><UiButton v-if="quiz" :disabled="unavailable" @click="confirming = true">Regenerate quiz</UiButton></div></header>
      <p class="quiz-scope">Agent-generated questions may be inaccurate or ambiguous. Answers never approve a PR, certify understanding, create a thread or mark anything Understood/Viewed. Selections and checks are page-memory only; reload starts unchecked.</p>
      <p v-if="reading" role="status">Reading saved quiz… No inference is started.</p>
      <p v-if="sending" role="status">Requesting generation admission… This is not saved completion.</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="!diff">Load pinned source in Files changed to generate a quiz. Saved questions and checks need no source loading.</p>
      <div v-if="confirming" class="quiz-confirm" role="group" aria-label="Confirm quiz regeneration"><p>Replace the saved quiz with five new questions for the displayed source? The previous set and your selections remain until a complete replacement is saved.</p><UiButton :disabled="unavailable" @click="generate">Generate replacement</UiButton><UiButton @click="confirming = false">Keep current quiz</UiButton></div>
      <div v-if="status" class="generation-status" role="status"><p>Generation: {{ status.state }}{{ status.cancelRequested ? ' · Cancellation requested; idle or rollback is not guaranteed.' : '' }}</p><p v-if="status.state === 'completed'">Saving acknowledged for version {{ status.savedVersion }}. The saved read below identifies the displayed set.</p><p v-if="status.state === 'failed'">Generation did not acknowledge a saved replacement ({{ status.failureCode }}). Read saved state before retrying uncertain work.</p><p v-if="status.state === 'cancelled'">Generation cancelled. The previously saved set is retained.</p><p v-if="status.executionUnknown">Execution is uncertain. Do not resend; generation remains blocked in this runtime.</p><UiButton v-if="working" :disabled="cancelling || status.cancelRequested || needsRead || status.state === 'saving'" @click="cancel">{{ cancelling ? 'Requesting cancellation…' : 'Cancel generation' }}</UiButton><p v-if="working">Accepted work belongs to the app and may continue when you leave this view or disconnect.</p></div>
      <template v-if="quiz && question">
        <p class="quiz-revision">Saved version {{ saved?.version }} · {{ quiz.revision.baseBranch }} · {{ quiz.revision.mergeBaseSHA.slice(0, 8) }} → {{ quiz.revision.headSHA.slice(0, 8) }}</p>
        <p v-if="outdated" role="status">Older revision: this quiz does not match the displayed source. Checks still use its original saved key.</p><p v-else-if="!diff">Original revision retained; currentness is unverified until source is loaded.</p>
        <div class="quiz-progress"><nav aria-label="Quiz questions"><UiButton v-for="(q, index) in quiz.questions" :key="q.questionID" :aria-current="position === index ? 'step' : undefined" :aria-label="`Quiz question ${index + 1}, ${checks[q.questionID] ? 'checked' : 'not checked'}`" @click="go(index)">{{ index + 1 }}</UiButton></nav><p>{{ count }} of 5 checked in this page</p></div>
        <form class="quiz-question" aria-label="Check quiz answer" @submit.prevent="check">
          <p>Question {{ position + 1 }} / 5 · Single answer</p>
          <fieldset :key="`${quiz.quizID}-${position}`" :aria-describedby="`${id}-check-hint`"><legend ref="legend" tabindex="-1">{{ question.prompt }}</legend>
            <label v-for="c in question.choices" :key="c.choiceID" class="quiz-option" :class="{ 'is-selected': selected === c.choiceID, 'is-correct': checked?.correctChoiceID === c.choiceID, 'is-wrong': checked && !checked.correct && selected === c.choiceID }"><input type="radio" :name="`${id}-${question.questionID}`" :value="c.choiceID" :checked="selected === c.choiceID" :aria-disabled="!!checked || checking === question.questionID || undefined" @change="choose(c.choiceID)" @click="(checked || checking === question.questionID) && $event.preventDefault()" @keydown="guardRadio" /><span>{{ c.choiceID.toUpperCase() }}</span><span class="quiz-copy">{{ c.text }}</span><span v-if="checked?.correctChoiceID === c.choiceID">✓ Correct answer{{ selected === c.choiceID ? ' · Your answer' : '' }}</span><span v-else-if="checked && selected === c.choiceID">× Your answer</span></label>
          </fieldset>
          <p :id="`${id}-check-hint`">{{ checked ? 'Your checked selection is read-only in this page.' : 'Select one alternative, then Check answer. Selection alone reveals no solution.' }}</p>
          <p v-if="checked" role="status">{{ checked.correct ? 'Matches the saved agent-generated key.' : 'Incorrect against the saved key. Your selected answer is retained.' }}</p>
          <div class="quiz-actions"><UiButton :disabled="position === 0" @click="go(position - 1)">Previous</UiButton><UiButton v-if="!checked" type="submit" variant="primary" :disabled="!selected || !!checking || reading || needsRead || !client">{{ checking ? 'Checking…' : 'Check answer' }}</UiButton><UiButton :disabled="position === 4" @click="go(position + 1)">Next question</UiButton></div>
        </form>
      </template>
      <div v-else-if="saved && !reading" class="quiz-empty"><h3>No saved quiz</h3><p>Generate five questions explicitly from the displayed pinned source. Entering Quiz never generates automatically.</p><UiButton variant="primary" :disabled="unavailable" @click="generate">Generate quiz</UiButton></div>
    </ScrollRegion>
    <aside class="quiz-explanation" aria-label="Quiz answer explanation"><h2>{{ checked ? 'Answer explanation' : 'Before you check' }}</h2><ScrollRegion label="Quiz explanation and original evidence" class="explanation-body"><template v-if="checked"><p>Agent-generated explanation · Verify against the original source.</p><p class="explanation-text">{{ checked.explanation }}</p><section v-for="(quote, index) in checked.evidence" :key="index" class="quiz-evidence"><h3>{{ quote.newPath ?? quote.oldPath }} · {{ quote.anchor.side }} {{ quote.anchor.start }}–{{ quote.anchor.end }}</h3><SourceDiff :lines="lines(quote)" label="Original quiz evidence" :selection="null" :selectable="false" /><UiButton :disabled="!navigable(quote)" @click="emit('source', quote, checked.revisionID)">View in original diff</UiButton><p v-if="!navigable(quote)">Original source navigation unavailable. Load the matching revision and finish or discard any pinned source draft; this quote remains readable.</p></section></template><p v-else>The saved key, explanation and source evidence are withheld until an explicit answer check. Selecting or navigating questions does not check an answer.</p></ScrollRegion></aside>
  </section>
</template>

<style scoped>
.real-quiz { container: real-quiz / inline-size; display: grid; grid-template-columns: minmax(0, 1fr) minmax(16rem, 27.5rem); gap: var(--space-5); flex: 1; min-height: 0; min-width: 0; }
.quiz-main { padding: var(--space-3) var(--space-2); }
.quiz-main p, .explanation-body p { margin-block: var(--space-3); overflow-wrap: anywhere; }
.quiz-heading, .quiz-actions, .quiz-progress, .quiz-progress nav { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); }
.quiz-heading, .quiz-progress { justify-content: space-between; }
.quiz-heading h2 { font-size: var(--text-heading); }
.quiz-scope, .quiz-revision { color: var(--color-muted); font-size: var(--text-small); }
.quiz-confirm, .generation-status { border: 1px solid var(--color-border); padding: var(--space-3); border-radius: var(--radius-panel); margin-block: var(--space-3); }
.quiz-progress { padding-block: var(--space-3); border-block: 1px solid var(--color-border); }
.quiz-progress [aria-current] { border-color: var(--color-accent); background: var(--color-accent-soft); }
.quiz-question { margin-top: var(--space-5); }
fieldset { display: grid; gap: var(--space-3); border: 0; padding: 0; margin: 0; min-width: 0; }
legend { padding: 0; margin-bottom: var(--space-4); font-size: 1.25rem; line-height: 1.55; white-space: pre-wrap; overflow-wrap: anywhere; }
.quiz-option { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); min-height: 4.5rem; padding: var(--space-4); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); cursor: pointer; overflow-wrap: anywhere; }
.quiz-option input { accent-color: var(--color-accent); }
.quiz-copy { flex: 1 1 12rem; white-space: pre-wrap; }
.is-selected { background: var(--color-accent-soft); border-color: var(--color-accent-border); }
.is-wrong { background: var(--color-error-soft); border-color: var(--color-error); }
.is-correct { background: var(--color-success-soft); border-color: var(--color-success); }
.quiz-explanation { display: flex; flex-direction: column; min-width: 0; min-height: 0; border: 1px solid var(--color-border); border-radius: var(--radius-panel); }
.quiz-explanation > h2 { padding: var(--space-4); border-bottom: 1px solid var(--color-border); font-size: var(--text-body); }
.explanation-body { padding: var(--space-4); }
.explanation-text { white-space: pre-wrap; }
.quiz-evidence { margin-top: var(--space-5); min-width: 0; }
.quiz-evidence h3 { font-size: var(--text-small); overflow-wrap: anywhere; margin-block: var(--space-3); }
.quiz-empty { margin-block: var(--space-5); }
@container workspace (max-width: 72rem) { .real-quiz { grid-template-columns: minmax(0, 1fr); grid-auto-rows: max-content; align-content: start; overflow: auto; } .quiz-main, .explanation-body { overflow: visible; flex: 0 0 auto; } .quiz-explanation { min-height: 12rem; } }
</style>
