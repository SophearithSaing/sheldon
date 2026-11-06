import type { Page } from '@playwright/test'
import type { RegisteredPR } from '../src/runtime/client'
import type { QuizRead, QuizChecked, QuizStatus } from '../src/runtime/quiz'
import type { SourceResponse } from '../src/runtime/source'
import { digest, sourceFixture } from './runtime-fixture'

/**
 * Builds public renderer fixtures, never an actual generated success claim.
 * @param pr Registered fixture PR.
 * @returns Unanswered set, one checked result and scoped generation.
 */
export function quizFixture(pr: RegisteredPR): { source: SourceResponse; read: QuizRead; checked: QuizChecked; status: QuizStatus } {
  const source = sourceFixture(pr, 1)
  const diff = source.diff!
  const read: QuizRead = { prID: pr.id, version: '1', generation: null, quiz: { quizID: digest(`${pr.id}/quiz`), revision: diff.revision, revisionID: diff.revisionID, questions: Array.from({ length: 5 }, (_, i): NonNullable<QuizRead['quiz']>['questions'][number] => ({ questionID: `q${i+1}`, prompt: `Renderer question ${i+1}: <literal question>`, choices: ['First alternative', 'Second alternative', 'Third alternative', 'Fourth alternative'].map((text, j): { choiceID: string; text: string } => ({ choiceID: 'abcd'[j]!, text })) })) } }
  const file = diff.files[0]!
  const hunk = file.hunks[0]!
  const checked: QuizChecked = { prID: pr.id, version: '1', quizID: read.quiz!.quizID, revisionID: diff.revisionID, questionID: 'q1', choiceID: 'a', correct: false, correctChoiceID: 'b', explanation: '<script>literal agent explanation</script> Verify the original changed boundary.', evidence: [{ anchor: { fileID: file.id, hunkID: hunk.id, side: 'new', start: 1, end: 1 }, oldPath: file.oldPath, newPath: file.newPath, selected: [hunk.lines[0]!], surrounding: hunk }] }
  const status: QuizStatus = { prID: pr.id, generationID: digest(`${pr.id}/generation`), quizID: digest(`${pr.id}/replacement`), revisionID: diff.revisionID, sequence: '1', state: 'pending', cancelRequested: false, executionUnknown: false }
  return { source, read, checked, status }
}

/**
 * Delivers delayed quiz replies despite transport cancellation to exercise owner guards.
 * @param page Fixture browser.
 */
export async function ignoreQuizAbort(page: Page): Promise<void> {
  await page.evaluate((): void => {
    const original = window.fetch.bind(window)
    /**
     * Removes only quiz fixture transport signals, not production ownership checks.
     * @param input Fetch target.
     * @param init Request options.
     * @returns Held fixture response even after caller cancellation.
     */
    window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const path = new URL(input instanceof Request ? input.url : String(input), location.href).pathname
      return original(input, /\/quiz(?:\/|$)/.test(path) ? { ...init, signal: undefined } : init)
    }
  })
}
