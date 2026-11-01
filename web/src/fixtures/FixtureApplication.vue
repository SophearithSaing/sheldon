<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import ApplicationHeader from '../components/ApplicationHeader.vue'
import UiButton from '../components/ui/UiButton.vue'
import SampleWorkspace from './SampleWorkspace.vue'
import PullRequestLibrary from './PullRequestLibrary.vue'
import SourceStatusPreview from './SourceStatusPreview.vue'
import HistoricalWorkspace from './HistoricalWorkspace.vue'
import ConfigurationPreview from './ConfigurationPreview.vue'
import { samplePullRequests, sampleWorkspaceEntry, type QuestionSummary, type SourcePreviewState } from './library'
import monoLicense from '../assets/fonts/OFL.txt?url'
import sansLicense from '../assets/fonts/DM-Sans-LICENSE.txt?url'

const requested = new URLSearchParams(location.search).get('preview')
const workspaces = ref([sampleWorkspaceEntry(samplePullRequests[0]!, 8), sampleWorkspaceEntry(samplePullRequests[1]!, 7)])
const requestedPR = samplePullRequests.find((pr): boolean => pr.id === requested)
if (requestedPR && !workspaces.value.some((item): boolean => item.id === requestedPR.id)) workspaces.value.push(sampleWorkspaceEntry(requestedPR, 9))
const summaries = ref<Record<string, QuestionSummary>>({})
let visit = 10
const current = ref(
  requested === 'library' || requested === 'config' ||
  workspaces.value.some((workspace): boolean => workspace.id === requested)
    ? requested!
    : 'github.com/acme/platform/248',
)
const previewOpen = ref(false)
const announcement = ref('')
const header = ref<InstanceType<typeof ApplicationHeader>>()
const application = ref<HTMLElement>()
const configuration = ref<InstanceType<typeof ConfigurationPreview>>()
const configurationOrigin = ref(current.value === 'config' ? 'library' : current.value)
const pendingNavigation = ref<string | null>(null)
const originLabel = computed(configurationOriginLabel)
const openWorkspaces = computed((): typeof workspaces.value =>
  workspaces.value.filter((workspace): boolean => workspace.open),
)
const headerWorkspaces = computed(headerEntries)

/**
 * Keeps compact tab titles while exposing the complete canonical PR identity and title.
 * @returns Global-header entries for currently open page-memory workspaces.
 */
function headerEntries(): { id: string; repository: string; number: number; title: string; fullTitle: string }[] {
  return openWorkspaces.value.map((workspace): { id: string; repository: string; number: number; title: string; fullTitle: string } => ({ id: workspace.id, repository: workspace.repository, number: workspace.number, title: workspace.tabTitle, fullTitle: workspace.title }))
}

/**
 * Switches local preview destinations without remounting workspace drafts.
 * @param id Destination identifier.
 * @param event Link activation event.
 */
function navigate(id: string, event: MouseEvent): void {
  if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  if (current.value === 'config' && id !== 'config' && configuration.value?.dirty) {
    pendingNavigation.value = id
    configuration.value.confirmLeave()
    return
  }
  applyDestination(id)
  void focusCurrentWorkspace()
}

/**
 * Changes destinations without implicitly saving settings or replacing retained PR state.
 * @param id Local destination to show.
 */
function applyDestination(id: string): void {
  if (id === 'config' && current.value !== 'config') configurationOrigin.value = current.value
  current.value = id
  const workspace = workspaces.value.find((item): boolean => item.id === id)
  if (workspace) {
    workspace.open = true
    workspace.visited = ++visit
  }
}

/**
 * Labels the actual settings origin even when its workspace tab has been closed.
 * @returns Recoverable origin description.
 */
function configurationOriginLabel(): string {
  if (configurationOrigin.value === 'library') return 'All PRs'
  const workspace = workspaces.value.find((item): boolean => item.id === configurationOrigin.value)
  return workspace ? `${workspace.repository} #${workspace.number}` : 'All PRs'
}

/**
 * Honors a pending departure only after explicit settings discard.
 */
function leaveConfiguration(): void {
  if (!pendingNavigation.value) return
  applyDestination(pendingNavigation.value)
  pendingNavigation.value = null
  void focusCurrentWorkspace()
}

