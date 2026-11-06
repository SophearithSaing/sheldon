<script setup lang="ts">
import { computed, nextTick, onUnmounted, ref, shallowRef, watch } from 'vue'
import WorkspaceShell from '../components/WorkspaceShell.vue'
import ChangedFileTree from '../components/ChangedFileTree.vue'
import SourceQuestions from './SourceQuestions.vue'
import QuestionConversation from './QuestionConversation.vue'
import SavedQuestions from './SavedQuestions.vue'
import QuizView from './QuizView.vue'
import type { QuizEvidence } from './quiz'
import { useConversations } from './conversations'
import UiButton from '../components/ui/UiButton.vue'
import OpenPR from './OpenPR.vue'
import { lineCounts, type SourceRequest, type SourceResponse, type SourceFile, type PinnedDiff } from './source'
import type { DiffLine } from '../components/diff'
import type { ReviewState, RuntimeClient, RegisteredPR } from './client'
import type { InitialQuestion, QuestionAnswer } from './questions'

const props = defineProps<{ reviewId: string; reviewState: ReviewState; client: RuntimeClient | null }>()
const emit = defineEmits<{ questions: [present: boolean] }>()
const source = shallowRef<SourceResponse>()
const displayed = shallowRef<SourceResponse>()
const pending = ref(false)
const error = ref('')
const stale = ref(false)
const notice = ref('')
const selected = ref('')
const search = ref('')
const branch = ref('')
const sourceHeading = ref<HTMLElement>()
const sourcePane = ref<HTMLElement>()
const workspace = ref<InstanceType<typeof WorkspaceShell>>()
const drafting = ref(false)
const conversationOpen = ref(false)
const conversations = useConversations((): RuntimeClient | null => props.client)
const { threads, selectedID, current: currentThread } = conversations
const { list: savedList, pending: historyPending, error: historyError } = conversations.history
const view = ref<'files' | 'questions' | 'quiz'>('files')
const quizOpened = ref(false)
const opening = ref('')
let openEpoch = 0
const initialSending = ref(false)
const revisionBusy = computed((): string => diff.value ? initialSending.value ? 'A question is awaiting admission in this shared PR-revision session. Drafts stay editable.' : conversations.busy(diff.value.revisionID) : '')
const currentBusy = computed((): string => currentThread.value ? conversations.busy(currentThread.value.turns[0]!.answer.revisionID) || (initialSending.value && currentThread.value.turns[0]!.answer.revisionID === diff.value?.revisionID ? 'Another question is awaiting admission in this shared PR-revision session.' : '') : '')
const activeMarkers = computed((): { answer: QuestionAnswer; anchor: InitialQuestion['anchor'] }[] => threads.value.filter((item): boolean => item.turns[0]!.answer.revisionID === diff.value?.revisionID && item.input.anchor.fileID === active.value?.id).map((item): { answer: QuestionAnswer; anchor: InitialQuestion['anchor'] } => ({ answer: item.turns[0]!.answer, anchor: item.input.anchor })))
const questionCounts = computed((): Record<string, number> => {
  const result: Record<string, number> = {}
  for (const item of threads.value) if (item.turns[0]!.answer.revisionID === diff.value?.revisionID) result[item.input.anchor.fileID] = (result[item.input.anchor.fileID] ?? 0) + 1
  return result
})
let controller: AbortController | undefined
let lastAction: SourceRequest = { action: 'load' }
const pr = computed((): RegisteredPR | undefined => displayed.value?.pr ?? source.value?.pr ?? props.reviewState.pr)
const diff = computed((): PinnedDiff | undefined => displayed.value?.diff)
const files = computed((): SourceFile[] => diff.value?.files ?? [])
const treeFiles = computed((): { id: string; path: string }[] => files.value.map((file): { id: string; path: string } => ({ id: file.id, path: file.newPath ?? file.oldPath ?? '' })))
const active = computed((): SourceFile | undefined => files.value.find((file): boolean => file.id === selected.value))
const counts = computed((): ReturnType<typeof lineCounts> => lineCounts(files.value))
const currentCounts = computed((): ReturnType<typeof lineCounts> => lineCounts(active.value ? [active.value] : []))
const lines = computed(displayLines)

