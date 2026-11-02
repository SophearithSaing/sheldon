<script setup lang="ts">
import { computed, nextTick, ref, useId } from 'vue'
import ChangedFileTree from '../components/ChangedFileTree.vue'
import InlineQuestionComposer from '../components/InlineQuestionComposer.vue'
import SourceDiff from '../components/SourceDiff.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiBadge from '../components/ui/UiBadge.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import { semanticGroups, semanticHunks, hunksFor, ungrouped, type SemanticGroup } from './semantic'
import { makeSampleThread, sampleFiles, sampleSourceLabel, type SampleAnchor, type SampleThread } from './workspace'

interface GroupDraft {
  anchors: SampleAnchor[]
  text: string
  error: string
  edition: number
}
const props = defineProps<{ threads: SampleThread[]; counts: Record<string, number>; viewed: Record<string, boolean>; file: string; busy: boolean }>()
const emit = defineEmits<{ file: [id: string]; source: [anchor: SampleAnchor]; files: []; create: [thread: SampleThread]; open: [id: number, trigger: HTMLElement] }>()
const id = useId()
const root = ref<HTMLElement>()
const sourceRegion = ref<InstanceType<typeof ScrollRegion>>()
const groupHeading = ref<HTMLElement>()
const confirmation = ref<HTMLDialogElement>()
const hasReview = ref(new URLSearchParams(location.search).get('sample') === 'semantic')
const edition = ref(hasReview.value ? 1 : 0)
const job = ref<'idle' | 'generating' | 'error' | 'cancelled'>('idle')
const outcome = ref('success')
const outdated = ref(false)
const selected = ref('logout')
const treeOpen = ref(!hasReview.value)
const search = ref('')
const expanded = ref<Record<string, boolean>>({})
const drafts = ref<Record<string, GroupDraft>>({})
const composerGroup = ref<string | null>(null)
const pending = ref(false)
const creating = ref(false)
const creationOutcome = ref('success')
const markers = ref<Record<string, number[]>>({})
const announcement = ref('')
const current = computed(currentGroup)
const hunks = computed(currentHunks)
const draft = computed(currentDraft)
const groupedCount = new Set(semanticGroups.flatMap((group): string[] => group.hunks)).size
const generationOptions = [{ value: 'success', label: 'Complete sample grouping' }, { value: 'error', label: 'Sample generation failure' }]
const creationOptions = [{ value: 'success', label: 'Answered sample thread' }, { value: 'creation-error', label: 'Creation failure (no thread)' }, { value: 'response-error', label: 'Response failure (thread retained)' }, { value: 'pending', label: 'Pending creation (finish manually)' }]

/**
 * Resolves a selected sample group or the explicitly ungrouped bucket.
 * @returns Its fixed organizing hypothesis and hunk ids.
 */
function currentGroup(): SemanticGroup {
  return semanticGroups.find((group): boolean => group.id === selected.value) ?? ungrouped
}

/**
 * Keeps each source excerpt separate and in manifest order.
 * @returns Pinned hunks for the selected sample group.
 */
function currentHunks(): typeof semanticHunks {
  return hunksFor(current.value)
}

/**
 * Reads only the currently expanded group draft without retargeting retained drafts.
 * @returns Draft with its captured anchors, or undefined when closed.
 */
function currentDraft(): GroupDraft | undefined {
  return composerGroup.value === selected.value ? drafts.value[selected.value] : undefined
}

/**
 * Copies a pinned source anchor so later grouping state cannot rewrite submitted context.
 * @param anchor Source range to capture.
 * @returns Independent file/range/revision metadata.
 */
function copyAnchor(anchor: SampleAnchor): SampleAnchor {
  return { ...anchor, range: anchor.range ? { ...anchor.range } : null }
}

/**
 * Counts distinct files without counting expanded context as additional coverage.
 * @param group Manifest group to describe.
 * @returns Number of unique file ids in that group's hunks.
 */
function fileCount(group: SemanticGroup): number {
  return new Set(hunksFor(group).map((hunk): string => hunk.anchor.fileId)).size
}

/**
 * Starts only an explicit local generation simulation, never a job or completion timer.
 */
function beginGeneration(): void {
  if (job.value === 'generating' || pending.value || creating.value) return
  job.value = 'generating'
  announcement.value = 'Sample grouping pending. No agent is running. Finish or cancel this preview explicitly.'
}

