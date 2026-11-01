<script setup lang="ts">
import { ref } from 'vue'
import UiButton from './ui/UiButton.vue'

defineProps<{
  current: string
  workspaces: readonly { id: string; repository: string; number: number; title: string }[]
}>()
const emit = defineEmits<{ navigate: [id: string, event: MouseEvent]; close: [id: string] }>()
const header = ref<HTMLElement>()

/**
 * Restores focus to a surviving destination after a workspace closes.
 * @param id Destination identifier.
 */
function focusDestination(id: string): void {
  const links = header.value?.querySelectorAll<HTMLAnchorElement>('[data-destination]')
  for (const link of links ?? []) {
    if (link.dataset.destination === id) {
      link.focus()
      return
    }
  }
}
defineExpose({ focusDestination })
</script>

<template>
  <header ref="header" class="application-header" aria-label="Sheldon navigation">
    <a
      class="application-brand"
      href="?preview=library"
      aria-label="Sheldon — All PRs"
      @click="emit('navigate', 'library', $event)"
    >
      <span class="brand-mark" aria-hidden="true">S.</span><span class="brand-name">Sheldon</span>
    </a>
    <a
      class="all-prs"
      href="?preview=library"
      data-destination="library"
      :aria-current="current === 'library' ? 'page' : undefined"
      @click="emit('navigate', 'library', $event)"
    >All PRs</a>
    <nav class="workspace-links" aria-label="PR workspaces">
      <div
        v-for="workspace in workspaces"
        :key="workspace.id"
        class="workspace-link"
        :class="{ 'is-current': current === workspace.id }"
      >
        <a
          :href="`?preview=${encodeURIComponent(workspace.id)}`"
          :data-destination="workspace.id"
          :aria-current="current === workspace.id ? 'page' : undefined"
          :aria-label="`${workspace.repository} #${workspace.number} — ${workspace.title}`"
          :aria-description="workspace.id"
          :title="`${workspace.id} — ${workspace.title}`"
          @click="emit('navigate', workspace.id, $event)"
        >
          <strong>{{ workspace.repository }} #{{ workspace.number }}</strong>
          <span>{{ workspace.title }}</span>
        </a>
        <UiButton
          variant="quiet"
          :aria-label="`Close sample workspace ${workspace.repository} #${workspace.number}`"
          @click="emit('close', workspace.id)"
        >×</UiButton>
      </div>
    </nav>
    <a
      class="configuration-link"
      href="?preview=config"
      data-destination="config"
      aria-label="Configuration"
      :aria-current="current === 'config' ? 'page' : undefined"
      @click="emit('navigate', 'config', $event)"
    ><span aria-hidden="true">⚙</span><span class="configuration-text">Configuration</span></a>
    <slot name="tools" />
  </header>
</template>

<style scoped>
.application-header {
  display: flex;
  align-items: center;
  gap: var(--space-3);
  min-height: 3rem;
  padding-inline: var(--space-6);
  border-bottom: 1px solid var(--color-border);
  background: var(--color-canvas);
}
.application-header a {
  display: inline-flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--target-control);
  flex-shrink: 0;
  padding-inline: var(--space-2);
  border-radius: var(--radius-control);
  color: var(--color-muted);
  text-decoration: none;
}
.application-header a:focus-visible { outline-offset: -2px; }
.application-header a:hover { color: var(--color-text); background: var(--color-surface); }
.application-header a[aria-current='page'] { color: var(--color-accent); background: var(--color-accent-soft); }
.application-header .application-brand { color: var(--color-text); font-size: 1.375rem; font-weight: 600; padding: 0; }
.brand-mark { color: var(--color-accent); font-size: 1.875rem; }
.workspace-links { display: flex; flex: 1; min-width: 0; overflow-x: auto; gap: var(--space-2); }
.workspace-link { display: flex; flex-shrink: 0; align-items: center; border-bottom: 2px solid transparent; }
.workspace-link.is-current { background: var(--color-panel); border-bottom-color: var(--color-accent); }
.workspace-link a[aria-current='page'] { color: var(--color-text); background: transparent; }
.workspace-link a { font-size: var(--text-small); }
.workspace-link strong { color: var(--color-accent); font-weight: 500; white-space: nowrap; }
.workspace-link a span { max-width: 10rem; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.workspace-link .ui-button { padding-inline: var(--space-2); }
.configuration-link { font-size: var(--text-small); }
@container application (max-width: 70rem) {
  .brand-name, .configuration-text { display: none; }
  .application-header { gap: var(--space-2); padding-inline: var(--space-3); }
  .configuration-link { min-width: var(--target-control); justify-content: center; }
}
@container application (max-width: 45rem) {
  .workspace-link a span { display: none; }
}
</style>
