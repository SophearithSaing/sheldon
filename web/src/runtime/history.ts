import { computed, ref, type ComputedRef, type Ref } from 'vue'
import type { RuntimeClient } from './client'
import { pinnedID, validSourceHunk, type PinnedRevision, type SourceHunk, type SourceLine } from './source'
import { parseQuestionAnswer, questionError, type QuestionAnchor, type QuestionAnswer } from './questions'

export interface SessionStatus { state: 'unverified' | 'busy' | 'unknown' | 'unavailable' | 'ready'; reason: string }
export interface ThreadSummary {
  threadID: string; revisionID: string; question: string; oldPath?: string; newPath?: string; anchor: QuestionAnchor
  answerCount: number; state: QuestionAnswer['state']; executionUnknown: boolean; persistence: 'saved' | 'saving' | 'failed'
}
export interface ThreadList { prID: string; version: string; threads: ThreadSummary[] }
export interface SavedContext {
  revision: PinnedRevision; revisionID: string; anchor: QuestionAnchor; oldPath?: string; newPath?: string
  selected: SourceLine[]; surrounding: SourceHunk
}
export interface ThreadDetail {
  prID: string; version: string; threadID: string; revisionID: string; context: SavedContext; answers: QuestionAnswer[]
  persistence: 'saved' | 'saving' | 'failed'; session: SessionStatus
}
export interface SavedThreads {
  list: Ref<ThreadList | undefined>; details: Ref<Record<string, ThreadDetail>>; pending: ComputedRef<boolean>; error: Ref<string>
  refreshList: (prID: string) => Promise<void>
  refreshThread: (prID: string, threadID: string) => Promise<ThreadDetail | undefined>
  clear: () => void
}

/**
 * Checks decoded object shape before inspecting public fields.
 * @param value Decoded field.
 * @returns Whether the field is a plain record.
 */
function record(value: unknown): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value) }

/**
 * Checks canonical nonzero identities.
 * @param value Candidate identity.
 * @returns Whether the identity is a full digest.
 */
export function historyID(value: unknown): value is string { return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value) && !/^0+$/.test(value) }

/**
 * Checks lossless canonical saved generations.
 * @param value Candidate decimal string.
 * @param empty Whether generation zero is allowed.
 * @returns Whether the generation is a supported uint64 string.
 */
function version(value: unknown, empty: boolean): value is string {
  return typeof value === 'string' && /^(0|[1-9][0-9]{0,19})$/.test(value) && BigInt(value) <= 18446744073709551615n && (empty || value !== '0')
}

/**
 * Checks bounded source paths as untrusted display text.
 * @param value Candidate optional path.
 * @returns Whether the path is bounded literal text.
 */
function path(value: unknown): value is string | undefined { return value === undefined || typeof value === 'string' && !value.includes('\0') && new TextEncoder().encode(value).length <= 65536 }

/**
 * Checks normalized persistence feedback.
 * @param value Candidate state.
 * @returns Whether the state is public saving feedback.
 */
function persistence(value: unknown): value is ThreadDetail['persistence'] { return value === 'saved' || value === 'saving' || value === 'failed' }

/**
 * Rebuilds only supported anchor fields, never arbitrary saved metadata.
 * @param value Decoded anchor.
 * @returns Validated immutable anchor.
 */
function anchor(value: unknown): QuestionAnchor {
  if (!record(value) || !historyID(value.fileID) || !historyID(value.hunkID)) throw new Error('Invalid saved anchor.')
  if (value.kind === 'hunk' && value.side === undefined && value.start === undefined && value.end === undefined) return { kind: 'hunk', fileID: value.fileID, hunkID: value.hunkID }
  if (value.kind !== 'lines' || (value.side !== 'old' && value.side !== 'new') || !Number.isSafeInteger(value.start) || !Number.isSafeInteger(value.end) || Number(value.start) < 1 || Number(value.end) < Number(value.start) || Number(value.end) > 1000100000 || Number(value.end) - Number(value.start) >= 1000) throw new Error('Invalid saved anchor.')
  return { kind: 'lines', fileID: value.fileID, hunkID: value.hunkID, side: value.side, start: Number(value.start), end: Number(value.end) }
}