/**
 * Requires confirmation before previewing a replacement for a complete sample grouping.
 */
function rebuild(): void {
  if (job.value === 'generating' || pending.value || creating.value) return
  confirmation.value?.showModal()
}

/**
 * Confirms a replacement simulation while retaining the complete grouping and all drafts.
 */
function confirmRebuild(): void {
  confirmation.value?.close()
  beginGeneration()
}

/**
 * Ends only the grouping simulation, leaving question sessions and previous source intact.
 */
function cancelGeneration(): void {
  if (job.value !== 'generating') return
  job.value = 'cancelled'
  announcement.value = 'Sample grouping cancelled. No process was stopped; source, drafts and any complete grouping remain.'
}

/**
 * Publishes the fixed complete sample only after explicit manual completion.
 */
async function finishGeneration(): Promise<void> {
  if (job.value !== 'generating') return
  if (outcome.value === 'error') {
    job.value = 'error'
    announcement.value = 'Sample generation failed. No agent ran; any complete grouping and local drafts remain.'
    return
  }
  const first = !hasReview.value
  hasReview.value = true
  edition.value += 1
  job.value = 'idle'
  announcement.value = 'Complete fictional grouping displayed. No generation or source validation ran.'
  if (first) {
    selected.value = semanticGroups[0]!.id
    treeOpen.value = false
    await nextTick()
    groupHeading.value?.focus()
  }
}

/**
 * Restores a group's source/draft state without marking files Viewed or losing other drafts.
 * @param groupId Group identifier, including the ungrouped bucket.
 */
async function selectGroup(groupId: string): Promise<void> {
  if (pending.value || creating.value) {
    announcement.value = 'Finish or cancel pending sample thread creation before switching groups.'
    return
  }
  selected.value = groupId
  composerGroup.value = null
  await nextTick()
  groupHeading.value?.focus()
}

/**
 * Opens a pinned multi-range draft in normal flow beneath the group summary.
 */
async function ask(): Promise<void> {
  if (pending.value || creating.value || !hasReview.value) return
  if (!drafts.value[selected.value]) {
    drafts.value[selected.value] = { anchors: hunks.value.map((hunk): SampleAnchor => copyAnchor(hunk.anchor)), text: 'How do these changes work together?', error: '', edition: edition.value }
  }
  composerGroup.value = selected.value
  await nextTick()
  root.value?.querySelector<HTMLTextAreaElement>('[data-draft-input]')?.focus()
}

/**
 * Recovers one retained group draft without silently merging its anchors into another group.
 * @param groupId Identifier of the draft to recover.
 */
async function recover(groupId: string): Promise<void> {
  if (pending.value || creating.value) return
  await selectGroup(groupId)
  await ask()
}

/**
 * Closes the composer while retaining its text, original ranges and grouping edition.
 */
async function cancelDraft(): Promise<void> {
  pending.value = false
  composerGroup.value = null
  announcement.value = 'Group draft retained in this page. Recover it from the navigation rail.'
  await nextTick()
  root.value?.querySelector<HTMLButtonElement>('[data-group-ask]')?.focus()
}

/**
 * Validates and previews local creation with no model dispatch or implicit session reset.
 */
async function submit(): Promise<void> {
  const currentDraft = draft.value
  if (!currentDraft || props.busy || outdated.value || pending.value || creating.value) return
  if (!currentDraft.text.trim()) {
    currentDraft.error = 'Enter a question before previewing thread creation.'
    await nextTick()
    root.value?.querySelector<HTMLTextAreaElement>('[data-draft-input]')?.focus()
    return
  }
  currentDraft.error = ''
  if (creationOutcome.value === 'creation-error') {
    currentDraft.error = 'Sample creation failed. No thread was created; the draft and all pinned ranges are retained.'
    return
  }
  if (creationOutcome.value === 'pending') pending.value = true
  else await completeCreation()
}

/**
 * Creates exactly one UI thread with explicit multi-range context in the shared revision session.
 */
