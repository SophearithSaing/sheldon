<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import UiBadge from '../components/ui/UiBadge.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'

defineProps<{ originId: string; originLabel: string }>()
const emit = defineEmits<{ navigate: [id: string, event: MouseEvent]; leave: []; cancelLeave: [] }>()
const id = useId()
const harnesses = [
  { id: 'opencode', name: 'OpenCode', icon: '⌘', description: 'Illustrative multi-provider harness row.', options: [{ value: 'default', label: 'Harness default' }, { value: 'sample-sonnet', label: 'Claude Sonnet · sample' }, { value: 'sample-opus', label: 'Claude Opus · sample' }, { value: 'sample-codex', label: 'GPT Codex · sample' }, { value: 'sample-gemini', label: 'Gemini Pro · sample' }] },
  { id: 'claude', name: 'Claude Code', icon: '›_', description: 'Illustrative Claude Code model choices.', options: [{ value: 'default', label: 'Harness default' }, { value: 'sample-sonnet', label: 'Sonnet · sample' }, { value: 'sample-opus', label: 'Opus · sample' }, { value: 'sample-haiku', label: 'Haiku · sample' }] },
  { id: 'codex', name: 'Codex', icon: '▣', description: 'Illustrative Codex model choices.', options: [{ value: 'default', label: 'Harness default' }, { value: 'sample-codex', label: 'GPT Codex · sample' }] },
]
const baseline: Record<string, string> = { opencode: 'sample-sonnet', claude: 'sample-sonnet', codex: 'default' }
const draft = ref({ ...baseline })
if (new URLSearchParams(location.search).get('sample') === 'config') draft.value.opencode = 'sample-opus'
const dirty = computed(changeCount)
const scenario = ref('available')
const error = ref('')
const guard = ref<HTMLDialogElement>()
const scenarios = [{ value: 'available', label: 'Illustrative rows' }, { value: 'unavailable', label: 'Previous OpenCode model unavailable' }]

/**
 * Counts sample assignments changed by id, not by their presentation labels.
 * @returns Number of unsaved page-memory changes.
 */
function changeCount(): number {
  return harnesses.filter((harness): boolean => draft.value[harness.id] !== baseline[harness.id]).length
}

/**
 * Preserves an unavailable previous selection instead of substituting a model silently.
 * @param harnessId Illustrative harness row.
 * @returns Its fixed sample options, never a discovered model catalog.
 */
function options(harnessId: string): { value: string; label: string; disabled?: boolean }[] {
  return harnesses.find((harness): boolean => harness.id === harnessId)!.options.map((option): { value: string; label: string; disabled: boolean } => ({ ...option, disabled: scenario.value === 'unavailable' && harnessId === 'opencode' && option.value === 'sample-sonnet' }))
}

/**
 * Labels baseline and default choices without claiming any real saved preference exists.
 * @param harnessId Sample harness identity.
 * @returns Associated previous/default/unavailable guidance.
 */
function hint(harnessId: string): string {
  const harness = harnesses.find((item): boolean => item.id === harnessId)!
  const previous = harness.options.find((option): boolean => option.value === baseline[harnessId])!.label
  const changed = draft.value[harnessId] !== baseline[harnessId]
  return `${changed ? `Previously (sample): ${previous}. ` : ''}${draft.value[harnessId] === 'default' ? 'Follow this harness’s default for new work; not resolved in this preview. ' : ''}${scenario.value === 'unavailable' && harnessId === 'opencode' ? 'Previous model unavailable; value retained. Choose another sample choice or Harness default. No discovery or fallback ran.' : ''}`
}

/**
 * Explicitly discards draft assignments to their original sample baseline, without writing settings.
 */
function discard(): void {
  draft.value = { ...baseline }
  error.value = ''
}

/**
 * Exercises a labeled save-error state without making a settings request or updating the baseline.
 */
function previewFailure(): void {
  if (!dirty.value) return
  error.value = 'Sample save failure. No save was attempted. Draft values are retained and previous sample choices are unchanged. Real retry is unavailable.'
}

/**
 * Opens the same dirty-state guard for global links and the stored-origin return action.
 */
function confirmLeave(): void {
  if (!guard.value?.open) guard.value?.showModal()
}