/**
 * Parses scoped bounded saved navigation without trusting extra server fields.
 * @param value Decoded list.
 * @param prID Routed PR.
 * @returns Validated creation-ordered saved summaries.
 */
export function parseThreadList(value: unknown, prID: string): ThreadList {
  if (!historyID(prID) || !record(value) || value.prID !== prID || !version(value.version, true) || !Array.isArray(value.threads) || value.threads.length > 128 || value.version === '0' && value.threads.length !== 0) throw new Error('Unexpected saved thread list.')
  const ids = new Set<string>()
  const threads = value.threads.map((item: unknown): ThreadSummary => {
    if (!record(item) || !historyID(item.threadID) || ids.has(item.threadID) || !historyID(item.revisionID) || typeof item.question !== 'string' || questionError(item.question) || new TextEncoder().encode(item.question).length > 256 || !path(item.oldPath) || !path(item.newPath) || !Number.isSafeInteger(item.answerCount) || Number(item.answerCount) < 1 || Number(item.answerCount) > 512 || !['pending', 'streaming', 'completed', 'failed', 'interrupted'].includes(String(item.state)) || typeof item.executionUnknown !== 'boolean' || item.executionUnknown && item.state !== 'interrupted' || !persistence(item.persistence)) throw new Error('Unexpected saved thread summary.')
    ids.add(item.threadID)
    return { threadID: item.threadID, revisionID: item.revisionID, question: item.question, oldPath: item.oldPath, newPath: item.newPath, anchor: anchor(item.anchor), answerCount: Number(item.answerCount), state: item.state as QuestionAnswer['state'], executionUnknown: item.executionUnknown, persistence: item.persistence }
  })
  if (threads.reduce((total: number, item: ThreadSummary): number => total + item.answerCount, 0) > 512) throw new Error('Saved answer count exceeds limits.')
  return { prID, version: value.version, threads }
}

/**
 * Copies supported literal line fields only.
 * @param line Validated source line.
 * @returns Detached display data.
 */
function copyLine(line: SourceLine): SourceLine { return { id: line.id, sign: line.sign, text: line.text, oldLine: line.oldLine, newLine: line.newLine, noNewline: line.noNewline } }

/**
 * Parses quoted source and recomputes its original immutable revision identity.
 * @param value Decoded context.
 * @param prID Routed PR.
 * @param revisionID Expected revision digest.
 * @returns Validated saved context, never source authorization.
 */