/** Clears only this tab's transient source and cancels requests on disconnection. */
function reset(): void {
  openEpoch++
  opening.value = ''
  view.value = 'files'
  quizOpened.value = false
  controller?.abort()
  controller = undefined
  source.value = undefined
  displayed.value = undefined
  pending.value = false
  error.value = ''
  stale.value = false
  notice.value = ''
  selected.value = ''
  branch.value = ''
  search.value = ''
  drafting.value = false
  conversations.clear()
  initialSending.value = false
  conversationOpen.value = false
  emit('questions', false)
}

/** Cancels only source work and returns keyboard focus to its surviving region. */
async function cancelSource(): Promise<void> {
  if (diff.value) {
    controller?.abort()
    controller = undefined
    pending.value = false
    stale.value = true
    notice.value = 'Source request cancelled. The displayed pinned revision was not replaced.'
    source.value = displayed.value
  } else {
    controller?.abort()
    controller = undefined
    pending.value = false
    source.value = undefined
    error.value = ''
  }
  await nextTick()
  sourcePane.value?.focus()
}

/**
 * Runs one explicit source action without advertising registration as source readiness.
 * @param input Metadata load, fallback consent or exact base selection.
 */
async function load(input: SourceRequest = { action: 'load' }): Promise<void> {
  if (!props.client || pending.value) return
  if (drafting.value) { notice.value = 'Finish or discard the pinned question draft before replacing source.'; return }
  const current = new AbortController()
  const requestedID = props.reviewId
  const client = props.client
  const requestedSnapshot = diff.value?.revisionID
  controller = current
  pending.value = true
  error.value = ''
  notice.value = ''
  lastAction = input
  try {
    const response = await client.source(requestedID, input, current.signal)
    if (current.signal.aborted || controller !== current || requestedID !== props.reviewId || client !== props.client || diff.value?.revisionID !== requestedSnapshot) return
    source.value = response
    if (response.state === 'ready' && response.diff) {
      const path = active.value?.newPath ?? active.value?.oldPath
      const unchanged = response.diff.revisionID === requestedSnapshot
      displayed.value = response
      selected.value = response.diff.files.find((file): boolean => (file.newPath ?? file.oldPath) === path)?.id ?? response.diff.files[0]?.id ?? ''
      stale.value = false
      notice.value = requestedSnapshot ? unchanged ? 'Source unchanged. The displayed revision is still pinned.' : 'Refresh complete. Only this tab now displays the new pinned revision.' : ''
    } else if (requestedSnapshot) {
      stale.value = true
      notice.value = 'Refresh needs an explicit base choice. The previous pinned revision remains displayed.'
    }
    branch.value = ''
    await nextTick()
    if (current.signal.aborted || controller !== current || requestedID !== props.reviewId || client !== props.client) return
    if (document.activeElement === document.body || document.activeElement instanceof Node && sourcePane.value?.contains(document.activeElement)) {
      if (sourceHeading.value) sourceHeading.value.focus()
      else sourcePane.value?.focus()
    }
  } catch (failure: unknown) {
    if (!current.signal.aborted && controller === current && requestedID === props.reviewId && client === props.client) {
      error.value = failure instanceof Error ? failure.message : 'Source loading failed. Retry explicitly.'
      stale.value = !!requestedSnapshot
    }
  } finally {
    if (controller === current) pending.value = false
  }
}

/** Fetches current tips explicitly without moving any other tab's snapshot. */
function refresh(): void { if (diff.value) void load({ action: 'refresh', revision: diff.value.revision }) }

/** Repeats the failed action only after an explicit reader request. */
function retry(): void { void load(lastAction) }

