<script setup lang="ts">
import { computed, onUnmounted, ref, shallowRef, watch } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import SourceDiff from '../components/SourceDiff.vue'
import type { DiffLine, LineRange } from '../components/diff'
import type { RuntimeClient } from './client'
import type { PinnedDiff, SourceFile } from './source'
import { semanticActive, semanticLines, semanticUngrouped, type SemanticRead, type SemanticStatus, type SemanticAnchor, type SemanticGroup } from './semantic'

const props = defineProps<{ prID: string; client: RuntimeClient | null; active: boolean; diff?: PinnedDiff; drafting: boolean }>()
const emit = defineEmits<{ source: [anchor: SemanticAnchor, revisionID: string, compose: boolean] }>()
const selection = shallowRef<SemanticAnchor | null>(null)
const saved = shallowRef<SemanticRead>()
const status = shallowRef<SemanticStatus | null>(null)
const selected = ref('g1')
const reading = ref(false)
const sending = ref(false)
const cancelling = ref(false)
const confirming = ref(false)
const needsRead = ref(false)
const error = ref('')
const review = computed((): SemanticRead['review'] => saved.value?.review ?? null)
const ungrouped = computed((): SemanticAnchor[] => review.value ? semanticUngrouped(review.value) : [])
const current = computed((): SemanticAnchor[] => selected.value === 'ungrouped' ? ungrouped.value : review.value?.groups.find((g): boolean => g.groupID === selected.value)?.ranges ?? [])
const group = computed((): SemanticGroup | undefined => review.value?.groups.find((g): boolean => g.groupID === selected.value))
const unsupported = computed((): SourceFile[] => review.value?.diff.files.filter((f): boolean => f.kind !== 'text') ?? [])
const working = computed((): boolean => semanticActive(status.value))
const unavailable = computed((): boolean => !props.client || !saved.value || !props.diff || reading.value || sending.value || cancelling.value || working.value || needsRead.value || status.value?.executionUnknown === true)
let epoch = 0
let readRequest: AbortController | undefined
let pollRequest: AbortController | undefined
let timer: ReturnType<typeof setTimeout> | undefined
const mutations = new Set<AbortController>()

/** Stops observation only, never accepted app-owned generation. */
function stopPolling(): void { clearTimeout(timer); timer = undefined; pollRequest?.abort(); pollRequest = undefined }

/** Clears only connection-scoped reading state and invalidates obsolete callbacks. */
function reset(): void {
  epoch++; stopPolling(); readRequest?.abort(); readRequest = undefined
  for (const request of mutations) request.abort()
  mutations.clear(); saved.value = undefined; status.value = null; selected.value = 'g1'; selection.value = null
  reading.value = false; sending.value = false; cancelling.value = false; confirming.value = false; needsRead.value = false; error.value = ''
}

/** Applies monotonic exact-attempt feedback without rewriting terminal outcomes.
 * @param next Validated feedback.
 */
function acceptStatus(next: SemanticStatus): void {
  const previous = status.value
  if (previous?.generationID === next.generationID) {
    if (previous.reviewID !== next.reviewID || previous.revisionID !== next.revisionID) throw new Error('Changed semantic generation scope.')
    if (BigInt(next.sequence) < BigInt(previous.sequence)) return
    if ((next.sequence === previous.sequence || !semanticActive(previous)) && JSON.stringify(next) !== JSON.stringify(previous) || previous.cancelRequested && !next.cancelRequested) throw new Error('Inconsistent semantic status.')
  }
  status.value = next
}

/** Schedules bounded read-only observation of a known active attempt. */
function schedule(): void { clearTimeout(timer); if (props.active && working.value && !needsRead.value && !pollRequest) timer = setTimeout((): void => { void poll() }, 1000) }

/** Polls one exact attempt without resubmitting or exposing structured output. */
async function poll(): Promise<void> {
  const client = props.client; const original = status.value
  if (!props.active || !client || !original || pollRequest || reading.value || needsRead.value || !working.value) return
  const request = new AbortController(); const scope = epoch; pollRequest = request
  try {
    const next = await client.readSemanticGeneration(original, request.signal)
    if (request.signal.aborted || pollRequest !== request || scope !== epoch || client !== props.client || status.value?.generationID !== original.generationID) return
    acceptStatus(next)
    if (status.value?.state === 'completed') await refresh()
  } catch {
    if (!request.signal.aborted && scope === epoch && client === props.client) { needsRead.value = true; error.value = 'Observation stopped. Accepted generation may continue. Refresh saved review; do not resend automatically.' }
  } finally { if (pollRequest === request) { pollRequest = undefined; schedule() } }
}

