import { historyID } from './history'
import { pinnedID, validSourceHunk, type PinnedDiff, type PinnedRevision, type SourceLine, type SourceFile } from './source'

export interface SemanticAnchor { fileID: string; hunkID: string; side: 'old' | 'new'; start: number; end: number }
export interface SemanticGroup { groupID: string; title: string; explanation: string; ranges: SemanticAnchor[] }
export interface SemanticReview { reviewID: string; diff: PinnedDiff; groups: SemanticGroup[] }
export interface SemanticStatus {
  prID: string; generationID: string; reviewID: string; revisionID: string; sequence: string
  state: 'pending' | 'generating' | 'validating' | 'saving' | 'completed' | 'failed' | 'cancelled' | 'unknown'
  cancelRequested: boolean; executionUnknown: boolean; failureCode?: string; savedVersion?: string
}
export interface SemanticRead { prID: string; version: string; review: SemanticReview | null; generation: SemanticStatus | null }
export interface SemanticClient {
  /** Reads saved groups without source or inference.
   * @param id Registered PR.
   * @param signal Read lifetime.
   * @returns Saved result and separate current feedback.
   */
  readSemantic(id: string, signal: AbortSignal): Promise<SemanticRead>
  /** Admits one explicit generation without retry.
   * @param id Registered PR.
   * @param revision Displayed pinned comparison.
   * @param version Saved predecessor version.
   * @param signal Request observation lifetime.
   * @returns Pending runtime admission.
   */
  generateSemantic(id: string, revision: PinnedRevision, version: string, signal: AbortSignal): Promise<SemanticStatus>
  /** Observes one exact attempt without dispatch.
   * @param status Retained attempt identity.
   * @param signal Request lifetime.
   * @returns Scoped current feedback.
   */
  readSemanticGeneration(status: SemanticStatus, signal: AbortSignal): Promise<SemanticStatus>
  /** Signals only the exact attempt, without idle/rollback promises.
   * @param status Retained attempt identity.
   * @param signal Request lifetime.
   * @returns Actual cancellation feedback.
   */
  cancelSemanticGeneration(status: SemanticStatus, signal: AbortSignal): Promise<SemanticStatus>
}

/** Checks allowlisted object fields.
 * @param value Decoded object.
 * @param keys Supported property names.
 * @returns Whether the object contains supported fields only.
 */
function fields(value: unknown, keys: string[]): value is Record<string, unknown> { return value !== null && typeof value === 'object' && !Array.isArray(value) && Object.keys(value).every((key: string): boolean => keys.includes(key)) }

/** Checks canonical lossless uint64 generations.
 * @param value Candidate decimal version.
 * @param zero Whether absent-state zero is allowed.
 * @returns Whether the version is supported.
 */
export function semanticVersion(value: unknown, zero = true): value is string { return typeof value === 'string' && /^(0|[1-9][0-9]{0,19})$/.test(value) && BigInt(value) <= 18446744073709551615n && (zero || value !== '0') }

/** Checks bounded literal text without accepting malformed Unicode or markup instructions.
 * @param value Candidate text.
 * @param max Encoded byte limit.
 * @returns Whether literal text is valid.
 */
function text(value: unknown, max: number): value is string { return typeof value === 'string' && !!value.trim() && !value.includes('\0') && !/[\uD800-\uDBFF](?![\uDC00-\uDFFF])|(?<![\uD800-\uDBFF])[\uDC00-\uDFFF]/u.test(value) && new TextEncoder().encode(value).length <= max }

/** Recognizes only genuinely active generation states.
 * @param status Current feedback.
 * @returns Whether observation should continue.
 */
export function semanticActive(status: SemanticStatus | null): boolean { return !!status && ['pending', 'generating', 'validating', 'saving'].includes(status.state) }

/** Validates fixed scoped feedback without retaining private fields.
 * @param value Decoded feedback.
 * @param prID Routed PR.
 * @param previous Optional exact attempt identity.
 * @returns Detached public feedback.
 */