/** Confirms an exact fetched branch/SHA pair; no default branch is assumed. */
function selectBase(): void {
  const candidate = source.value?.candidates?.find((item): boolean => item.branch === branch.value)
  if (candidate) void load({ action: 'select-base', branch: candidate.branch, sha: candidate.sha })
}

/**
 * Navigates a file in the same immutable snapshot and restores source heading focus.
 * @param id Revision-scoped changed file reference.
 */
async function selectFile(id: string): Promise<void> {
  openEpoch++
  if (drafting.value && id !== selected.value) { notice.value = 'Finish or discard the pinned question draft before changing files.'; return }
  selected.value = id
  await nextTick()
  sourceHeading.value?.focus()
}

/**
 * Adapts verified hunks to the shared literal-source renderer without sample content.
 * @returns Pinned display lines including hunk boundaries and final-LF markers.
 */
function displayLines(): DiffLine[] {
  const result: DiffLine[] = []
  for (const hunk of active.value?.hunks ?? []) {
    for (const [index, line] of hunk.lines.entries()) result.push({
      id: line.id, hunkID: hunk.id, base: line.oldLine ?? null, head: line.newLine ?? null,
      kind: line.sign === '+' ? 'added' : line.sign === '-' ? 'removed' : 'context', text: line.text, noNewline: line.noNewline,
      hunkHeader: index === 0 ? `@@ -${hunk.old.start},${hunk.old.count} +${hunk.new.start},${hunk.new.count} @@${hunk.section}` : undefined,
    })
  }
  return result
}

/**
 * Adds one validated admission without claiming completion or changing its source binding.
 * @param answer Exact submitted answer identity.
 * @param input Original question and pinned selection.
 */
function admitted(answer: QuestionAnswer, input: InitialQuestion): void {
  const file = active.value
  const path = input.anchor.kind === 'lines' && input.anchor.side === 'old' ? file?.oldPath : file?.newPath ?? file?.oldPath
  conversations.add(answer, input, path ?? 'Original pinned source')
  conversationOpen.value = true
}

/**
 * Guards draft navigation and removes only feedback belonging to a resolved draft guard.
 * @param active Whether a pinned draft still needs an explicit decision.
 */
function guardDraft(active: boolean): void {
  drafting.value = active
  if (!active && notice.value.startsWith('Finish or discard the pinned question draft')) notice.value = ''
}

/**
 * Opens a marker's own admission without changing source selection or its bound revision.
 * @param answer The marker's original answer.
 * @param trigger Marker control used for focus restoration.
 */
function openAnswer(answer: QuestionAnswer, trigger: HTMLElement): void {
  openEpoch++
  selectedID.value = answer.threadID
  void workspace.value?.openConversation(trigger)
}

/**
 * Switches views without unmounting source, its inline draft or retained conversations.
 * @param next Requested PR view.
 */
function changeView(next: 'files' | 'questions' | 'quiz'): void {
  openEpoch++
  opening.value = ''
  view.value = next
  if (next === 'quiz') quizOpened.value = true
  if (next === 'questions' && !savedList.value && !historyPending.value) void conversations.history.refreshList(props.reviewId)
}

/**
 * Opens checked evidence only in its exact original displayed comparison.
 * @param evidence Checked original quote and range.
 * @param revisionID Original quiz comparison.
 */
async function quizSource(evidence: QuizEvidence, revisionID: string): Promise<void> {
  if (drafting.value || diff.value?.revisionID !== revisionID) return
  const file = diff.value.files.find((item): boolean => item.id === evidence.anchor.fileID)
  const hunk = file?.hunks.find((item): boolean => item.id === evidence.anchor.hunkID)
  if (!file || !hunk || !evidence.selected.every((line): boolean => hunk.lines.some((actual): boolean => actual.id === line.id && actual.text === line.text && actual.oldLine === line.oldLine && actual.newLine === line.newLine))) return
  selected.value = file.id
  changeView('files')
  notice.value = `Quiz evidence: original ${evidence.anchor.side} lines ${evidence.anchor.start}–${evidence.anchor.end}. No draft or thread was created.`
  await nextTick()
  if (diff.value?.revisionID === revisionID && view.value === 'files') sourceHeading.value?.focus()
}