/**
 * Hides a local workspace tab while retaining its page-memory state.
 * @param id Workspace identifier to close.
 */
async function closeWorkspace(id: string): Promise<void> {
  const index = openWorkspaces.value.findIndex((workspace): boolean => workspace.id === id)
  const workspace = workspaces.value.find((item): boolean => item.id === id)
  if (!workspace) return
  workspace.open = false
  const neighbor = openWorkspaces.value[index] ?? openWorkspaces.value[index - 1]
  if (current.value === id) current.value = neighbor?.id ?? 'library'
  announcement.value = 'Sample tab closed. Drafts remain in page memory only; nothing was saved or closed on GitHub.'
  await nextTick()
  header.value?.focusDestination(current.value)
}

/**
 * Reopens an existing sample workspace or adds another local overflow specimen.
 * @param id Existing sample workspace identifier, when reopening.
 */
function openSample(id?: string): void {
  const existing = workspaces.value.find((workspace): boolean => workspace.id === id)
  if (existing) {
    existing.open = true
    current.value = existing.id
    existing.visited = ++visit
    void focusCurrentWorkspace()
    return
  }
  const pr = samplePullRequests.find((item): boolean => item.id === id)
  if (pr) {
    const workspace = sampleWorkspaceEntry(pr, ++visit)
    workspaces.value.push(workspace)
    current.value = workspace.id
    void focusCurrentWorkspace()
    return
  }
  const number = 252 + workspaces.value.length - 2
  const workspace = sampleWorkspaceEntry({ id: `github.com/acme/platform/${number}`, ownerRepository: 'acme/platform', repository: 'platform', number, title: 'Extra sample workspace', tabTitle: 'Extra sample workspace', status: 'Open', author: 'Sample author', updated: 'Not loaded', updateOrder: 0, createdOrder: 0 }, ++visit)
  workspaces.value.push(workspace)
  current.value = workspace.id
  void focusCurrentWorkspace()
}

/**
 * Moves focus to a workspace opened from an action that has just become hidden.
 */
async function focusCurrentWorkspace(): Promise<void> {
  await nextTick()
  header.value?.focusDestination(current.value)
}

/**
 * Opens a canonical library destination while preserving native modified-link behavior.
 * @param id Canonical sample identity to open or resume.
 * @param event Link activation event.
 */
function openLibraryPR(id: string, event: MouseEvent): void {
  if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  openSample(id)
}

/**
 * Selects an explicit source-only simulation without overwriting retained workspaces.
 * @param id Canonical sample PR identity.
 * @param state Selected loading/error/cache preview.
 */
function previewSource(id: string, state: SourcePreviewState): void {
  openSample(id)
  const workspace = workspaces.value.find((item): boolean => item.id === id)
  if (workspace) workspace.sourceState = state === 'stale' && !hasSnapshot(id) ? 'unavailable' : state
}

/**
 * Checks for an application-owned sample snapshot, not a real Git cache.
 * @param id Canonical sample PR identity.
 * @returns Whether local preview content is available.
 */
function hasSnapshot(id: string): boolean {
  return id === samplePullRequests[0]!.id || id === samplePullRequests[1]!.id || !!workspaces.value.find((item): boolean => item.id === id)?.snapshot
}

/**
 * Restores retained local content and focus without claiming a successful source refresh.
 * @param id Canonical sample workspace identity.
 */
function restoreSnapshot(id: string): void {
  const workspace = workspaces.value.find((item): boolean => item.id === id)
  if (!workspace || !hasSnapshot(id)) return
  workspace.sourceState = 'ready'
  void focusCurrentWorkspace()
}

/**
 * Returns to the library without deleting or saving page-memory state.
 */
async function showLibrary(): Promise<void> {
  current.value = 'library'
  await nextTick()
  header.value?.focusDestination('library')
}

/**
 * Focuses the currently visible content landmark from the global skip link.
 * @param event Skip-link activation event.
 */
function focusContent(event: MouseEvent): void {
  event.preventDefault()
  for (const main of application.value?.querySelectorAll<HTMLElement>('main') ?? []) {
    if (main.getClientRects().length) {
      main.focus()
      return
    }
  }
}
</script>

