<script setup lang="ts">
import { computed, useId } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import type { SamplePullRequest, SourcePreviewState } from './library'

const props = defineProps<{ pr: SamplePullRequest; state: SourcePreviewState; hasCache: boolean }>()
const emit = defineEmits<{ library: []; restore: [] }>()
const failure = computed(failureText)
const id = useId()

/**
 * Describes an explicitly selected source-only error without implying an actual request.
 * @returns Readable preview recovery context.
 */
function failureText(): string {
  if (props.state === 'offline') return 'Offline source preview. Metadata, changed files and diff are unavailable.'
  if (props.state === 'denied') return 'Access denied preview. No credentials were requested and no GitHub access was attempted.'
  if (props.state === 'missing') return 'Missing pull request preview. No PR was fetched or removed from the sample library.'
  return 'Source unavailable preview. A complete metadata/files/diff snapshot is not available; partial source is not shown as a loaded PR.'
}
</script>

<template>
  <div class="source-status-workspace">
    <section class="source-request-summary" aria-label="Pull request being opened"><div><h1>{{ state === 'loading' ? 'Opening pull request' : 'Source unavailable' }} <span>#{{ pr.number }}</span></h1><p><code>{{ pr.ownerRepository }}</code> · {{ state === 'loading' ? 'Metadata, changed files and diff · sample loading state' : 'Selected source-error preview' }}</p></div><UiButton @click="emit('library')">Back to all PRs</UiButton></section>
    <nav class="source-pending-navigation" aria-label="Unavailable PR content"><div><UiButton v-for="label in ['Files changed', 'Questions', 'Quiz', 'Semantic review']" :key="label" variant="quiet" disabled :aria-describedby="`${id}-pending-reason`">{{ label }}</UiButton></div><span>Waiting for source revision</span></nav>
    <main class="source-status-body" tabindex="-1" aria-label="Source loading preview">
      <p :id="`${id}-pending-reason`" class="source-disclosure" role="status">{{ state === 'loading' ? 'Source-loading UI preview only. No fetch is running, no progress is measured and nothing completes automatically.' : failure }} No agent is running; semantic review never starts from source loading.</p>
      <div class="source-pending-panes">
        <aside class="pending-files" aria-label="Changed files placeholder"><h2>Changed files</h2><div v-if="state === 'loading'" class="skeleton-file-list" aria-hidden="true"><span v-for="index in 5" :key="index" /></div><p v-else>Unavailable in this source-error preview.</p></aside>
        <section class="pending-source" aria-label="Unresolved source" :aria-busy="state === 'loading'">
          <div v-if="state === 'loading'" class="skeleton-source-head" aria-hidden="true"><span /></div>
          <div class="source-feedback"><span aria-hidden="true">{{ state === 'loading' ? '↻' : '!' }}</span><h2>{{ state === 'loading' ? 'Fetching pull request · preview' : 'Source could not be loaded · preview' }}</h2><p>{{ state === 'loading' ? 'Getting source ready for review is not implemented. This snapshot remains unresolved until you choose another preview.' : failure }}</p><p>Metadata · Changed files · Diff</p><UiButton v-if="state !== 'loading'" disabled :aria-describedby="`${id}-pending-reason`">Retry source loading — unavailable</UiButton><UiButton @click="emit('library')">Back to all PRs</UiButton><UiButton v-if="hasCache" @click="emit('restore')">Return to retained sample snapshot</UiButton></div>
          <div v-if="state === 'loading'" class="skeleton-code" aria-hidden="true"><span v-for="index in 5" :key="index" /></div>
        </section>
        <aside class="pending-conversation" aria-label="Conversation placeholder"><span aria-hidden="true">↳</span><h2>Your conversations belong here</h2><p>No new answer or source context is available before a coherent snapshot exists. Retained local history is not erased by this preview.</p><p>New questions share one agent context per PR revision. No agent session is started here.</p></aside>
      </div>
    </main>
  </div>
</template>

<style scoped>
.source-status-workspace { display: flex; flex: 1; flex-direction: column; min-width: 0; min-height: 0; }
.source-request-summary { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); min-height: 4.1875rem; padding: var(--space-2) var(--space-6); }
.source-request-summary h1 { font-size: var(--text-heading); font-weight: 500; line-height: 1.3; }
.source-request-summary span, .source-request-summary p { color: var(--color-muted); }
.source-request-summary p { margin-top: var(--space-1); font-size: var(--text-small); }
.source-pending-navigation { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-2); min-height: 2.8125rem; padding-inline: var(--space-6); border-bottom: 1px solid var(--color-border); }
.source-pending-navigation > div { display: flex; flex-wrap: wrap; }
.source-pending-navigation .ui-button { min-height: var(--target-control); padding-block: 0; border: 0; border-radius: 0; background: transparent; font-size: var(--text-small); }
.source-pending-navigation > span { color: var(--color-muted); font-size: var(--text-code-meta); }
.source-status-body { flex: 1; min-height: 0; overflow: auto; padding: var(--space-3) var(--space-6) var(--space-4); }
.source-disclosure { margin-bottom: var(--space-3); color: var(--color-muted); font-size: var(--text-small); }
.source-pending-panes { display: grid; grid-template-columns: var(--width-file-rail) minmax(0, 1fr) var(--width-conversation); gap: var(--space-4); min-height: 42rem; }
.pending-files { border-right: 1px solid var(--color-border); padding: var(--space-4); }
.pending-files h2 { font-size: var(--text-small); font-weight: 500; }
.pending-files p { margin-top: var(--space-4); color: var(--color-muted); font-size: var(--text-small); }
.skeleton-file-list, .skeleton-code { display: grid; gap: var(--space-4); margin-top: var(--space-6); }
.skeleton-file-list span, .skeleton-code span, .skeleton-source-head span { display: block; height: 0.75rem; border-radius: var(--radius-control); background: var(--color-surface); }
.skeleton-file-list span:nth-child(even), .skeleton-code span:nth-child(even) { width: 70%; }
.pending-source { min-width: 0; overflow: hidden; border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); }
.skeleton-source-head { padding: var(--space-6); border-bottom: 1px solid var(--color-border); }
.skeleton-source-head span { width: 50%; }
.source-feedback { display: grid; justify-items: center; gap: var(--space-4); padding: var(--space-8) var(--space-6); text-align: center; }
.source-feedback > span { color: var(--color-accent); font-size: 2rem; }
.source-feedback h2 { font-size: var(--text-heading); font-weight: 500; }
.source-feedback p, .pending-conversation p { color: var(--color-muted); font-size: var(--text-small); }
.skeleton-code { padding: var(--space-6); border-top: 1px solid var(--color-border); margin-top: 0; }
.pending-conversation { display: grid; align-content: center; gap: var(--space-4); padding: var(--space-8); border: 1px solid var(--color-border); border-radius: var(--radius-panel); text-align: center; }
.pending-conversation > span { color: var(--color-muted); font-size: 2rem; }
.pending-conversation h2 { font-size: var(--text-heading); font-weight: 500; }
@container application (max-width: 68.75rem) { .source-pending-panes { grid-template-columns: 12rem minmax(0, 1fr); } .pending-conversation { display: none; } }
@container application (max-width: 47.5rem) { .source-pending-panes { grid-template-columns: minmax(0, 1fr); min-height: 0; } .pending-files { border-right: 0; } .skeleton-file-list { display: none; } }
</style>
