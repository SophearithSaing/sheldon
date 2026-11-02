import type { DiffLine } from '../components/diff'
import { sampleDiffs, type SampleAnchor } from './workspace'

export interface SemanticHunk {
  id: string
  title: string
  anchor: SampleAnchor
  lines: DiffLine[]
  expanded?: DiffLine[]
  caption: string
}

export interface SemanticGroup {
  id: string
  title: string
  rationale: string
  hunks: string[]
}

export const semanticHunks: SemanticHunk[] = [
  { id: 'generation', title: 'Capture and guard', anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 25, end: 30 } }, lines: sampleDiffs.refresh!.slice(4, 10), expanded: sampleDiffs.refresh!.slice(0, 16), caption: 'The promise still returns the session; the guard protects the store write.' },
  { id: 'logout', title: 'Invalidate older work', anchor: { fileId: 'store', revision: 'a81c2f4', range: { side: 'head', start: 41, end: 44 } }, lines: sampleDiffs.store!, caption: 'Logout invalidates the captured generation.' },
  { id: 'boundary-test', title: 'Exercise the logout boundary', anchor: { fileId: 'test', revision: 'a81c2f4', range: { side: 'head', start: 12, end: 18 } }, lines: sampleDiffs.test!.slice(0, 7), caption: 'The fictional test checks the store after logout.' },
  { id: 'shared-promise', title: 'Share pending work', anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 23, end: 24 } }, lines: sampleDiffs.refresh!.slice(1, 4), caption: 'The removed base line and added head lines retain their separate coordinates.' },
  { id: 'http-caller', title: 'Await a session for the request', anchor: { fileId: 'client', revision: 'a81c2f4', range: { side: 'head', start: 12, end: 15 } }, lines: sampleDiffs.client!, caption: 'An organizing hypothesis, not a claim that every caller is safe.' },
  { id: 'cleanup', title: 'Reset the in-flight promise', anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 31, end: 35 } }, lines: sampleDiffs.refresh!.slice(10, 15), caption: 'Cleanup runs after the promise settles.' },
  { id: 'failure-test', title: 'Exercise another request after failure', anchor: { fileId: 'test', revision: 'a81c2f4', range: { side: 'head', start: 30, end: 36 } }, lines: sampleDiffs.test!.slice(7), caption: 'A separate fictional test excerpt. Lines 19–29 have not been spliced into this hunk.' },
  { id: 'documentation', title: 'Document session refresh behavior', anchor: { fileId: 'docs', revision: 'a81c2f4', range: { side: 'head', start: 1, end: 4 } }, lines: sampleDiffs.docs!, caption: 'Explicitly ungrouped. This change is not silently omitted from the sample inventory.' },
]

export const semanticGroups: SemanticGroup[] = [
  { id: 'logout', title: 'Prevent stale sessions after logout', rationale: 'The refresh captures a generation before the request. Logout advances it, so an older response skips the store write. The test exercises that boundary.', hunks: ['generation', 'logout', 'boundary-test'] },
  { id: 'sharing', title: 'Share concurrent refresh work', rationale: 'Concurrent calls share a pending promise. The HTTP caller awaits the resulting session; this grouping does not prove the return value is safe for every caller.', hunks: ['shared-promise', 'http-caller'] },
  { id: 'failure', title: 'Reset after a failed request', rationale: 'Cleanup releases the shared promise after settlement. A separate test excerpt illustrates a later attempt after rejection.', hunks: ['cleanup', 'failure-test'] },
]

export const ungrouped: SemanticGroup = { id: 'ungrouped', title: 'Ungrouped changes', rationale: 'One documentation hunk remains outside the three groups. Ordinary source review is available for it too.', hunks: ['documentation'] }

/**
 * Resolves explicit manifest hunk ids without concatenating noncontiguous source ranges.
 * @param group Sample group or the ungrouped bucket.
 * @returns Separate excerpts in the manifest's original order.
 */
export function hunksFor(group: SemanticGroup): SemanticHunk[] {
  return group.hunks.map((id): SemanticHunk => semanticHunks.find((hunk): boolean => hunk.id === id)!)
}