/**
 * Opens an exact saved thread only if its initiating selection and connection are still current.
 * @param threadID Requested saved or local thread.
 * @param trigger Row control used for focus recovery.
 */
async function openThread(threadID: string, trigger: HTMLElement): Promise<void> {
  const epoch = ++openEpoch
  const connection = props.client
  const prID = props.reviewId
  const previous = selectedID.value
  const local = threads.value.find((thread): boolean => thread.id === threadID)
  opening.value = threadID
  /**
   * Keeps a delayed open from replacing later source, thread or keyboard intent.
   * @returns Whether the initiating row still owns the current open intent.
   */
  function currentOpen(): boolean { return epoch === openEpoch && connection === props.client && prID === props.reviewId && selectedID.value === previous && (document.activeElement === trigger || document.activeElement === document.body) }
  const thread = local ?? await conversations.openSaved(prID, threadID, currentOpen)
  if (epoch !== openEpoch || connection !== props.client || prID !== props.reviewId) return
  opening.value = ''
  if (!thread || selectedID.value !== previous || document.activeElement !== trigger && document.activeElement !== document.body) return
  selectedID.value = thread.id
  conversations.activate(thread)
  await workspace.value?.openConversation(trigger)
}

/**
 * Selects retained conversations without replacing their drafts or the displayed source.
 * @param threadID Retained thread.
 */
function selectThread(threadID: string): void { openEpoch++; selectedID.value = threadID }
/**
 * Reports only the presence of disposable question state, never transcript text or credentials.
 * @param present Whether disconnect needs an explicit discard decision.
 */
function reportQuestions(present: boolean): void { emit('questions', present) }
watch((): boolean => drafting.value || threads.value.length > 0, reportQuestions)
watch((): RuntimeClient | null => props.client, reset, { flush: 'sync' })
watch((): string => props.reviewId, reset, { flush: 'sync' })
onUnmounted(reset)
</script>