export function parseSemanticStatus(value: unknown, prID: string, previous?: SemanticStatus): SemanticStatus {
  if (!fields(value, ['prID', 'generationID', 'reviewID', 'revisionID', 'sequence', 'state', 'cancelRequested', 'executionUnknown', 'failureCode', 'savedVersion']) || value.prID !== prID || !historyID(prID) || !historyID(value.generationID) || !historyID(value.reviewID) || value.reviewID === value.generationID || !historyID(value.revisionID) || !semanticVersion(value.sequence, false) || typeof value.state !== 'string' || !['pending', 'generating', 'validating', 'saving', 'completed', 'failed', 'cancelled', 'unknown'].includes(value.state) || typeof value.cancelRequested !== 'boolean' || value.executionUnknown !== (value.state === 'unknown')) throw new Error('Unexpected semantic status.')
  const failures = ['source-unavailable', 'source-too-large', 'generation-timeout', 'native-unavailable', 'output-too-large', 'execution-failed', 'invalid-output', 'save-failed', 'review-changed', 'save-uncertain']
  if (value.state === 'failed' ? !failures.includes(String(value.failureCode)) : value.state === 'unknown' ? !['session-uncertain', 'execution-uncertain'].includes(String(value.failureCode)) : value.failureCode !== undefined) throw new Error('Unexpected semantic failure.')
  if (value.state === 'completed' ? !semanticVersion(value.savedVersion, false) || value.cancelRequested : value.savedVersion !== undefined) throw new Error('Unexpected semantic save acknowledgement.')
  if (previous && (value.generationID !== previous.generationID || value.reviewID !== previous.reviewID || value.revisionID !== previous.revisionID)) throw new Error('Changed semantic scope.')
  return { prID, generationID: value.generationID, reviewID: value.reviewID, revisionID: value.revisionID, sequence: value.sequence, state: value.state as SemanticStatus['state'], cancelRequested: value.cancelRequested, executionUnknown: value.executionUnknown, failureCode: value.failureCode as string | undefined, savedVersion: value.savedVersion as string | undefined }
}

/** Resolves literal range lines, never generated code or host paths.
 * @param diff Retained original inventory.
 * @param anchor Exact side/range.
 * @returns Selected literal lines, rejecting invented/context-only references.
 */
export function semanticLines(diff: PinnedDiff, anchor: SemanticAnchor): SourceLine[] {
  const file = diff.files.find((item: SourceFile): boolean => item.id === anchor.fileID && item.kind === 'text')
  const hunk = file?.hunks.find((item): boolean => item.id === anchor.hunkID)
  if (!historyID(anchor.fileID) || !historyID(anchor.hunkID) || !['old', 'new'].includes(anchor.side) || !Number.isSafeInteger(anchor.start) || !Number.isSafeInteger(anchor.end) || anchor.start < 1 || anchor.end < anchor.start || anchor.end > 1000100000 || anchor.end-anchor.start >= 200 || !hunk) throw new Error('Unexpected semantic range.')
  const lines = hunk.lines.filter((line: SourceLine): boolean => { const n = anchor.side === 'old' ? line.oldLine ?? 0 : line.newLine ?? 0; return n >= anchor.start && n <= anchor.end })
  if (lines.length !== anchor.end-anchor.start+1 || !lines.some((line: SourceLine): boolean => line.sign === (anchor.side === 'old' ? '-' : '+'))) throw new Error('Unexpected semantic range.')
  return lines
}

/** Parses complete inventory and groups without accepting private/extra properties.
 * @param value Decoded saved response.
 * @param prID Registered PR.
 * @returns Validated detached saved result and current feedback.
 */
