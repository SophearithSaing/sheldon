import type { PinnedRevision } from './source'

export type QuestionAnchor = { kind: 'hunk'; fileID: string; hunkID: string } | { kind: 'lines'; fileID: string; hunkID: string; side: 'old' | 'new'; start: number; end: number }
export interface InitialQuestion { revision: PinnedRevision; anchor: QuestionAnchor; question: string }
export interface QuestionAnswer {
  answerID: string
  threadID: string
  prID: string
  revisionID: string
  question: string
  text: string
  state: 'pending' | 'streaming' | 'completed' | 'failed' | 'interrupted'
  stopRequested: boolean
  executionUnknown: boolean
  reference: { revisionID: string; anchor: QuestionAnchor; oldPath?: string; newPath?: string }
}

export class QuestionAdmissionError extends Error {
  /**
   * Keeps uncertain delivery distinct from a rejected request without exposing diagnostics.
   * @param message Safe reader-facing explanation.
   * @param uncertain Whether admission might already have started execution.
   */
  constructor(message: string, public readonly uncertain: boolean) { super(message) }
}

/**
 * Validates the literal question's UTF-8 byte bound before any request.
 * @param text Unmodified question text.
 * @returns Validation feedback, or an empty string for usable input.
 */
export function questionError(text: string): string {
  if (!text.trim()) return 'Enter a question before sending.'
  if (text.includes('\0') || new TextEncoder().encode(text).length > 16384) return 'Use at most 16,384 UTF-8 bytes without NUL characters.'
  return ''
}

/**
 * Compares exact immutable selection coordinates without depending on object key order.
 * @param left First pinned selection.
 * @param right Second pinned selection.
 * @returns Whether both selections target the same source.
 */
export function sameAnchor(left: QuestionAnchor, right: QuestionAnchor): boolean {
  return left.kind === right.kind && left.fileID === right.fileID && left.hunkID === right.hunkID &&
    (left.kind === 'hunk' || right.kind === 'lines' && left.side === right.side && left.start === right.start && left.end === right.end)
}

/**
 * Checks a response against its submitting request before creating any visible thread.
 * @param value Decoded public answer snapshot.
 * @param prID Routed PR identity.
 * @param revisionID Exact comparison identity.
 * @param input Original literal question and selection.
 * @returns Scoped answer snapshot.
 */
export function parseQuestionAnswer(value: unknown, prID: string, revisionID: string, input: InitialQuestion): QuestionAnswer {
  const answer = value as Partial<QuestionAnswer> | null
  if (!answer || typeof answer !== 'object' || !/^[a-f0-9]{64}$/.test(answer.answerID ?? '') || /^0+$/.test(answer.answerID ?? '') || !/^[a-f0-9]{64}$/.test(answer.threadID ?? '') || /^0+$/.test(answer.threadID ?? '') ||
      answer.prID !== prID || answer.revisionID !== revisionID || answer.question !== input.question || typeof answer.text !== 'string' ||
      new TextEncoder().encode(answer.text).length > 262144 || !['pending', 'streaming', 'completed', 'failed', 'interrupted'].includes(answer.state ?? '') ||
      typeof answer.stopRequested !== 'boolean' || typeof answer.executionUnknown !== 'boolean' || answer.reference?.revisionID !== revisionID ||
      !answer.reference.anchor || !sameAnchor(input.anchor, answer.reference.anchor)) throw new QuestionAdmissionError('Unexpected admission response. Delivery may be uncertain; do not resend automatically.', true)
  return answer as QuestionAnswer
}
