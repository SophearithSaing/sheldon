<script setup lang="ts">
import type { DiffLine, LineRange } from './diff'

const props = withDefaults(defineProps<{
  lines: readonly DiffLine[]
  label: string
  selection: LineRange | null
  highlight?: boolean
  selectable?: boolean
  real?: boolean
}>(), { selectable: true })
const emit = defineEmits<{ select: [side: 'base' | 'head', line: number, extend: boolean] }>()

/**
 * Checks a displayed line against the explicitly selected side and coordinates.
 * @param line Displayed source line.
 * @returns Whether the line belongs to the selected range.
 */
function selected(line: DiffLine): boolean {
  const range = props.selection
  if (!range) return false
  const number = line[range.side]
  return number !== null && number >= range.start && number <= range.end
}

/**
 * Splits sample source into escaped text spans without generating HTML.
 * @param text Literal source text.
 * @returns Original text fragments with restrained syntax roles.
 */
function tokens(text: string): { text: string; tone: string }[] {
  if (props.highlight === false) return [{ text, tone: '' }]
  const pattern = /\/\/.*$|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|\b(?:export|function|return|if|const|let|null|async|await|true|false|throw|new)\b|[A-Za-z_$][\w$]*(?=\()/g
  const result: { text: string; tone: string }[] = []
  let offset = 0
  for (const match of text.matchAll(pattern)) {
    if (match.index > offset) result.push({ text: text.slice(offset, match.index), tone: '' })
    const value = match[0]
    const tone = value.startsWith('//') ? 'comment' : /^["']/.test(value) ? '' :
      /^(export|function|return|if|const|let|null|async|await|true|false|throw|new)$/.test(value) ? 'keyword' : 'function'
    result.push({ text: value, tone })
    offset = match.index + value.length
  }
  if (offset < text.length) result.push({ text: text.slice(offset), tone: '' })
  return result
}
</script>

<template>
  <section class="sample-source" :aria-label="label" tabindex="0">
    <table class="diff-table">
      <caption class="sr-only">
        {{ real ? 'Unified diff.' : 'Sample unified diff.' }} Plus and minus signs identify additions and removals.
      </caption>
      <thead class="sr-only">
        <tr>
          <th>{{ real ? 'Merge base' : 'Base' }}</th>
          <th>Head</th>
          <th>Change</th>
          <th>Source</th>
        </tr>
      </thead>
      <tbody>
        <template v-for="(line, index) in lines" :key="line.id ?? index">
          <tr v-if="line.hunkHeader" class="diff-omission"><td colspan="4">{{ line.hunkHeader }}</td></tr>
          <tr :class="[`diff-line--${line.kind}`, { 'is-selected': selected(line) }]">
            <td class="line-number">
              <button v-if="line.base !== null && selectable !== false" type="button" :data-line="line.base" data-side="base" :aria-label="`Select base line ${line.base}`" :aria-pressed="selected(line) && selection?.side === 'base'" @keydown.shift.enter.prevent="emit('select', 'base', line.base, true)" @click="emit('select', 'base', line.base, $event.shiftKey)">{{ line.base }}</button><span v-else>{{ line.base }}</span>
            </td>
            <td class="line-number">
              <button v-if="line.head !== null && selectable !== false" type="button" :data-line="line.head" data-side="head" :aria-label="`Select head line ${line.head}`" :aria-pressed="selected(line) && selection?.side === 'head'" @keydown.shift.enter.prevent="emit('select', 'head', line.head, true)" @click="emit('select', 'head', line.head, $event.shiftKey)">{{ line.head }}</button><span v-else>{{ line.head }}</span>
            </td>
            <td class="line-sign">
              {{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : '' }}
            </td>
            <td class="line-source">
              <code><span v-for="(token, tokenIndex) in tokens(line.text || (real ? '' : ' '))" :key="tokenIndex" :class="token.tone ? `syntax-${token.tone}` : undefined">{{ token.text }}</span></code>
            </td>
          </tr>
          <tr v-if="line.noNewline" class="diff-omission"><td colspan="4">\ No newline at end of file</td></tr>
        </template>
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
  font-family: var(--font-code);
  font-size: var(--text-code);
  line-height: var(--line-code);
}
.diff-table td {
  padding-block: 0;
  vertical-align: top;
}
.diff-omission td { padding: var(--space-3); border-block: 1px solid var(--color-border); color: var(--color-muted); font-size: var(--text-code-meta); white-space: normal; }
.line-number {
  font-size: var(--text-code-meta);
  width: 4ch;
  min-width: 4ch;
  padding-inline: var(--space-1);
  color: var(--color-faint);
  text-align: right;
  user-select: none;
}
.line-number button {
  display: block;
  width: 100%;
  min-height: var(--line-code);
  padding: 0;
  border: 0;
  background: transparent;
  color: inherit;
  font: inherit;
  text-align: right;
}
.line-number button:hover { background: var(--color-accent-soft); color: var(--color-accent); }
.line-number button:focus-visible { outline-offset: -2px; }
.syntax-keyword { color: var(--color-code-keyword); }
.syntax-function { color: var(--color-code-function); }
.syntax-comment { color: var(--color-code-comment); }
.diff-table tr.is-selected { background: var(--color-accent-soft); outline: 1px solid var(--color-accent-border); outline-offset: -1px; }
.is-selected .line-number { color: var(--color-accent); }
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
