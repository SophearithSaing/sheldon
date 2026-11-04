import type { Page } from '@playwright/test'
import type { RuntimeClient, RegisteredPR } from '../src/runtime/client'
import type { SavedThreads, ThreadList, ThreadDetail } from '../src/runtime/history'
import { digest, sourceFixture, token } from './runtime-fixture'

export interface HistoryHarness { state: SavedThreads; replace: () => void }

/**
 * Supplies labeled renderer-only persisted data with the same immutable quoted source contract.
 * @param pr Registered fixture PR.
 * @returns Public saved list and detail without live/native claims.
 */
export function historyFixture(pr: RegisteredPR): { list: ThreadList; detail: ThreadDetail } {
  const diff = sourceFixture(pr, 1).diff!
  const file = diff.files[0]!
  const hunk = file.hunks[0]!
  const anchor = { kind: 'lines' as const, fileID: file.id, hunkID: hunk.id, side: 'new' as const, start: 1, end: 1 }
  const threadID = digest(`saved-thread-${pr.id}`)
  const detail: ThreadDetail = { prID: pr.id, threadID, revisionID: diff.revisionID, version: '7', context: { revision: diff.revision, revisionID: diff.revisionID, anchor, oldPath: file.oldPath, newPath: file.newPath, selected: [hunk.lines[0]!], surrounding: hunk }, answers: [{ prID: pr.id, threadID, answerID: digest(`saved-answer-${pr.id}`), revisionID: diff.revisionID, question: 'Synthetic saved question.', text: 'Synthetic saved answer <script>literal</script>.', state: 'completed', stopRequested: false, executionUnknown: false, reference: { revisionID: diff.revisionID, anchor, oldPath: file.oldPath, newPath: file.newPath } }], persistence: 'saved', session: { state: 'unverified', reason: 'continuation-requires-verification' } }
  const list: ThreadList = { prID: pr.id, version: '7', threads: [{ threadID, revisionID: diff.revisionID, question: detail.answers[0]!.question, anchor, oldPath: file.oldPath, newPath: file.newPath, answerCount: 1, state: 'completed', executionUnknown: false, persistence: 'saved' }] }
  return { list, detail }
}

/**
 * Exercises the production refresh composable/client in a test-owned browser harness, not a saved-history UI.
 * @param page Verified fixture page.
 */
export async function installHistoryHarness(page: Page): Promise<void> {
  await page.evaluate(async (credential: string): Promise<void> => {
    const clientModule = '/src/runtime/client.ts'
    const historyModule = '/src/runtime/history.ts'
    const { runtimeClient, inspectBackend } = await import(clientModule)
    const { useSavedThreads } = await import(historyModule)
    let lifetime = new AbortController()
    await inspectBackend(credential, lifetime.signal)
    let connection: RuntimeClient = runtimeClient(credential, lifetime.signal)
    const state: SavedThreads = useSavedThreads((): RuntimeClient => connection)
    /** Replaces only the fixture connection so delayed reads must pass the identity guard. */
    function replace(): void {
      lifetime.abort()
      lifetime = new AbortController()
      connection = runtimeClient(credential, lifetime.signal)
    }
    ;(window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory = { state, replace }
  }, await token())
}

/**
 * Makes synthetic delayed GET replies ignore fetch cancellation to test stale-generation guards.
 * @param page Fixture browser.
 */
export async function ignoreHistoryAbort(page: Page): Promise<void> {
  await page.evaluate((): void => {
    const original = window.fetch.bind(window)
    /**
     * Delivers held fixture reads despite cancellation; all non-history requests keep their real signal.
     * @param input Fetch target.
     * @param init Request options.
     * @returns Original response.
     */
    window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const url = new URL(input instanceof Request ? input.url : String(input), location.href)
      return original(input, /\/threads(?:\/[a-f0-9]{64})?$/.test(url.pathname) ? { ...init, signal: undefined } : init)
    }
  })
}
