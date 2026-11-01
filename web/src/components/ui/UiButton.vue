<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'quiet' | 'soft'
    type?: 'button' | 'submit' | 'reset'
    disabled?: boolean
    busy?: boolean
  }>(),
  { variant: 'secondary', type: 'button', disabled: false, busy: false },
)

const emit = defineEmits<{ click: [event: MouseEvent] }>()
</script>

<template>
  <button
    class="ui-button"
    :class="`ui-button--${variant}`"
    :type="disabled || busy ? 'button' : type"
    :aria-disabled="disabled || busy || undefined"
    :aria-busy="busy || undefined"
    @click="disabled || busy ? $event.preventDefault() : emit('click', $event)"
  >
    <slot />
  </button>
</template>

<style scoped>
.ui-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: var(--space-2);
  min-height: var(--target-control);
  min-width: var(--target-control);
  padding: var(--space-2) 0.875rem;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-panel);
  font-size: var(--text-body);
  font-weight: 500;
  line-height: 1.5;
  text-align: center;
  overflow-wrap: anywhere;
}
.ui-button:hover:not([aria-disabled]) {
  background: var(--color-canvas);
}
.ui-button--primary {
  color: var(--color-on-accent);
  border-color: var(--color-accent);
  background: var(--color-accent);
}
.ui-button--primary:hover:not([aria-disabled]) {
  background: color-mix(in srgb, var(--color-accent), var(--color-text) 15%);
}
.ui-button--quiet {
  color: var(--color-muted);
  border-color: transparent;
  background: transparent;
}
.ui-button--soft {
  color: var(--color-accent);
  border-color: var(--color-accent-border);
  background: var(--color-accent-soft);
}
.ui-button[aria-pressed='true'] {
  color: var(--color-accent);
  border-color: var(--color-accent-border);
  background: var(--color-accent-soft);
}
.ui-button[aria-pressed='true']:hover:not([aria-disabled]) {
  background: var(--color-accent-soft);
}
.ui-button[aria-disabled] {
  cursor: not-allowed;
  color: var(--color-muted);
  border-color: var(--color-border-strong);
  background: var(--color-panel);
}
.ui-button[aria-busy='true'] {
  cursor: progress;
}
</style>