/** Reads only saved content on entry or explicit refresh, retaining healthy previous content on failure. */
async function refresh(): Promise<void> {
  const client = props.client
  if (!client || !props.active || reading.value || sending.value || cancelling.value) return
  stopPolling(); const request = new AbortController(); const scope = epoch; readRequest = request; reading.value = true; confirming.value = false; error.value = ''
  try {
    const next = await client.readSemantic(props.prID, request.signal)
    if (request.signal.aborted || readRequest !== request || scope !== epoch || client !== props.client) return
    const previous = saved.value
    if (previous && (BigInt(next.version) < BigInt(previous.version) || next.version === previous.version && JSON.stringify(next.review) !== JSON.stringify(previous.review) || next.version !== previous.version && next.review?.reviewID === previous.review?.reviewID)) throw new Error('Changed saved semantic identity.')
    if (!next.generation && status.value || next.generation?.state === 'completed' && next.generation.savedVersion === next.version && next.generation.reviewID !== next.review?.reviewID) throw new Error('Inconsistent saved semantic feedback.')
    if (next.generation) acceptStatus(next.generation)
    if (next.review?.reviewID !== review.value?.reviewID) selected.value = 'g1'
    saved.value = next; needsRead.value = false
    if (status.value?.state === 'completed' && BigInt(status.value.savedVersion!) > BigInt(next.version)) { needsRead.value = true; error.value = 'Saving finished after this read. Refresh saved review to load the acknowledged result.' }
  } catch {
    if (!request.signal.aborted && scope === epoch && client === props.client) { needsRead.value = true; error.value = 'Cannot read saved semantic review. Previous complete groups were retained. Refresh explicitly.' }
  } finally { if (readRequest === request) { readRequest = undefined; reading.value = false; schedule() } }
}

/** Starts one explicitly confirmed replacement pinned to the currently displayed source. */
async function generate(): Promise<void> {
  const client = props.client; const diff = props.diff; const read = saved.value
  if (!client || !diff || !read || unavailable.value) return
  const request = new AbortController(); const scope = epoch; mutations.add(request); sending.value = true; confirming.value = false; error.value = ''; stopPolling()
  try {
    const next = await client.generateSemantic(props.prID, diff.revision, read.version, request.signal)
    if (request.signal.aborted || scope !== epoch || client !== props.client) return
    status.value = next
  } catch {
    if (!request.signal.aborted && scope === epoch && client === props.client) { needsRead.value = true; error.value = 'Generation admission could not be confirmed. Read saved state before another explicit action; do not resend automatically.' }
  } finally { mutations.delete(request); if (scope === epoch) { sending.value = false; schedule() } }
}

/** Requests exact-attempt interruption without removing saved content or promising rollback. */
async function cancel(): Promise<void> {
  const client = props.client; const original = status.value
  if (!client || !original || !working.value || cancelling.value || needsRead.value) return
  const request = new AbortController(); const scope = epoch; cancelling.value = true; mutations.add(request)
  try {
    const next = await client.cancelSemanticGeneration(original, request.signal)
    if (request.signal.aborted || scope !== epoch || client !== props.client || status.value?.generationID !== original.generationID) return
    acceptStatus(next)
  } catch { if (!request.signal.aborted && scope === epoch) { needsRead.value = true; error.value = 'Cancellation could not be confirmed. Refresh saved state; accepted work may continue.' } }
  finally { mutations.delete(request); if (scope === epoch) { cancelling.value = false; if (status.value?.state === 'completed') void refresh(); else schedule() } }
}

/** Adapts one literal selected excerpt without splicing noncontiguous source.
 * @param anchor Original range.
 * @returns Shared source renderer lines.
 */
function lines(anchor: SemanticAnchor): DiffLine[] { return review.value ? semanticLines(review.value.diff, anchor).map((line): DiffLine => ({ id: line.id, hunkID: anchor.hunkID, base: line.oldLine ?? null, head: line.newLine ?? null, kind: line.sign === '+' ? 'added' : line.sign === '-' ? 'removed' : 'context', text: line.text, noNewline: line.noNewline })) : [] }

/** Resolves an original display label without host-file access.
 * @param anchor Original range.
 * @returns Retained path label.
 */
function path(anchor: SemanticAnchor): string { const f = review.value?.diff.files.find((file): boolean => file.id === anchor.fileID); return (anchor.side === 'old' ? f?.oldPath : f?.newPath) ?? f?.oldPath ?? 'Original source' }

/** Checks original displayed source identity and quote equality before navigation/asking.
 * @param anchor Original selected range.
 * @returns Whether navigation can safely use already-loaded source.
 */
