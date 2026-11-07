import { historyID } from './history'
import { pinnedID, validSourceHunk, type PinnedRevision, type SourceHunk, type SourceLine } from './source'

export interface QuizQuestion { questionID: string; prompt: string; choices: { choiceID: string; text: string }[] }
export interface PublicQuiz { quizID: string; revision: PinnedRevision; revisionID: string; questions: QuizQuestion[] }
export interface QuizStatus {
  prID: string; generationID: string; quizID: string; revisionID: string; sequence: string
  state: 'pending' | 'generating' | 'validating' | 'saving' | 'completed' | 'failed' | 'cancelled' | 'unknown'
  cancelRequested: boolean; executionUnknown: boolean; failureCode?: string; savedVersion?: string
}
export interface QuizRead { prID: string; version: string; quiz: PublicQuiz | null; generation: QuizStatus | null }
export interface QuizEvidence {
  anchor: { fileID: string; hunkID: string; side: 'old' | 'new'; start: number; end: number }
  oldPath?: string; newPath?: string; selected: SourceLine[]; surrounding?: SourceHunk
}
export interface QuizChecked {
  prID: string; version: string; quizID: string; revisionID: string; questionID: string; choiceID: string
  correct: boolean; correctChoiceID: string; explanation: string; evidence: QuizEvidence[]
}
export interface QuizClient {
  /**
   * Reads the current saved unanswered set without inference.
   * @param id Registered PR.
   * @param signal Request lifetime.
   * @returns Saved set and separate runtime feedback.
   */
  readQuiz(id: string, signal: AbortSignal): Promise<QuizRead>
  /**
   * Admits one explicit generation without automatic retry.
   * @param id Registered PR.
   * @param revision Displayed immutable comparison.
   * @param version Expected saved predecessor.
   * @param signal Request lifetime, not worker lifetime.
   * @returns Admission only, not saved completion.
   */
  generateQuiz(id: string, revision: PinnedRevision, version: string, signal: AbortSignal): Promise<QuizStatus>
  /**
   * Observes the exact retained generation.
   * @param status Previously admitted generation.
   * @param signal Request lifetime.
   * @returns Current scoped feedback.
   */
  readQuizGeneration(status: QuizStatus, signal: AbortSignal): Promise<QuizStatus>
  /**
   * Requests cancellation without promising rollback or idle.
   * @param status Exact retained generation.
   * @param signal Request lifetime.
   * @returns Actual generation feedback.
   */
  cancelQuizGeneration(status: QuizStatus, signal: AbortSignal): Promise<QuizStatus>
  /**
   * Checks one explicit selection against the exact saved set.
   * @param read Saved set identity and version.
   * @param questionID Question within that set.
   * @param choiceID Selected alternative.
   * @param signal Request lifetime.
   * @returns One checked question, never sibling keys.
   */
  checkQuiz(read: QuizRead, questionID: string, choiceID: string, signal: AbortSignal): Promise<QuizChecked>
}

/**
 * Rejects unsupported fields, including accidentally exposed private keys.
 * @param value Decoded object.
 * @param keys Supported field names.
 * @returns Whether only supported fields exist.
 */
function fields(value: unknown, keys: string[]): value is Record<string, unknown> {
  return value !== null && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).every((key: string): boolean => keys.includes(key))
}

/**
 * Checks bounded literal Unicode text without interpreting markup.
 * @param value Candidate text.
 * @param max UTF-8 byte ceiling.
 * @returns Whether the nonblank text is supported.
 */
function text(value: unknown, max: number): value is string {
  return typeof value === 'string' && !!value.trim() && !value.includes('\0') && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value) && new TextEncoder().encode(value).length <= max
}

/**
 * Checks lossless canonical uint64 versions and sequences.
 * @param value Decimal version.
 * @param empty Whether zero is supported.
 * @returns Whether the value is canonical and bounded.
 */
export function quizVersion(value: unknown, empty = true): value is string {
  return typeof value === 'string' && /^(0|[1-9][0-9]{0,19})$/.test(value) && BigInt(value) <= 18446744073709551615n && (empty || value !== '0')
}

/**
 * Recognizes one alternative, scoped by its question and quiz.
 * @param value Candidate alternative.
 * @returns Whether the alternative is supported.
 */
function choice(value: unknown): value is string { return typeof value === 'string' && /^[a-d]$/.test(value) }

/**
 * Validates fixed public generation feedback without retaining diagnostics.
 * @param value Decoded feedback.
 * @param prID Routed PR.
 * @param previous Optional exact generation scope.
 * @returns Detached allowlisted feedback.
 */
