<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue'
import SourceDiff from '../components/SourceDiff.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiBadge from '../components/ui/UiBadge.vue'
import { sampleQuiz } from './quiz'
import { sampleDiffs, sampleSourceLabel, type SampleAnchor } from './workspace'
import type { DiffLine } from '../components/diff'

const emit = defineEmits<{ source: [anchor: SampleAnchor] }>()
const id = useId()
const shown = new URLSearchParams(location.search).get('sample') === 'quiz'
const position = ref(shown ? 1 : 0)
const attempts = ref(sampleQuiz.map((_question, index): { selected: number | null; checked: boolean } => ({ selected: shown && index < 2 ? index === 0 ? 1 : 0 : null, checked: shown && index < 2 })))
const question = computed((): typeof sampleQuiz[number] => sampleQuiz[position.value]!)
const attempt = computed((): typeof attempts.value[number] => attempts.value[position.value]!)
const checked = computed((): number => attempts.value.filter((item): boolean => item.checked).length)
const missed = computed(countMissed)
const evidence = computed(sourceEvidence)
const legend = ref<HTMLElement>()
const completed = ref(false)
const announcement = ref('')

/**
 * Counts checked sample selections against the fixed illustrative key, never reader understanding.
 * @returns Number of checked selections to revisit.
 */
function countMissed(): number {
  return attempts.value.filter((item, index): boolean => item.checked && item.selected !== sampleQuiz[index]!.correct).length
}

/**
 * Keeps evidence in its original side, revision and source order.
 * @returns The pinned literal source excerpt for the current sample question.
 */
function sourceEvidence(): DiffLine[] {
  const range = question.value.anchor.range!
  return (sampleDiffs[question.value.anchor.fileId] ?? []).filter((line): boolean => {
    const number = line[range.side]
    return number !== null && number >= range.start && number <= range.end
  })
}

/**
 * Selects a native radio choice without checking or overwriting a submitted attempt.
 * @param choice Local answer index.
 */
function choose(choice: number): void {
  if (!attempt.value.checked) attempt.value.selected = choice
}

/**
 * Prevents keyboard changes to an already checked native radio group.
 * @param event Keyboard input from a submitted choice.
 */
function guardRadio(event: KeyboardEvent): void {
  if (attempt.value.checked && [' ', 'ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight'].includes(event.key)) event.preventDefault()
}

/**
 * Explicitly checks a fixed sample key without inference, saving or thread mutations.
 */
function check(): void {
  if (attempt.value.checked || attempt.value.selected === null) return
  attempt.value.checked = true
  announcement.value = attempt.value.selected === question.value.correct ? 'Sample answer matches the illustrative key.' : 'Sample answer needs review. Your selected answer is retained.'
}

/**
 * Restores a local attempt and moves keyboard focus to its question without submitting it.
 * @param index Question position to restore.
 */
async function go(index: number): Promise<void> {
  if (index < 0 || index >= sampleQuiz.length) return
  position.value = index
  completed.value = false
  announcement.value = ''
  await nextTick()
  legend.value?.focus()
}

/**
 * Shows factual sample results only after every local question has been checked.
 */
function results(): void {
  if (checked.value === sampleQuiz.length) completed.value = true
}
</script>

