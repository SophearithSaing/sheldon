<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue'
import WorkspaceShell from '../components/WorkspaceShell.vue'
import SourceDiff from '../components/SourceDiff.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiBadge from '../components/ui/UiBadge.vue'
import ThreadConversation from './ThreadConversation.vue'
import { historicalSampleThreads, type QuestionSummary, type SamplePullRequest } from './library'
import type { DiffLine } from '../components/diff'

const props = defineProps<{ pr: SamplePullRequest }>()
const emit = defineEmits<{ summary: [value: QuestionSummary] }>()
const threads = ref(historicalSampleThreads(props.pr))
const selected = ref(threads.value[0]?.id ?? null)
const conversationOpen = ref(threads.value.length > 0)
const shell = ref<InstanceType<typeof WorkspaceShell>>()
const source = ref<HTMLElement>()
const current = computed(currentThread)
const lines = computed(sourceLines)
const summary = computed(questionSummary)
watch(summary, reportSummary, { immediate: true })

/**
 * Reads only this historical PR's selected transcript.
 * @returns Selected retained sample thread.
 */
function currentThread(): typeof threads.value[number] | undefined {
  return threads.value.find((thread): boolean => thread.id === selected.value)
}

/**
 * Renders an explicitly incomplete, fictional quoted snapshot without inventing a full diff.
 * @returns Literal sample excerpt with head-only coordinates.
 */
function sourceLines(): DiffLine[] {
  return props.pr.snapshot?.source.map((text, index): DiffLine => ({ base: null, head: index + 1, text, kind: 'context' })) ?? []
}

/**
 * Keeps open question counts separate from merged or closed source status.
 * @returns Reader-controlled sample totals.
 */
function questionSummary(): QuestionSummary {
  const understood = threads.value.filter((thread): boolean => thread.status === 'understood').length
  return { open: threads.value.length - understood, understood }
}

/**
 * Reports local history changes to the library without persistence.
 * @param value Current page-memory question totals.
 */
function reportSummary(value: QuestionSummary): void {
  emit('summary', value)
}

/**
 * Opens an existing historical thread without regenerating an answer.
 * @param id Local historical question id.
 * @param event Row activation for return-focus restoration.
 */
async function openThread(id: number, event: MouseEvent): Promise<void> {
  selected.value = id
  await shell.value?.openConversation(event)
}

/**
 * Applies explicit understanding only to the selected retained history.
 * @param id Local thread identifier.
 * @param value Reader confirmation or reopen choice.
 */
function understood(id: number, value: boolean): void {
  const thread = threads.value.find((item): boolean => item.id === id)
  if (thread) thread.status = value ? 'understood' : 'answered'
}

/**
 * Returns focus to the quoted cache, never a live repository location.
 */
async function viewSource(): Promise<void> {
  await nextTick()
  if (!source.value) return
  await shell.value?.openConversation(source.value, false)
  source.value.focus()
}
</script>

<template>
  <div class="historical-workspace">
    <WorkspaceShell ref="shell" v-model:conversation-open="conversationOpen" :has-file-rail="false" :conversation-title="`Question ${String(selected ?? 1).padStart(2, '0')}`">
      <template #summary><h1>{{ pr.title }} <span>#{{ pr.number }} · sample PR</span></h1><div class="fixture-meta"><UiBadge :tone="pr.status === 'Closed' ? 'neutral' : 'success'">{{ pr.status }} source</UiBadge><span>{{ pr.ownerRepository }} · Retained sample revision {{ pr.snapshot?.revision }}</span></div></template>
      <template #tabs><p class="history-navigation">Retained source and question history · Local sample only</p></template>
      <template #default><ScrollRegion label="Retained sample source and history" class="history-content"><p class="history-note">This {{ pr.status.toLowerCase() }} sample PR keeps its question history. Source status does not mark anything understood. This is a quoted illustrative cache, not a complete fetched diff or durable store.</p><section ref="source" tabindex="-1" class="history-source"><h2>{{ pr.snapshot?.path }}</h2><SourceDiff :lines="lines" label="Retained sample source excerpt" :selection="null" :selectable="false" /></section><h2>Retained sample questions</h2><UiButton v-for="thread in threads" :key="thread.id" class="history-question" :aria-current="thread.id === selected && conversationOpen ? 'true' : undefined" @click="openThread(thread.id, $event)">{{ String(thread.id).padStart(2, '0') }} · {{ thread.question }} · {{ thread.status }}</UiButton><p>New questions and full source review are unavailable for this historical excerpt. No Git operation or agent run has occurred.</p></ScrollRegion></template>
      <template #conversation><KeepAlive><ThreadConversation v-if="current" :key="current.id" v-model:followup="current.followup" :thread="current" :busy-thread="null" :preview-controls="false" source-action-label="View retained excerpt" @source="viewSource" @understood="understood" /></KeepAlive></template>
    </WorkspaceShell>
  </div>
</template>

<style scoped>
.historical-workspace { display: flex; flex: 1; min-height: 0; min-width: 0; }
h1 { font-size: var(--text-heading); font-weight: 500; overflow-wrap: anywhere; }
h1 span { color: var(--color-muted); }
.fixture-meta { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-3); color: var(--color-muted); }
.history-navigation { display: flex; align-items: center; min-height: var(--target-control); font-size: var(--text-small); color: var(--color-muted); }
.history-content { flex: 1; padding: var(--space-4); }
.history-content h2 { margin-block: var(--space-4); font-size: var(--text-body); font-weight: 500; }
.history-note, .history-content > p { color: var(--color-muted); font-size: var(--text-small); }
.history-source { border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); margin-block: var(--space-5); overflow: hidden; }
.history-source h2 { padding-inline: var(--space-4); overflow-wrap: anywhere; font-family: var(--font-code); }
.history-question { display: flex; width: 100%; text-align: left; margin-bottom: var(--space-3); }
.history-question[aria-current] { border-color: var(--color-accent-border); background: var(--color-accent-soft); }
</style>
