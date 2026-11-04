import type { RegisteredPR } from './client'

export interface SourceRequest { action: 'load' | 'git-only' | 'select-base' | 'refresh'; branch?: string; sha?: string; revision?: PinnedRevision }
export interface SourceCandidate { branch: string; sha: string }
export interface PinnedRevision { prID: string; baseBranch: string; baseSource: 'github' | 'user-selected'; baseSHA: string; headSHA: string; mergeBaseSHA: string }
export interface SourceLine { id: string; sign: ' ' | '+' | '-'; text: string; oldLine?: number; newLine?: number; noNewline?: boolean }
export interface SourceRange { id: string; start: number; count: number }
export interface SourceHunk { id: string; old: SourceRange; new: SourceRange; section: string; lines: SourceLine[] }
export interface SourceFile { id: string; status: string; oldPath?: string; newPath?: string; oldMode: string; newMode: string; oldObject: string; newObject: string; similarity?: number; kind: string; reason?: string; hunks: SourceHunk[] }
export interface PinnedDiff { revision: PinnedRevision; revisionID: string; complete: true; files: SourceFile[] }
export interface SourceResponse { state: 'fallback-required' | 'base-selection-required' | 'ready'; pr: RegisteredPR; candidates?: SourceCandidate[]; diff?: PinnedDiff; requestedRevisionID?: string }

/**
 * Derives the canonical comparison identity without consulting moving refs.
 * @param revision Pinned comparison descriptors.
 * @returns Canonical revision digest.
 */
export async function pinnedID(revision: PinnedRevision): Promise<string> {
  const bytes = await crypto.subtle.digest('SHA-256', new TextEncoder().encode([revision.prID, revision.baseSHA, revision.headSHA, revision.mergeBaseSHA].join('\n')))
  return Array.from(new Uint8Array(bytes), (byte: number): string => byte.toString(16).padStart(2, '0')).join('')
}

/**
 * Recognizes a plain response object before inspecting its fields.
 * @param value Decoded JSON value.
 * @returns Whether the value is a record.
 */
