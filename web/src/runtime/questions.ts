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
  failureCode?: 'source-unavailable' | 'execution-failed' | 'answer-too-large' | 'execution-uncertain'
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
export function parseQuestionAnswer(value: unknown, prID: string, revisionID: string, input: Pick<InitialQuestion, 'question' | 'anchor'>): QuestionAnswer {
  const answer = value as Partial<QuestionAnswer> | null
  if (!answer || typeof answer !== 'object' || !/^[a-f0-9]{64}$/.test(answer.answerID ?? '') || /^0+$/.test(answer.answerID ?? '') || !/^[a-f0-9]{64}$/.test(answer.threadID ?? '') || /^0+$/.test(answer.threadID ?? '') ||
      answer.prID !== prID || answer.revisionID !== revisionID || answer.question !== input.question || typeof answer.text !== 'string' ||
      new TextEncoder().encode(answer.text).length > 262144 || answer.text.includes('\0') || !['pending', 'streaming', 'completed', 'failed', 'interrupted'].includes(answer.state ?? '') ||
      typeof answer.stopRequested !== 'boolean' || typeof answer.executionUnknown !== 'boolean' || answer.reference?.revisionID !== revisionID ||
      answer.state === 'completed' && (!answer.text.trim() || answer.executionUnknown) ||
      answer.failureCode !== undefined && !['source-unavailable', 'execution-failed', 'answer-too-large', 'execution-uncertain'].includes(answer.failureCode) ||
      !answer.reference.anchor || !sameAnchor(input.anchor, answer.reference.anchor)) throw new QuestionAdmissionError('Unexpected admission response. Delivery may be uncertain; do not resend automatically.', true)
  return {
    answerID: answer.answerID!, threadID: answer.threadID!, prID, revisionID, question: input.question,
    text: answer.text, state: answer.state!, stopRequested: answer.stopRequested, executionUnknown: answer.executionUnknown,
    failureCode: answer.failureCode, reference: { revisionID, anchor: { ...input.anchor } },
  }
}

/**
 * Checks an observed snapshot against the exact admitted answer, not just its thread.
 * @param value Decoded snapshot.
 * @param original Admitted answer identity and selection.
 * @returns Validated cumulative snapshot.
 */
export function parseObservedAnswer(value: unknown, original: QuestionAnswer): QuestionAnswer {
  const answer = parseQuestionAnswer(value, original.prID, original.revisionID, { question: original.question, anchor: original.reference.anchor })
  if (answer.answerID !== original.answerID || answer.threadID !== original.threadID) throw new Error('Answer scope mismatch.')
  return answer
}

/**
 * Separates in-progress presentation from known terminal outcomes and unknown execution.
 * @param answer Public answer snapshot.
 * @returns Whether native execution is still reported in progress.
 */
export function activeAnswer(answer: QuestionAnswer): boolean { return answer.state === 'pending' || answer.state === 'streaming' }

/**
 * Bounds decoded JSON bytes while reading, rather than after allocating an unbounded body.
 * @param response Finite authenticated HTTP response.
 * @param limit Maximum UTF-8 response size in bytes.
 * @returns Decoded JSON value, subject to the caller's scope validation.
 */
export async function readBoundedJSON(response: Response, limit: number): Promise<unknown> {
  if (!response.body) throw new Error('Missing JSON response.')
  const reader = response.body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let bytes = 0
  let text = ''
  try {
    while (true) {
      const chunk = await reader.read()
      bytes += chunk.value?.byteLength ?? 0
      if (bytes > limit) throw new Error('JSON response exceeds limits.')
      text += decoder.decode(chunk.value, { stream: !chunk.done })
      if (chunk.done) return JSON.parse(text)
    }
  } finally {
    await reader.cancel().catch((): void => {})
    reader.releaseLock()
  }
}

/**
 * Reads bounded fetch/SSE snapshots without replay, prompt dispatch or text concatenation.
 * @param response Authenticated events response.
 * @param original Original admission snapshot used for exact routing.
 * @param emit Receives validated cumulative snapshots.
 */
export async function readAnswerEvents(response: Response, original: QuestionAnswer, emit: (answer: QuestionAnswer) => void): Promise<void> {
  if (!response.ok || response.headers.get('content-type')?.split(';')[0] !== 'text/event-stream' || !response.body) {
    await response.body?.cancel().catch((): void => {})
    throw new Error('Original answer observation is unavailable. No prompt was resent.')
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder('utf-8', { fatal: true })
  let buffer = ''
  let data = ''
  let event = ''
  let frameSize = 0
  try {
    while (true) {
      const chunk = await reader.read()
      buffer += decoder.decode(chunk.value, { stream: !chunk.done })
      let newline = buffer.indexOf('\n')
      while (newline >= 0) {
        const line = buffer.slice(0, newline).replace(/\r$/, '')
        buffer = buffer.slice(newline + 1)
        frameSize += new TextEncoder().encode(line).length + 1
        if (frameSize > 2 * 1024 * 1024) throw new Error('Answer frame exceeds limits.')
        if (line === '') {
          if (data) {
            if (event !== 'answer') throw new Error('Unexpected answer event.')
            const answer = parseObservedAnswer(JSON.parse(data), original)
            emit(answer)
            if (!activeAnswer(answer)) return
          }
          data = ''; event = ''; frameSize = 0
        } else if (line.startsWith('data:')) data += (data ? '\n' : '') + line.slice(5).replace(/^ /, '')
        else if (line.startsWith('event:')) event = line.slice(6).trim()
        else if (!line.startsWith(':')) throw new Error('Unexpected answer framing.')
        newline = buffer.indexOf('\n')
      }
      if (new TextEncoder().encode(buffer).length + frameSize > 2 * 1024 * 1024) throw new Error('Answer frame exceeds limits.')
      if (chunk.done) throw new Error('Answer observation ended before a terminal snapshot. Execution may still be active.')
    }
  } finally {
    await reader.cancel().catch((): void => {})
    reader.releaseLock()
  }
}
