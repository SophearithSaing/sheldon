<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import SourceDiff from '../components/SourceDiff.vue'
import InlineQuestionComposer from '../components/InlineQuestionComposer.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import type { LineRange } from '../components/diff'
import { makeSampleThread, sampleDiffs, sampleFiles, threadAnchors, type SampleAnchor, type SampleThread } from './workspace'

interface Draft {
  id: number
  anchor: SampleAnchor
  text: string
  error: string
}
type Change = { fileId: string; range: LineRange | null; draftId?: number; compose?: boolean; origin?: number; revealId?: number; sourceOnly?: boolean }
const props = defineProps<{ threads: SampleThread[]; viewed: Record<string, boolean> }>()
const file = defineModel<string>('file', { required: true })
const emit = defineEmits<{
  create: [thread: SampleThread]
  open: [id: number, trigger: HTMLElement]
  viewed: [id: string, value: boolean]
  reveal: [id: number, trigger: HTMLElement]
}>()
const root = ref<HTMLElement>()
const selection = ref<LineRange | null>({ side: 'head', start: 27, end: 30 })
const origin = ref(27)
const drafts = ref<Draft[]>(props.threads.length ? [] : [
  { id: 1, anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 27, end: 30 } }, text: '', error: '' },
])
const expanded = ref<number | null>(drafts.value[0]?.id ?? null)
const pendingChange = ref<Change | null>(null)
const pending = ref(false)
const creating = ref(false)
const outcome = ref('success')
const message = ref('')
const outcomes = [
  { value: 'success', label: 'Answered sample thread' },
  { value: 'creation-error', label: 'Creation failure (no thread)' },
  { value: 'response-error', label: 'Response failure (thread retained)' },
  { value: 'pending', label: 'Pending creation (finish manually)' },
] as const
const selectedFile = computed(() => sampleFiles.find((item): boolean => item.id === file.value)!)
const lines = computed(() => sampleDiffs[file.value] ?? [])
const draft = computed(() => drafts.value.find((item): boolean => item.id === expanded.value && item.anchor.fileId === file.value))
const fileThreads = computed(threadsForFile)
const revisionBusy = computed((): boolean => props.threads.some((thread): boolean => thread.status === 'investigating'))

/**
 * Displays one marker per thread/file while retaining its full multi-range context in the panel.
 * @returns Local marker projections, never mutations of the submitted thread anchors.
 */
function threadsForFile(): SampleThread[] {
  return props.threads.flatMap((thread): SampleThread[] => {
    const anchor = threadAnchors(thread).find((item): boolean => item.fileId === file.value)
    return anchor ? [{ ...thread, anchor }] : []
  })
}

/**
 * Describes pinned source without implying repository-backed validation.
 * @param anchor Local sample source anchor.
 * @returns Compact source coordinates and revision.
 */
function context(anchor: SampleAnchor): string {
  const range = anchor.range
  return `${range ? `${range.side} L${range.start}–${range.end}` : 'Whole file'} · ${anchor.revision}`
}

/**
 * Finds the last displayed row belonging to a pinned sample anchor.
 * @param anchor Source anchor to locate.
 * @returns Matching row index, or -1 for whole-file anchors.
 */
function anchorIndex(anchor: SampleAnchor): number {
  if (!anchor.range) return -1
  const range = anchor.range
  return lines.value.findIndex((line): boolean => line[range.side] === range.end)
}

/**
 * Splits source at real insertion boundaries so long code never stretches a composer.
 * @returns Source blocks with their original line order and insertion positions.
 */
function sourceBlocks(): { start: number; end: number; lines: typeof lines.value }[] {
  const positions = new Set(fileThreads.value.map((thread): number => anchorIndex(thread.anchor)))
  if (draft.value?.anchor.range) positions.add(anchorIndex(draft.value.anchor))
  positions.add(lines.value.length - 1)
  const blocks: { start: number; end: number; lines: typeof lines.value }[] = []
  let start = 0
  for (let index = 0; index < lines.value.length; index++) {
    if (!positions.has(index)) continue
    blocks.push({ start, end: index, lines: lines.value.slice(start, index + 1) })
    start = index + 1
  }
  return blocks
}
const blocks = computed(sourceBlocks)

/**
 * Finds the visible gutter control for focus restoration and scroll anchoring.
 * @param range Selected source range.
 * @returns Its final gutter control, or null when not displayed.
 */