<template>
  <div class="registered-pr">
    <a class="skip-link" :href="view === 'questions' ? '#saved-questions' : view === 'quiz' ? '#real-quiz' : '#review-content'">Skip to PR content</a>
    <header class="review-header" aria-label="Sheldon navigation">
      <a href="/" aria-label="Sheldon — Open a PR">Sheldon</a><a href="/?preview=library">Sample UI previews</a>
      <span>Registered PR</span><OpenPR :client="client" /><slot name="runtime-trigger" />
    </header>
    <slot name="runtime-panel" />
    <WorkspaceShell v-if="reviewState.status === 'registered' && pr" ref="workspace" v-model:conversation-open="conversationOpen" :has-file-rail="view === 'files' && !!diff && files.length > 0" conversation-title="Question conversation">
      <template #summary>
        <h1 class="pr-title">{{ pr.title ?? `Pull request #${pr.number}` }}</h1>
        <p class="fixture-meta"><span>{{ pr.owner }}/{{ pr.repository }} #{{ pr.number }}</span> · <span>{{ pr.author ? `Author: ${pr.author}` : 'Author unavailable' }}</span><span v-if="diff"> · {{ diff.revision.baseBranch }} · {{ diff.revision.mergeBaseSHA.slice(0, 8) }} → {{ diff.revision.headSHA.slice(0, 8) }}</span></p>
        <a class="github-link" :href="pr.url" target="_blank" rel="noreferrer">View on GitHub ↗</a>
      </template>
      <template #tabs>
        <nav class="real-tabs" aria-label="PR views"><UiButton variant="quiet" :aria-current="view === 'files' ? 'page' : undefined" @click="changeView('files')">Files changed<span v-if="diff"> ({{ files.length }})</span></UiButton><UiButton variant="quiet" :aria-current="view === 'questions' ? 'page' : undefined" @click="changeView('questions')">Questions</UiButton><UiButton variant="quiet" :aria-current="view === 'quiz' ? 'page' : undefined" @click="changeView('quiz')">Quiz</UiButton><UiButton variant="quiet" disabled title="Not implemented for real PRs">Semantic review — unavailable</UiButton><UiButton v-if="threads.length && !conversationOpen" variant="quiet" @click="workspace?.openConversation($event)">Open conversation</UiButton></nav>
      </template>
      <template #files><ChangedFileTree v-model="search" :files="treeFiles" :current="selected" :counts="questionCounts" :viewed="{}" real @select="selectFile" /></template>
      <template #conversation>
        <QuestionConversation v-if="currentThread" :thread="currentThread" :threads="threads" :busy-reason="currentBusy" :displayed-revision="diff?.revisionID" :history-pending="historyPending" :history-error="historyError" @refresh="conversations.refreshSaved(currentThread)" @followup="conversations.followup" @stop="conversations.stop" @observe="conversations.observe" @save="conversations.save" @select="selectThread" />
      </template>
      <template #default>
        <QuizView v-if="quizOpened" v-show="view === 'quiz'" :pr-i-d="reviewId" :client="client" :active="view === 'quiz'" :diff="diff" :drafting="drafting" @source="quizSource" />
        <SavedQuestions v-show="view === 'questions'" :list="savedList" :threads="threads" :pending="historyPending" :error="historyError" :opening="opening" :selected="selectedID" :drafting="drafting" @refresh="conversations.history.refreshList(reviewId)" @open="openThread" />
        <section v-show="view === 'files'" id="review-content" ref="sourcePane" class="source-pane" tabindex="-1" :aria-busy="pending && !diff" aria-label="Pinned PR source">
          <div v-if="diff" class="refresh-controls">
            <div class="refresh-actions"><UiButton :disabled="pending || !client" @click="refresh">Refresh source</UiButton><UiButton v-if="pending" variant="quiet" @click="cancelSource">Cancel source request</UiButton></div>
            <p v-if="pending" role="status">Refreshing source… The displayed revision remains readable until a complete response succeeds.</p>
            <p v-if="stale" class="stale-notice" role="status">Stale / refresh needs attention. Still displaying pinned revision {{ diff.revisionID.slice(0, 8) }}; no newer revision has been loaded.</p>
            <p v-if="notice" role="status">{{ notice }}</p>
            <p v-if="error" role="alert">{{ error }}</p><UiButton v-if="error" :disabled="pending" @click="retry">Retry source loading</UiButton>
          </div>
          <div v-if="!pending && (source?.state === 'fallback-required' || source?.state === 'base-selection-required')" class="source-notice">
            <template v-if="source.state === 'fallback-required'"><h2>GitHub metadata unavailable</h2><p>The actual target branch is unknown. Retry metadata or explicitly choose Git-only mode to fetch upstream branch candidates. No default base is assumed.</p><UiButton @click="load({ action: 'git-only' })">Use Git-only fallback</UiButton><UiButton variant="quiet" @click="diff ? refresh() : load()">Retry GitHub metadata</UiButton></template>
            <form v-else @submit.prevent="selectBase">
              <h2>Choose the comparison base</h2>
              <p>These branches were fetched from the intended upstream repository. GitHub has not verified this PR's target. Select one explicitly.</p>
              <label for="source-base">Fetched base branch</label>
              <select id="source-base" v-model="branch" required>
                <option value="" disabled>Choose a branch…</option>
                <template v-for="candidate in source.candidates" :key="candidate.branch"><option :value="candidate.branch">{{ candidate.branch }} · {{ candidate.sha.slice(0, 8) }}</option></template>
              </select>
              <UiButton type="submit" :disabled="!branch">Load selected base</UiButton>
              <UiButton variant="quiet" @click="load({ action: 'git-only' })">Fetch branch candidates again</UiButton>
            </form>
          </div>
          <template v-if="diff">
            <div class="revision-bar"><p>{{ files.length }} changed files · +{{ counts.added }} / −{{ counts.removed }} text lines</p><details><summary>Pinned revision {{ diff.revisionID.slice(0, 8) }}</summary><dl><dt>Base branch / tip</dt><dd>{{ diff.revision.baseBranch }} / {{ diff.revision.baseSHA }}</dd><dt>Merge base</dt><dd>{{ diff.revision.mergeBaseSHA }}</dd><dt>Head</dt><dd>{{ diff.revision.headSHA }}</dd><dt>Base selection</dt><dd>{{ diff.revision.baseSource === 'github' ? 'GitHub metadata' : 'Explicit Git-only selection; target unverified by GitHub' }}</dd><dt>Revision</dt><dd>{{ diff.revisionID }}</dd></dl></details></div>
            <template v-if="active">
              <header class="file-header"><h2 ref="sourceHeading" tabindex="-1">{{ active.newPath ?? active.oldPath }}</h2><div class="file-tools"><span>{{ active.status }} · {{ active.kind }} · +{{ currentCounts.added }} / −{{ currentCounts.removed }}</span><span>Unified</span><UiButton variant="quiet" disabled title="Split diff is not implemented">Split — unavailable</UiButton></div><p v-if="active.oldPath && active.newPath && active.oldPath !== active.newPath">Renamed from {{ active.oldPath }}</p><p v-if="active.oldMode !== active.newMode">Mode {{ active.oldMode }} → {{ active.newMode }}</p></header>
              <SourceQuestions v-if="active.kind === 'text' && client" :key="diff.revisionID" :diff="diff" :file="active" :lines="lines" :client="client" :source-pending="pending" :markers="activeMarkers" :busy-reason="revisionBusy" @guard="guardDraft" @sending="initialSending = $event" @admitted="admitted" @open="openAnswer" />
              <div v-else class="source-notice" role="status"><h3>{{ active.kind }} change</h3><p>{{ active.reason ?? 'This change has no unified text lines. No source lines are invented.' }}</p><p>Line questions are unavailable for non-text changes.</p><p v-if="active.kind === 'submodule'">Submodules are not initialized.</p><p>Old object: {{ active.oldObject }}</p><p>New object: {{ active.newObject }}</p></div>
            </template>
            <div v-else class="source-notice" role="status"><h2>No changes in this comparison</h2><p>The pinned merge base and head have an empty diff.</p></div>
          </template>
          <div v-else class="source-notice">
            <template v-if="pending"><h2>Loading pinned source…</h2><p role="status">Resolving metadata, fetched commits, source and a complete diff. Loading source does not start inference.</p><div class="source-skeleton" aria-hidden="true"><span v-for="n in 6" :key="n" /></div><UiButton variant="quiet" @click="cancelSource">Cancel source loading</UiButton></template>
            <template v-else>
              <p v-if="error" role="alert">{{ error }}</p><UiButton v-if="error" @click="retry">Retry source loading</UiButton>
              <template v-if="!error && !source"><h2>PR registered. Source not loaded.</h2><p>Registration is saved independently. Load a pinned merge-base → head comparison using your existing GitHub CLI/Git authentication.</p><UiButton :disabled="!client" @click="load()">Load pinned source</UiButton></template>
              <p>{{ pr.url }}</p><p>Open Questions to read saved history without loading source. Previously accepted questions may still be running; reconnecting does not cancel them.</p>
            </template>
          </div>
        </section>
      </template>
    </WorkspaceShell>
    <main v-else id="review-content" class="review-message" tabindex="-1" :aria-busy="reviewState.status === 'loading'">
      <template v-if="reviewState.status === 'not-found'"><h1>PR not found</h1><p role="status">This PR is not registered in this Sheldon instance. Reopen its GitHub URL with sheldon open.</p></template>
      <template v-else><h1>PR workspace</h1><p v-if="reviewState.status === 'loading'" role="status">Reading registered PR…</p><p v-else-if="reviewState.status === 'failed'" role="status">Cannot read this PR. Check the backend and private prs.json index, then reconnect.</p><p v-else role="status">Connect the local runtime to read this registered PR. Reloading requires explicit reconnection.</p></template>
      <p>Workspace ID: {{ reviewId }}</p><a href="/">Open a PR</a>
    </main>
  </div>