<template>
  <div class="quiz-preview">
    <ScrollRegion label="Sample comprehension quiz" class="quiz-main">
      <header class="quiz-heading"><div><h2>Check your understanding</h2><p>Five fictional questions · This PR revision · a81c2f4</p></div><UiButton disabled :aria-describedby="`${id}-scope`">New quiz — unavailable</UiButton></header>
      <p :id="`${id}-scope`" class="quiz-scope">Sample-data simulation only. Generation, real grading and saving are not implemented. Quiz results never approve a PR, create a thread or mark a question Understood.</p>
      <div class="quiz-progress"><nav aria-label="Quiz questions"><UiButton v-for="(_item, index) in sampleQuiz" :key="index" :aria-current="position === index ? 'step' : undefined" :aria-label="`Quiz question ${index + 1}, ${attempts[index]!.checked ? attempts[index]!.selected === sampleQuiz[index]!.correct ? 'checked, matches sample key' : 'checked, to revisit' : 'not checked'}`" @click="go(index)">{{ String(index + 1).padStart(2, '0') }}</UiButton></nav><p>{{ checked }} of 5 checked · {{ missed }} to revisit</p></div>
      <section v-if="completed" class="quiz-results"><h3>Sample results</h3><p>{{ checked - missed }} match the illustrative key · {{ missed }} to revisit. Not a comprehension score or PR approval.</p><UiButton @click="go(0)">Review checked questions</UiButton><UiButton @click="emit('source', question.anchor)">Back to diff</UiButton></section>
      <form v-else class="quiz-question" @submit.prevent="check">
        <p class="quiz-question-meta">Question {{ String(position + 1).padStart(2, '0') }} / 05 · {{ question.topic }} · Single answer</p>
        <fieldset :key="position" class="quiz-choices" :aria-describedby="`${id}-context ${id}-check-hint`">
          <legend ref="legend" tabindex="-1">{{ question.prompt }}</legend>
          <p :id="`${id}-context`" class="quiz-context">{{ sampleSourceLabel(question.anchor) }}</p>
          <label v-for="(choice, index) in question.choices" :key="index" class="quiz-option" :class="{ 'is-selected': attempt.selected === index, 'is-correct': attempt.checked && question.correct === index, 'is-wrong': attempt.checked && attempt.selected === index && question.correct !== index }">
            <input type="radio" :name="`${id}-question-${position}`" :value="index" :checked="attempt.selected === index" :aria-disabled="attempt.checked || undefined" @change="choose(index)" @click="attempt.checked && $event.preventDefault()" @keydown="guardRadio" />
            <span class="quiz-letter">{{ String.fromCharCode(65 + index) }}</span><span class="quiz-choice-copy">{{ choice }}</span>
            <span v-if="attempt.checked && attempt.selected === index" class="answer-label">{{ index === question.correct ? '✓ Your answer · Correct answer' : '× Your answer' }}</span><span v-else-if="attempt.checked && question.correct === index" class="answer-label">✓ Correct answer</span>
          </label>
        </fieldset>
        <p :id="`${id}-check-hint`" class="quiz-check-hint">{{ attempt.checked ? 'Checked against a fixed sample key. Your original selection is read-only.' : 'Select one answer, then explicitly preview checking. Selection alone reveals no solution.' }}</p>
        <p v-if="attempt.checked" class="quiz-answer-status">{{ attempt.selected === question.correct ? 'Matches the illustrative key.' : 'Not quite. Review the sample explanation, then continue.' }}</p>
        <div class="quiz-navigation"><UiButton :disabled="position === 0" @click="go(position - 1)">Previous</UiButton><UiButton v-if="!attempt.checked" type="submit" variant="primary" :disabled="attempt.selected === null" :aria-describedby="`${id}-check-hint ${id}-scope`">Preview answer check</UiButton><UiButton v-else-if="position < 4" variant="primary" @click="go(position + 1)">Next question</UiButton><UiButton v-else variant="primary" :disabled="checked !== 5" @click="results">Review sample results</UiButton></div>
      </form>
      <p role="status" class="sr-only">{{ announcement }}</p>
    </ScrollRegion>
    <aside class="quiz-explanation" aria-label="Quiz answer explanation">
      <header><h2>{{ attempt.checked ? 'Answer explanation' : 'Before you check' }}</h2><UiBadge v-if="attempt.checked" :tone="attempt.selected === question.correct ? 'success' : 'warning'">{{ attempt.selected === question.correct ? 'Matches sample key' : 'Needs review' }}</UiBadge></header>
      <ScrollRegion label="Sample quiz explanation" class="quiz-explanation-body">
        <template v-if="attempt.checked"><p class="explanation-origin">Illustrative explanation · Not an agent response</p><h3>{{ position === 1 ? 'The store write is skipped.' : question.topic }}</h3><p v-for="paragraph in question.explanation" :key="paragraph">{{ paragraph }}</p><section class="quiz-evidence" aria-label="Illustrative source evidence"><h4>{{ sampleSourceLabel(question.anchor) }}</h4><SourceDiff :lines="evidence" label="Pinned sample quiz evidence" :selection="null" :selectable="false" /></section><UiButton @click="emit('source', question.anchor)">View in diff</UiButton><p :id="`${id}-thread-unavailable`">Starting a new thread from a quiz is unavailable. No existing thread or understanding state is changed.</p><UiButton variant="soft" disabled :aria-describedby="`${id}-thread-unavailable`">Ask in a new thread — unavailable</UiButton></template>
        <template v-else><h3>Choose an answer at your own pace.</h3><p>The fixed sample key, explanation and evidence are revealed only after an explicit answer check.</p><UiButton @click="emit('source', question.anchor)">View source context</UiButton></template>
      </ScrollRegion>
    </aside>
  </div>
