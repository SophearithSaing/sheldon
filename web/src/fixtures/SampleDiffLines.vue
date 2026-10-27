<script setup lang="ts">
import type { DiffLine } from './workspace'

defineProps<{ lines: DiffLine[]; label: string }>()
</script>

<template>
  <section class="sample-source" :aria-label="label" tabindex="0">
    <table class="diff-table">
      <caption class="sr-only">
        Sample unified diff. Plus and minus signs identify additions and removals.
      </caption>
      <thead class="sr-only">
        <tr>
          <th>Base</th>
          <th>Head</th>
          <th>Change</th>
          <th>Source</th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(line, index) in lines" :key="index" :class="`diff-line--${line.kind}`">
          <td class="line-number" aria-hidden="true">{{ line.base }}</td>
          <td class="line-number" aria-hidden="true">{{ line.head }}</td>
          <td class="line-sign">
            {{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : '' }}
          </td>
          <td class="line-source">
            <code>{{ line.text || ' ' }}</code>
          </td>
        </tr>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.sample-source {
  position: relative;
  min-width: 0;
  overflow-x: auto;
  overscroll-behavior-x: contain;
}
.sample-source:focus-visible {
  outline-offset: -3px;
}
.diff-table {
  width: 100%;
  border-collapse: collapse;
  font-size: var(--text-body);
  line-height: var(--line-code);
}
.diff-table td {
  padding-block: 0;
  vertical-align: top;
}
.line-number {
  width: 4ch;
  min-width: 4ch;
  padding-inline: var(--space-1);
  color: var(--color-faint);
  text-align: right;
  user-select: none;
}
.line-sign {
  width: 3ch;
  min-width: 3ch;
  color: var(--color-muted);
  text-align: center;
  user-select: none;
}
.line-source {
  padding-inline: var(--space-2) var(--space-4);
  white-space: pre;
}
.diff-line--added {
  background: var(--color-success-soft);
}
.diff-line--removed {
  background: var(--color-error-soft);
}
</style>
