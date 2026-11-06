import { expect, test, type Page } from '@playwright/test'
import { digest, registered, sourceFixture, token } from './runtime-fixture'
import type { RegisteredPR } from '../src/runtime/client'
import type { QuizRead, QuizChecked, QuizStatus } from '../src/runtime/quiz'
import type { SourceResponse } from '../src/runtime/source'

test.use({ baseURL: 'http://127.0.0.1:5175' })

/**
 * Builds explicit public renderer fixtures, never an actual generated success claim.
 * @param pr Registered fixture PR.
 * @returns Unanswered set, one checked result and scoped generation.
 */
function fixture(pr: RegisteredPR): { source: SourceResponse; read: QuizRead; checked: QuizChecked; status: QuizStatus } {
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
 * Enters the real destination without touching the fictional preview.
 * @param page Connected browser fixture.
 */
async function enter(page: Page): Promise<void> { await page.getByRole('button', { name: 'Quiz', exact: true }).click() }

test('real quiz entry reads only, retains wrong selection and checks explicitly with literal evidence', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 940)
  const f = fixture(pr)
  const traffic: string[] = []
  page.on('request', (r): void => { if (r.url().includes('/api/')) traffic.push(r.method()+' '+new URL(r.url()).pathname) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { expect(route.request().headers()['authorization']).toBe(`Bearer ${await token()}`); await route.fulfill({ json: f.read }) })
  let checks = 0
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/quiz/${f.read.quiz!.quizID}/questions/q1/check`, async (route): Promise<void> => {
    checks++
    expect(route.request().postDataJSON()).toEqual({ revisionID: f.read.quiz!.revisionID, choiceID: 'a' })
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ json: f.checked })
  })
  await enter(page)
  const panel = page.getByRole('region', { name: 'PR quiz', exact: true })
  await expect(panel.getByText('Renderer question 1: <literal question>', { exact: true })).toBeVisible()
  expect(traffic.filter((s): boolean => s.includes('/quiz'))).toHaveLength(1)
  expect(traffic.some((s): boolean => s.startsWith('POST '))).toBe(false)
  await expect(panel.getByText('✓ Correct answer', { exact: true })).toHaveCount(0)
  await panel.getByRole('radio').first().focus()
  await page.keyboard.press('Space')
  expect(checks).toBe(0)
  await expect(panel.getByRole('radio').first()).toBeChecked()
  await panel.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect.poll((): number => checks).toBe(1)
  await expect(panel.getByRole('button', { name: 'Checking…', exact: true })).toBeDisabled()
  release!()
  await expect(panel.getByText('× Your answer', { exact: true })).toBeVisible()
  await expect(panel.getByText('✓ Correct answer', { exact: true })).toBeVisible()
  await expect(panel.getByRole('radio').first()).toBeChecked()
  await expect(panel.getByText(f.checked.explanation, { exact: true })).toBeVisible()
  await expect(panel.locator('script')).toHaveCount(0)
  await expect(panel.getByRole('button', { name: 'View in original diff', exact: true })).toBeDisabled()
  await panel.getByRole('radio').first().focus()
  await page.keyboard.press('ArrowDown')
  await expect(panel.getByRole('radio').first()).toBeChecked()
  await panel.getByRole('button', { name: 'Next question', exact: true }).click()
  await expect(panel.locator('legend')).toBeFocused()
  await panel.getByRole('radio').nth(2).check()
  await page.getByRole('button', { name: 'Files changed', exact: true }).click()
  await enter(page)
  await expect(panel.getByRole('radio').nth(2)).toBeChecked()
  await panel.getByRole('button', { name: 'Previous', exact: true }).click()
  await expect(panel.getByRole('radio').first()).toBeChecked()
  await expect(panel.getByText(f.checked.explanation, { exact: true })).toBeVisible()
  expect(checks).toBe(1)
  await page.screenshot({ path: testInfo.outputPath('real-quiz-checked-desktop.png'), fullPage: true })
})

test('empty quiz explicitly admits generation, polls read-only and reads the acknowledged saved set', async ({ page }): Promise<void> => {
  const pr = await registered(page, 941)
  const f = fixture(pr)
  let complete = false
  let generated = 0
  let polled = 0
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: complete ? { ...f.read, quiz: { ...f.read.quiz, quizID: f.status.quizID }, generation: { ...f.status, state: 'completed', sequence: '4', savedVersion: '1' } } : { prID: pr.id, version: '0', quiz: null, generation: null } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => { generated++; expect(route.request().postDataJSON()).toEqual({ revision: f.source.diff!.revision, expectedVersion: '0' }); await route.fulfill({ status: 202, json: f.status }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/${f.status.generationID}`, async (route): Promise<void> => { polled++; expect(route.request().method()).toBe('GET'); complete = polled >= 2; await route.fulfill({ json: complete ? { ...f.status, sequence: '4', state: 'completed', savedVersion: '1' } : { ...f.status, sequence: '2', state: 'generating' } }) })
  await enter(page)
  await expect(page.getByRole('button', { name: 'Generate quiz', exact: true })).toBeDisabled()
  expect(generated).toBe(0)
  await page.getByRole('button', { name: 'Files changed', exact: true }).click()
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Refresh source', exact: true })).toBeVisible()
  await enter(page)
  await page.getByRole('button', { name: 'Generate quiz', exact: true }).click()
  await expect(page.getByText('Generation: pending', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Generate quiz', exact: true })).toBeDisabled()
  await expect(page.getByText('Renderer question 1: <literal question>', { exact: true })).toBeVisible()
  expect(generated).toBe(1)
  expect(polled).toBe(2)
  await expect(page.getByText('Saving acknowledged for version 1.', { exact: false })).toBeVisible()
})

test('regeneration confirmation, failure and targeted cancellation retain the old set and answers', async ({ page }): Promise<void> => {
  const pr = await registered(page, 942)
  const f = fixture(pr)
  let latest: QuizStatus | null = null
  let generated = 0
  let cancelled = 0
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: { ...f.read, generation: latest } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => { generated++; latest = { ...f.status, generationID: digest(`regeneration-${generated}`) }; await route.fulfill({ status: 202, json: latest }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*`, async (route): Promise<void> => { latest = generated === 1 ? { ...latest!, sequence: '3', state: 'failed', failureCode: 'invalid-output' } : latest; await route.fulfill({ json: latest }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*/cancel`, async (route): Promise<void> => { cancelled++; expect(route.request().postDataJSON()).toEqual({ revisionID: f.status.revisionID }); latest = { ...latest!, sequence: '4', state: 'cancelled', cancelRequested: true }; await route.fulfill({ status: 202, json: latest }) })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Refresh source', exact: true })).toBeVisible()
  await enter(page)
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
  await page.getByRole('button', { name: 'Keep current quiz', exact: true }).click()
  expect(generated).toBe(0)
  await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
  await page.getByRole('button', { name: 'Generate replacement', exact: true }).click()
  await expect(page.getByText('Generation: failed', { exact: true })).toBeVisible()
  await expect(page.getByRole('radio').first()).toBeChecked()
  await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
  await page.getByRole('button', { name: 'Generate replacement', exact: true }).click()
  await page.getByRole('button', { name: 'Cancel generation', exact: true }).click()
  await expect(page.getByText('Generation cancelled. The previously saved set is retained.', { exact: true })).toBeVisible()
  await expect(page.getByRole('radio').first()).toBeChecked()
  expect(generated).toBe(2)
  expect(cancelled).toBe(1)
})

test('malformed, key-bearing and oversized saved refreshes retain the healthy set and selections', async ({ page }): Promise<void> => {
  const pr = await registered(page, 943)
  const f = fixture(pr)
  let mode = 'good'
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => {
    if (mode === 'oversized') await route.fulfill({ contentType: 'application/json', body: ' '.repeat((256 << 10)+1) })
    else if (mode === 'error') await route.fulfill({ status: 500, json: { error: 'PRIVATE RAW SECRET' } })
    else if (mode === 'private') await route.fulfill({ json: { ...f.read, quiz: { ...f.read.quiz, questions: [{ ...f.read.quiz!.questions[0], correctChoiceID: 'b' }, ...f.read.quiz!.questions.slice(1)] } } })
    else await route.fulfill({ json: f.read })
  })
  await enter(page)
  await page.getByRole('radio').first().check()
  for (mode of ['private', 'error', 'oversized']) {
    await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
    await expect(page.getByRole('alert')).toHaveText('Cannot read saved quiz. Existing questions and selections were retained. Refresh explicitly.')
    await expect(page.getByRole('radio').first()).toBeChecked()
    await expect(page.getByRole('button', { name: 'Check answer', exact: true })).toBeDisabled()
    expect(await page.content()).not.toContain('PRIVATE RAW SECRET')
  }
})

test('uncertain admission is never replayed and stops generation until an explicit saved read', async ({ page }): Promise<void> => {
  const pr = await registered(page, 944)
  const f = fixture(pr)
  let generated = 0
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: { prID: pr.id, version: '0', quiz: null, generation: null } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => { generated++; await route.abort('failed') })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Refresh source', exact: true })).toBeVisible()
  await enter(page)
  await page.getByRole('button', { name: 'Generate quiz', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('admission could not be confirmed')
  await expect(page.getByRole('button', { name: 'Generate quiz', exact: true })).toBeDisabled()
  expect(generated).toBe(1)
  await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Generate quiz', exact: true })).toBeEnabled()
  expect(generated).toBe(1)
})

test('checked original evidence navigates only the matching source and preserves a pinned draft', async ({ page }): Promise<void> => {
  const pr = await registered(page, 945)
  const f = fixture(pr)
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: f.read }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => { await route.fulfill({ json: f.checked }) })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await page.getByRole('button', { name: 'Select hunk 1', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Retain pinned draft across Quiz')
  await enter(page)
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect(page.getByRole('button', { name: 'View in original diff', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Files changed', exact: false }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Retain pinned draft across Quiz')
  await page.getByRole('button', { name: 'Discard draft', exact: true }).click()
  await enter(page)
  await expect(page.getByRole('button', { name: 'View in original diff', exact: true })).toBeEnabled()
  await page.getByRole('button', { name: 'View in original diff', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/file0000.ts', exact: true })).toBeFocused()
  await expect(page.getByText('Quiz evidence: original new lines 1–1. No draft or thread was created.', { exact: true })).toBeVisible()
})

test('real quiz reflows at 700px and 200% text without losing selection or explanations', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 946)
  const f = fixture(pr)
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: f.read }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => { await route.fulfill({ json: f.checked }) })
  await enter(page)
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toBeVisible()
  await page.setViewportSize({ width: 700, height: 1000 })
  await expect(page.getByRole('radio').first()).toBeChecked()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toBeVisible()
  expect(await page.locator('.real-quiz').evaluate((panel): boolean => panel.querySelector('.quiz-explanation')!.getBoundingClientRect().top >= panel.querySelector('.quiz-main')!.getBoundingClientRect().bottom)).toBe(true)
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('real-quiz-checked-700.png'), fullPage: true })
  await page.getByText(f.checked.explanation, { exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('real-quiz-evidence-700.png'), fullPage: true })
  await page.addStyleTag({ content: 'html { font-size: 200% !important; }' })
  await expect(page.getByRole('button', { name: 'Next question', exact: true })).toBeVisible()
  await expect(page.getByRole('radio').first()).toBeChecked()
  expect(await page.locator('.real-quiz').evaluate((panel): boolean => panel.querySelector('.quiz-explanation')!.getBoundingClientRect().top >= panel.querySelector('.quiz-main')!.getBoundingClientRect().bottom)).toBe(true)
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByText(f.checked.explanation, { exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('real-quiz-checked-200-text.png'), fullPage: true })
})