</template>

<style scoped>
.quiz-preview { container: quiz / inline-size; display: grid; grid-template-columns: minmax(0, 1fr) var(--width-conversation); gap: var(--space-6); flex: 1; min-height: 0; min-width: 0; }
.quiz-main { padding: var(--space-3) var(--space-2); }
.quiz-heading, .quiz-progress, .quiz-navigation { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); }
.quiz-heading h2 { font-size: var(--text-heading); font-weight: 500; }
.quiz-heading p, .quiz-scope, .quiz-progress p, .quiz-context, .quiz-question-meta, .quiz-check-hint { color: var(--color-muted); font-size: var(--text-small); }
.quiz-heading p { margin-top: var(--space-1); }
.quiz-scope { margin-block: var(--space-4); }
.quiz-progress { padding-block: var(--space-3); border-bottom: 1px solid var(--color-border); }
.quiz-progress nav { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.quiz-progress [aria-current] { border-color: var(--color-accent); background: var(--color-accent-soft); color: var(--color-accent); }
.quiz-question { margin-top: var(--space-6); }
.quiz-question-meta { margin-bottom: var(--space-4); }
.quiz-choices { display: grid; gap: var(--space-3); margin: 0; padding: 0; border: 0; min-width: 0; }
.quiz-choices legend { width: 100%; padding: 0; margin-bottom: var(--space-4); font-size: 1.25rem; font-weight: 500; line-height: 1.55; }
.quiz-context { margin-bottom: var(--space-3); overflow-wrap: anywhere; }
.quiz-option { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-3); min-height: 4.5rem; padding: var(--space-4); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); cursor: pointer; }
.quiz-option:hover { border-color: var(--color-border-strong); }
.quiz-option input { accent-color: var(--color-accent); }
.quiz-letter { color: var(--color-muted); font-size: var(--text-small); }
.quiz-choice-copy { flex: 1 1 14rem; font-size: var(--text-prose); }
.answer-label { font-size: var(--text-small); }
.is-selected { background: var(--color-accent-soft); border-color: var(--color-accent-border); }
.is-wrong { background: var(--color-error-soft); border-color: var(--color-error); }
.is-correct { background: var(--color-success-soft); border-color: var(--color-success); }
.quiz-check-hint, .quiz-answer-status { margin-top: var(--space-4); font-size: var(--text-small); }
.quiz-navigation { margin-top: var(--space-5); }
.quiz-explanation { display: flex; flex-direction: column; min-height: 0; min-width: 0; border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.quiz-explanation > header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); padding: var(--space-4) var(--space-6); border-bottom: 1px solid var(--color-border); }
.quiz-explanation h2 { font-size: 1.0625rem; font-weight: 500; }
.quiz-explanation-body { flex: 1; padding: var(--space-6); font-size: var(--text-prose); line-height: 1.75; }
.quiz-explanation-body h3 { margin-bottom: var(--space-4); font-size: 1.125rem; font-weight: 500; }
.quiz-explanation-body p, .quiz-explanation-body .ui-button { margin-bottom: var(--space-4); }
.explanation-origin { color: var(--color-muted); font-size: var(--text-small); }
.quiz-evidence { margin-block: var(--space-5); overflow: hidden; border: 1px solid var(--color-border); border-radius: var(--radius-control); }
.quiz-evidence h4 { padding: var(--space-3); font: var(--text-code-meta)/1.6 var(--font-code); overflow-wrap: anywhere; border-bottom: 1px solid var(--color-border); }
.quiz-results { display: grid; justify-items: start; gap: var(--space-4); margin-top: var(--space-6); }
@container workspace (max-width: 56.25rem) { .quiz-preview { display: flex; flex-direction: column; overflow: auto; } .quiz-main, .quiz-explanation { flex: 0 0 auto; overflow: visible; } .quiz-explanation-body { overflow: visible; } }
</style>
