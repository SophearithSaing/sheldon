import type { RegisteredPR } from '../src/runtime/client'
import type { SemanticRead, SemanticStatus } from '../src/runtime/semantic'
import type { SourceResponse } from '../src/runtime/source'
import { digest, sourceFixture } from './runtime-fixture'

/** Builds explicit renderer fixtures, not generated or saved application success.
 * @param pr Registered fixture identity.
 * @returns Source, saved groups and pending generation feedback.
 */
export function semanticFixture(pr: RegisteredPR): { source: SourceResponse; read: SemanticRead; status: SemanticStatus } {
  const source = sourceFixture(pr, 3)
  const diff = source.diff!
  const read: SemanticRead = { prID: pr.id, version: '1', generation: null, review: { reviewID: digest(`${pr.id}/semantic`), diff, groups: [{ groupID: 'g1', title: 'Guard and caller', explanation: '<script>Literal organizing hypothesis</script> — not a safety claim.', ranges: diff.files.slice(0, 2).map((file): { fileID: string; hunkID: string; side: 'new'; start: number; end: number } => ({ fileID: file.id, hunkID: file.hunks[0]!.id, side: 'new', start: 1, end: 1 })) }] } }
  const status: SemanticStatus = { prID: pr.id, generationID: digest(`${pr.id}/semantic-generation`), reviewID: digest(`${pr.id}/semantic-replacement`), revisionID: diff.revisionID, sequence: '1', state: 'pending', cancelRequested: false, executionUnknown: false }
  return { source, read, status }
}