async function completeCreation(): Promise<void> {
  const currentDraft = draft.value
  if (!currentDraft || props.busy || outdated.value || creating.value) return
  creating.value = true
  pending.value = false
  const anchors = currentDraft.anchors.map(copyAnchor)
  const thread = makeSampleThread(Math.max(0, ...props.threads.map((item): number => item.id)) + 1, anchors[0]!, currentDraft.text.trim(), creationOutcome.value === 'response-error' ? 'failed' : 'answered')
  thread.relatedAnchors = anchors.slice(1)
  const reply = thread.messages.find((message): boolean => message.author === 'Agent')
  if (reply) reply.citations = anchors.map(copyAnchor)
  const groupId = selected.value
  const scroll = sourceRegion.value?.$el as HTMLElement | undefined
  const position = scroll?.scrollTop ?? 0
  emit('create', thread)
  markers.value[groupId] = [...(markers.value[groupId] ?? []), thread.id]
  delete drafts.value[groupId]
  composerGroup.value = null
  await nextTick()
  const marker = root.value?.querySelector<HTMLButtonElement>(`[data-semantic-thread="${thread.id}"]`)
  if (marker) emit('open', thread.id, marker)
  await nextTick()
  if (scroll) scroll.scrollTop = position
  creating.value = false
}

/**
 * Reopens a submitted UI thread rather than creating another native history.
 * @param threadId Existing sample thread identifier.
 * @param event Marker activation for focus restoration.
 */
function openThread(threadId: number, event: MouseEvent): void {
  emit('open', threadId, event.currentTarget as HTMLElement)
}
</script>

