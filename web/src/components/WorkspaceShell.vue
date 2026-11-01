<script setup lang="ts">
import { nextTick, ref, useId } from 'vue'
import UiButton from './ui/UiButton.vue'

defineProps<{ hasFileRail: boolean; conversationTitle: string }>()
const conversationOpen = defineModel<boolean>('conversationOpen', { default: false })
const id = useId()
const content = ref<HTMLElement>()
const conversationHeading = ref<HTMLElement>()
const fileDisclosure = ref<HTMLDetailsElement>()
let conversationTrigger: HTMLElement | null = null

/**
 * Collapses the native file disclosure and restores its summary focus.
 */
function collapseFiles(): void {
  if (!fileDisclosure.value) return
  fileDisclosure.value.open = false
  fileDisclosure.value.querySelector('summary')?.focus()
}

/**
 * Opens the conversation without hiding or collapsing source navigation.
 * @param event Activation of the initiating conversation control.
 */
async function openConversation(event: MouseEvent | HTMLElement): Promise<void> {
  conversationTrigger = event instanceof HTMLElement ? event : event.currentTarget as HTMLElement
  conversationOpen.value = true
  await nextTick()
  conversationHeading.value?.focus()
}
defineExpose({ openConversation })

/**
 * Closes the panel and restores the initiating source control when available.
 */
async function closeConversation(): Promise<void> {
  conversationOpen.value = false
  await nextTick()
  if (conversationTrigger?.getClientRects().length) {
    conversationTrigger.focus()
  } else {
    content.value?.focus()
  }
}
</script>

<template>
  <div class="workspace-container">
    <a class="skip-link" :href="`#${id}-content`">Skip to workspace</a>
    <a v-show="conversationOpen" class="skip-link" :href="`#${id}-conversation-title`">
      Skip to conversation
    </a>
    <div class="workspace" :class="{ 'has-file-rail': hasFileRail, 'has-conversation': conversationOpen }">
      <section class="workspace-summary" aria-label="Pull request context">
        <slot name="summary" />
      </section>
      <div class="workspace-navigation"><slot name="tabs" /></div>
      <div class="workspace-panes" role="region" aria-label="Review panes" tabindex="0">
        <aside v-show="hasFileRail" class="workspace-files" aria-label="Changed files">
          <details ref="fileDisclosure" class="file-disclosure" open @keydown.esc.stop="collapseFiles">
            <summary aria-label="Changed files">
              <span aria-hidden="true">☷</span>
              <span class="files-title">Changed files</span>
              <span class="files-chevron" aria-hidden="true">‹</span>
            </summary>
            <div class="files-body" role="region" aria-label="Changed file list" tabindex="0">
              <slot name="files" />
            </div>
          </details>
        </aside>
        <main :id="`${id}-content`" ref="content" class="workspace-content" tabindex="-1">
          <slot :open-conversation="openConversation" />
        </main>
        <aside
          v-show="conversationOpen"
          class="workspace-conversation"
          :aria-labelledby="`${id}-conversation-title`"
          tabindex="0"
        >
          <header class="conversation-header">
            <div class="conversation-heading">
              <h2 :id="`${id}-conversation-title`" ref="conversationHeading" tabindex="-1">
                {{ conversationTitle }}
              </h2>
              <UiButton variant="quiet" @click="closeConversation">Close conversation</UiButton>
            </div>
            <UiButton class="pane-return" @click="closeConversation">Back to workspace</UiButton>
          </header>
          <slot name="conversation" />
        </aside>
      </div>
    </div>
  </div>
</template>

<style scoped>
.workspace-container {
  container: workspace / inline-size;
  flex: 1;
  min-height: 0;
  min-width: 0;
}
.workspace {
  display: grid;
  grid-template-rows: minmax(4.1875rem, auto) minmax(2.8125rem, auto) minmax(0, 1fr);
  height: 100%;
  min-height: 0;
}
.workspace-summary {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto;
  align-content: center;
  gap: 0.25rem;
  padding: 0.5rem var(--space-6);
  background: var(--color-canvas);
}
.workspace-summary :deep(.fixture-meta) { grid-column: 1; grid-row: 2; font-size: var(--text-code-meta); }
.workspace-navigation {
  display: flex;
  align-items: stretch;
  padding-inline: var(--space-6);
  border-bottom: 1px solid var(--color-border);
}
.workspace-navigation :deep(.ui-tabs button) { min-height: var(--target-control); }
.workspace-panes {
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  gap: var(--space-4);
  padding: var(--space-3) var(--space-6) var(--space-4);
  min-width: 0;
  min-height: 0;
}
.workspace-panes:focus-visible, .files-body:focus-visible { outline-offset: -3px; }
.has-file-rail .workspace-panes {
  grid-template-columns: 3rem minmax(0, 1fr);
}
.has-conversation .workspace-panes {
  grid-template-columns: minmax(0, 1fr) var(--width-conversation);
}
.has-file-rail.has-conversation .workspace-panes {
  grid-template-columns: 3rem minmax(0, 1fr) var(--width-conversation);
}
.has-file-rail:has(.file-disclosure[open]) .workspace-panes {
  grid-template-columns: var(--width-file-rail) minmax(0, 1fr);
}
.has-file-rail.has-conversation:has(.file-disclosure[open]) .workspace-panes {
  grid-template-columns: var(--width-file-rail) minmax(0, 1fr) var(--width-conversation);
}
.workspace-files {
  min-width: 0;
  min-height: 0;
  border-right: 1px solid var(--color-border);
  padding-right: var(--space-3);
}
.workspace-files:has(.file-disclosure:not([open])) { padding-right: 0; }
.file-disclosure {
  height: 100%;
}
.file-disclosure summary {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--target-control);
  padding-inline: var(--space-2);
  border-radius: var(--radius-control);
  list-style: none;
  cursor: pointer;
}
.file-disclosure summary::-webkit-details-marker { display: none; }
.file-disclosure summary:hover { background: var(--color-surface); }
.file-disclosure summary:focus-visible { outline-offset: -2px; }
.files-chevron { margin-left: auto; }
.file-disclosure:not([open]) :is(.files-title, .files-chevron) { display: none; }
.files-body {
  height: calc(100% - var(--target-control));
  overflow: auto;
  padding: var(--space-3) var(--space-1);
  scrollbar-gutter: stable;
}
.workspace-content,
.workspace-conversation {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.workspace-content:focus-visible,
.workspace-conversation:focus-visible { outline-offset: -3px; }
.workspace-conversation {
  overflow: auto;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
  background: var(--color-panel);
}
.conversation-header {
  flex-shrink: 0;
  padding: 0.625rem var(--space-4);
  border-bottom: 1px solid var(--color-border);
}
.conversation-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}
.conversation-heading h2 { font-size: 1.0625rem; font-weight: 500; }
.pane-return { display: none; }
@container workspace (max-width: 56.25rem) {
  .workspace .workspace-panes {
    display: flex;
    flex-direction: column;
    overflow: auto;
    padding-inline: var(--space-4);
  }
  .workspace-files { flex: 0 0 18rem; border-right: 0; padding-right: 0; }
  .workspace-files:has(.file-disclosure:not([open])) { flex-basis: 3rem; }
  .workspace-content { flex: 0 0 38rem; }
  .workspace-conversation { flex: 0 0 38rem; }
  .pane-return { display: inline-flex; margin-top: var(--space-2); }
}
</style>
