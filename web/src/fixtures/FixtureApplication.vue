<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import ApplicationHeader from '../components/ApplicationHeader.vue'
import UiButton from '../components/ui/UiButton.vue'
import SampleWorkspace from './SampleWorkspace.vue'
import monoLicense from '../assets/fonts/OFL.txt?url'
import sansLicense from '../assets/fonts/DM-Sans-LICENSE.txt?url'

const workspaces = ref([
  { id: 'github.com/acme/platform/248', repository: 'platform', number: 248, title: 'Session refresh', open: true },
  { id: 'github.com/acme/platform/251', repository: 'platform', number: 251, title: 'Retry handling', open: true },
])
const requested = new URLSearchParams(location.search).get('preview')
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
const openWorkspaces = computed((): typeof workspaces.value =>
  workspaces.value.filter((workspace): boolean => workspace.open),
)

/**
 * Switches local preview destinations without remounting workspace drafts.
 * @param id Destination identifier.
 * @param event Link activation event.
 */
function navigate(id: string, event: MouseEvent): void {
  if (event.button !== 0 || event.ctrlKey || event.metaKey || event.shiftKey || event.altKey) return
  event.preventDefault()
  current.value = id
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
    return
  }
  const number = 252 + workspaces.value.length - 2
  const workspace = { id: `github.com/acme/platform/${number}`, repository: 'platform', number, title: 'Extra sample workspace', open: true }
  workspaces.value.push(workspace)
  current.value = workspace.id
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
      :workspaces="openWorkspaces"
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
      <SampleWorkspace
        v-for="workspace in workspaces"
        v-show="current === workspace.id"
        :key="workspace.id"
        :workspace-number="workspace.number"
      />
      <main v-show="current === 'library'" id="preview-content" class="destination-placeholder" tabindex="-1">
        <h1>Pull requests</h1>
        <p>Local navigation preview only. The saved-PR library and real PR loading are not implemented.</p>
        <div class="placeholder-actions">
          <UiButton @click="openSample('github.com/acme/platform/248')">Open sample platform #248</UiButton>
          <UiButton @click="openSample('github.com/acme/platform/251')">Open sample platform #251</UiButton>
          <UiButton @click="openSample()">Open another sample workspace</UiButton>
        </div>
      </main>
      <main v-show="current === 'config'" class="destination-placeholder" tabindex="-1">
        <h1>Configuration</h1>
        <p>Personal configuration preview. Model discovery and saved preferences are not implemented.</p>
        <p>Existing PR-revision sessions keep their model; this navigation preview changes no settings.</p>
      </main>
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
.preview-details { padding: var(--space-4) var(--space-6); border-bottom: 1px solid var(--color-border); background: var(--color-panel); }
.preview-details p + p { margin-top: var(--space-2); }
.preview-links, .placeholder-actions { display: flex; flex-wrap: wrap; gap: var(--space-4); margin-top: var(--space-3); }
.preview-links a { display: inline-flex; align-items: center; min-height: var(--target-control); }
.destination-placeholder { flex: 1; min-width: 0; overflow: auto; padding: var(--space-6); }
.destination-placeholder h1 { font-size: 1.5rem; font-weight: 500; }
.destination-placeholder p { margin-top: var(--space-3); color: var(--color-muted); }
.preview-trigger { white-space: nowrap; font-size: var(--text-small); }
</style>
