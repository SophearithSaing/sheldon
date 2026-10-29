<script setup lang="ts">
import { useId } from 'vue'

defineOptions({ inheritAttrs: false })
withDefaults(
  defineProps<{
    label: string
    hint?: string
    multiline?: boolean
    type?: 'text' | 'search'
    placeholder?: string
  }>(),
  { multiline: false, type: 'text' },
)
const model = defineModel<string>({ default: '' })
const id = useId()
</script>

<template>
  <div class="ui-field">
    <label :for="id">{{ label }}</label>
    <textarea
      v-if="multiline"
      :id="id"
      v-model="model"
      v-bind="$attrs"
      rows="3"
      :placeholder="placeholder"
      :aria-describedby="hint ? `${id}-hint` : undefined"
    />
    <input
      v-else
      :id="id"
      v-model="model"
      v-bind="$attrs"
      :type="type"
      :placeholder="placeholder"
      :aria-describedby="hint ? `${id}-hint` : undefined"
    />
    <p v-if="hint" :id="`${id}-hint`" class="ui-field__hint">{{ hint }}</p>
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
.ui-field textarea {
  width: 100%;
  min-width: 0;
  min-height: var(--target-control);
  padding: var(--space-3);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
  background: var(--color-canvas);
  font-size: var(--text-body);
  line-height: 1.7;
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
