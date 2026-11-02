<script setup lang="ts">
import OpenPR from './OpenPR.vue'
import type { RuntimeClient } from './client'
defineProps<{ client: RuntimeClient | null }>()
</script>

<template>
  <div class="pr-landing">
    <header aria-label="Sheldon navigation"><a href="/">Sheldon</a><a href="/?preview=library">Sample UI previews</a><slot name="runtime-trigger" /></header>
    <slot name="runtime-panel" />
    <main><p class="eyebrow">Local pull-request explorer</p><h1>Open a pull request</h1><p>Register a GitHub PR, then load its real, pinned Files changed view.</p><OpenPR :client="client" /><p v-if="!client">Connect the local runtime to register a PR.</p><p>No agent is connected. Questions, Quiz and Semantic review are unavailable for real PRs.</p><a href="/?preview=library">Explore sample UI previews instead (not real source)</a></main>
  </div>
</template>

<style scoped>
.pr-landing { container: application / inline-size; min-height: 100dvh; }
header { display: flex; align-items: center; flex-wrap: wrap; gap: var(--space-4); min-height: 3rem; padding-inline: var(--space-6); border-bottom: 1px solid var(--color-border); }
header > a:first-child { flex: 1; font-size: 1.375rem; }
header a { display: inline-flex; align-items: center; min-height: var(--target-control); }
main { max-width: 52rem; padding: var(--space-8) var(--space-6); margin: auto; overflow-wrap: anywhere; }
main > * + * { margin-top: var(--space-5); }
.eyebrow { color: var(--color-muted); }
</style>