async function context(value: unknown, prID: string, revisionID: string): Promise<SavedContext> {
  if (!record(value) || !record(value.revision) || value.revision.prID !== prID || value.revisionID !== revisionID || typeof value.revision.baseBranch !== 'string' || value.revision.baseBranch.length > 1024 || !['github', 'user-selected'].includes(String(value.revision.baseSource)) || ![value.revision.baseSHA, value.revision.headSHA, value.revision.mergeBaseSHA].every((sha: unknown): boolean => typeof sha === 'string' && /^[a-f0-9]{40}$/.test(sha)) || !path(value.oldPath) || !path(value.newPath) || !(value.oldPath || value.newPath) || value.directory !== undefined || !validSourceHunk(value.surrounding) || !Array.isArray(value.selected) || value.selected.length < 1 || value.selected.length > 1000) throw new Error('Unexpected saved context.')
  const revision: PinnedRevision = { prID, baseBranch: value.revision.baseBranch, baseSource: value.revision.baseSource as PinnedRevision['baseSource'], baseSHA: String(value.revision.baseSHA), headSHA: String(value.revision.headSHA), mergeBaseSHA: String(value.revision.mergeBaseSHA) }
  if (await pinnedID(revision) !== revisionID) throw new Error('Unexpected saved revision.')
  const selection = anchor(value.anchor)
  const hunk = value.surrounding
  if (hunk.id !== selection.hunkID || !historyID(hunk.old.id) || !historyID(hunk.new.id)) throw new Error('Unexpected saved hunk.')
  let old = hunk.old.start
  let next = hunk.new.start
  let oldCount = 0
  let newCount = 0
  const lineIDs = new Set<string>()
  for (const line of hunk.lines) {
    if (!historyID(line.id) || lineIDs.has(line.id) || line.text.includes('\n') || line.text.includes('\0')) throw new Error('Unexpected saved line.')
    lineIDs.add(line.id)
    if (line.sign === ' ' || line.sign === '-') {
      if (old <= 0 || line.oldLine !== old++) throw new Error('Unexpected saved old coordinate.')
      oldCount++
    } else if (line.oldLine) throw new Error('Unexpected saved old coordinate.')
    if (line.sign === ' ' || line.sign === '+') {
      if (next <= 0 || line.newLine !== next++) throw new Error('Unexpected saved new coordinate.')
      newCount++
    } else if (line.newLine) throw new Error('Unexpected saved new coordinate.')
  }
  if (oldCount !== hunk.old.count || newCount !== hunk.new.count || old > 1000100001 || next > 1000100001) throw new Error('Unexpected saved hunk counts.')
  const selected = hunk.lines.filter((line: SourceLine): boolean => selection.kind === 'hunk' || (selection.side === 'old' ? line.oldLine ?? 0 : line.newLine ?? 0) >= selection.start && (selection.side === 'old' ? line.oldLine ?? 0 : line.newLine ?? 0) <= selection.end)
  if (selected.length !== value.selected.length || !selected.some((line: SourceLine): boolean => line.sign !== ' ') || selection.kind === 'lines' && selected.length !== selection.end - selection.start + 1) throw new Error('Unexpected saved selection.')
  for (let n = 0; n < selected.length; n++) {
    const saved = value.selected[n]
    const actual = selected[n]!
    if (!record(saved) || saved.id !== actual.id || saved.sign !== actual.sign || saved.text !== actual.text || (saved.oldLine ?? 0) !== (actual.oldLine ?? 0) || (saved.newLine ?? 0) !== (actual.newLine ?? 0) || !!saved.noNewline !== !!actual.noNewline) throw new Error('Unexpected saved quotes.')
  }
  const text = [value.oldPath ?? '', value.newPath ?? '', hunk.section, ...hunk.lines.map((line: SourceLine): string => `${line.text}\n`), ...selected.map((line: SourceLine): string => `${line.text}\n`)].join('')
  if (text.includes('\0') || new TextEncoder().encode(text).length > 65536) throw new Error('Saved context exceeds limits.')
  return { revision, revisionID, anchor: selection, oldPath: value.oldPath, newPath: value.newPath, selected: selected.map(copyLine), surrounding: { id: hunk.id, old: { id: hunk.old.id, start: hunk.old.start, count: hunk.old.count }, new: { id: hunk.new.id, start: hunk.new.start, count: hunk.new.count }, section: hunk.section, lines: hunk.lines.map(copyLine) } }
}

/**
 * Parses saved transcript data separately from new pending-admission responses.
 * @param value Decoded detail.
 * @param prID Routed PR.
 * @param threadID Requested saved thread.
 * @returns Validated persisted transcript with separate current saving/session feedback.
 */
export async function parseThreadDetail(value: unknown, prID: string, threadID: string): Promise<ThreadDetail> {
  if (!historyID(prID) || !historyID(threadID) || !record(value) || value.prID !== prID || value.threadID !== threadID || !historyID(value.revisionID) || !version(value.version, false) || !persistence(value.persistence) || !record(value.session) || !Array.isArray(value.answers) || value.answers.length < 1 || value.answers.length > 512) throw new Error('Unexpected saved thread.')
  const reasons: Record<string, string[]> = { unverified: ['continuation-requires-verification'], busy: ['question-active'], unknown: ['execution-uncertain'], unavailable: ['incompatible-binding', 'runtime-closed', 'original-session-unavailable', 'answer-not-saved'], ready: ['original-session-verified'] }
  if (typeof value.session.state !== 'string' || !reasons[value.session.state]?.includes(String(value.session.reason))) throw new Error('Unexpected session availability.')
  const quoted = await context(value.context, prID, value.revisionID)
  const ids = new Set<string>()
  const rawAnswers = value.answers
  const answers = rawAnswers.map((item: unknown, index: number): QuestionAnswer => {
    if (!record(item) || typeof item.question !== 'string' || questionError(item.question) || item.persistence !== undefined || item.savedVersion !== undefined) throw new Error('Unexpected saved answer.')
    const answer = parseQuestionAnswer(item, prID, quoted.revisionID, { question: item.question, anchor: quoted.anchor })
    if (answer.threadID !== threadID || answer.answerID === threadID || ids.has(answer.answerID) || !record(item.reference) || (item.reference.oldPath ?? '') !== (quoted.oldPath ?? '') || (item.reference.newPath ?? '') !== (quoted.newPath ?? '') || answer.executionUnknown && answer.state !== 'interrupted' || (answer.state === 'pending' || answer.state === 'streaming') && index !== rawAnswers.length - 1) throw new Error('Unexpected saved answer scope.')
    ids.add(answer.answerID)
    return { ...answer, persistence: 'saved', savedVersion: value.version as string, reference: { revisionID: quoted.revisionID, anchor: { ...quoted.anchor }, oldPath: quoted.oldPath, newPath: quoted.newPath } }
  })
  return { prID, threadID, version: value.version, revisionID: value.revisionID, context: quoted, answers, persistence: value.persistence, session: { state: value.session.state as SessionStatus['state'], reason: String(value.session.reason) } }
}