<template>
  <div ref="root" class="semantic-preview">
    <ScrollRegion label="Semantic review navigation" class="semantic-navigation">
      <details :open="treeOpen" class="semantic-files" @toggle="treeOpen = ($event.target as HTMLDetailsElement).open"><summary>Changed files · 5</summary><ChangedFileTree v-model="search" :files="sampleFiles" :current="file" :counts="counts" :viewed="viewed" @select="emit('file', $event)" /></details>
      <template v-if="hasReview"><h2>Review groups <span>3</span></h2><nav aria-label="Groups by intent"><UiButton v-for="(group, index) in semanticGroups" :key="group.id" :aria-label="`Group ${String(index + 1).padStart(2, '0')}: ${group.title}`" :aria-current="selected === group.id ? 'true' : undefined" @click="selectGroup(group.id)"><span>{{ String(index + 1).padStart(2, '0') }}</span><span><strong>{{ group.title }}</strong><small>{{ group.hunks.length }} hunks · {{ fileCount(group) }} files</small></span></UiButton><UiButton :aria-current="selected === 'ungrouped' ? 'true' : undefined" @click="selectGroup('ungrouped')">Ungrouped changes · 1 hunk · sessions.md</UiButton></nav><p class="semantic-coverage">{{ groupedCount }} of {{ semanticHunks.length }} changed hunks grouped</p><p>Fictional sample inventory, not verified source coverage or a review score. Expanded context does not increase this count.</p></template>
      <details v-if="Object.keys(drafts).length" class="group-recovery"><summary>Recover group drafts ({{ Object.keys(drafts).length }})</summary><UiButton v-for="(_item, groupId) in drafts" :key="groupId" @click="recover(String(groupId))">Recover {{ semanticGroups.find((group) => group.id === groupId)?.title ?? 'Ungrouped changes' }}</UiButton></details>
      <p class="semantic-rail-note">All five files remain available without grouping. Semantic-generation context is separate from the shared PR-revision question session. No agent is connected.</p>
    </ScrollRegion>
    <ScrollRegion ref="sourceRegion" label="Semantic review source" class="semantic-source">
      <header class="semantic-heading"><div><h2>Semantic review</h2><p>Local UI preview · Pinned to a81c2f4</p></div><UiBadge v-if="!hasReview">{{ job === 'generating' ? 'Generating preview' : job === 'error' ? 'Generation failed' : job === 'cancelled' ? 'Cancelled preview' : 'Not started' }}</UiBadge><UiButton v-else :disabled="job === 'generating' || pending || creating" @click="rebuild">Rebuild groups · preview</UiButton></header>
      <section v-if="job === 'generating'" class="semantic-feedback" role="status"><h3>Organizing changed hunks · simulation</h3><p>No agent is running and no completion timer is scheduled. {{ hasReview ? 'The previous complete grouping and reading state stay available below.' : 'No groups have been published.' }} Question-session activity is unchanged.</p><UiButton @click="cancelGeneration">Cancel sample grouping</UiButton><UiButton @click="finishGeneration">Finish sample grouping</UiButton></section>
      <section v-if="job === 'error' || job === 'cancelled'" class="semantic-feedback" :role="job === 'error' ? 'alert' : 'status'"><h3>{{ job === 'error' ? 'Sample generation failed' : 'Sample grouping cancelled' }}</h3><p>No agent ran or stopped. Source, drafts, histories and any previously complete grouping remain intact.</p><UiButton :disabled="pending || creating" @click="beginGeneration">Preview generation again</UiButton></section>
      <section v-if="outdated && hasReview" class="semantic-feedback" role="status"><h3>Outdated grouping · preview</h3><p>Reviewing the retained a81c2f4 snapshot. No source revision actually changed. Existing drafts and threads keep their original ranges; creating from this preview is unavailable.</p><UiButton disabled>Rebuild for latest — unavailable</UiButton><p>No newer source snapshot or live generation is implemented.</p></section>
      <template v-if="!hasReview"><article class="semantic-intro"><span aria-hidden="true">≋</span><p>OPTIONAL · FICTIONAL GROUPING PREVIEW</p><h3>Review the intent.<br />Not just the file order.</h3><p>A semantic pass would group related changed hunks across files. This preview shows a fixed example, not an analysis of a loaded repository.</p><UiButton variant="primary" :disabled="job === 'generating'" @click="beginGeneration">Preview semantic generation</UiButton><p>Runs no agent, fetches no source and saves nothing. Entering this tab never starts work.</p><div class="semantic-steps"><div><strong>01 · Group related hunks</strong><p>Follow behavior across source and tests.</p></div><div><strong>02 · Keep source close</strong><p>Expand adjacent context or use the ordinary diff.</p></div><div><strong>03 · Leave nothing hidden</strong><p>Ungrouped changes stay independently reachable.</p></div></div></article></template>
      <template v-else>
        <section class="semantic-summary"><p>{{ selected === 'ungrouped' ? 'UNGROUPED · 1 HUNK' : `GROUP ${String(semanticGroups.indexOf(current) + 1).padStart(2, '0')} / 03` }} · Sample grouping {{ edition }}</p><h3 ref="groupHeading" tabindex="-1">{{ current.title }}</h3><p>{{ current.rationale }}</p><p>Organizing hypothesis only, not verified safety or comprehension. {{ hunks.length }} separate hunks across {{ fileCount(current) }} files.</p></section>
        <div class="semantic-toolbar"><p>Source order is preserved within each excerpt. Noncontiguous ranges are never joined.</p><UiButton data-group-ask variant="primary" :disabled="pending || creating" @click="ask">{{ selected === 'ungrouped' ? 'Ask about this hunk' : 'Ask about this group' }}</UiButton></div>
        <section v-if="draft" class="semantic-draft" aria-label="Pinned group question draft"><p>{{ draft.anchors.length }} pinned ranges · Sample grouping {{ draft.edition }}{{ draft.edition !== edition ? ' · Retained from an earlier grouping' : '' }}</p><ul><li v-for="(anchor, index) in draft.anchors" :key="index">{{ sampleSourceLabel(anchor) }}</li></ul><InlineQuestionComposer v-model="draft.text" heading="Ask about this group" :context="`${draft.anchors.length} pinned ranges · a81c2f4`" :error="draft.error" :pending="pending" :busy="busy" :unavailable-reason="outdated ? 'Outdated grouping preview. The draft stays pinned; creation is unavailable.' : undefined" @submit="submit" @finish="completeCreation" @cancel="cancelDraft" /></section>
        <UiButton v-for="threadId in markers[selected] ?? []" :key="threadId" class="semantic-question-marker" :data-semantic-thread="threadId" :aria-label="`Open sample question ${threadId}`" @click="openThread(threadId, $event)">Question {{ threadId }} · Open existing group thread →</UiButton>
        <article v-for="hunk in hunks" :key="hunk.id" class="semantic-hunk" :data-hunk-id="hunk.id" :aria-label="hunk.title"><header><div><h4>{{ sampleSourceLabel(hunk.anchor) }}</h4><p>{{ hunk.title }}</p></div><UiButton @click="emit('source', hunk.anchor)">View in diff</UiButton></header><p v-if="expanded[hunk.id]" class="expanded-note">Expanded adjacent sample context. The original grouped range remains {{ sampleSourceLabel(hunk.anchor) }}; this adds no coverage.</p><SourceDiff :lines="expanded[hunk.id] && hunk.expanded ? hunk.expanded : hunk.lines" :label="`Semantic excerpt: ${hunk.id}`" :selection="null" :selectable="false" :highlight="hunk.anchor.fileId !== 'docs'" /><footer><p>{{ hunk.caption }}</p><UiButton v-if="hunk.expanded" @click="expanded[hunk.id] = !expanded[hunk.id]">{{ expanded[hunk.id] ? 'Collapse context' : 'Expand context' }}</UiButton></footer></article>
        <div class="semantic-bottom"><p>Grouping never changes Viewed, Understood or quiz results. Only explicit sample thread creation changes question counts.</p><UiButton :disabled="pending || creating" @click="selectGroup(selected === 'logout' ? 'sharing' : selected === 'sharing' ? 'failure' : selected === 'failure' ? 'ungrouped' : 'logout')">{{ selected === 'failure' ? 'Next: ungrouped changes' : 'Next group' }}</UiButton></div>
      </template>
      <UiButton class="continue-files" @click="emit('files')">Continue in Files changed</UiButton>
      <details class="semantic-tools"><summary>Semantic preview controls</summary><p>Generation and creation below are explicitly manual local simulations, never native sessions, source validation or durable saving.</p><UiField v-model="outcome" label="Sample grouping outcome" type="select" :options="generationOptions" :disabled="job === 'generating'" /><label><input v-model="outdated" type="checkbox" :disabled="!hasReview || pending || creating" />Preview groups outdated</label><UiField v-model="creationOutcome" label="Group thread creation outcome" type="select" :options="creationOptions" :disabled="pending || creating" /></details>
      <p class="sr-only" role="status">{{ announcement }}</p>
    </ScrollRegion>
    <dialog ref="confirmation" class="semantic-confirmation" :aria-labelledby="`${id}-rebuild-title`"><h2 :id="`${id}-rebuild-title`">Preview rebuilding these groups?</h2><p>The existing complete grouping, selected group, expanded source, drafts and submitted thread anchors remain unchanged until explicit sample completion. The fixed example stays pinned to a81c2f4; no newer source is fetched.</p><UiButton autofocus @click="confirmation?.close()">Keep current groups</UiButton><UiButton @click="confirmRebuild">Confirm preview rebuild</UiButton></dialog>
  </div>
