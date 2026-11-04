<script setup lang="ts">
import { computed } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import type { ThreadList } from './history'
import type { RuntimeThread } from './conversations'

const props = defineProps<{ list?: ThreadList; threads: RuntimeThread[]; pending: boolean; error: string; opening: string; selected: string; drafting: boolean }>()
const emit = defineEmits<{ refresh: []; open: [threadID: string, trigger: HTMLElement] }>()
const rows = computed((): { id: string; question: string; path: string; revision: string; state: string; count: number; persistence: string }[] => {
  const result = (props.list?.threads ?? []).map((saved): { id: string; question: string; path: string; revision: string; state: string; count: number; persistence: string } => {
    const local = props.threads.find((thread): boolean => thread.id === saved.threadID)
    const latest = local?.turns.at(-1)?.answer
    return { id: saved.threadID, question: local?.input.question ?? saved.question, path: local?.path ?? saved.newPath ?? saved.oldPath ?? 'Original pinned source', revision: saved.revisionID, state: latest?.executionUnknown || !latest && saved.executionUnknown ? 'Interrupted / prior execution unknown' : latest?.state ?? saved.state, count: Math.max(local?.turns.length ?? 0, saved.answerCount), persistence: latest?.persistence ?? saved.persistence }
  })
  for (const thread of props.threads) {
    if (result.some((row): boolean => row.id === thread.id)) continue
    const latest = thread.turns.at(-1)!.answer
    result.push({ id: thread.id, question: thread.input.question, path: thread.path, revision: latest.revisionID, state: latest.executionUnknown ? 'Interrupted / execution unknown' : latest.state, count: thread.turns.length, persistence: latest.persistence ?? 'unavailable' })
  }
  return result
})

/**
 * Opens only the explicitly activated row, retaining its control for focus recovery.
 * @param threadID Exact saved or retained thread.
 * @param event Row activation.
 */
function open(threadID: string, event: MouseEvent): void { emit('open', threadID, event.currentTarget as HTMLElement) }
</script>

<template>
  <section id="saved-questions" class="saved-questions" tabindex="-1" aria-label="Saved questions in this PR" :aria-busy="pending">
    <header><div><h2>Questions in this PR</h2><p>Saved questions and answers · Shared context per original revision.</p></div><UiButton :disabled="pending" @click="emit('refresh')">{{ list ? 'Refresh saved questions' : 'Load saved questions' }}</UiButton></header>
    <p>Opening history reads local saved data only. It does not fetch source, verify an agent session or resend a question. Drafts are not saved.</p>
    <p v-if="drafting" role="status">Your inline question draft is retained in Files changed, on its original selection.</p>
    <p v-if="pending" role="status">Reading saved question history… Previous rows remain available.</p>
    <p v-if="error" class="history-error" role="alert">{{ error }}</p>
    <p v-if="!list && !pending && !error">Load saved questions to read this PR's local history. Current-page threads remain available below.</p>
    <p v-if="list && !rows.length && !pending && !error" role="status">No saved questions in this PR. Load source and ask about a changed line or hunk to start one.</p>
    <ul v-if="rows.length" class="saved-list">
      <li v-for="row in rows" :key="row.id">
        <UiButton variant="quiet" :aria-pressed="selected === row.id" :aria-label="`Open thread: ${row.question}`" :disabled="opening === row.id" @click="open(row.id, $event)"><strong>{{ row.question }}</strong><span>{{ row.path }} · revision {{ row.revision.slice(0, 8) }}</span><span>{{ row.state }} · {{ row.count }} {{ row.count === 1 ? 'answer' : 'answers' }} · {{ row.persistence === 'failed' ? 'latest output not saved' : row.persistence === 'saving' ? 'latest answer in progress / saving' : row.persistence === 'saved' ? 'saved snapshot available' : 'saving status unavailable' }}</span><span v-if="opening === row.id">Opening saved thread…</span></UiButton>
      </li>
    </ul>
    <p v-if="list" class="saved-version">Saved list version {{ list.version }}. Refresh explicitly to see other tabs' submissions; live answers and drafts stay attached to their own threads.</p>
  </section>
</template>

<style scoped>
.saved-questions { flex: 1; min-width: 0; min-height: 0; overflow: auto; padding: var(--space-4); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); overflow-wrap: anywhere; }
header { display: flex; align-items: start; flex-wrap: wrap; gap: var(--space-3); }
header > div { flex: 1 1 15rem; }
h2 { font-size: var(--text-heading); font-weight: 550; }
p, .saved-list span { font-size: var(--text-small); color: var(--color-muted); }
p { margin-top: var(--space-3); }
.saved-list { display: grid; gap: var(--space-2); list-style: none; padding: 0; margin-top: var(--space-4); }
.saved-list .ui-button { display: grid; gap: var(--space-2); width: 100%; justify-content: start; text-align: left; white-space: normal; overflow-wrap: anywhere; padding: var(--space-3); border: 1px solid var(--color-border); }
.saved-list strong { font-weight: 550; }
.saved-list [aria-pressed='true'] { border-color: var(--color-accent); background: var(--color-surface); }
.history-error { color: var(--color-warning); }
.saved-version { margin-top: var(--space-4); }
</style>
