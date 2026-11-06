import { expect, test, type Page } from '@playwright/test'
import { digest, registered, sourceFixture, token } from './runtime-fixture'
import { quizFixture, ignoreQuizAbort } from './quiz-fixture'
import type { QuizRead, QuizStatus } from '../src/runtime/quiz'

test.use({ baseURL: 'http://127.0.0.1:5175' })

/**
 * Opens the real quiz destination without submitting work.
 * @param page Connected fixture page.
 */
async function enter(page: Page): Promise<void> { await page.getByRole('button', { name: 'Quiz', exact: true }).click() }

/**
 * Loads explicit renderer source before entering Quiz.
 * @param page Connected fixture page.
 */
async function load(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Refresh source', exact: true })).toBeVisible()
}

/**
 * Disconnects and explicitly verifies a fresh connection without reloading the document.
 * @param page Connected fixture page.
 */
async function reconnect(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  const consent = page.getByRole('button', { name: 'Discard page state and disconnect', exact: true })
  if (await consent.isVisible()) await consent.click()
  await page.getByRole('textbox', { name: 'API token', exact: true }).fill(await token())
  await page.getByRole('button', { name: 'Connect backend', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Quiz', exact: true })).toBeVisible()
}

for (const change of ['base', 'head']) test(`${change} refresh and older generation completion never retarget saved checks or source evidence`, async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, change === 'base' ? 960 : 961)
  const f = quizFixture(pr)
  const newer = sourceFixture(pr, 1, change === 'base' ? 'd'.repeat(40) : undefined, change === 'head' ? 'e'.repeat(40) : undefined)
  let finished = false
  let saved = f.read
  let latest: QuizStatus | null = null
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    await route.fulfill({ json: route.request().postDataJSON().action === 'refresh' ? { ...newer, requestedRevisionID: f.source.diff!.revisionID } : f.source })
  })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: { ...saved, generation: latest } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => { expect(route.request().postDataJSON().revision).toEqual(f.source.diff!.revision); latest = f.status; await route.fulfill({ status: 202, json: f.status }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/${f.status.generationID}`, async (route): Promise<void> => {
    const status: QuizStatus = finished ? { ...f.status, sequence: '4', state: 'completed', savedVersion: '2' } : { ...f.status, sequence: '2', state: 'generating' }
    latest = status
    if (finished) saved = { ...f.read, version: '2', quiz: { ...f.read.quiz!, quizID: f.status.quizID }, generation: status }
    await route.fulfill({ json: status })
  })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => {
    expect(route.request().postDataJSON()).toEqual({ revisionID: f.read.quiz!.revisionID, choiceID: 'a' })
    await route.fulfill({ json: { ...f.checked, version: saved.version, quizID: saved.quiz!.quizID } })
  })
  await load(page)
  await enter(page)
  await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
  await page.getByRole('button', { name: 'Generate replacement', exact: true }).click()
  await page.getByRole('button', { name: 'Files changed', exact: false }).click()
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByText('Refresh complete. Only this tab now displays the new pinned revision.', { exact: true })).toBeVisible()
  await enter(page)
  await expect(page.getByText('Older revision:', { exact: false })).toBeVisible()
  finished = true
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await expect(page.getByText('Older revision:', { exact: false })).toBeVisible()
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'View in original diff', exact: true })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath(`quiz-old-${change}-completion.png`), fullPage: true })
})

test('a delayed check cannot grade a replacement or move a later question and focus', async ({ page }): Promise<void> => {
  const pr = await registered(page, 962)
  const f = quizFixture(pr)
  let current: QuizRead = { ...f.read, generation: f.status }
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: current }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*`, async (route): Promise<void> => { await route.fulfill({ json: current.generation }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => { await new Promise<void>((resolve): void => { release = resolve }); await route.fulfill({ json: f.checked }) })
  await ignoreQuizAbort(page)
  await enter(page)
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  current = { ...f.read, version: '2', quiz: { ...f.read.quiz!, quizID: f.status.quizID }, generation: { ...f.status, state: 'completed', sequence: '4', savedVersion: '2' } }
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await page.getByRole('button', { name: 'Next question', exact: true }).click()
  await page.getByRole('radio').nth(2).check()
  await expect(page.getByRole('radio').nth(2)).toBeFocused()
  release!()
  await expect(page.locator('legend')).toHaveText('Renderer question 2: <literal question>')
  await expect(page.getByRole('radio').nth(2)).toBeChecked()
  await expect(page.getByRole('radio').nth(2)).toBeFocused()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toHaveCount(0)
  await expect(page.getByText('0 of 5 checked in this page', { exact: true })).toBeVisible()
})

test('a same-set late check stays on its original question without stealing another selection or focus', async ({ page }): Promise<void> => {
  const pr = await registered(page, 963)
  const f = quizFixture(pr)
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: f.read }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => { await new Promise<void>((resolve): void => { release = resolve }); await route.fulfill({ json: f.checked }) })
  await enter(page)
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Next question', exact: true }).click()
  await page.getByRole('radio').nth(2).check()
  release!()
  await expect(page.getByText('1 of 5 checked in this page', { exact: true })).toBeVisible()
  await expect(page.locator('legend')).toHaveText('Renderer question 2: <literal question>')
  await expect(page.getByRole('radio').nth(2)).toBeFocused()
  await expect(page.getByRole('radio').nth(2)).toBeChecked()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Previous', exact: true }).click()
  await expect(page.getByText(f.checked.explanation, { exact: true })).toBeVisible()
  await expect(page.getByRole('radio').first()).toBeChecked()
})