function gutter(range: LineRange | null): HTMLButtonElement | null {
  if (!range) return null
  for (const button of root.value?.querySelectorAll<HTMLButtonElement>('button[data-line]') ?? []) {
    if (button.dataset.side === range.side && Number(button.dataset.line) === range.end) return button
  }
  return null
}

/**
 * Focuses the local composer after an explicit Ask or recovery action.
 */
async function focusDraft(): Promise<void> {
  await nextTick()
  root.value?.querySelector<HTMLTextAreaElement>('[data-draft-input]')?.focus()
}

/**
 * Applies a confirmed context change while retaining other local drafts.
 * @param change Destination file/range or recovered draft.
 */
async function applyChange(change: Change): Promise<void> {
  file.value = change.fileId
  selection.value = change.range
  origin.value = change.origin ?? change.range?.start ?? 0
  const recovered = change.draftId === undefined
    ? drafts.value.filter((item): boolean => item.anchor.fileId === change.fileId).at(-1)
    : drafts.value.find((item): boolean => item.id === change.draftId)
  expanded.value = change.compose || change.sourceOnly || change.revealId !== undefined || (change.draftId === undefined && change.range) ? null : recovered?.id ?? null
  if (recovered && expanded.value !== null) selection.value = recovered.anchor.range
  pendingChange.value = null
  await nextTick()
  if (change.compose) await ask(change.range === null)
  else if (change.draftId !== undefined) await focusDraft()
  if (change.revealId !== undefined || change.sourceOnly) {
    const target = gutter(change.range) ?? root.value?.querySelector<HTMLButtonElement>('[data-file-ask]')
    if (target) {
      target.scrollIntoView({ block: 'center' })
      if (change.revealId !== undefined) emit('reveal', change.revealId, target)
      else target.focus({ preventScroll: true })
    }
  }
}

/**
 * Reveals pinned sample evidence without silently retargeting an unsent draft.
 * @param id Thread whose panel remains selected.
 * @param anchor Pinned source to select.
 */
function revealSource(id: number, anchor: SampleAnchor): void {
  if (anchor.revision !== 'a81c2f4') return
  requestChange({ fileId: anchor.fileId, range: anchor.range, revealId: id })
}

/**
 * Reveals pinned quiz context without changing question selection or creating a thread.
 * @param anchor Illustrative source coordinates.
 */
function revealAnchor(anchor: SampleAnchor): void {
  if (anchor.revision !== 'a81c2f4') return
  requestChange({ fileId: anchor.fileId, range: anchor.range, sourceOnly: true })
}

/**
 * Guards context changes against pending creation and silent draft retargeting.
 * @param change Requested sample context.
 */
function requestChange(change: Change): void {
  if (pending.value || creating.value) {
    message.value = 'Finish or cancel the pending sample creation before changing context.'
    return
  }
  if (change.draftId !== undefined && change.draftId === expanded.value) {
    void focusDraft()
    return
  }
  if (draft.value?.text.length) {
    pendingChange.value = change
    void focusGuard()
    return
  }
  void applyChange(change)
}

/**
 * Makes a blocked context change and its recovery choices keyboard-reachable.
 */
async function focusGuard(): Promise<void> {
  await nextTick()
  root.value?.querySelector<HTMLElement>('.context-guard')?.focus()
}

/**
 * Dismisses a context switch and restores the original draft's focus.
 */
function keepEditing(): void {
  pendingChange.value = null
  void focusDraft()
}

/**
 * Requests a different sample file without changing the current draft's anchor.
 * @param id Sample file identifier.
 */
function selectFile(id: string): void {
  if (id !== file.value) requestChange({ fileId: id, range: null })
}

/**
 * Selects gutter coordinates without treating native text selection as Ask.
 * @param side Base or head side.
 * @param line Selected line number.
 * @param extend Whether Shift extends the existing range.
 */
function selectRange(side: 'base' | 'head', line: number, extend: boolean): void {
  const start = extend && selection.value?.side === side ? origin.value : line
  const range = { side, start: Math.min(start, line), end: Math.max(start, line) }
  requestChange({ fileId: file.value, range, origin: start })
}

/**
 * Confirms a context switch with explicit retention or discard of the current text.
 * @param discard Whether to discard the current draft before switching.
 */