test('checked parser rejects foreign identities, inconsistent keys and invented evidence', async ({ page }): Promise<void> => {
  const pr = await registered(page, 947)
  const f = fixture(pr)
  const result = await page.evaluate(async (f): Promise<boolean[]> => {
    const modulePath = '/src/runtime/quiz.ts'
    const { parseQuizRead, parseQuizChecked, parseQuizStatus } = await import(modulePath)
    const read = await parseQuizRead(f.read, f.read.prID)
    const invalid: unknown[] = [
      { ...f.checked, prID: 'f'.repeat(64) }, { ...f.checked, quizID: 'f'.repeat(64) }, { ...f.checked, version: '2' },
      { ...f.checked, questionID: 'q2' }, { ...f.checked, choiceID: 'b' }, { ...f.checked, correct: true },
      { ...f.checked, correctChoiceID: 'e' }, { ...f.checked, explanation: 'x'.repeat(4097) },
      { ...f.checked, evidence: [{ ...f.checked.evidence[0], selected: [{ ...f.checked.evidence[0]!.selected[0], text: 'invented' }] }] },
      { ...f.checked, evidence: [f.checked.evidence[0], f.checked.evidence[0]] },
    ]
    const rejected = invalid.map((value: unknown): boolean => { try { parseQuizChecked(value, read, 'q1', 'a'); return false } catch { return true } })
    for (const value of [{ ...f.status, state: 'completed' }, { ...f.status, state: 'unknown', executionUnknown: false }, { ...f.status, failureCode: 'PRIVATE RAW SECRET' }, { ...f.status, sequence: '01' }]) {
      try { parseQuizStatus(value, read.prID); rejected.push(false) } catch { rejected.push(true) }
    }
    return rejected
  }, f)
  expect(result.every((valid: boolean): boolean => valid)).toBe(true)
})

