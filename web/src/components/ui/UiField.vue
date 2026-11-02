<script setup lang="ts">
import { useAttrs, useId } from 'vue'

defineOptions({ inheritAttrs: false })
const props = withDefaults(
  defineProps<{
    label: string
    hint?: string
    error?: string
    multiline?: boolean
    type?: 'text' | 'search' | 'select'
    options?: readonly { value: string; label: string; disabled?: boolean }[]
    placeholder?: string
    disabled?: boolean
    busy?: boolean
  }>(),
  { multiline: false, type: 'text', disabled: false, busy: false },
)
const model = defineModel<string>({ default: '' })
const id = useId()
const attrs = useAttrs()

/**
 * Combines caller descriptions with field hints and validation feedback.
 * @returns Description IDs, or undefined when no descriptions exist.
 */
function descriptions(): string | undefined {
  const external = typeof attrs['aria-describedby'] === 'string' ? attrs['aria-describedby'] : ''
  const ids = [external, props.hint ? `${id}-hint` : '', props.error ? `${id}-error` : '']
  return ids.filter(Boolean).join(' ') || undefined
}
</script>

<template>
  <div class="ui-field">
    <label :for="id">{{ label }}</label>
    <select
      v-if="type === 'select'"
      :id="id"
      v-model="model"
      v-bind="$attrs"
      :disabled="disabled || busy"
      :aria-busy="busy || undefined"
      :aria-invalid="error ? true : undefined"
      :aria-describedby="descriptions()"
    >
      <option
        v-for="option in options"
        :key="option.value"
        :value="option.value"
        :disabled="option.disabled"
      >
        {{ option.label }}
      </option>
    </select>
    <textarea
      v-else-if="multiline"
      :id="id"
      v-model="model"
      v-bind="$attrs"
      rows="3"
      :placeholder="placeholder"
      :disabled="disabled || busy"
      :aria-busy="busy || undefined"
      :aria-invalid="error ? true : undefined"
      :aria-describedby="descriptions()"
    />
    <input
      v-else
      :id="id"
      v-model="model"
      v-bind="$attrs"
      :type="type"
      :placeholder="placeholder"
      :disabled="disabled || busy"
      :aria-busy="busy || undefined"
      :aria-invalid="error ? true : undefined"
      :aria-describedby="descriptions()"
    />
    <p v-if="hint" :id="`${id}-hint`" class="ui-field__hint">{{ hint }}</p>
    <p v-if="error" :id="`${id}-error`" class="ui-field__error" aria-live="polite">{{ error }}</p>
  </div>
</template>

<style scoped>
.ui-field {
  display: grid;
  gap: var(--space-2);
  min-width: 0;
  font-size: var(--text-small);
}
.ui-field label {
  font-weight: 550;
}
.ui-field input,
.ui-field textarea,
.ui-field select {
  width: 100%;
  min-width: 0;
  min-height: var(--target-control);
  padding: var(--space-3);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-canvas);
  font-size: var(--text-body);
  line-height: 1.6;
}
.ui-field select {
  min-height: 3rem;
  font-size: var(--text-prose);
}
.ui-field :is(input, textarea, select):hover:not(:disabled) {
  border-color: var(--color-border-strong);
}
.ui-field :is(input, textarea, select)[aria-invalid='true'] {
  border-color: var(--color-error);
}
.ui-field :is(input, textarea, select):disabled {
  opacity: 1;
  color: var(--color-muted);
  cursor: not-allowed;
}
.ui-field [aria-busy='true'] {
  cursor: progress;
}
.ui-field__error {
  color: var(--color-error);
}
.ui-field textarea {
  min-height: 3.5rem;
  resize: vertical;
}
.ui-field input::placeholder,
.ui-field textarea::placeholder,
.ui-field__hint {
  color: var(--color-muted);
}
</style>