function confirmChange(discard: boolean): void {
  const change = pendingChange.value
  if (!change) return
  if (discard) drafts.value = drafts.value.filter((item): boolean => item.id !== expanded.value)
  void applyChange(change)
}

/**
 * Opens or recovers the draft for an explicit line/file Ask action.
 * @param wholeFile Whether to anchor beneath the file header instead of a line range.
 */
async function ask(wholeFile = false): Promise<void> {
  if (pending.value || creating.value || (!wholeFile && !selection.value)) return
  const range = wholeFile ? null : selection.value
  if (draft.value?.text.length && JSON.stringify(draft.value.anchor.range) !== JSON.stringify(range)) {
    pendingChange.value = { fileId: file.value, range, compose: true }
    await focusGuard()
    return
  }
  let existing = drafts.value.find((item): boolean => item.anchor.fileId === file.value && JSON.stringify(item.anchor.range) === JSON.stringify(range))
  if (!existing) {
    existing = { id: Math.max(0, ...drafts.value.map((item): number => item.id)) + 1, anchor: { fileId: file.value, revision: 'a81c2f4', range: range ? { ...range } : null }, text: '', error: '' }
    drafts.value.push(existing)
  }
  expanded.value = existing.id
  await focusDraft()
}

/**
 * Closes but retains a local draft and restores its source control.
 */
async function cancel(): Promise<void> {
  const range = draft.value ? draft.value.anchor.range : selection.value
  pending.value = false
  pendingChange.value = null
  expanded.value = null
  message.value = 'Draft kept in this page. Use Recover local drafts to reopen it.'
  await nextTick()
  const target = gutter(range) ?? root.value?.querySelector<HTMLButtonElement>('[data-file-ask]')
  target?.focus({ preventScroll: true })
}

/**
 * Creates one explicitly local sample thread and hands off to its panel.
 */
async function complete(): Promise<void> {
  const current = draft.value
  if (!current || creating.value || revisionBusy.value) return
  creating.value = true
  pending.value = false
  const anchorElement = gutter(current.anchor.range)
  const top = anchorElement?.getBoundingClientRect().top
  const thread = makeSampleThread(
    Math.max(0, ...props.threads.map((item): number => item.id)) + 1,
    { ...current.anchor, range: current.anchor.range ? { ...current.anchor.range } : null },
    current.text.trim(),
    outcome.value === 'response-error' ? 'failed' : 'answered',
  )
  emit('create', thread)
  drafts.value = drafts.value.filter((item): boolean => item.id !== current.id)
  expanded.value = null
  await nextTick()
  const marker = root.value?.querySelector<HTMLButtonElement>(`[data-thread-id="${thread.id}"]`)
  if (marker) emit('open', thread.id, marker)
  await nextTick()
  const scroll = root.value?.querySelector<HTMLElement>('.diff-scroll')
  const restoredAnchor = gutter(current.anchor.range)
  if (scroll && top !== undefined && restoredAnchor) scroll.scrollTop += restoredAnchor.getBoundingClientRect().top - top
  creating.value = false
}

/**
 * Validates and previews creation without a request, retry timer or automatic answer.
 */
async function submit(): Promise<void> {
  const current = draft.value
  if (!current || pending.value || creating.value || revisionBusy.value) return
  if (!current.text.trim()) {
    current.error = 'Enter a question before previewing thread creation.'
    await focusDraft()
    return
  }
  current.error = ''
  if (outcome.value === 'creation-error') {
    current.error = 'Sample creation failed. No thread was created; your draft is retained.'
  } else if (outcome.value === 'pending') {
    pending.value = true
  } else {
    await complete()
  }
}

/**
 * Reopens an existing panel without creating a duplicate or losing an inline draft.
 * @param thread Existing local thread.
 * @param event Marker activation event.
 */
function openThread(thread: SampleThread, event: MouseEvent): void {
  if (pending.value || creating.value) return
  selection.value = thread.anchor.range
  pendingChange.value = null
  expanded.value = null
  emit('open', thread.id, event.currentTarget as HTMLElement)
}
defineExpose({ selectFile, revealSource, revealAnchor })
</script>

