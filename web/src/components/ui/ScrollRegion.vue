<script setup lang="ts">
defineProps<{ label: string }>()
</script>

<template>
  <section class="scroll-region" :aria-label="label" tabindex="0"><slot /></section>
</template>

<style scoped>
.scroll-region {
  min-width: 0;
  min-height: 0;
  overflow: auto;
  overscroll-behavior: contain;
  scrollbar-gutter: stable;
}
.scroll-region:focus-visible {
  outline-offset: -3px;
}
</style>
