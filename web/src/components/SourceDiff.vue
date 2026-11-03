<script setup lang="ts">
import { ref } from 'vue'
import type { DiffLine, LineRange } from './diff'

const props = withDefaults(defineProps<{
  lines: readonly DiffLine[]
  label: string
  selection: LineRange | null
  highlight?: boolean
  selectable?: boolean
  dragSelectable?: boolean
  real?: boolean
  insertAfter?: string
  markedLines?: readonly string[]
  selectedHunk?: string
}>(), { selectable: true })
const emit = defineEmits<{
  select: [side: 'base' | 'head', line: number, extend: boolean]
  drag: [side: 'base' | 'head', origin: number, current: number]
  dragend: [side: 'base' | 'head', origin: number, current: number]
}>()
const dragSelection = ref<{
  pointerId: number
  side: 'base' | 'head'
  origin: number
  current: number
  extend: boolean
  moved: boolean
} | null>(null)

/**
 * Starts an opt-in pointer selection on a line-number gutter.
 * @param event Pointer-down event.
 * @param side Base or head side.
 * @param line Initial line number.
 */
function startDrag(event: PointerEvent, side: 'base' | 'head', line: number): void {
  if (!props.dragSelectable || event.button !== 0) return
  const trigger = event.currentTarget as HTMLButtonElement
  trigger.setPointerCapture(event.pointerId)
  dragSelection.value = { pointerId: event.pointerId, side, origin: line, current: line, extend: event.shiftKey, moved: false }
}

/**
 * Extends an active pointer selection across displayed line-number gutters.
 * @param event Captured pointer-move event.
 */
function moveDrag(event: PointerEvent): void {
  const drag = dragSelection.value
  if (!drag || drag.pointerId !== event.pointerId || drag.extend) return
  const target = document.elementFromPoint(event.clientX, event.clientY)?.closest<HTMLButtonElement>('button[data-line][data-side]')
  if (!target || target.dataset.side !== drag.side) return
  const line = Number(target.dataset.line)
  if (!Number.isInteger(line) || line === drag.current) return
  drag.current = line
  drag.moved = true
  emit('drag', drag.side, drag.origin, line)
}

/**
 * Finishes a pointer selection and emits either a click or a completed range.
 * @param event Captured pointer-up event.
 */
function endDrag(event: PointerEvent): void {
  const drag = dragSelection.value
  if (!drag || drag.pointerId !== event.pointerId) return
  const trigger = event.currentTarget as HTMLButtonElement
  if (trigger.hasPointerCapture(event.pointerId)) trigger.releasePointerCapture(event.pointerId)
  dragSelection.value = null
  if (!drag.moved) emit('select', drag.side, drag.origin, drag.extend)
  else emit('dragend', drag.side, drag.origin, drag.current)
}

/**
 * Clears an interrupted pointer selection without opening a question.
 * @param event Pointer cancellation or capture-loss event.
 */
function cancelDrag(event: PointerEvent): void {
  if (dragSelection.value?.pointerId === event.pointerId) dragSelection.value = null
}

/**
 * Activates a line from a keyboard click, or from pointer input when dragging is disabled.
 * @param event Line-button click event.
 * @param side Base or head side.
 * @param line Selected line number.
 */
function clickLine(event: MouseEvent, side: 'base' | 'head', line: number): void {
  if (props.dragSelectable && event.detail !== 0) return
  emit('select', side, line, event.shiftKey)
}

/**
 * Checks a displayed line against the explicitly selected side and coordinates.
 * @param line Displayed source line.
 * @returns Whether the line belongs to the selected range.
 */
function selected(line: DiffLine): boolean {
  if (props.selectedHunk && line.hunkID === props.selectedHunk) return true
  const range = props.selection
  if (!range) return false
  const number = line[range.side]
  return number !== null && number >= range.start && number <= range.end
}

/**
 * Checks whether a row starts a contiguous selected range within this source block.
 * @param index Displayed line index.
 * @returns Whether the row needs the selection's top border.
 */
function selectionStart(index: number): boolean {
  const line = props.lines[index]
  const previous = props.lines[index - 1]
  return !!line && selected(line) && (!previous || !selected(previous))
}

/**
 * Checks whether a row ends a contiguous selected range within this source block.
 * @param index Displayed line index.
 * @returns Whether the row needs the selection's bottom border.
 */
function selectionEnd(index: number): boolean {
  const line = props.lines[index]
  const next = props.lines[index + 1]
  return !!line && selected(line) && (!next || !selected(next))
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
  <section class="sample-source" :class="{ 'drag-selectable': dragSelectable }" :aria-label="label" tabindex="0">
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
          <tr :class="[`diff-line--${line.kind}`, { 'is-selected': selected(line), 'is-selection-start': selectionStart(index), 'is-selection-end': selectionEnd(index) }]">
            <td class="line-number">
              <button v-if="line.base !== null && selectable !== false" type="button" :data-line="line.base" data-side="base" :aria-label="`Select base line ${line.base}`" :aria-pressed="selected(line) && selection?.side === 'base'" @keydown.shift.enter.prevent="emit('select', 'base', line.base, true)" @pointerdown="startDrag($event, 'base', line.base)" @pointermove="moveDrag" @pointerup="endDrag" @pointercancel="cancelDrag" @lostpointercapture="cancelDrag" @click="clickLine($event, 'base', line.base)">{{ line.base }}</button><span v-else>{{ line.base }}</span>
            </td>
            <td class="line-number">
              <button v-if="line.head !== null && selectable !== false" type="button" :data-line="line.head" data-side="head" :aria-label="`Select head line ${line.head}`" :aria-pressed="selected(line) && selection?.side === 'head'" @keydown.shift.enter.prevent="emit('select', 'head', line.head, true)" @pointerdown="startDrag($event, 'head', line.head)" @pointermove="moveDrag" @pointerup="endDrag" @pointercancel="cancelDrag" @lostpointercapture="cancelDrag" @click="clickLine($event, 'head', line.head)">{{ line.head }}</button><span v-else>{{ line.head }}</span>
            </td>
            <td class="line-sign">
              {{ line.kind === 'added' ? '+' : line.kind === 'removed' ? '−' : '' }}
            </td>
            <td class="line-source">
              <code><span v-for="(token, tokenIndex) in tokens(line.text || (real ? '' : ' '))" :key="tokenIndex" :class="token.tone ? `syntax-${token.tone}` : undefined">{{ token.text }}</span></code>
            </td>
          </tr>
          <tr v-if="line.noNewline" class="diff-omission"><td colspan="4">\ No newline at end of file</td></tr>
          <tr v-if="line.id && (line.id === insertAfter || markedLines?.includes(line.id))" class="diff-insertion"><td colspan="4"><div class="diff-inline"><slot name="inline" :line-id="line.id" /></div></td></tr>
        </template>
      </tbody>
    </table>
  </section>
</template>

<style scoped>
.sample-source {
  container-type: inline-size;
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
.drag-selectable tr.is-selected { outline: 0; }
.drag-selectable tr.is-selected td:first-child { border-left: 1px solid var(--color-accent-border); }
.drag-selectable tr.is-selected td:last-child { border-right: 1px solid var(--color-accent-border); }
.drag-selectable tr.is-selection-start td { border-top: 1px solid var(--color-accent-border); }
.drag-selectable tr.is-selection-end td { border-bottom: 1px solid var(--color-accent-border); }
.diff-inline { position: sticky; left: 0; width: calc(100cqw - 2px); white-space: normal; font-family: var(--font-ui); }
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