</template>

<style scoped>
.registered-pr { container: application / inline-size; display: flex; flex-direction: column; height: 100dvh; min-width: 0; overflow: auto; }
.review-header { display: flex; align-items: center; flex-wrap: wrap; flex-shrink: 0; gap: var(--space-3); min-height: 3rem; padding-inline: var(--space-6); border-bottom: 1px solid var(--color-border); }
.review-header > span { flex: 1; color: var(--color-muted); }
.review-header > a { display: inline-flex; align-items: center; min-height: var(--target-control); }
.review-header > a:first-child { font-size: 1.375rem; }
.pr-title { grid-column: 1; font-size: var(--text-heading); font-weight: 550; line-height: 1.3; overflow-wrap: anywhere; }
.fixture-meta { grid-column: 1; overflow-wrap: anywhere; color: var(--color-muted); }
.github-link { grid-column: 2; grid-row: 1 / 3; align-self: center; }
.real-tabs { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.real-tabs .ui-button { min-height: var(--target-control); font-size: var(--text-small); }
.real-tabs [aria-current] { border-bottom: 2px solid var(--color-accent); border-radius: 0; }
.source-pane { display: flex; flex: 1; flex-direction: column; min-width: 0; min-height: 0; overflow: auto; border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.revision-bar, .file-header { flex-shrink: 0; padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--color-border); overflow-wrap: anywhere; }
.revision-bar { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); font-size: var(--text-small); }
.revision-bar details { max-width: 100%; }
.revision-bar summary { min-height: var(--target-control); cursor: pointer; }
.revision-bar dl { font-family: var(--font-code); font-size: var(--text-code-meta); padding-block: var(--space-3); }
.revision-bar dt { color: var(--color-muted); margin-top: var(--space-2); }
.file-header h2 { font-family: var(--font-code); font-size: var(--text-body); white-space: pre-wrap; }
.file-tools { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); margin-top: var(--space-2); font-size: var(--text-small); }
.file-tools > span:first-child { flex: 1; }
.source-notice, .review-message { padding: var(--space-6); overflow-wrap: anywhere; }
.source-notice > * + *, .source-notice form > * + *, .review-message > * + * { margin-top: var(--space-4); }
.source-notice select { display: block; width: min(100%, 35rem); min-height: var(--target-control); color: var(--color-text); background: var(--color-canvas); border: 1px solid var(--color-border-strong); padding: var(--space-2); }
.review-message { overflow: auto; }
.source-skeleton { display: grid; gap: var(--space-5); max-width: 45rem; padding-block: var(--space-4); }
.source-skeleton span { height: 0.75rem; background: var(--color-surface); border-radius: var(--radius-control); }
.source-skeleton span:nth-child(even) { width: 65%; }
.real-source { flex-shrink: 0; }
.refresh-controls { flex-shrink: 0; padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--color-border); font-size: var(--text-small); overflow-wrap: anywhere; }
.refresh-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.refresh-controls p { margin-top: var(--space-2); }
.stale-notice { color: var(--color-warning); }
@container application (max-width: 56.25rem) {
  :deep(.workspace-container) { flex: 0 0 auto; }
  :deep(.workspace) { height: auto; grid-template-rows: auto auto auto; }
  :deep(.workspace-panes) { overflow: visible; }
}
@container application (max-width: 45rem) { .review-header { padding-inline: var(--space-3); } .review-header > span { display: none; } .github-link { grid-column: 1; grid-row: 3; } }
</style>
