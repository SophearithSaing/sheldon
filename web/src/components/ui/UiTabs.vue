<script setup lang="ts">
const props = defineProps<{
  id: string
  label: string
  items: readonly { id: string; label: string }[]
}>()
const selected = defineModel<string>({ required: true })

function navigate(event: KeyboardEvent, index: number) {
  let next = index
  switch (event.key) {
    case 'ArrowRight':
      next = (index + 1) % props.items.length
      break
    case 'ArrowLeft':
      next = (index - 1 + props.items.length) % props.items.length
      break
    case 'Home':
      next = 0
      break
    case 'End':
      next = props.items.length - 1
      break
    default:
      return
  }
  event.preventDefault()
  const item = props.items[next]
  if (!item) return
  selected.value = item.id
  const button = event.currentTarget as HTMLButtonElement
  button.parentElement?.querySelectorAll<HTMLButtonElement>('[role="tab"]')[next]?.focus()
}
</script>

<template>
  <div class="ui-tabs" role="tablist" :aria-label="label">
    <button
      v-for="(item, index) in items"
      :id="`${id}-tab-${item.id}`"
      :key="item.id"
      type="button"
      role="tab"
      :aria-selected="selected === item.id"
      :aria-controls="`${id}-panel-${item.id}`"
      :tabindex="selected === item.id ? 0 : -1"
      @click="selected = item.id"
      @keydown="navigate($event, index)"
    >
      {{ item.label }}
    </button>
  </div>
</template>

<style scoped>
.ui-tabs {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-6);
  min-width: 0;
}
.ui-tabs button {
  min-height: 3.25rem;
  padding: var(--space-2) var(--space-1);
  color: var(--color-muted);
  border: 0;
  border-bottom: 2px solid transparent;
  background: transparent;
}
.ui-tabs button:hover {
  color: var(--color-text);
}
.ui-tabs button[aria-selected='true'] {
  color: var(--color-text);
  border-bottom-color: var(--color-accent);
  font-weight: 600;
}
</style>
