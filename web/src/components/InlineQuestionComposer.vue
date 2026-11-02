<script setup lang="ts">
import UiButton from './ui/UiButton.vue'
import UiField from './ui/UiField.vue'

defineProps<{ context: string; error: string; pending: boolean; busy: boolean; heading?: string; unavailableReason?: string }>()
const text = defineModel<string>({ required: true })
const emit = defineEmits<{ submit: []; cancel: []; finish: [] }>()

/**
 * Previews local creation only on the documented shortcut, never during IME input.
 * @param event Keyboard event from the question textarea.
 */
function shortcut(event: KeyboardEvent): void {
  if (event.isComposing || event.key !== 'Enter' || (!event.ctrlKey && !event.metaKey)) return
  event.preventDefault()
  emit('submit')
}
</script>

<template>
  <form class="inline-question" aria-label="New sample question" @submit.prevent="emit('submit')">
    <div class="draft-heading"><h3>{{ heading ?? `Ask about ${context.startsWith('Whole') ? 'this file' : 'these lines'}` }}</h3><span>{{ context }}</span></div>
    <UiField v-model="text" label="Your question" multiline data-draft-input :error="error" :readonly="pending" hint="Ctrl/Cmd+Enter previews creation. Enter inserts a new line; nothing is sent." @keydown="shortcut" />
    <div class="draft-actions">
      <p>New thread · Shared PR-revision context<br />Local preview only. Live agent requests and saving are unavailable.</p>
      <UiButton @click="emit('cancel')">Cancel</UiButton>
      <UiButton v-if="pending" variant="soft" :disabled="busy || !!unavailableReason" @click="emit('finish')">Finish sample creation</UiButton>
      <UiButton type="submit" variant="primary" :busy="pending" :disabled="busy || !!unavailableReason">{{ pending ? 'Sample creation pending…' : 'Preview thread creation' }}</UiButton>
    </div>
    <p v-if="busy" role="status">Another sample question is investigating in this PR revision. Creation is unavailable.</p>
    <p v-if="unavailableReason" role="status">{{ unavailableReason }}</p>
  </form>
</template>

<style scoped>
.inline-question { display: grid; gap: var(--space-3); margin: var(--space-3) var(--space-4); padding: var(--space-4); border: 1px solid var(--color-accent-border); border-left: 2px solid var(--color-accent); border-radius: var(--radius-panel); background: var(--color-panel); font-family: var(--font-ui); }
.draft-heading { display: flex; flex-wrap: wrap; justify-content: space-between; gap: var(--space-2); }
.draft-heading h3 { font-size: var(--text-body); font-weight: 500; }
.draft-heading span { color: var(--color-muted); font: var(--text-code-meta) var(--font-code); }
.draft-actions { display: flex; flex-wrap: wrap; align-items: center; gap: var(--space-2); }
.draft-actions p { flex: 1 1 12rem; color: var(--color-muted); font-size: var(--text-code-meta); line-height: 1.7; }
.inline-question > p { color: var(--color-warning); }
</style>