export function parseQuizStatus(value: unknown, prID: string, previous?: QuizStatus): QuizStatus {
  if (!fields(value, ['prID', 'generationID', 'quizID', 'revisionID', 'sequence', 'state', 'cancelRequested', 'executionUnknown', 'failureCode', 'savedVersion']) || !historyID(prID) || value.prID !== prID || !historyID(value.generationID) || !historyID(value.quizID) || value.generationID === value.quizID || !historyID(value.revisionID) || !quizVersion(value.sequence, false) || typeof value.state !== 'string' || !['pending', 'generating', 'validating', 'saving', 'completed', 'failed', 'cancelled', 'unknown'].includes(value.state) || typeof value.cancelRequested !== 'boolean' || value.executionUnknown !== (value.state === 'unknown')) throw new Error('Unexpected quiz status.')
  const failures = ['source-unavailable', 'source-too-large', 'generation-timeout', 'native-unavailable', 'output-too-large', 'execution-failed', 'invalid-output', 'save-failed', 'quiz-changed', 'save-uncertain']
  if (value.state === 'failed' ? !failures.includes(String(value.failureCode)) : value.state === 'unknown' ? !['session-uncertain', 'execution-uncertain'].includes(String(value.failureCode)) : value.failureCode !== undefined) throw new Error('Unexpected quiz failure feedback.')
  if (value.state === 'completed' ? !quizVersion(value.savedVersion, false) || value.cancelRequested : value.savedVersion !== undefined) throw new Error('Unexpected quiz save feedback.')
  if (previous && (value.generationID !== previous.generationID || value.quizID !== previous.quizID || value.revisionID !== previous.revisionID)) throw new Error('Quiz status changed scope.')
  return { prID, generationID: value.generationID, quizID: value.quizID, revisionID: value.revisionID, sequence: value.sequence, state: value.state as QuizStatus['state'], cancelRequested: value.cancelRequested, executionUnknown: value.executionUnknown, failureCode: value.failureCode as string | undefined, savedVersion: value.savedVersion as string | undefined }
}

/**
 * Parses unanswered saved data and recomputes its immutable revision digest.
 * @param value Decoded saved response.
 * @param prID Routed PR.
 * @returns Public-only set; private fields cause rejection rather than hidden retention.
 */
export async function parseQuizRead(value: unknown, prID: string): Promise<QuizRead> {
  if (!historyID(prID) || !fields(value, ['prID', 'version', 'quiz', 'generation']) || value.prID !== prID || !quizVersion(value.version) || (value.quiz === null) !== (value.version === '0')) throw new Error('Unexpected saved quiz.')
  const generation = value.generation === null ? null : parseQuizStatus(value.generation, prID)
  if (value.quiz === null) return { prID, version: value.version, quiz: null, generation }
  const q = value.quiz
  if (!fields(q, ['quizID', 'revision', 'revisionID', 'questions']) || !historyID(q.quizID) || !historyID(q.revisionID) || !Array.isArray(q.questions) || q.questions.length !== 5) throw new Error('Unexpected quiz set.')
  const r = q.revision
  if (!fields(r, ['prID', 'baseBranch', 'baseSource', 'baseSHA', 'headSHA', 'mergeBaseSHA']) || r.prID !== prID || !text(r.baseBranch, 1024) || !['github', 'user-selected'].includes(String(r.baseSource)) || ![r.baseSHA, r.headSHA, r.mergeBaseSHA].every((sha: unknown): boolean => typeof sha === 'string' && /^[a-f0-9]{40}$/.test(sha) && !/^0+$/.test(sha))) throw new Error('Unexpected quiz revision.')
  const revision: PinnedRevision = { prID, baseBranch: r.baseBranch, baseSource: r.baseSource as PinnedRevision['baseSource'], baseSHA: String(r.baseSHA), headSHA: String(r.headSHA), mergeBaseSHA: String(r.mergeBaseSHA) }
  if (await pinnedID(revision) !== q.revisionID) throw new Error('Unexpected quiz revision identity.')
  const prompts = new Set<string>()
  const questions = q.questions.map((item: unknown, i: number): QuizQuestion => {
    if (!fields(item, ['questionID', 'prompt', 'choices']) || item.questionID !== `q${i+1}` || !text(item.prompt, 2048) || prompts.has(item.prompt.trim().toLowerCase()) || !Array.isArray(item.choices) || item.choices.length !== 4) throw new Error('Unexpected quiz question.')
    prompts.add(item.prompt.trim().toLowerCase())
    const labels = new Set<string>()
    const choices = item.choices.map((c: unknown, j: number): QuizQuestion['choices'][number] => {
      if (!fields(c, ['choiceID', 'text']) || c.choiceID !== 'abcd'[j] || !text(c.text, 1024) || labels.has(c.text.trim().toLowerCase())) throw new Error('Unexpected quiz alternative.')
      labels.add(c.text.trim().toLowerCase())
      return { choiceID: c.choiceID as string, text: c.text }
    })
    return { questionID: item.questionID as string, prompt: item.prompt, choices }
  })
  return { prID, version: value.version, quiz: { quizID: q.quizID, revision, revisionID: q.revisionID, questions }, generation }
}

