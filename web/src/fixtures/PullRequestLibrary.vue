<script setup lang="ts">
import { computed, ref } from 'vue'
import UiBadge from '../components/ui/UiBadge.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import { samplePullRequests, type QuestionSummary, type SamplePullRequest, type SampleWorkspaceEntry, type SourcePreviewState } from './library'

const props = defineProps<{ workspaces: SampleWorkspaceEntry[]; summaries: Record<string, QuestionSummary> }>()
const emit = defineEmits<{ open: [id: string, event: MouseEvent]; source: [id: string, state: SourcePreviewState]; extra: [] }>()
const search = ref('')
const repository = ref('all')
const status = ref('All')
const sort = ref('visited')
const scenario = ref('populated')
const sourcePR = ref(samplePullRequests[0]!.id)
const sourceState = ref<SourcePreviewState>('loading')
const dialog = ref<HTMLDialogElement>()
const url = ref('')
const urlError = ref('')
const urlNotice = ref('')
const rows = computed(filteredRows)
const statuses = ['All', 'Open', 'Merged', 'Closed'] as const
const repositories = [{ value: 'all', label: 'All repositories' }, { value: 'acme/platform', label: 'acme/platform' }, { value: 'acme/dashboard', label: 'acme/dashboard' }]
const sorts = [{ value: 'visited', label: 'Recently visited' }, { value: 'updated', label: 'Recently updated' }, { value: 'newest', label: 'Newest first' }]
const scenarios = [{ value: 'populated', label: 'Eight-row sample library' }, { value: 'empty', label: 'Empty library preview' }, { value: 'error', label: 'Library unavailable preview' }, { value: 'stale', label: 'Stale library preview' }]
const sourceOptions = [{ value: 'loading', label: 'Initial source loading' }, { value: 'offline', label: 'Offline error' }, { value: 'denied', label: 'Access denied error' }, { value: 'missing', label: 'Missing PR error' }, { value: 'unavailable', label: 'Incomplete/unavailable source error' }, { value: 'stale', label: 'Cached-refresh failure (where a sample snapshot exists)' }]

/**
 * Counts disjoint fictional source-status categories before search and repository filtering.
 * @param value Source status bucket.
 * @returns Sample library count, never a remote GitHub total.
 */
function count(value: string): number {
  if (scenario.value === 'empty') return 0
  return samplePullRequests.filter((pr): boolean => value === 'All' || pr.status === value).length
}

/**
 * Filters library records and sorts personal visits independently of source update order.
 * @returns Visible fictional PR records.
 */