</template>

<style scoped>
.semantic-preview { display: grid; grid-template-columns: var(--width-file-rail) minmax(0, 1fr); gap: var(--space-6); flex: 1; min-height: 0; min-width: 0; }
.semantic-navigation { border-right: 1px solid var(--color-border); padding: var(--space-2) var(--space-3) var(--space-3) 0; font-size: var(--text-small); }
.semantic-files summary, .group-recovery summary { display: flex; align-items: center; min-height: var(--target-control); cursor: pointer; }
.semantic-navigation h2 { display: flex; justify-content: space-between; margin-top: var(--space-5); margin-bottom: var(--space-3); font-size: var(--text-small); font-weight: 550; }
.semantic-navigation nav { display: grid; gap: var(--space-2); }
.semantic-navigation nav .ui-button { justify-content: start; align-items: start; gap: var(--space-2); width: 100%; text-align: left; padding: var(--space-3); }
.semantic-navigation nav strong { display: block; font-size: var(--text-small); font-weight: 500; }
.semantic-navigation nav .ui-button > span:first-child { flex: 0 0 1.5rem; white-space: nowrap; }
.semantic-navigation nav .ui-button > span:last-child { min-width: 0; }
.semantic-navigation nav small { display: block; margin-top: var(--space-1); color: var(--color-muted); }
.semantic-navigation nav [aria-current] { border-color: var(--color-accent-border); background: var(--color-accent-soft); }
.semantic-navigation > p, .group-recovery { margin-top: var(--space-4); color: var(--color-muted); }
.semantic-coverage { border-top: 1px solid var(--color-border); padding-top: var(--space-4); }
.group-recovery .ui-button { display: flex; margin-top: var(--space-2); }
.semantic-source { padding: var(--space-2); }
.semantic-heading, .semantic-toolbar, .semantic-bottom { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); }
.semantic-heading h2 { font-size: var(--text-heading); font-weight: 500; }
.semantic-heading p { margin-top: var(--space-1); color: var(--color-muted); font-size: var(--text-small); }
.semantic-intro { margin-top: var(--space-6); padding: var(--space-8); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.semantic-intro > span { display: block; color: var(--color-accent); font-size: 2.5rem; }
.semantic-intro > p { max-width: 40rem; margin-block: var(--space-4); color: var(--color-muted); }
.semantic-intro h3 { font-size: 2rem; line-height: 1.3; font-weight: 500; }
.semantic-steps { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: var(--space-5); margin-top: var(--space-8); padding-top: var(--space-5); border-top: 1px solid var(--color-border); font-size: var(--text-small); }
.semantic-steps strong { font-weight: 500; }
.semantic-steps p { color: var(--color-muted); margin-top: var(--space-2); }
.semantic-summary { margin-top: var(--space-5); }
.semantic-summary h3 { margin-block: var(--space-3); font-size: 1.5rem; font-weight: 500; line-height: 1.4; }
.semantic-summary > p:first-child { font-size: var(--text-code-meta); color: var(--color-muted); }
.semantic-summary > p + p { margin-top: var(--space-2); }
.semantic-toolbar { margin-block: var(--space-4); padding-block: var(--space-3); border-block: 1px solid var(--color-border); font-size: var(--text-small); color: var(--color-muted); }
.semantic-toolbar > p { flex: 1 1 14rem; }
.semantic-hunk { overflow: hidden; margin-top: var(--space-4); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.semantic-hunk > header { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: center; gap: var(--space-2); padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--color-border); }
.semantic-hunk h4 { font: 500 var(--text-code-meta)/1.6 var(--font-code); overflow-wrap: anywhere; }
.semantic-hunk header p { margin-top: var(--space-1); font-size: var(--text-small); color: var(--color-muted); }
.semantic-hunk > footer, .semantic-bottom { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); padding: var(--space-3) var(--space-4); font-size: var(--text-small); color: var(--color-muted); }
.semantic-hunk footer p { flex: 1 1 15rem; }
.expanded-note { padding: var(--space-3) var(--space-4); border-bottom: 1px solid var(--color-border); color: var(--color-muted); font-size: var(--text-small); }
.semantic-bottom { margin-top: var(--space-4); padding-inline: 0; }
.semantic-bottom p { flex: 1 1 16rem; }
.semantic-draft { border: 1px solid var(--color-accent-border); border-radius: var(--radius-panel); padding: var(--space-3); margin-block: var(--space-4); }
.semantic-draft > p, .semantic-draft li { color: var(--color-muted); font-size: var(--text-small); overflow-wrap: anywhere; }
.semantic-draft ul { padding-left: var(--space-5); margin-top: var(--space-2); }
.semantic-question-marker { display: flex; justify-content: start; width: 100%; margin-block: var(--space-3); border-color: var(--color-accent-border); background: var(--color-accent-soft); }
.semantic-feedback { margin-top: var(--space-4); padding: var(--space-4); border: 1px solid var(--color-border-strong); border-radius: var(--radius-control); background: var(--color-panel); }
.semantic-feedback h3 { font-size: var(--text-body); font-weight: 550; }
.semantic-feedback p { margin-block: var(--space-3); font-size: var(--text-small); color: var(--color-muted); }
.semantic-feedback .ui-button + .ui-button { margin-left: var(--space-2); }
.semantic-tools { margin-top: var(--space-5); color: var(--color-muted); font-size: var(--text-small); }
.semantic-tools summary { min-height: var(--target-control); cursor: pointer; }
.semantic-tools .ui-field { margin-block: var(--space-3); }
.semantic-tools label { display: flex; align-items: center; gap: var(--space-2); min-height: var(--target-control); }
.continue-files { margin-top: var(--space-4); }
.semantic-confirmation { width: min(36rem, calc(100% - 2rem)); padding: var(--space-6); border: 1px solid var(--color-border-strong); border-radius: var(--radius-panel); background: var(--color-panel); color: var(--color-text); }
.semantic-confirmation::backdrop { background: #0009; }
.semantic-confirmation h2 { font-size: var(--text-heading); font-weight: 500; }
.semantic-confirmation p { margin-block: var(--space-4); }
.semantic-confirmation .ui-button + .ui-button { margin-left: var(--space-2); }
@container semantic-panel (max-width: 56.25rem) { .semantic-preview { display: flex; flex-direction: column; overflow: auto; } .semantic-navigation { flex: 0 0 auto; overflow: visible; border-right: 0; border-bottom: 1px solid var(--color-border); } .semantic-source { flex: 0 0 auto; overflow: visible; } .semantic-steps { grid-template-columns: minmax(0, 1fr); } }
</style>