<template>
  <div ref="root" class="files-preview">
    <div class="diff-toolbar">
      <div class="diff-display" role="group" aria-label="Diff display">
        <button type="button" aria-pressed="true">Unified</button>
        <button type="button" aria-pressed="false" aria-disabled="true" title="Split display is not implemented." @click.prevent>Split — unavailable</button>
      </div>
      <div class="file-navigation">
        <span>{{ sampleFiles.indexOf(selectedFile) + 1 }} of {{ sampleFiles.length }} files</span>
        <UiButton :disabled="selectedFile.id === sampleFiles[0].id" aria-label="Previous changed file" @click="selectFile(sampleFiles[sampleFiles.indexOf(selectedFile) - 1]!.id)">↑</UiButton>
        <UiButton :disabled="selectedFile.id === sampleFiles.at(-1)?.id" aria-label="Next changed file" @click="selectFile(sampleFiles[sampleFiles.indexOf(selectedFile) + 1]!.id)">↓</UiButton>
      </div>
    </div>
    <div class="selection-actions">
      <span>{{ selection ? context({ fileId: file, revision: 'a81c2f4', range: selection }) : 'Choose line numbers. Shift+click or Shift+Enter extends a range.' }}</span>
      <UiButton :disabled="!selection || pending" @click="ask()">Ask about selected lines</UiButton>
    </div>
    <p class="sr-only" role="status">{{ message }}</p>
    <section v-if="pendingChange" class="context-guard" role="alert" tabindex="-1" aria-label="Keep your draft context">
      <p>Your draft is still anchored to {{ context(draft!.anchor) }} in {{ selectedFile.path }}.</p>
      <UiButton @click="keepEditing">Keep editing</UiButton>
      <UiButton @click="confirmChange(false)">Keep draft and switch</UiButton>
      <UiButton @click="confirmChange(true)">Discard draft and switch</UiButton>
    </section>
    <ScrollRegion label="Sample code diff" class="diff-scroll">
      <article class="diff-card" :aria-label="selectedFile.path">
        <header class="diff-head">
          <h2>{{ selectedFile.path }}</h2>
          <span class="additions">+{{ selectedFile.added }}</span><span class="deletions">−{{ selectedFile.removed }}</span>
          <label class="viewed-check"><input type="checkbox" :checked="viewed[file]" @change="emit('viewed', file, ($event.target as HTMLInputElement).checked)" />Viewed</label>
        </header>
        <InlineQuestionComposer v-if="draft && !draft.anchor.range" v-model="draft.text" :context="context(draft.anchor)" :error="draft.error" :pending="pending" :busy="revisionBusy" @submit="submit" @cancel="cancel" @finish="complete" />
        <button v-for="thread in fileThreads.filter((item) => !item.anchor.range)" :key="thread.id" class="question-marker" :data-thread-id="thread.id" :aria-label="`Open sample question ${thread.id}`" @click="openThread(thread, $event)">Question {{ thread.id }} · {{ thread.question }} →</button>
        <div class="hunk-header">{{ selectedFile.hunk }}</div>
        <template v-for="block in blocks" :key="block.start">
          <SourceDiff :lines="block.lines" :label="block.start === 0 ? 'Sample source before draft' : 'Sample source after draft'" :selection="selection" :highlight="file !== 'docs'" @select="selectRange" />
          <InlineQuestionComposer v-if="draft?.anchor.range && anchorIndex(draft.anchor) === block.end" v-model="draft.text" :context="context(draft.anchor)" :error="draft.error" :pending="pending" :busy="revisionBusy" @submit="submit" @cancel="cancel" @finish="complete" />
          <button v-for="thread in fileThreads.filter((item) => item.anchor.range && anchorIndex(item.anchor) === block.end)" :key="thread.id" class="question-marker" :data-thread-id="thread.id" :aria-label="`Open sample question ${thread.id}`" @click="openThread(thread, $event)"><span>{{ thread.id }}</span> {{ thread.question }} <span>Open thread →</span></button>
        </template>
        <footer class="diff-bottom"><span>{{ fileThreads.length ? `${fileThreads.length} sample questions in this file` : 'No submitted sample questions yet' }}</span><UiButton data-file-ask variant="quiet" @click="ask(true)">Ask about this file ↗</UiButton></footer>
      </article>
      <details class="preview-tools">
        <summary>Sample preview controls</summary>
        <p>Source excerpts and file totals are illustrative, not a fetched complete PR. Preview creation adds one local thread, not a native session or saved conversation.</p>
        <UiField v-model="outcome" label="Preview creation outcome" type="select" :options="outcomes" :disabled="pending" />
        <a href="?sample=review" target="_blank" rel="noopener noreferrer">Open four-thread sample in a new tab</a>
      </details>
      <details v-if="drafts.length" class="recovery-tools">
        <summary>Recover local drafts ({{ drafts.length }})</summary>
        <UiButton v-for="item in drafts" :key="item.id" @click="requestChange({ fileId: item.anchor.fileId, range: item.anchor.range, draftId: item.id })">Recover {{ sampleFiles.find((entry) => entry.id === item.anchor.fileId)?.path }} · {{ context(item.anchor) }}</UiButton>
      </details>
    </ScrollRegion>
  </div>