function filteredRows(): SamplePullRequest[] {
  if (scenario.value === 'empty' || scenario.value === 'error') return []
  const query = search.value.trim().toLowerCase().replace(/^#/, '')
  return samplePullRequests.filter((pr): boolean =>
    (status.value === 'All' || pr.status === status.value) &&
    (repository.value === 'all' || pr.ownerRepository === repository.value) &&
    `${pr.title} ${pr.ownerRepository} ${pr.number}`.toLowerCase().includes(query),
  ).sort(compareRows)
}

/**
 * Compares selected sample sort keys without changing GitHub/source status.
 * @param left First sample record.
 * @param right Second sample record.
 * @returns Relative order with a stable source-update tiebreaker.
 */
function compareRows(left: SamplePullRequest, right: SamplePullRequest): number {
  if (sort.value === 'updated') return right.updateOrder - left.updateOrder
  if (sort.value === 'newest') return right.createdOrder - left.createdOrder
  return (props.workspaces.find((item): boolean => item.id === right.id)?.visited ?? 0) - (props.workspaces.find((item): boolean => item.id === left.id)?.visited ?? 0) || right.updateOrder - left.updateOrder
}

/**
 * Checks tab presence independently from the fictional PR source status.
 * @param id Canonical sample PR identity.
 * @returns Whether this record currently has a workspace tab.
 */
function inTabs(id: string): boolean {
  return props.workspaces.some((workspace): boolean => workspace.id === id && workspace.open)
}

/**
 * Reads the local question counts, including retained historical sample transcripts.
 * @param pr Selected fictional PR.
 * @returns Open and reader-understood sample counts.
 */
function summary(pr: SamplePullRequest): QuestionSummary {
  return props.summaries[pr.id] ?? { open: (pr.snapshot?.questions.length ?? 0) - (pr.snapshot?.understood ?? 0), understood: pr.snapshot?.understood ?? 0 }
}

/**
 * Clears only search and filter controls, never workspaces, drafts or histories.
 */
function clearFilters(): void {
  search.value = ''
  repository.value = 'all'
  status.value = 'All'
}

/**
 * Opens the unavailable import dialog without starting a request.
 */
function openDialog(): void {
  dialog.value?.showModal()
}

/**
 * Validates format locally while keeping real import disabled and input recoverable.
 */
function validateURL(): void {
  urlError.value = ''
  urlNotice.value = ''
  const match = /^https:\/\/github\.com\/([A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?)\/([A-Za-z0-9_.-]+)\/pull\/[1-9]\d*$/.exec(url.value.trim())
  if (!match || match[2] === '.' || match[2] === '..') {
    urlError.value = 'Use a canonical https://github.com/owner/repository/pull/123 URL without credentials, query or fragment.'
    return
  }
  urlNotice.value = 'URL format accepted locally. Import is unavailable; no PR, row or workspace was created.'
}
</script>

<template>
  <main id="preview-content" class="pr-library" tabindex="-1" aria-label="Your pull requests">
    <header class="library-heading"><div><p class="eyebrow">YOUR SAMPLE WORKSPACE</p><h1>Pull requests</h1><p>Pick up where you left off, or explore another sample.</p></div><UiButton variant="primary" @click="openDialog">Open a PR</UiButton></header>
    <p class="library-disclosure">Fictional library preview · Nothing is fetched or saved. Source status and question understanding are separate.</p>
    <div class="library-tools"><UiField v-model="search" label="Search pull requests by title, number or repository" type="search" placeholder="Search by title, #number, or repository…" /><UiField v-model="repository" label="Filter by repository" type="select" :options="repositories" /><UiField v-model="sort" label="Sort pull requests" type="select" :options="sorts" /></div>
    <div class="library-filterbar"><div role="group" aria-label="Filter by pull request status" class="library-filters"><UiButton v-for="value in statuses" :key="value" :aria-label="value" :aria-pressed="status === value" @click="status = value">{{ value }} <span>{{ count(value) }}</span></UiButton></div><p role="status">{{ scenario === 'error' ? 'Sample library unavailable' : `${rows.length} matching sample pull requests` }}</p></div>
    <section v-if="scenario === 'stale'" class="library-feedback" role="status"><h2>Stale library preview</h2><p>The last sample library remains visible. No refresh ran; source status and histories have not been changed.</p><UiButton disabled>Retry library refresh — unavailable</UiButton></section>
    <section v-if="scenario === 'error'" class="library-feedback" role="alert"><h2>Sample library unavailable</h2><p>This is a selected error preview, not an actual failed request. Existing workspace tabs, drafts and histories remain in page memory.</p><UiButton disabled>Retry library loading — unavailable</UiButton><UiButton @click="scenario = 'populated'">Return to sample library</UiButton></section>
    <div v-else-if="rows.length" class="library-table" role="table" aria-label="Sample pull requests">
      <div class="library-row library-table-head" role="row"><span role="columnheader">Pull request</span><span role="columnheader">Source status</span><span role="columnheader">Your questions</span><span role="columnheader" class="updated-cell">Sample update</span><span role="columnheader">Workspace</span></div>
      <div v-for="pr in rows" :key="pr.id" class="library-row" role="row">
        <div role="cell" class="pr-title-cell"><a :href="`?preview=${encodeURIComponent(pr.id)}`" @click="emit('open', pr.id, $event)">{{ pr.title }}</a><div class="pr-row-meta"><code>{{ pr.ownerRepository }} #{{ pr.number }}</code><span>{{ pr.author }}</span><span v-if="inTabs(pr.id)" class="in-tabs">In tabs</span></div></div>
        <div role="cell"><UiBadge :tone="pr.status === 'Closed' ? 'neutral' : 'success'">{{ pr.status === 'Open' ? '◉' : pr.status === 'Merged' ? '↗' : '⊘' }} {{ pr.status }}</UiBadge></div>
        <div role="cell" class="pr-question-count"><span>{{ summary(pr).open }} open</span><span>{{ summary(pr).understood }} understood</span></div>
        <div role="cell" class="updated-cell">{{ pr.updated }}<span class="sr-only">, fixed sample time</span></div>
        <div role="cell"><a class="pr-row-action" :href="`?preview=${encodeURIComponent(pr.id)}`" :aria-label="`Open sample ${pr.repository} #${pr.number}`" @click="emit('open', pr.id, $event)">{{ inTabs(pr.id) ? 'Resume' : pr.status === 'Open' ? 'Open' : 'Revisit' }} →</a></div>
      </div>
    </div>
    <section v-else class="library-feedback"><h2>{{ scenario === 'empty' ? 'No sample pull requests yet' : 'No matching sample pull requests' }}</h2><p>{{ scenario === 'empty' ? 'An empty-library preview. Existing page-memory workspaces have not been deleted.' : 'Try a different title, number, repository or source-status filter.' }}</p><UiButton v-if="scenario !== 'empty'" @click="clearFilters">Clear filters</UiButton><UiButton v-else @click="openDialog">Open a PR</UiButton></section>
    <footer class="library-footer"><p>Closing a workspace tab never closes its PR. Drafts and histories survive only in this page, not a reload.</p><span>Showing {{ rows.length }} of {{ scenario === 'empty' ? 0 : samplePullRequests.length }} sample records</span></footer>
    <details class="library-preview-tools"><summary>Sample library controls</summary><p>Explicit preview selection only. No loading timer, Git operation or agent execution runs.</p><UiField v-model="scenario" label="Library preview state" type="select" :options="scenarios" /><UiField v-model="sourcePR" label="PR for source preview" type="select" :options="samplePullRequests.map((pr) => ({ value: pr.id, label: `${pr.ownerRepository} #${pr.number}` }))" /><UiField v-model="sourceState" label="Source preview state" type="select" :options="sourceOptions" /><UiButton @click="emit('source', sourcePR, sourceState)">Show selected source preview</UiButton><UiButton @click="emit('extra')">Open another sample workspace</UiButton></details>
    <dialog ref="dialog" class="pr-import-dialog" aria-labelledby="pr-import-title"><h2 id="pr-import-title">Open a PR · unavailable preview</h2><p>Check a canonical URL's format locally. Real PR import, GitHub access and durable saving are not implemented. Do not paste credentials.</p><form @submit.prevent="validateURL"><UiField v-model="url" label="PR URL" :error="urlError" placeholder="https://github.com/owner/repository/pull/123" autofocus /><p v-if="urlNotice" role="status">{{ urlNotice }}</p><div class="dialog-actions"><UiButton type="submit">Preview URL validation</UiButton><UiButton disabled>Import PR — unavailable</UiButton><UiButton @click="dialog?.close()">Cancel</UiButton></div></form></dialog>
  </main>
</template>

<style scoped>
.pr-library { flex: 1; min-width: 0; width: 100%; max-width: 84rem; margin-inline: auto; padding: var(--space-6) var(--space-8) var(--space-5); overflow: auto; }
.library-heading, .library-filterbar, .library-footer { display: flex; align-items: center; flex-wrap: wrap; justify-content: space-between; gap: var(--space-4); }
.eyebrow { color: var(--color-muted); font-size: var(--text-code-meta); letter-spacing: 1px; }
.library-heading h1 { margin-top: var(--space-1); font-size: 1.6875rem; font-weight: 500; letter-spacing: -0.6px; }
.library-heading p:last-child { margin-top: var(--space-1); color: var(--color-muted); }
.library-disclosure { margin-top: var(--space-3); color: var(--color-muted); font-size: var(--text-small); }
.library-tools { display: flex; align-items: end; flex-wrap: wrap; gap: var(--space-4); margin-top: var(--space-6); }
.library-tools > :first-child { flex: 1 1 20rem; }
.library-tools > :not(:first-child) { flex: 1 1 11rem; max-width: 15rem; }
.library-filterbar { margin: var(--space-4) 0 var(--space-3); font-size: var(--text-small); color: var(--color-muted); }
.library-filters { display: flex; flex-wrap: wrap; gap: var(--space-1); }
.library-filters .ui-button { background: transparent; border-color: transparent; color: var(--color-muted); }
.library-filters [aria-pressed='true'] { border-color: var(--color-border-strong); background: var(--color-panel); color: var(--color-text); }
.library-filters span { margin-left: var(--space-2); font-size: var(--text-code-meta); }
.library-table { border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); overflow: hidden; }
.library-row { display: grid; grid-template-columns: minmax(0, 1fr) 6.875rem 8.4375rem 7.1875rem 5.625rem; align-items: center; gap: var(--space-4); padding: var(--space-1) 1.375rem; min-height: 4.625rem; border-top: 1px solid var(--color-border); }
.library-row:hover:not(.library-table-head) { background: var(--color-surface); }
.library-table-head { min-height: 2.625rem; border-top: 0; font-size: var(--text-code-meta); color: var(--color-muted); }
.pr-title-cell { min-width: 0; }
.pr-title-cell > a { display: inline-flex; align-items: center; min-height: var(--target-control); color: var(--color-text); font-size: var(--text-prose); font-weight: 500; text-decoration: none; overflow-wrap: anywhere; }
.pr-title-cell > a:hover { color: var(--color-accent); }
.pr-row-meta { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); padding-bottom: var(--space-2); color: var(--color-muted); font-size: var(--text-code-meta); }
.pr-row-meta code { font-size: var(--text-code-meta); }
.in-tabs { color: var(--color-accent); }
.pr-question-count { display: grid; gap: var(--space-1); font-size: var(--text-small); }
.pr-question-count span:last-child, .updated-cell { color: var(--color-muted); font-size: var(--text-small); }
.pr-row-action { display: flex; align-items: center; justify-content: end; min-height: var(--target-control); color: var(--color-accent); font-size: var(--text-small); text-decoration: none; }
.library-footer { margin-top: var(--space-4); color: var(--color-muted); font-size: var(--text-small); }
.library-feedback { display: grid; justify-items: start; gap: var(--space-3); padding: var(--space-5); margin-bottom: var(--space-3); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.library-feedback h2 { font-size: var(--text-heading); font-weight: 500; }
.library-preview-tools { margin-top: var(--space-6); color: var(--color-muted); font-size: var(--text-small); }
.library-preview-tools summary { min-height: var(--target-control); cursor: pointer; }
.library-preview-tools .ui-field { max-width: 32rem; margin-block: var(--space-3); }
.library-preview-tools .ui-button + .ui-button { margin-left: var(--space-2); }
.pr-import-dialog { width: min(36rem, calc(100% - 2rem)); max-height: calc(100dvh - 2rem); overflow: auto; padding: var(--space-6); border: 1px solid var(--color-border-strong); border-radius: var(--radius-panel); background: var(--color-panel); color: var(--color-text); }
.pr-import-dialog::backdrop { background: #0009; }
.pr-import-dialog h2 { font-size: var(--text-heading); font-weight: 500; }
.pr-import-dialog p, .pr-import-dialog form { margin-top: var(--space-4); }
.dialog-actions { display: flex; flex-wrap: wrap; gap: var(--space-2); margin-top: var(--space-4); }
@container application (max-width: 68.75rem) { .pr-library { padding-inline: var(--space-6); } .library-row { grid-template-columns: minmax(0, 1fr) 5.625rem 6.875rem 5.625rem; gap: var(--space-3); padding-inline: var(--space-4); } .library-row .updated-cell { display: none; } }
@container application (max-width: 47.5rem) { .library-row { grid-template-columns: minmax(0, 1fr) 5.625rem; padding-block: var(--space-3); } .library-row .pr-question-count { grid-column: 1; } .library-row > :last-child { grid-column: 2; } .library-table-head > :nth-child(3) { display: none; } }
</style>