function canNavigate(anchor: SemanticAnchor): boolean {
  if (props.drafting || !review.value || !props.diff || props.diff.revisionID !== review.value.diff.revisionID) return false
  try { return JSON.stringify(semanticLines(props.diff, anchor)) === JSON.stringify(semanticLines(review.value.diff, anchor)) } catch { return false }
}

/** Selects only coordinates inside one visible excerpt without submitting or opening a draft.
 * @param excerpt Original excerpt boundaries.
 * @param side Display side.
 * @param line Explicitly selected coordinate.
 * @param extend Whether to extend the current selection.
 */
function selectLine(excerpt: SemanticAnchor, side: 'base' | 'head', line: number, extend: boolean): void {
  if (side !== (excerpt.side === 'old' ? 'base' : 'head') || line < excerpt.start || line > excerpt.end) return
  const previous = selection.value
  const start = extend && previous?.fileID === excerpt.fileID && previous.hunkID === excerpt.hunkID && previous.side === excerpt.side ? Math.min(previous.start, line) : line
  const end = extend && previous?.fileID === excerpt.fileID && previous.hunkID === excerpt.hunkID && previous.side === excerpt.side ? Math.max(previous.end, line) : line
  if (start < excerpt.start || end > excerpt.end) return
  selection.value = { ...excerpt, start, end }
}

/** Resolves the current selection only when it remains inside this original excerpt.
 * @param excerpt Original excerpt.
 * @returns Explicit selection or the full original range.
 */
function target(excerpt: SemanticAnchor): SemanticAnchor { const s = selection.value; return s?.fileID === excerpt.fileID && s.hunkID === excerpt.hunkID && s.side === excerpt.side && s.start >= excerpt.start && s.end <= excerpt.end ? s : excerpt }

/** Adapts an excerpt's optional selection to the shared gutter renderer.
 * @param excerpt Original excerpt.
 * @returns Selected visual range, or null.
 */
function selectedRange(excerpt: SemanticAnchor): LineRange | null { const s = selection.value; return s && target(excerpt) === s ? { side: s.side === 'old' ? 'base' : 'head', start: s.start, end: s.end } : null }

/** Opens original source or an explicit existing-flow draft, never dispatching a question.
 * @param excerpt Original excerpt.
 * @param compose Whether the reader explicitly requested a question.
 */
function navigate(excerpt: SemanticAnchor, compose: boolean): void { const a = target(excerpt); if (review.value && canNavigate(a)) emit('source', { ...a }, review.value.diff.revisionID, compose) }

watch((): RuntimeClient | null => props.client, reset, { flush: 'sync' })
watch((): string => props.prID, reset, { flush: 'sync' })
watch((): boolean => props.active, (active: boolean): void => { if (active) void refresh(); else { stopPolling(); readRequest?.abort(); readRequest = undefined; reading.value = false } }, { immediate: true })
onUnmounted(reset)
</script>