<template>
  <div ref="application" class="fixture-application">
    <a class="skip-link" href="#preview-content" @click="focusContent">Skip to content</a>
    <ApplicationHeader
      ref="header"
      :current="current"
      :workspaces="headerWorkspaces"
      @navigate="navigate"
      @close="closeWorkspace"
    >
      <template #tools>
        <UiButton
          class="preview-trigger"
          variant="quiet"
          :aria-expanded="previewOpen"
          aria-controls="preview-details"
          @click="previewOpen = !previewOpen"
        >UI preview</UiButton>
        <slot name="runtime-trigger" />
      </template>
    </ApplicationHeader>
    <section v-show="previewOpen" id="preview-details" class="preview-details" aria-label="Preview details">
      <p>No PR loaded. No agent connected. All source and conversation content is sample data.</p>
      <p>Navigation and drafts stay in this page only. Reloading discards them; closing a sample tab does not save data or close a real PR.</p>
      <div class="preview-links">
        <a :href="monoLicense" target="_blank" rel="noopener noreferrer">JetBrains Mono license <span class="sr-only">(opens in a new tab)</span></a>
        <a :href="sansLicense" target="_blank" rel="noopener noreferrer">DM Sans license <span class="sr-only">(opens in a new tab)</span></a>
      </div>
    </section>
    <slot name="runtime-panel" />
    <p class="sr-only" role="status">{{ announcement }}</p>
    <div class="fixture-destinations">
      <div
        v-for="workspace in workspaces"
        v-show="current === workspace.id"
        :key="workspace.id"
        class="sample-workspace-host"
      >
        <section v-show="workspace.sourceState === 'stale'" class="source-cache-notice" role="status"><strong>Cached-refresh failure · sample preview</strong><p>Retained sample snapshot only. No source refresh ran; source status, revision, history and drafts are unchanged. No agent is running.</p><UiButton disabled>Retry source refresh — unavailable</UiButton><UiButton @click="restoreSnapshot(workspace.id)">Dismiss cache preview notice</UiButton></section>
        <SampleWorkspace v-if="workspace.id === samplePullRequests[0]!.id || workspace.id === samplePullRequests[1]!.id" v-show="workspace.sourceState === 'ready' || workspace.sourceState === 'stale'" :workspace-number="workspace.number" @summary="(value) => summaries[workspace.id] = value" />
        <HistoricalWorkspace v-else-if="workspace.snapshot" v-show="workspace.sourceState === 'ready' || workspace.sourceState === 'stale'" :pr="workspace" @summary="(value) => summaries[workspace.id] = value" />
        <SourceStatusPreview v-show="workspace.sourceState !== 'ready' && workspace.sourceState !== 'stale'" :pr="workspace" :state="workspace.sourceState" :has-cache="hasSnapshot(workspace.id)" @library="showLibrary" @restore="restoreSnapshot(workspace.id)" />
      </div>
      <PullRequestLibrary v-show="current === 'library'" :workspaces="workspaces" :summaries="summaries" @open="openLibraryPR" @source="previewSource" @extra="openSample()" />
      <ConfigurationPreview v-show="current === 'config'" ref="configuration" :origin-id="configurationOrigin" :origin-label="originLabel" @navigate="navigate" @leave="leaveConfiguration" @cancel-leave="pendingNavigation = null" />
    </div>
  </div>
</template>

<style scoped>
.fixture-application {
  container: application / inline-size;
  display: flex;
  flex-direction: column;
  height: 100dvh;
  min-width: 0;
}
.fixture-destinations { display: flex; flex: 1; min-height: 0; min-width: 0; }
.sample-workspace-host { display: flex; flex: 1; flex-direction: column; min-height: 0; min-width: 0; }
.source-cache-notice { padding: var(--space-3) var(--space-6); border-bottom: 1px solid var(--color-warning); color: var(--color-muted); font-size: var(--text-small); }
.source-cache-notice .ui-button { margin-top: var(--space-2); margin-right: var(--space-2); }
.preview-details { padding: var(--space-4) var(--space-6); border-bottom: 1px solid var(--color-border); background: var(--color-panel); }
.preview-details p + p { margin-top: var(--space-2); }
.preview-links { display: flex; flex-wrap: wrap; gap: var(--space-4); margin-top: var(--space-3); }
.preview-links a { display: inline-flex; align-items: center; min-height: var(--target-control); }
.preview-trigger { white-space: nowrap; font-size: var(--text-small); }
</style>