for (const action of ['read', 'generate', 'check', 'status', 'cancel']) test(`late ${action} ignores transport cancellation but cannot populate a reconnected quiz`, async ({ page }): Promise<void> => {
  const pr = await registered(page, 964 + ['read', 'generate', 'check', 'status', 'cancel'].indexOf(action))
  const f = quizFixture(pr)
  let current: QuizRead = { ...f.read, generation: action === 'status' || action === 'cancel' ? f.status : null }
  let release: (() => void) | undefined
  let delivered = false
  let held = false
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => {
    const response = current
    if (action === 'read' && !held) { held = true; await new Promise<void>((resolve): void => { release = resolve }); delivered = true }
    await route.fulfill({ json: response })
  })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/**`, async (route): Promise<void> => {
    if (action === 'status' || action === 'cancel' && route.request().url().endsWith('/cancel')) { held = true; await new Promise<void>((resolve): void => { release = resolve }); delivered = true }
    await route.fulfill({ status: route.request().method() === 'POST' ? 202 : 200, json: f.status })
  })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => { held = true; await new Promise<void>((resolve): void => { release = resolve }); delivered = true; await route.fulfill({ status: 202, json: f.status }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => { held = true; await new Promise<void>((resolve): void => { release = resolve }); delivered = true; await route.fulfill({ json: f.checked }) })
  await ignoreQuizAbort(page)
  if (action === 'generate') await load(page)
  await enter(page)
  if (action === 'generate') { await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click(); await page.getByRole('button', { name: 'Generate replacement', exact: true }).click() }
  if (action === 'check') { await page.getByRole('radio').first().check(); await page.getByRole('button', { name: 'Check answer', exact: true }).click() }
  if (action === 'cancel') await page.getByRole('button', { name: 'Cancel generation', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  current = { ...f.read, version: '2', quiz: { ...f.read.quiz!, quizID: f.status.quizID }, generation: null }
  await reconnect(page)
  await enter(page)
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await page.getByRole('button', { name: 'Next question', exact: true }).click()
  await page.getByRole('radio').nth(2).check()
  release!()
  await expect.poll((): boolean => delivered).toBe(true)
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await expect(page.locator('legend')).toHaveText('Renderer question 2: <literal question>')
  await expect(page.getByRole('radio').nth(2)).toBeFocused()
  await expect(page.getByRole('radio').nth(2)).toBeChecked()
  await expect(page.getByText('0 of 5 checked in this page', { exact: true })).toBeVisible()
  await expect(page.locator('.generation-status')).toHaveCount(0)
})

test('regressing and mutable snapshots cannot overwrite an immutable saved quiz or checked answers', async ({ page }): Promise<void> => {
  const pr = await registered(page, 969)
  const f = quizFixture(pr)
  let current = { ...f.read, version: '18446744073709551615' }
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: current }) })
  await enter(page)
  await page.getByRole('radio').first().check()
  for (const invalid of [
    { ...f.read, version: '18446744073709551614' },
    { ...current, quiz: { ...f.read.quiz!, quizID: f.status.quizID } },
    { ...current, quiz: { ...f.read.quiz!, questions: [{ ...f.read.quiz!.questions[0]!, prompt: 'MUTATED IMMUTABLE QUESTION' }, ...f.read.quiz!.questions.slice(1)] } },
  ]) {
    current = invalid
    await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
    await expect(page.getByRole('alert')).toContainText('Cannot read saved quiz')
    await expect(page.locator('.quiz-revision')).toContainText('18446744073709551615')
    await expect(page.getByRole('radio').first()).toBeChecked()
    await expect(page.locator('legend')).toHaveText('Renderer question 1: <literal question>')
  }
})

test('two tabs compete only for their own PR generation and keep independent choices and question positions', async ({ page, context }): Promise<void> => {
  const pages = [page, await context.newPage(), await context.newPage()]
  const active = new Map<string, QuizStatus>()
  const admissions = new Map<string, number>()
  for (const [i, tab] of pages.entries()) {
    const pr = await registered(tab, i < 2 ? 970 : 971)
    const f = quizFixture(pr)
    await tab.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
    await tab.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: { ...f.read, generation: active.get(pr.id) ?? null } }) })
    await tab.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => {
      admissions.set(pr.id, (admissions.get(pr.id) ?? 0)+1)
      if (active.has(pr.id)) { await route.fulfill({ status: 409, json: { code: 'quiz-busy' } }); return }
      active.set(pr.id, f.status)
      await route.fulfill({ status: 202, json: f.status })
    })
    await tab.route(`**/api/reviews/${pr.id}/quiz/generations/*`, async (route): Promise<void> => { await route.fulfill({ json: active.get(pr.id) }) })
    await load(tab)
    await enter(tab)
    await tab.getByRole('radio').nth(i).check()
  }
  for (const tab of [pages[0]!, pages[1]!, pages[2]!]) {
    await tab.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
    await tab.getByRole('button', { name: 'Generate replacement', exact: true }).click()
  }
  await expect(pages[1]!.getByRole('alert')).toContainText('admission could not be confirmed')
  await expect(pages[0]!.getByText('Generation: pending', { exact: true })).toBeVisible()
  await expect(pages[2]!.getByText('Generation: pending', { exact: true })).toBeVisible()
  await pages[1]!.getByRole('button', { name: 'Next question', exact: true }).click()
  await pages[1]!.getByRole('radio').nth(3).check()
  await pages[1]!.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
  await expect(pages[1]!.getByText('Generation: pending', { exact: true })).toBeVisible()
  await expect(pages[1]!.getByRole('radio').nth(3)).toBeChecked()
  await expect(pages[0]!.getByRole('radio').first()).toBeChecked()
  await expect(pages[2]!.getByRole('radio').nth(2)).toBeChecked()
  expect([...admissions.values()].sort()).toEqual([1, 2])
  expect(active.size).toBe(2)
  for (const tab of pages) expect(await tab.content()).not.toContain(await token())
})

test('a retired status read cannot displace the next attempt', async ({ page }): Promise<void> => {
  const pr = await registered(page, 972)
  const f = quizFixture(pr)
  let latest: QuizStatus = { ...f.status, state: 'generating', sequence: '2' }
  let release: (() => void) | undefined
  let delivered = false
  let held = false
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: { ...f.read, generation: latest } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*`, async (route): Promise<void> => {
    if (!held) { held = true; await new Promise<void>((resolve): void => { release = resolve }); delivered = true; await route.fulfill({ json: { ...f.status, state: 'generating', sequence: '2' } }) }
    else await route.fulfill({ json: latest })
  })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*/cancel`, async (route): Promise<void> => { latest = { ...f.status, state: 'cancelled', sequence: '4', cancelRequested: true }; await route.fulfill({ status: 202, json: latest }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations`, async (route): Promise<void> => { latest = { ...f.status, generationID: digest('new-attempt-972'), quizID: digest('new-candidate-972') }; await route.fulfill({ status: 202, json: latest }) })
  await ignoreQuizAbort(page)
  await load(page)
  await enter(page)
  await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Cancel generation', exact: true }).click()
  await expect(page.getByText('Generation cancelled. The previously saved set is retained.', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
  await page.getByRole('button', { name: 'Generate replacement', exact: true }).click()
  release!()
  await expect.poll((): boolean => delivered).toBe(true)
  await expect(page.getByText('Generation: pending', { exact: true })).toBeVisible()
  await expect(page.getByText('Generation cancelled. The previously saved set is retained.', { exact: true })).toHaveCount(0)
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('a read from a previous view visit cannot replace a fresh saved read even when transport ignores abort', async ({ page }): Promise<void> => {
  const pr = await registered(page, 973)
  const f = quizFixture(pr)
  let held = false
  let release: (() => void) | undefined
  let delivered = false
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => {
    if (!held) { held = true; await new Promise<void>((resolve): void => { release = resolve }); delivered = true; await route.fulfill({ json: f.read }) }
    else await route.fulfill({ json: { ...f.read, version: '2', quiz: { ...f.read.quiz, quizID: f.status.quizID } } })
  })
  await ignoreQuizAbort(page)
  await enter(page)
  await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Files changed', exact: true }).click()
  await enter(page)
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await page.getByRole('button', { name: 'Next question', exact: true }).click()
  release!()
  await expect.poll((): boolean => delivered).toBe(true)
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await expect(page.locator('legend')).toBeFocused()
  await expect(page.locator('legend')).toHaveText('Renderer question 2: <literal question>')
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('late cancellation feedback cannot regress a higher-sequence saved completion', async ({ page }): Promise<void> => {
  const pr = await registered(page, 975)
  const f = quizFixture(pr)
  let completed = false
  let release: (() => void) | undefined
  const final: QuizStatus = { ...f.status, state: 'completed', sequence: '5', savedVersion: '2' }
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: completed ? { ...f.read, version: '2', quiz: { ...f.read.quiz, quizID: f.status.quizID }, generation: final } : { ...f.read, generation: f.status } }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*`, async (route): Promise<void> => { completed = true; await route.fulfill({ json: final }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*/cancel`, async (route): Promise<void> => { await new Promise<void>((resolve): void => { release = resolve }); await route.fulfill({ status: 202, json: { ...f.status, state: 'saving', sequence: '3' } }) })
  await enter(page)
  await page.getByRole('button', { name: 'Cancel generation', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  await expect(page.getByText('Generation: completed', { exact: true })).toBeVisible()
  release!()
  await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
  await expect(page.getByText('Generation: completed', { exact: true })).toBeVisible()
  await expect(page.getByRole('alert')).toHaveCount(0)
})

test('long literal quiz content keeps independent keyboard scrolling and reachable actions', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 976)
  const f = quizFixture(pr)
  const read: QuizRead = { ...f.read, quiz: { ...f.read.quiz!, questions: f.read.quiz!.questions.map((q, i): typeof q => ({ ...q, prompt: `${q.prompt} ${'Literal boundary details <not markup>. '.repeat(30)} ${i}`, choices: q.choices.map((c): typeof c => ({ ...c, text: `${c.text} ${'Long literal alternative details. '.repeat(25)}` })) })) } }
  const explanation = `Original retained evidence, not a new source read.\n${'<literal explanation> '.repeat(150)}`
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: read }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/*/questions/q1/check`, async (route): Promise<void> => { await route.fulfill({ json: { ...f.checked, explanation } }) })
  await enter(page)
  await page.getByRole('radio').first().check()
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  await expect(page.getByText(explanation, { exact: true })).toBeVisible()
  const main = page.getByRole('region', { name: 'Quiz questions and generation', exact: true })
  const side = page.getByRole('region', { name: 'Quiz explanation and original evidence', exact: true })
  await main.focus()
  await page.keyboard.press('Home')
  await expect.poll((): Promise<number> => main.evaluate((node): number => node.scrollTop)).toBe(0)
  await side.focus()
  await page.keyboard.press('End')
  await expect.poll((): Promise<boolean> => side.evaluate((node): boolean => node.scrollTop > 0 && Math.abs(node.scrollHeight-node.clientHeight-node.scrollTop) < 1)).toBe(true)
  expect(await main.evaluate((node): number => node.scrollTop)).toBe(0)
  const sideScroll = await side.evaluate((node): number => node.scrollTop)
  await main.focus()
  await page.keyboard.press('End')
  await expect(page.getByRole('button', { name: 'Next question', exact: true })).toBeInViewport()
  expect(await side.evaluate((node): number => node.scrollTop)).toBe(sideScroll)
  await page.screenshot({ path: testInfo.outputPath('quiz-long-independent-scroll.png'), fullPage: true })
  await page.setViewportSize({ width: 700, height: 1000 })
  await page.addStyleTag({ content: 'html { font-size: 200% !important; }' })
  await page.getByRole('button', { name: 'Next question', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('button', { name: 'Next question', exact: true })).toBeInViewport()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('quiz-long-700-200-text.png'), fullPage: true })
})

for (const fault of ['same sequence', 'changed scope']) test(`saved reads reject ${fault} generation feedback without clearing the current set`, async ({ page }): Promise<void> => {
  const pr = await registered(page, fault === 'same sequence' ? 977 : 978)
  const f = quizFixture(pr)
  let current: QuizRead = { ...f.read, generation: f.status }
  await page.route(`**/api/reviews/${pr.id}/quiz`, async (route): Promise<void> => { await route.fulfill({ json: current }) })
  await page.route(`**/api/reviews/${pr.id}/quiz/generations/*`, async (route): Promise<void> => { await route.fulfill({ json: f.status }) })
  await enter(page)
  await page.getByRole('radio').first().check()
  current = { ...f.read, generation: fault === 'same sequence' ? { ...f.status, state: 'generating' } : { ...f.status, sequence: '2', quizID: digest('changed-candidate') } }
  await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Cannot read saved quiz')
  await expect(page.getByText('Generation: pending', { exact: true })).toBeVisible()
  await expect(page.getByRole('radio').first()).toBeChecked()
})
