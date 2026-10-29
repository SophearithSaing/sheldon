<script setup lang="ts">
withDefaults(
  defineProps<{
    variant?: 'primary' | 'secondary' | 'quiet'
    type?: 'button' | 'submit' | 'reset'
    disabled?: boolean
  }>(),
  { variant: 'secondary', type: 'button', disabled: false },
)

const emit = defineEmits<{ click: [event: MouseEvent] }>()
</script>

<template>
  <button
    class="ui-button"
    :class="`ui-button--${variant}`"
    :type="disabled ? 'button' : type"
    :aria-disabled="disabled || undefined"
    @click="disabled ? $event.preventDefault() : emit('click', $event)"
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
  padding: var(--space-2) var(--space-4);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
  background: var(--color-panel);
  font-size: var(--text-small);
  font-weight: 550;
  line-height: 1.5;
  text-align: center;
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
.ui-button[aria-disabled] {
  cursor: not-allowed;
  color: var(--color-muted);
  border-color: var(--color-border-strong);
  background: var(--color-panel);
}
</style>
