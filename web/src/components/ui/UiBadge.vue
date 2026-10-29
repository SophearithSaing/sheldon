<script setup lang="ts">
withDefaults(
  defineProps<{
    tone?: 'neutral' | 'info' | 'success' | 'warning' | 'error'
  }>(),
  { tone: 'neutral' },
)
</script>

<template>
  <span class="ui-badge" :class="`ui-badge--${tone}`"><slot /></span>
</template>

<style scoped>
.ui-badge {
  display: inline-flex;
  align-items: center;
  padding: var(--space-1) var(--space-2);
  border-radius: var(--radius-pill);
  font-size: var(--text-small);
  font-weight: 550;
  color: var(--color-muted);
  background: var(--color-surface);
}
.ui-badge--info {
  color: var(--color-accent);
  background: var(--color-accent-soft);
}
.ui-badge--success {
  color: var(--color-success);
  background: var(--color-success-soft);
}
.ui-badge--warning {
  color: var(--color-warning);
  background: var(--color-warning-soft);
}
.ui-badge--error {
  color: var(--color-error);
  background: var(--color-error-soft);
}
</style>