function record(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

/**
 * Checks an opaque revision-scoped reference.
 * @param value Candidate reference.
 * @returns Whether the value is a full digest.
 */
function reference(value: unknown): value is string {
  return typeof value === 'string' && /^[a-f0-9]{64}$/.test(value)
}

/**
 * Checks a bounded line coordinate/count.
 * @param value Candidate coordinate.
 * @returns Whether the value is a nonnegative integer.
 */
function coordinate(value: unknown): value is number {
  return typeof value === 'number' && Number.isSafeInteger(value) && value >= 0 && value <= 1000100000
}

/**
 * Checks a pinned hunk range.
 * @param value Candidate range.
 * @returns Whether the range is structurally valid.
 */
function range(value: unknown): value is SourceRange {
  return record(value) && reference(value.id) && coordinate(value.start) && coordinate(value.count)
}

/**
 * Checks literal lines without treating source as markup.
 * @param value Candidate source line.
 * @returns Whether the line has usable coordinates and text.
 */
function line(value: unknown): value is SourceLine {
  return record(value) && reference(value.id) && [' ', '+', '-'].includes(String(value.sign)) && typeof value.text === 'string' &&
    (value.oldLine === undefined || coordinate(value.oldLine)) && (value.newLine === undefined || coordinate(value.newLine)) &&
    (value.noNewline === undefined || typeof value.noNewline === 'boolean')
}

/**
 * Validates complete diff files before exposing them to the renderer.
 * @param value Candidate changed file.
 * @returns Whether file metadata and hunks are usable.
 */
function file(value: unknown): value is SourceFile {
  return record(value) && reference(value.id) && ['added', 'deleted', 'modified', 'renamed', 'copied', 'type-changed'].includes(String(value.status)) &&
    ['text', 'binary', 'submodule', 'symlink', 'type-change', 'mode-only', 'metadata-only', 'unsupported'].includes(String(value.kind)) &&
    (typeof value.oldPath === 'string' || value.oldPath === undefined) && (typeof value.newPath === 'string' || value.newPath === undefined) &&
    Boolean(value.oldPath || value.newPath) && typeof value.oldMode === 'string' && typeof value.newMode === 'string' &&
    typeof value.oldObject === 'string' && typeof value.newObject === 'string' && (value.reason === undefined || typeof value.reason === 'string') &&
    Array.isArray(value.hunks) && value.hunks.length <= 10000 && value.hunks.every(hunk)
}

/**
 * Checks a counted hunk and its literal lines.
 * @param value Candidate hunk.
 * @returns Whether the hunk is structurally complete.
 */
function hunk(value: unknown): value is SourceHunk {
  return record(value) && reference(value.id) && range(value.old) && range(value.new) && typeof value.section === 'string' &&
    Array.isArray(value.lines) && value.lines.length <= 100000 && value.lines.every(line)
}

/**
 * Checks retained hunk structure without loading or authorizing source.
 * @param value Decoded saved hunk.
 * @returns Whether the hunk uses supported literal source fields.
 */
export function validSourceHunk(value: unknown): value is SourceHunk { return hunk(value) }

/**
 * Checks the PR association and canonical URL of source metadata.
 * @param value Candidate PR.
 * @param id Expected routed identifier.
 * @returns Whether the PR belongs to the current route.
 */
function pr(value: unknown, id: string): value is RegisteredPR {
  return record(value) && value.id === id && typeof value.owner === 'string' && typeof value.repository === 'string' &&
    typeof value.number === 'string' && /^[1-9][0-9]*$/.test(value.number) && value.url === `https://github.com/${value.owner}/${value.repository}/pull/${value.number}` &&
    (value.title === null || typeof value.title === 'string') && (value.author === null || typeof value.author === 'string')
}

/**
 * Parses a routed source response and checks its canonical revision digest.
 * @param value Decoded source response.
 * @param id Requested PR identifier.
 * @returns Validated source state, never a partially accepted diff.
 */
export async function parseSourceResponse(value: unknown, id: string): Promise<SourceResponse> {
  if (!record(value) || !pr(value.pr, id)) throw new Error('Unexpected source response.')
  if (value.state === 'fallback-required' && value.diff === undefined) return value as unknown as SourceResponse
  if (value.state === 'base-selection-required' && value.diff === undefined && Array.isArray(value.candidates) && value.candidates.length > 0 && value.candidates.length <= 1000 &&
    value.candidates.every((candidate: unknown): boolean => record(candidate) && typeof candidate.branch === 'string' && typeof candidate.sha === 'string' && /^[a-f0-9]{40}$/.test(candidate.sha))) return value as unknown as SourceResponse
  const diff = value.diff
  if (value.state !== 'ready' || !record(diff) || diff.complete !== true || !reference(diff.revisionID) || !record(diff.revision) ||
      !Array.isArray(diff.files) || diff.files.length > 1000 || !diff.files.every(file)) throw new Error('Unexpected source response.')
  const revision = diff.revision
  if (revision.prID !== id || typeof revision.baseBranch !== 'string' || !['github', 'user-selected'].includes(String(revision.baseSource)) ||
      ![revision.baseSHA, revision.headSHA, revision.mergeBaseSHA].every((sha: unknown): boolean => typeof sha === 'string' && /^[a-f0-9]{40}$/.test(sha))) throw new Error('Unexpected revision.')
  const digest = await pinnedID(revision as unknown as PinnedRevision)
  if (digest !== diff.revisionID) throw new Error('Unexpected revision identity.')
  return value as unknown as SourceResponse
}

/**
 * Counts actual added/removed text lines, excluding context and non-text changes.
 * @param files Pinned changed files.
 * @returns Addition/removal totals.
 */
export function lineCounts(files: readonly SourceFile[]): { added: number; removed: number } {
  let added = 0
  let removed = 0
  for (const file of files) for (const hunk of file.hunks) for (const line of hunk.lines) {
    if (line.sign === '+') added++
    if (line.sign === '-') removed++
  }
  return { added, removed }
}