<template>
  <section id="real-semantic" class="semantic-view" aria-label="Semantic review">
    <header class="semantic-header">
      <h2>Semantic review</h2>
      <p>Related changed ranges, organized by purpose. Agent-generated interpretation can be wrong; it is not approval, a safety check or proof of understanding.</p>
      <div class="semantic-actions"><UiButton :disabled="unavailable" @click="review ? confirming = true : generate()">{{ review ? 'Regenerate semantic review' : 'Generate semantic review' }}</UiButton><UiButton variant="quiet" :disabled="!client || reading || sending || cancelling" @click="refresh">Refresh saved review</UiButton><UiButton v-if="working" variant="quiet" :disabled="cancelling || needsRead" @click="cancel">Cancel generation</UiButton></div>
      <div v-if="confirming" role="group" aria-label="Confirm semantic replacement"><p>Replace this PR's complete grouping? The previous result stays readable until a new result is saved.</p><UiButton @click="generate">Confirm regenerate review</UiButton><UiButton variant="quiet" @click="confirming = false">Keep current review</UiButton></div>
      <p v-if="!diff">Load pinned source in Files changed before generating. Saved groups remain readable without source.</p>
      <p v-if="reading" role="status">Reading saved semantic review…</p>
      <p v-if="sending" role="status">Requesting generation… Admission is not completion.</p>
      <p v-if="status" role="status">Generation: {{ status.state }}<span v-if="status.failureCode"> · {{ status.failureCode }}</span><span v-if="status.cancelRequested"> · cancellation requested, not idle or rollback proof</span><span v-if="status.savedVersion"> · saved version {{ status.savedVersion }}</span></p>
      <p v-if="status?.failureCode === 'source-too-large'">The complete source inventory exceeds supported limits. Nothing was truncated or sent for inference. Files changed remains available.</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <p v-if="saved && !review">No saved semantic review. Opening this view never starts generation.</p>
      <p v-if="review">Original revision {{ review.diff.revisionID.slice(0, 8) }} · saved version {{ saved?.version }} · {{ !diff ? 'Currentness unverified; source is not loaded.' : diff.revisionID !== review.diff.revisionID ? 'Older revision; source was not retargeted.' : 'Matches displayed source.' }}</p>
    </header>
    <div v-if="review" class="semantic-layout">
      <nav class="semantic-groups" aria-label="Semantic groups"><UiButton v-for="item in review.groups" :key="item.groupID" variant="quiet" :aria-current="selected === item.groupID ? 'page' : undefined" @click="selected = item.groupID">{{ item.title }} · {{ item.ranges.length }} ranges</UiButton><UiButton variant="quiet" :aria-current="selected === 'ungrouped' ? 'page' : undefined" @click="selected = 'ungrouped'">Ungrouped changes · {{ ungrouped.length }} changed lines</UiButton><p>{{ unsupported.length }} unsupported/non-text changes remain in Files changed. Counts describe source inventory, not semantic certainty.</p></nav>
      <section class="semantic-excerpts" tabindex="0" aria-label="Grouped source excerpts">
        <h3>{{ group?.title ?? 'Ungrouped changes' }}</h3><p v-if="group">{{ group.explanation }}</p><p v-if="!current.length">No ungrouped text changes.</p>
        <article v-for="(anchor, index) in current" :key="`${anchor.fileID}-${anchor.hunkID}-${anchor.side}-${anchor.start}`" class="semantic-excerpt" :aria-label="`${path(anchor)} ${anchor.side} lines ${anchor.start}–${anchor.end}`">
          <h4>{{ path(anchor) }} · {{ anchor.side === 'old' ? 'merge base' : 'head' }} L{{ anchor.start }}–{{ anchor.end }}</h4>
          <p>Excerpt {{ index + 1 }} · original revision {{ review.diff.revisionID.slice(0, 8) }}. Separate ranges are not contiguous code.</p>
          <SourceDiff :lines="lines(anchor)" :label="path(anchor)" :selection="selectedRange(anchor)" :highlight="false" :selectable="canNavigate(anchor)" real @select="(side, line, extend): void => selectLine(anchor, side, line, extend)" />
          <div class="semantic-actions"><UiButton variant="quiet" :disabled="!canNavigate(target(anchor))" @click="navigate(anchor, false)">View in original diff</UiButton><UiButton variant="quiet" :disabled="!canNavigate(target(anchor))" @click="navigate(anchor, true)">Ask about selected range</UiButton></div>
          <p v-if="!canNavigate(anchor)">Original matching source is unavailable, or a pinned draft/source request needs attention. Saved quotes do not authorize newer source.</p>
        </article>
        <p v-for="file in unsupported" :key="file.id">{{ file.newPath ?? file.oldPath }} · {{ file.kind }} — available in the ordinary file view; no invented line ranges.</p>
      </section>
    </div>
    <p class="semantic-footer">Only complete generated groups are saved. Reading position and drafts stay in page memory. Leaving this view stops observation, not accepted generation.</p>
  </section>
</template>

<style scoped>
.semantic-view { container: semantic / inline-size; display: flex; flex-direction: column; min-height: 0; height: 100%; overflow: auto; }
.semantic-header, .semantic-footer { padding: var(--space-4) var(--space-6); overflow-wrap: anywhere; }
.semantic-header > * + * { margin-top: var(--space-3); }
.semantic-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.semantic-layout { display: grid; grid-template-columns: 15.75rem minmax(0, 1fr); gap: var(--space-6); padding: var(--space-4) var(--space-6); min-height: 14rem; flex: 1; overflow: hidden; }
.semantic-groups { overflow: auto; min-height: 0; }
.semantic-groups .ui-button { width: 100%; white-space: normal; text-align: left; margin-bottom: var(--space-2); }
.semantic-groups [aria-current] { background: var(--color-accent-soft); border-color: var(--color-accent); }
.semantic-excerpts { overflow: auto; min-width: 0; overflow-wrap: anywhere; }
.semantic-excerpt { margin-top: var(--space-4); padding: var(--space-3); background: var(--color-panel); border: 1px solid var(--color-border); border-radius: var(--radius-panel); min-width: 0; }
.semantic-excerpt h4 { font-family: var(--font-code); font-size: var(--text-small); }
.semantic-excerpt p { margin-block: var(--space-2); }
.semantic-footer { color: var(--color-muted); font-size: var(--text-small); }
@container semantic (max-width: 56rem) { .semantic-layout { grid-template-columns: minmax(0, 1fr); overflow: visible; flex: none; } .semantic-groups { max-height: 16rem; } .semantic-excerpts { max-height: 40rem; } }
</style>