/**
 * Keeps all settings edits and restores the native dialog trigger's focus.
 */
function keepEditing(): void {
  guard.value?.close()
  emit('cancelLeave')
}

/**
 * Discards only explicit settings edits before honoring a pending destination.
 */
function discardAndLeave(): void {
  discard()
  guard.value?.close()
  emit('leave')
}
defineExpose({ dirty, confirmLeave })
</script>

<template>
  <main class="configuration-preview" tabindex="-1" aria-label="Personal configuration preview">
    <div class="configuration-content">
      <a class="configuration-back" :href="`?preview=${encodeURIComponent(originId)}`" @click="emit('navigate', originId, $event)">← {{ originId === 'library' ? 'Back to all PRs' : `Back to review · ${originLabel}` }}</a>
      <header class="configuration-title"><p>Personal preferences · UI preview</p><h1>Configuration</h1><p>Choose an illustrative model for each sample harness. Nothing here changes real harness settings.</p></header>
      <form @submit.prevent>
        <section class="harness-panel" :aria-labelledby="`${id}-harness-title`">
          <header><h2 :id="`${id}-harness-title`">Harness models</h2><p>Three fictional catalog rows · No discovery</p></header>
          <div v-for="harness in harnesses" :key="harness.id" class="harness-row" :class="{ 'is-changed': draft[harness.id] !== baseline[harness.id] }">
            <div class="harness-identity"><span class="harness-icon" aria-hidden="true">{{ harness.icon }}</span><div><h3>{{ harness.name }}</h3><p>{{ harness.description }}</p></div></div>
            <div class="harness-assignment"><div class="harness-state"><UiBadge v-if="draft[harness.id] !== baseline[harness.id]" tone="info">Changed</UiBadge><UiBadge v-if="scenario === 'unavailable' && harness.id === 'opencode'" tone="warning">Previous model unavailable</UiBadge></div><UiField v-model="draft[harness.id]" :label="`Model for ${harness.name}`" type="select" :options="options(harness.id)" :hint="hint(harness.id)" /></div>
          </div>
        </section>
        <section class="configuration-scope"><h2>Existing work keeps its model.</h2><p>In the design contract, defaults apply to new PR-revision question sessions, quizzes and semantic review generation jobs. New threads on an existing revision reuse that revision’s shared session and model. Existing conversations, running turns, quizzes and semantic groups are never rebound.</p><p>This preview chooses no active harness, starts no work and saves no defaults.</p></section>
        <div class="configuration-save-bar"><p role="status">{{ dirty ? `${dirty} unsaved sample ${dirty === 1 ? 'change' : 'changes'}` : 'No sample changes' }}</p><div><UiButton :disabled="!dirty" @click="discard">Discard changes</UiButton><UiButton variant="primary" disabled :aria-describedby="`${id}-save-reason`">Save changes — unavailable</UiButton></div></div>
        <p :id="`${id}-save-reason`" class="configuration-footnote">{{ dirty ? 'Draft changes are page-memory only.' : 'There are no draft changes.' }} Real saving, model discovery and availability validation are not implemented. These rows and labels are not proof of installed harnesses, model access or canonical model IDs. Reload clears edits; navigation never saves implicitly. Credentials and harness setup remain outside this page.</p>
        <p v-if="error" role="alert" class="configuration-error">{{ error }}</p>
      </form>
      <details class="configuration-tools"><summary>Configuration preview controls</summary><UiField v-model="scenario" label="Configuration availability preview" type="select" :options="scenarios" /><UiButton :disabled="!dirty" @click="previewFailure">Preview save failure</UiButton></details>
    </div>
    <dialog ref="guard" class="configuration-leave-dialog" :aria-labelledby="`${id}-leave-title`" @cancel="emit('cancelLeave')"><h2 :id="`${id}-leave-title`">Keep your configuration edits?</h2><p>These sample changes have not been saved. Keep editing, or explicitly discard them before leaving. Existing PR drafts, threads and quiz attempts are not discarded.</p><div><UiButton autofocus @click="keepEditing">Keep editing</UiButton><UiButton @click="discardAndLeave">Discard and leave</UiButton></div></dialog>
  </main>
</template>

