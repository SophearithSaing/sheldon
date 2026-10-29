<script setup lang="ts">
import { nextTick, ref, useId } from 'vue'
import UiButton from './ui/UiButton.vue'

defineProps<{ hasFileRail: boolean; conversationTitle: string }>()
const conversationOpen = defineModel<boolean>('conversationOpen', { default: false })
const filesOpen = ref(false)
const id = useId()
const fileRail = ref<HTMLElement>()
const content = ref<HTMLElement>()
const conversationHeading = ref<HTMLElement>()
let fileTrigger: HTMLElement | null = null
let conversationTrigger: HTMLElement | null = null

async function toggleFiles(event: MouseEvent) {
  if (filesOpen.value) {
    closeFiles()
    return
  }
  fileTrigger = event.currentTarget as HTMLElement
  filesOpen.value = true
  await nextTick()
  fileRail.value?.focus()
}

function closeFiles() {
  if (!filesOpen.value) return
  filesOpen.value = false
  fileTrigger?.focus()
}

async function openConversation(event: MouseEvent) {
  conversationTrigger = event.currentTarget as HTMLElement
  conversationOpen.value = true
  await nextTick()
  conversationHeading.value?.focus()
}

async function closeConversation() {
  conversationOpen.value = false
  await nextTick()
  if (conversationTrigger?.getClientRects().length) {
    conversationTrigger.focus()
    return
  }
  content.value?.focus()
}
</script>

<template>
  <div class="workspace-container">
    <a
      class="skip-link skip-to-content"
      :class="{ 'conversation-visible': conversationOpen }"
      :href="`#${id}-content`"
    >
      Skip to workspace
    </a>
    <a v-show="conversationOpen" class="skip-link" :href="`#${id}-conversation-title`">
      Skip to conversation
    </a>
    <div
      class="workspace"
      :class="{ 'has-file-rail': hasFileRail, 'has-conversation': conversationOpen }"
      @keydown.esc="closeFiles"
    >
      <header class="workspace-header">
        <span class="workspace-brand">Sheldon<span aria-hidden="true" class="brand-dot">.</span></span>
        <div class="workspace-header__context"><slot name="context" /></div>
      </header>
      <section class="workspace-summary" aria-label="Pull request context">
        <slot name="summary" />
      </section>
      <div class="workspace-navigation">
        <slot name="tabs" />
        <UiButton
          v-if="hasFileRail"
          class="files-trigger"
          :aria-expanded="filesOpen"
          :aria-controls="`${id}-files`"
          @click="toggleFiles"
        >
          Files
        </UiButton>
      </div>
      <div class="workspace-panes">
        <aside
          v-show="hasFileRail"
          :id="`${id}-files`"
          ref="fileRail"
          class="workspace-files"
          :class="{ 'is-open': filesOpen }"
          aria-label="Changed files"
          tabindex="-1"
        >
          <UiButton class="files-close" @click="closeFiles">Close files</UiButton>
          <slot name="files" :close-files="closeFiles" />
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
      <footer class="workspace-footer"><slot name="footer" /></footer>
    </div>
  </div>
</template>

<style scoped>
.workspace-container {
  container: workspace / inline-size;
  flex: 1;
  min-height: 0;
}
.workspace {
  display: grid;
  grid-template-rows:
    minmax(4rem, auto)
    minmax(7.875rem, auto)
    auto
    minmax(0, 1fr)
    minmax(2.125rem, auto);
  height: 100%;
  min-height: 0;
}
.workspace-header,
.workspace-summary,
.workspace-navigation,
.workspace-footer {
  background: var(--color-panel);
}
.workspace-header {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-3) var(--space-8);
  padding: var(--space-3) var(--space-8);
  border-bottom: 1px solid var(--color-border);
}
.workspace-brand {
  font-size: var(--text-heading);
  font-weight: 600;
  letter-spacing: -0.04em;
}
.brand-dot {
  color: var(--color-accent);
  padding-left: var(--space-2);
}
.workspace-header__context {
  min-width: 0;
  overflow-wrap: anywhere;
  color: var(--color-muted);
}
.workspace-summary {
  display: grid;
  align-content: center;
  gap: var(--space-3);
  padding: var(--space-5) var(--space-8);
}
.workspace-navigation {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  padding-inline: var(--space-8);
  border-bottom: 1px solid var(--color-border);
}
.workspace-panes {
  position: relative;
  display: grid;
  grid-template-columns: minmax(0, 1fr);
  min-width: 0;
  min-height: 0;
}
.has-file-rail .workspace-panes {
  grid-template-columns: var(--width-file-rail) minmax(0, 1fr);
}
.has-conversation .workspace-panes {
  grid-template-columns: minmax(0, 1fr) var(--width-conversation);
}
.has-file-rail.has-conversation .workspace-panes {
  grid-template-columns: var(--width-file-rail) minmax(0, 1fr) var(--width-conversation);
}
.workspace-files {
  min-width: 0;
  min-height: 0;
  overflow: auto;
  padding: var(--space-5) var(--space-4);
  border-right: 1px solid var(--color-border);
  background: var(--color-canvas);
}
.workspace-files:focus-visible {
  outline-offset: -3px;
}
.workspace-content,
.workspace-conversation {
  display: flex;
  flex-direction: column;
  min-width: 0;
  min-height: 0;
}
.workspace-content:focus-visible {
  outline-offset: -3px;
}
.workspace-conversation {
  overflow: auto;
  border-left: 1px solid var(--color-border);
  background: var(--color-panel);
}
.workspace-conversation:focus-visible {
  outline-offset: -3px;
}
.conversation-header {
  flex-shrink: 0;
  padding: var(--space-3) var(--space-6);
  border-bottom: 1px solid var(--color-border);
}
.conversation-heading {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
}
.conversation-heading h2 {
  font-size: var(--text-prose);
  font-weight: 600;
}
.workspace-footer {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2) var(--space-4);
  padding: var(--space-2) var(--space-6);
  border-top: 1px solid var(--color-border);
  color: var(--color-muted);
  font-size: var(--text-small);
}
.files-trigger,
.files-close,
.pane-return {
  display: none;
}

/* 1180px and 900px at the default 16px root; rem also accommodates text scaling. */
@container workspace (max-width: 73.75rem) {
  .has-file-rail .workspace-panes {
    grid-template-columns: minmax(0, 1fr);
  }
  .has-file-rail.has-conversation .workspace-panes {
    grid-template-columns: minmax(0, 1fr) var(--width-conversation);
  }
  .workspace-files {
    display: none;
  }
  .workspace-files.is-open {
    position: absolute;
    inset: 0 auto 0 0;
    z-index: 2;
    display: block;
    width: min(var(--width-file-rail), 100%);
    border-right-color: var(--color-border-strong);
  }
  .files-trigger,
  .files-close {
    display: inline-flex;
  }
  .files-close {
    margin-bottom: var(--space-4);
  }
}
@container workspace (max-width: 56.25rem) {
  .has-conversation .workspace-panes,
  .has-file-rail.has-conversation .workspace-panes {
    grid-template-columns: minmax(0, 1fr);
  }
  .has-conversation .workspace-content {
    display: none;
  }
  .has-conversation .workspace-conversation {
    border-left: 0;
  }
  .skip-to-content.conversation-visible {
    display: none;
  }
  .pane-return {
    display: inline-flex;
    margin-top: var(--space-2);
  }
  .workspace-header,
  .workspace-summary {
    padding-inline: var(--space-4);
  }
  .workspace-navigation {
    padding-inline: var(--space-4);
  }
}
</style>