test('a failed check retains selection and a successful replacement resets only quiz page-memory answers', async ({ page }): Promise<void> => {
  const pr = await registered(page, 948)
  const f = fixture(pr)
  let current = f.read
  let failed = true
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: current }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => {
    if (failed) await route.fulfill({ status: 409, json: { error: 'PRIVATE RAW SECRET' } })
    else await route.fulfill({ json: { ...f.checked, choiceID: 'b', correct: true } })
  })
  await enter(page)
  await page.getByRole('radio').nth(1).check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Answer check could not be confirmed')
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await expect(page.getByText('Answer explanation', { exact: true })).toHaveCount(0)
  expect(await page.content()).not.toContain('PRIVATE RAW SECRET')
  failed = false
  await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect(page.getByText('✓ Correct answer · Your answer', { exact: true })).toBeVisible()
  current = { ...f.read, version: '2', quiz: { ...f.read.quiz!, quizID: f.status.quizID } }
  await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
  await expect(page.getByText('0 of 5 checked in this page', { exact: true })).toBeVisible()
  for (const radio of await page.getByRole('radio').all()) await expect(radio).not.toBeChecked()
  await expect(page.getByText('Answer explanation', { exact: true })).toHaveCount(0)
})

test('leaving Quiz stops observation without cancelling accepted work and returning resumes by read', async ({ page }): Promise<void> => {
  const pr = await registered(page, 949)
  const f = fixture(pr)
  let polls = 0
  let mutations = 0
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: { ...f.read, generation: f.status } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/**`, async (route): Promise<void> => { if (route.request().method() === 'POST') mutations++; else polls++; await route.fulfill({ json: f.status }) })
  await enter(page)
  await expect.poll((): number => polls).toBe(1)
  await page.getByRole('button', { name: 'Files changed', exact: true }).click()
  const previous = polls
  await page.waitForTimeout(1200)
  expect(polls).toBe(previous)
  expect(mutations).toBe(0)
  await enter(page)
  await expect.poll((): number => polls).toBe(previous + 1)
  expect(mutations).toBe(0)
})
