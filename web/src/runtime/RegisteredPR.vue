<script setup lang="ts">
import type { ReviewState } from './client'

defineProps<{ reviewId: string; reviewState: ReviewState }>()
</script>

<template>
  <div class="registered-pr">
    <header class="review-header" aria-label="Sheldon navigation">
      <a href="/" aria-label="Sheldon — UI previews">Sheldon</a>
      <span>Registered PR</span>
      <slot name="runtime-trigger" />
    </header>
    <slot name="runtime-panel" />
    <main id="review-content" class="review-content" aria-labelledby="review-heading" :aria-busy="reviewState.status === 'loading'">
      <template v-if="reviewState.status === 'registered' && reviewState.pr">
        <p class="review-identity">{{ reviewState.pr.owner }}/{{ reviewState.pr.repository }} #{{ reviewState.pr.number }}</p>
        <h1 id="review-heading">{{ reviewState.pr.title ?? `Pull request #${reviewState.pr.number}` }}</h1>
        <p v-if="reviewState.pr.author">Author: {{ reviewState.pr.author }}</p>
        <p v-else>Author metadata is not available.</p>
        <p role="status">PR registered. Source fetching and pinned diffs are not implemented yet.</p>
        <p>No source revision is loaded. No agent is connected and no question has been sent.</p>
        <p class="review-identity">{{ reviewState.pr.url }}</p>
      </template>
      <template v-else-if="reviewState.status === 'not-found'">
        <h1 id="review-heading">PR not found</h1>
        <p role="status">This PR is not registered in this Sheldon instance. Reopen its GitHub URL with sheldon open.</p>
      </template>
      <template v-else>
        <h1 id="review-heading">PR workspace</h1>
        <p v-if="reviewState.status === 'loading'" role="status">Reading registered PR…</p>
        <p v-else-if="reviewState.status === 'failed'" role="status">Cannot read this PR. Check the backend and private prs.json index, then reconnect.</p>
        <p v-else role="status">Connect the local runtime to read this registered PR. Reloading requires explicit reconnection.</p>
      </template>
      <p class="review-identity">Workspace ID: {{ reviewId }}</p>
      <a href="/">View sample UI previews (not this PR)</a>
    </main>
  </div>
</template>

<style scoped>
.registered-pr { container: application / inline-size; min-height: 100dvh; }
.review-header { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-4); min-height: 3rem; padding-inline: var(--space-6); border-bottom: 1px solid var(--color-border); }
.review-header > span { flex: 1; color: var(--color-muted); }
.review-header > a, .review-content > a { display: inline-flex; align-items: center; min-height: var(--target-control); }
.review-content { max-width: 64rem; margin-inline: auto; padding: var(--space-6); overflow-wrap: anywhere; }
.review-content > * + * { margin-top: var(--space-4); }
.review-identity { font-family: var(--font-code); color: var(--color-muted); }
</style>