<style scoped>
.configuration-preview { flex: 1; min-width: 0; overflow: auto; }
.configuration-content { width: 100%; max-width: 64rem; margin-inline: auto; padding: var(--space-5) var(--space-8) 2.5rem; }
.configuration-back { display: inline-flex; align-items: center; min-height: var(--target-control); color: var(--color-muted); font-size: var(--text-small); text-decoration: none; }
.configuration-back:hover { color: var(--color-accent); }
.configuration-title { margin-top: var(--space-4); }
.configuration-title > p:first-child { font-size: var(--text-small); }
.configuration-title p { color: var(--color-muted); }
.configuration-title h1 { margin-top: var(--space-1); font-size: 1.5rem; line-height: 1.4; letter-spacing: -0.5px; font-weight: 500; }
.configuration-title > p:last-child { margin-top: var(--space-2); }
.harness-panel { margin-top: var(--space-6); border: 1px solid var(--color-border); border-radius: var(--radius-panel); overflow: hidden; background: var(--color-panel); }
.harness-panel > header { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); min-height: 4rem; padding: var(--space-4) var(--space-6); border-bottom: 1px solid var(--color-border); }
.harness-panel h2 { font-size: var(--text-prose); font-weight: 500; }
.harness-panel > header p { color: var(--color-muted); font-size: var(--text-small); }
.harness-row { display: grid; grid-template-columns: minmax(0, 1fr) 22.5rem; align-items: center; gap: 1.75rem; padding: var(--space-5) var(--space-6); border-left: 2px solid transparent; }
.harness-row + .harness-row { border-top: 1px solid var(--color-border); }
.harness-row.is-changed { border-left-color: var(--color-accent); }
.harness-identity { display: flex; align-items: center; gap: var(--space-4); min-width: 0; }
.harness-icon { display: grid; place-items: center; flex-shrink: 0; width: 2.625rem; height: 2.625rem; border: 1px solid var(--color-border); border-radius: var(--radius-control); color: var(--color-muted); background: var(--color-canvas); }
.harness-identity h3 { font-size: 1rem; font-weight: 500; }
.harness-identity p { margin-top: var(--space-1); font-size: var(--text-small); color: var(--color-muted); }
.harness-assignment { min-width: 0; }
.harness-state { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.harness-state:has(> *) { margin-bottom: var(--space-2); }
.configuration-scope { margin-top: var(--space-6); padding: var(--space-4); border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--color-panel); font-size: var(--text-small); }
.configuration-scope h2 { font-size: var(--text-small); font-weight: 550; }
.configuration-scope p { margin-top: var(--space-2); color: var(--color-muted); }
.configuration-save-bar { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-4); margin-top: var(--space-6); padding-top: var(--space-6); border-top: 1px solid var(--color-border); }
.configuration-save-bar > div { display: flex; flex-wrap: wrap; gap: var(--space-2); }
.configuration-save-bar p, .configuration-footnote { font-size: var(--text-small); color: var(--color-muted); }
.configuration-footnote { margin-top: var(--space-4); }
.configuration-error { margin-top: var(--space-4); color: var(--color-error); }
.configuration-tools { margin-top: var(--space-5); font-size: var(--text-small); color: var(--color-muted); }
.configuration-tools summary { min-height: var(--target-control); cursor: pointer; }
.configuration-tools .ui-field { margin-block: var(--space-3); }
.configuration-leave-dialog { width: min(34rem, calc(100% - 2rem)); padding: var(--space-6); border: 1px solid var(--color-border-strong); border-radius: var(--radius-panel); color: var(--color-text); background: var(--color-panel); }
.configuration-leave-dialog::backdrop { background: #0009; }
.configuration-leave-dialog h2 { font-size: var(--text-heading); font-weight: 500; }
.configuration-leave-dialog p { margin-block: var(--space-4); }
.configuration-leave-dialog > div { display: flex; flex-wrap: wrap; gap: var(--space-2); }
@container application (max-width: 62.5rem) { .configuration-content { padding-inline: var(--space-6); } .harness-row { grid-template-columns: minmax(0, 1fr) 18.75rem; gap: var(--space-5); } }
@container application (max-width: 47.5rem) { .harness-row { grid-template-columns: minmax(0, 1fr); } }
</style>