</template>

<style scoped>
.files-preview { container: files / inline-size; display: flex; flex-direction: column; flex: 1; min-height: 0; min-width: 0; }
.diff-toolbar, .selection-actions, .file-navigation, .diff-bottom { display: flex; align-items: center; justify-content: space-between; gap: var(--space-2); }
.diff-toolbar { min-height: var(--target-control); margin-bottom: var(--space-2); }
.file-navigation { color: var(--color-muted); font-size: var(--text-code-meta); }
.diff-display { display: flex; padding: 0.1875rem; border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--color-surface); }
.diff-display button { min-height: 1.875rem; padding-inline: 0.75rem; border: 0; border-radius: 0.25rem; color: var(--color-muted); background: transparent; font-size: var(--text-code-meta); }
.diff-display [aria-pressed='true'] { background: var(--color-panel); color: var(--color-text); }
.diff-display [aria-disabled] { cursor: not-allowed; }
.selection-actions { flex-wrap: wrap; padding-bottom: var(--space-2); font-size: var(--text-code-meta); color: var(--color-muted); }
.diff-scroll { flex: 1; min-height: 0; }
.diff-card { overflow: hidden; border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.diff-head { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-2); min-height: 4rem; padding: var(--space-2) 1.125rem; border-bottom: 1px solid var(--color-border); }
.diff-head h2 { min-width: 0; font: 500 var(--text-code-meta) var(--font-code); overflow-wrap: anywhere; }
.additions { color: var(--color-success); font-size: var(--text-code-meta); }
.deletions { color: var(--color-error); font-size: var(--text-code-meta); }
.viewed-check { display: flex; align-items: center; gap: 0.375rem; min-height: var(--target-control); margin-left: auto; color: var(--color-muted); font-size: var(--text-small); }
.viewed-check input { accent-color: var(--color-accent); }
.hunk-header { padding: 0.625rem 1.125rem; border-bottom: 1px solid var(--color-border); color: var(--color-muted); font: var(--text-code-meta)/1.5 var(--font-code); }
.question-marker { display: flex; align-items: center; gap: var(--space-2); width: calc(100% - 2rem); min-height: var(--target-control); margin: var(--space-2) var(--space-4); padding: var(--space-2) var(--space-3); border: 1px solid var(--color-accent-border); border-radius: var(--radius-control); background: var(--color-accent-soft); text-align: left; overflow-wrap: anywhere; font-size: var(--text-small); }
.question-marker > span:first-child { color: var(--color-accent); }
.question-marker > span:last-child { margin-left: auto; color: var(--color-muted); white-space: nowrap; }
.diff-bottom { flex-wrap: wrap; padding: 0 var(--space-4); border-top: 1px solid var(--color-border); color: var(--color-muted); font-size: var(--text-code-meta); }
.preview-tools, .recovery-tools { margin-top: var(--space-4); padding: var(--space-3); border: 1px solid var(--color-border); border-radius: var(--radius-control); font-size: var(--text-small); }
.preview-tools summary, .recovery-tools summary { min-height: var(--target-control); cursor: pointer; }
.preview-tools p, .preview-tools .ui-field { margin-bottom: var(--space-3); }
.recovery-tools .ui-button { margin: var(--space-2); }
.context-guard { display: flex; flex-wrap: wrap; gap: var(--space-2); padding: var(--space-3); border: 1px solid var(--color-warning); border-radius: var(--radius-control); margin-bottom: var(--space-2); }
.context-guard p { flex-basis: 100%; }
@container files (min-width: 50rem) {
  .question-marker { margin-left: 4.625rem; width: calc(100% - 5.625rem); }
}
</style>