/**
 * Owns explicit saved refresh snapshots separately from live transcripts and drafts.
 * @param client Current verified connection.
 * @returns Bounded refresh state without automatic inference, observation or selection changes.
 */
export function useSavedThreads(client: () => RuntimeClient | null): SavedThreads {
  const list = ref<ThreadList>()
  const details = ref<Record<string, ThreadDetail>>({})
  const error = ref('')
  const active = ref(0)
  const pending = computed((): boolean => active.value > 0)
  const requests = new Map<string, AbortController>()
  let generation = 0
  let scope = ''
  let highest = 0n

  /** Clears only saved refresh state and cancels its requests. */
  function clear(): void {
    generation++
    for (const request of requests.values()) request.abort()
    requests.clear()
    active.value = 0
    scope = ''
    highest = 0n
    list.value = undefined
    details.value = {}
    error.value = ''
  }

  /**
   * Reads explicit saved state with per-route cancellation and monotonic file-generation guards.
   * @param prID Requested PR.
   * @param threadID Optional exact thread, or empty for the list.
   * @returns Accepted detail, or undefined for list/stale/error results.
   */
  async function refresh(prID: string, threadID: string): Promise<ThreadDetail | undefined> {
    const connection = client()
    if (!connection || !historyID(prID) || threadID !== '' && !historyID(threadID)) return undefined
    if (scope !== prID) { clear(); scope = prID }
    const key = threadID || 'list'
    requests.get(key)?.abort()
    const controller = new AbortController()
    const epoch = generation
    requests.set(key, controller)
    active.value = requests.size
    error.value = ''
    try {
      const result = threadID ? await connection.readThread(prID, threadID, controller.signal) : await connection.listThreads(prID, controller.signal)
      if (controller.signal.aborted || epoch !== generation || client() !== connection || requests.get(key) !== controller || scope !== prID || BigInt(result.version) < highest) return undefined
      highest = BigInt(result.version)
      if ('answers' in result) { details.value = { ...details.value, [result.threadID]: result }; return result }
      list.value = result
    } catch {
      if (!controller.signal.aborted && epoch === generation && client() === connection && requests.get(key) === controller) error.value = 'Cannot refresh saved question history. Existing transcripts and drafts were retained.'
    } finally {
      if (requests.get(key) === controller) { requests.delete(key); active.value = requests.size }
    }
    return undefined
  }

  /**
   * Explicitly refreshes only saved navigation.
   * @param prID Routed PR identity.
   */
  async function refreshList(prID: string): Promise<void> { await refresh(prID, '') }

  /**
   * Explicitly refreshes one saved transcript without replacing a live observer or draft.
   * @param prID Routed PR identity.
   * @param threadID Exact saved thread.
   * @returns Accepted saved detail, or undefined after failure/stale response.
   */
  async function refreshThread(prID: string, threadID: string): Promise<ThreadDetail | undefined> { return refresh(prID, threadID) }

  return { list, details, pending, error, refreshList, refreshThread, clear }
}