/**
 * Copies supported literal source fields only.
 * @param line Validated quoted line.
 * @returns Detached literal line.
 */
function copyLine(line: SourceLine): SourceLine { return { id: line.id, sign: line.sign, text: line.text, oldLine: line.oldLine, newLine: line.newLine, noNewline: line.noNewline } }

/**
 * Validates compact selected evidence or legacy retained hunks without granting source access.
 * @param value Decoded evidence.
 * @returns Detached bounded original quote.
 */
function evidence(value: unknown): QuizEvidence {
  if (!fields(value, ['anchor', 'oldPath', 'newPath', 'selected', 'surrounding']) || !fields(value.anchor, ['fileID', 'hunkID', 'side', 'start', 'end']) || !Array.isArray(value.selected)) throw new Error('Unexpected quiz evidence.')
  const a = value.anchor
  if (!historyID(a.fileID) || !historyID(a.hunkID) || !['old', 'new'].includes(String(a.side)) || !Number.isSafeInteger(a.start) || !Number.isSafeInteger(a.end) || Number(a.start) < 1 || Number(a.end) < Number(a.start) || Number(a.end) > 1000100000 || Number(a.end)-Number(a.start) >= 200) throw new Error('Unexpected quiz evidence range.')
  for (const path of [value.oldPath, value.newPath]) if (path !== undefined && (typeof path !== 'string' || path.includes('\0'))) throw new Error('Unexpected quiz evidence path.')
  if (!(value.oldPath || value.newPath)) throw new Error('Unexpected quiz evidence labels.')
  if (value.surrounding === undefined) {
    if (value.selected.length !== Number(a.end)-Number(a.start)+1) throw new Error('Unexpected quiz evidence selection.')
    const selected: SourceLine[] = []; const ids = new Set<string>(); let previous = 0; let changed = false
    for (const [index, raw] of value.selected.entries()) {
      if (!fields(raw, ['id', 'sign', 'text', 'oldLine', 'newLine', 'noNewline']) || !historyID(raw.id) || ids.has(raw.id) || typeof raw.text !== 'string' || /[\0\n]|[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(raw.text) || raw.noNewline !== undefined && typeof raw.noNewline !== 'boolean' || [raw.oldLine, raw.newLine].some((number: unknown): boolean => number !== undefined && (!Number.isSafeInteger(number) || Number(number) < 0 || Number(number) > 1000100000))) throw new Error('Unexpected quiz evidence line.')
      ids.add(raw.id)
      const number = a.side === 'old' ? raw.oldLine : raw.newLine
      const other = a.side === 'old' ? raw.newLine ?? 0 : raw.oldLine ?? 0
      if (number !== Number(a.start)+index) throw new Error('Unexpected quiz evidence coordinate.')
      if (raw.sign === (a.side === 'old' ? '-' : '+')) { if (other !== 0) throw new Error('Unexpected opposite-side coordinate.'); changed = true }
      else if (raw.sign === ' ') { if (!Number.isSafeInteger(other) || Number(other) <= previous || Number(other) > 1000100000) throw new Error('Unexpected context coordinate.'); previous = Number(other) }
      else throw new Error('Unexpected quiz evidence side.')
      selected.push(copyLine(raw as unknown as SourceLine))
    }
    const bytes = new TextEncoder().encode([value.oldPath ?? '', value.newPath ?? '', ...selected.map((line: SourceLine): string => `${line.text}\n`)].join('')).length
    if (!changed || bytes > 8192) throw new Error('Unexpected or oversized quiz evidence.')
    return { anchor: { fileID: a.fileID, hunkID: a.hunkID, side: a.side as 'old' | 'new', start: Number(a.start), end: Number(a.end) }, oldPath: value.oldPath as string | undefined, newPath: value.newPath as string | undefined, selected }
  }
  const h = value.surrounding
  if (!validSourceHunk(h) || h.id !== a.hunkID || !historyID(h.old.id) || !historyID(h.new.id) || h.old.count > 100000 || h.new.count > 100000 || h.section.includes('\0')) throw new Error('Unexpected quiz evidence hunk.')
  let old = h.old.start
  let next = h.new.start
  const ids = new Set<string>()
  for (const line of h.lines) {
    if (!historyID(line.id) || ids.has(line.id) || line.text.includes('\n') || line.text.includes('\0')) throw new Error('Unexpected quiz evidence line.')
    ids.add(line.id)
    if (line.sign !== '+') { if (old <= 0 || line.oldLine !== old++) throw new Error('Unexpected old coordinate.') } else if (line.oldLine) throw new Error('Unexpected old coordinate.')
    if (line.sign !== '-') { if (next <= 0 || line.newLine !== next++) throw new Error('Unexpected new coordinate.') } else if (line.newLine) throw new Error('Unexpected new coordinate.')
  }
  if (old-h.old.start !== h.old.count || next-h.new.start !== h.new.count || old > 1000100001 || next > 1000100001) throw new Error('Unexpected quiz evidence counts.')
  const selected = h.lines.filter((line: SourceLine): boolean => { const n = a.side === 'old' ? line.oldLine ?? 0 : line.newLine ?? 0; return n >= Number(a.start) && n <= Number(a.end) })
  if (selected.length !== Number(a.end)-Number(a.start)+1 || value.selected.length !== selected.length || !selected.some((line: SourceLine): boolean => line.sign === (a.side === 'old' ? '-' : '+'))) throw new Error('Unexpected quiz evidence selection.')
  for (const [i, line] of selected.entries()) {
    const raw: unknown = value.selected[i]
    if (!fields(raw, ['id', 'sign', 'text', 'oldLine', 'newLine', 'noNewline']) || raw.id !== line.id || raw.sign !== line.sign || raw.text !== line.text || (raw.oldLine ?? 0) !== (line.oldLine ?? 0) || (raw.newLine ?? 0) !== (line.newLine ?? 0) || !!raw.noNewline !== !!line.noNewline) throw new Error('Unexpected quiz quotes.')
  }
  const bytes = new TextEncoder().encode([value.oldPath ?? '', value.newPath ?? '', h.section, ...h.lines.map((line: SourceLine): string => `${line.text}\n`), ...selected.map((line: SourceLine): string => `${line.text}\n`)].join('')).length
  if (bytes > 8192) throw new Error('Quiz evidence exceeds limits.')
  return { anchor: { fileID: a.fileID, hunkID: a.hunkID, side: a.side as 'old' | 'new', start: Number(a.start), end: Number(a.end) }, oldPath: value.oldPath as string | undefined, newPath: value.newPath as string | undefined, selected: selected.map(copyLine), surrounding: { id: h.id, old: { id: h.old.id, start: h.old.start, count: h.old.count }, new: { id: h.new.id, start: h.new.start, count: h.new.count }, section: h.section, lines: h.lines.map(copyLine) } }
}

/**
 * Accepts only the explicit question/selection's saved key and explanation.
 * @param value Decoded check response.
 * @param read Exact saved set being checked.
 * @param questionID Explicit question.
 * @param choiceID Explicit selected alternative.
 * @returns Scoped checked result without sibling keys or private metadata.
 */
export function parseQuizChecked(value: unknown, read: QuizRead, questionID: string, choiceID: string): QuizChecked {
  if (!read.quiz || !fields(value, ['prID', 'version', 'quizID', 'revisionID', 'questionID', 'choiceID', 'correct', 'correctChoiceID', 'explanation', 'evidence']) || value.prID !== read.prID || value.version !== read.version || value.quizID !== read.quiz.quizID || value.revisionID !== read.quiz.revisionID || value.questionID !== questionID || !read.quiz.questions.some((q: QuizQuestion): boolean => q.questionID === questionID) || !choice(choiceID) || value.choiceID !== choiceID || !choice(value.correctChoiceID) || value.correct !== (choiceID === value.correctChoiceID) || !text(value.explanation, 4096) || !Array.isArray(value.evidence) || value.evidence.length < 1 || value.evidence.length > 2) throw new Error('Unexpected quiz check response.')
  const quotes = value.evidence.map(evidence)
  if (quotes.length === 2 && JSON.stringify(quotes[0]!.anchor) === JSON.stringify(quotes[1]!.anchor)) throw new Error('Duplicate quiz evidence.')
  return { prID: read.prID, version: read.version, quizID: read.quiz.quizID, revisionID: read.quiz.revisionID, questionID, choiceID, correct: value.correct as boolean, correctChoiceID: value.correctChoiceID, explanation: value.explanation, evidence: quotes }
}

/**
 * Distinguishes retained active work from terminal or uncertain outcomes.
 * @param status Current feedback.
 * @returns Whether read-only polling may continue.
 */
export function quizActive(status: QuizStatus | null): boolean { return !!status && ['pending', 'generating', 'validating', 'saving'].includes(status.state) }
