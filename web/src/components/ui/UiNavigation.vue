<script setup lang="ts">
defineProps<{
  label: string
  current: string
  items: readonly { id: string; label: string; href: string }[]
}>()

const emit = defineEmits<{ navigate: [id: string, event: MouseEvent] }>()
</script>

<template>
  <nav class="ui-navigation" :aria-label="label">
    <a
      v-for="item in items"
      :key="item.id"
      :href="item.href"
      :aria-current="current === item.id ? 'page' : undefined"
      @click="emit('navigate', item.id, $event)"
    >
      {{ item.label }}
    </a>
  </nav>
</template>

<style scoped>
.ui-navigation {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-width: 0;
  overflow-x: auto;
  scrollbar-gutter: stable;
}
.ui-navigation a {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  flex-shrink: 0;
  min-width: var(--target-control);
  min-height: var(--target-control);
  padding: var(--space-2) var(--space-3);
  border-radius: var(--radius-control);
  color: var(--color-muted);
  text-decoration: none;
}
.ui-navigation a:hover {
  color: var(--color-text);
  background: var(--color-surface);
}
.ui-navigation a:focus-visible {
  outline-offset: -2px;
}
.ui-navigation a[aria-current='page'] {
  color: var(--color-accent);
  background: var(--color-accent-soft);
}
</style>