export async function parseSemanticRead(value: unknown, prID: string): Promise<SemanticRead> {
  if (!historyID(prID) || !fields(value, ['prID', 'version', 'review', 'generation']) || value.prID !== prID || !semanticVersion(value.version) || (value.review === null) !== (value.version === '0')) throw new Error('Unexpected saved semantic review.')
  const generation = value.generation === null ? null : parseSemanticStatus(value.generation, prID)
  if (value.review === null) return { prID, version: value.version, review: null, generation }
  const review = value.review
  if (!fields(review, ['reviewID', 'diff', 'groups']) || !historyID(review.reviewID) || !fields(review.diff, ['revision', 'revisionID', 'complete', 'files']) || !historyID(review.diff.revisionID) || review.diff.complete !== true || !Array.isArray(review.diff.files) || review.diff.files.length < 1 || review.diff.files.length > 1000 || !Array.isArray(review.groups) || review.groups.length < 1 || review.groups.length > 32) throw new Error('Unexpected semantic inventory.')
  const r = review.diff.revision
  if (!fields(r, ['prID', 'baseBranch', 'baseSource', 'baseSHA', 'headSHA', 'mergeBaseSHA']) || r.prID !== prID || !text(r.baseBranch, 1024) || !['github', 'user-selected'].includes(String(r.baseSource)) || ![r.baseSHA, r.headSHA, r.mergeBaseSHA].every((sha: unknown): boolean => typeof sha === 'string' && /^(?:[a-f0-9]{40}|[a-f0-9]{64})$/.test(sha) && !/^0+$/.test(sha)) || String(r.baseSHA).length !== String(r.headSHA).length || String(r.baseSHA).length !== String(r.mergeBaseSHA).length) throw new Error('Unexpected semantic revision.')
  const revision = r as unknown as PinnedRevision
  if (await pinnedID(revision) !== review.diff.revisionID) throw new Error('Unexpected semantic digest.')
  const ids = new Set<string>()
  let changed = 0
  for (const file of review.diff.files) {
    if (!fields(file, ['id', 'status', 'oldPath', 'newPath', 'oldMode', 'newMode', 'oldObject', 'newObject', 'similarity', 'kind', 'reason', 'hunks']) || !historyID(file.id) || ids.has(file.id) || !['added', 'deleted', 'modified', 'renamed', 'copied', 'type-changed'].includes(String(file.status)) || !['text', 'binary', 'submodule', 'symlink', 'type-change', 'mode-only', 'metadata-only', 'unsupported'].includes(String(file.kind)) || !(file.oldPath || file.newPath) || !Array.isArray(file.hunks) || file.hunks.length > 10000 || file.kind !== 'text' && file.hunks.length > 0) throw new Error('Unexpected semantic file.')
    ids.add(file.id)
    for (const field of ['oldPath', 'newPath', 'oldMode', 'newMode', 'oldObject', 'newObject', 'reason']) if (file[field] !== undefined && (typeof file[field] !== 'string' || String(file[field]).includes('\0'))) throw new Error('Unexpected semantic file label.')
    for (const h of file.hunks) {
      if (!fields(h, ['id', 'old', 'new', 'section', 'lines']) || !validSourceHunk(h) || ids.has(h.id) || h.lines.length > 100000 || !fields(h.old, ['id', 'start', 'count']) || !fields(h.new, ['id', 'start', 'count'])) throw new Error('Unexpected semantic hunk.')
      ids.add(h.id)
      let old = h.old.start; let next = h.new.start
      for (const line of h.lines) {
        if (!fields(line, ['id', 'sign', 'text', 'oldLine', 'newLine', 'noNewline']) || !historyID(line.id) || ids.has(line.id) || line.text.includes('\0') || line.text.includes('\n')) throw new Error('Unexpected semantic line.')
        ids.add(line.id)
        if (line.sign !== '+') { if (old <= 0 || line.oldLine !== old++) throw new Error('Unexpected semantic coordinate.') } else if (line.oldLine) throw new Error('Unexpected old coordinate.')
        if (line.sign !== '-') { if (next <= 0 || line.newLine !== next++) throw new Error('Unexpected semantic coordinate.') } else if (line.newLine) throw new Error('Unexpected new coordinate.')
        if (line.sign !== ' ') changed++
      }
      if (old-h.old.start !== h.old.count || next-h.new.start !== h.new.count || old > 1000100001 || next > 1000100001) throw new Error('Unexpected semantic counts.')
    }
  }
  if (!changed) throw new Error('Empty semantic text inventory.')
  const diff = review.diff as unknown as PinnedDiff
  const used = new Set<string>(); const titles = new Set<string>(); let ranges = 0
  const groups = review.groups.map((item: unknown, i: number): SemanticGroup => {
    if (!fields(item, ['groupID', 'title', 'explanation', 'ranges']) || item.groupID !== `g${i+1}` || !text(item.title, 160) || titles.has(item.title.trim().toLowerCase()) || !text(item.explanation, 4096) || !Array.isArray(item.ranges) || !item.ranges.length || item.ranges.length > 32) throw new Error('Unexpected semantic group.')
    titles.add(item.title.trim().toLowerCase())
    const anchors = item.ranges.map((raw: unknown): SemanticAnchor => {
      if (!fields(raw, ['fileID', 'hunkID', 'side', 'start', 'end']) || ++ranges > 128) throw new Error('Unexpected semantic references.')
      const anchor = raw as unknown as SemanticAnchor
      for (const line of semanticLines(diff, anchor)) if (line.sign !== ' ') { if (used.has(line.id)) throw new Error('Overlapping semantic coverage.'); used.add(line.id) }
      return { ...anchor }
    })
    return { groupID: item.groupID as string, title: item.title, explanation: item.explanation, ranges: anchors }
  })
  return { prID, version: value.version, review: { reviewID: review.reviewID, diff: structuredClone(diff), groups }, generation }
}

/** Derives all ungrouped changed lines without counting context or touched hunks as coverage.
 * @param review Validated complete review.
 * @returns Original ungrouped line references.
 */
export function semanticUngrouped(review: SemanticReview): SemanticAnchor[] {
  const used = new Set<string>()
  for (const group of review.groups) for (const anchor of group.ranges) for (const line of semanticLines(review.diff, anchor)) if (line.sign !== ' ') used.add(line.id)
  const result: SemanticAnchor[] = []
  for (const file of review.diff.files) for (const hunk of file.hunks) for (const line of hunk.lines) if (line.sign !== ' ' && !used.has(line.id)) { const n = line.sign === '-' ? line.oldLine! : line.newLine!; result.push({ fileID: file.id, hunkID: hunk.id, side: line.sign === '-' ? 'old' : 'new', start: n, end: n }) }
  return result
}
