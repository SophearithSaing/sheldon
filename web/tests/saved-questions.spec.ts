import { expect, test, type Page } from '@playwright/test'
import { registered, digest } from './runtime-fixture'
import { historyFixture } from './history-fixture'
import type { QuestionAnswer } from '../src/runtime/questions'

test.use({ baseURL: 'http://127.0.0.1:5175' })

/**
 * Routes explicitly synthetic saved records without loading source or native execution.
 * @param page Fixture browser.
 * @param number Fixture PR number.
 * @returns Mutable saved response fixtures.
 */
async function saved(page: Page, number: number): Promise<ReturnType<typeof historyFixture>> {
  const pr = await registered(page, number)
  const data = historyFixture(pr)
  await page.route(`**/api/reviews/${pr.id}/threads`, async (route): Promise<void> => { await route.fulfill({ json: data.list }) })
  await page.route(`**/api/reviews/${pr.id}/threads/${data.detail.threadID}`, async (route): Promise<void> => { await route.fulfill({ json: data.detail }) })
  return data
}

test('saved history opens source-free with literal quotes and responsive controls', async ({ page }, info): Promise<void> => {
  const data = await saved(page, 780)
  const forbidden: string[] = []
  page.on('request', (request): void => { if (/\/source$|\/answers|\/questions$/.test(new URL(request.url()).pathname)) forbidden.push(request.url()) })
  await page.getByRole('button', { name: 'Questions', exact: true }).click()
  const row = page.getByRole('button', { name: `Open thread: ${data.detail.answers[0]!.question}`, exact: true })
  await row.click()
  await expect(page.locator('.answer-text')).toHaveText(data.detail.answers[0]!.text)
  await expect(page.locator('.answer-text script')).toHaveCount(0)
  await expect(page.locator('.session-status')).toContainText('verifies the original pinned source')
  await page.getByText('Saved source quote', { exact: true }).click()
  await expect(page.locator('.saved-quotes pre')).toContainText(data.detail.context.selected[0]!.text)
  await page.screenshot({ path: info.outputPath('saved-history-desktop.png'), fullPage: true })
  for (const scaled of [false, true]) {
    await page.setViewportSize({ width: 700, height: 900 })
    if (scaled) await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
    await page.getByRole('button', { name: 'Send follow-up', exact: true }).scrollIntoViewIfNeeded()
    await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeInViewport()
    expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath(scaled ? 'saved-history-text-200.png' : 'saved-history-700.png'), fullPage: true })
  }
  expect(forbidden).toEqual([])
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await expect(row).toBeFocused()
})

test('list and detail errors remain explicit and empty history does not invent rows', async ({ page }): Promise<void> => {
  const data = await saved(page, 781)
  await page.route('**/threads', async (route): Promise<void> => { await route.fulfill({ status: 500, json: { error: 'PRIVATE STORAGE PATH' } }) })
  await page.getByRole('button', { name: 'Questions', exact: true }).click()
  await expect(page.locator('.saved-questions [role=alert]')).toBeVisible()
  expect(await page.content()).not.toContain('PRIVATE STORAGE PATH')
  await page.unroute('**/threads')
  data.list.threads = []
  await page.getByRole('button', { name: 'Load saved questions', exact: true }).click()
  await expect(page.getByText('No saved questions in this PR.', { exact: false })).toBeVisible()
  data.list.version = '8'
  data.list.threads = [{ threadID: data.detail.threadID, revisionID: data.detail.revisionID, question: data.detail.answers[0]!.question, anchor: data.detail.context.anchor, answerCount: 1, state: 'completed', executionUnknown: false, persistence: 'saved' }]
  await page.getByRole('button', { name: 'Refresh saved questions', exact: true }).click()
  await page.route(`**/threads/${data.detail.threadID}`, async (route): Promise<void> => { await route.fulfill({ status: 404, json: {} }) })
  await page.getByRole('button', { name: /Open thread:/ }).click()
  await expect(page.locator('.saved-questions [role=alert]')).toBeVisible()
  await expect(page.locator('.answer-text')).toHaveCount(0)
})

test('historical interruption permits explicit verification but unavailable sessions retain drafts', async ({ page }): Promise<void> => {
  const data = await saved(page, 782)
  Object.assign(data.detail.answers[0]!, { state: 'interrupted', executionUnknown: true, failureCode: 'runtime-restarted', text: 'Synthetic saved partial.' })
  await page.getByRole('button', { name: 'Questions', exact: true }).click()
  await page.getByRole('button', { name: /Open thread:/ }).click()
  await expect(page.locator('.execution-status')).toContainText('Interrupted after restart')
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Explicit new input only.')
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeEnabled()
  let attempts = 0
  await page.route('**/threads/*/answers', async (route): Promise<void> => { attempts++; await route.fulfill({ status: 410, json: {} }) })
  await draft.press('Enter')
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  await expect(draft).toHaveValue('Explicit new input only.')
  await expect(draft).toBeEditable()
  expect(attempts).toBe(1)
  data.detail.session = { state: 'unavailable', reason: 'incompatible-binding' }
  await page.getByRole('button', { name: 'Refresh saved thread', exact: true }).click()
  await expect(page.locator('.session-status')).toContainText('incompatible')
  await expect(draft).toHaveValue('Explicit new input only.')
  await expect(page.locator('.answer-text')).toHaveText('Synthetic saved partial.')
})

test('late row reads cannot steal a later view or focus intent', async ({ page }): Promise<void> => {
  const data = await saved(page, 783)
  let release: (() => void) | undefined
  await page.route(`**/threads/${data.detail.threadID}`, async (route): Promise<void> => {
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ json: data.detail })
  })
  await page.getByRole('button', { name: 'Questions', exact: true }).click()
  await page.getByRole('button', { name: /Open thread:/ }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Files changed', exact: true }).click()
  release!()
  await expect(page.getByRole('heading', { name: 'PR registered. Source not loaded.', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Files changed', exact: true })).toBeFocused()
  await expect(page.locator('.answer-text')).toHaveCount(0)
})

test('explicit refresh inserts remote turns without losing a retained follow-up draft', async ({ page }): Promise<void> => {
  const data = await saved(page, 784)
  await page.getByRole('button', { name: 'Questions', exact: true }).click()
  await page.getByRole('button', { name: /Open thread:/ }).click()
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Retain my thread-local draft.')
  data.detail.version = '8'
  data.detail.answers.push({ ...data.detail.answers[0]!, answerID: digest('remote-saved-followup'), question: 'Synthetic other-tab question.', text: 'Synthetic other-tab answer.' })
  await page.getByRole('button', { name: 'Refresh saved thread', exact: true }).click()
  await expect(page.locator('.answer-text')).toHaveText([data.detail.answers[0]!.text, 'Synthetic other-tab answer.'])
  await expect(draft).toHaveValue('Retain my thread-local draft.')
  await expect(page.getByRole('button', { name: 'Refresh saved thread', exact: true })).toBeFocused()
})

test('saved ordering inserts another tab turn before a locally observed follow-up', async ({ page }): Promise<void> => {
  const data = await saved(page, 786)
  const initial = data.detail.answers[0]!
  const admitted: QuestionAnswer = { ...initial, answerID: digest('local-admitted-followup'), question: 'Synthetic local follow-up.', state: 'pending', text: '', persistence: 'saved', savedVersion: '9' }
  await page.route('**/threads/*/answers', async (route): Promise<void> => { await route.fulfill({ status: 202, json: admitted }) })
  await page.route('**/answers/*/events', async (route): Promise<void> => { await route.fulfill({ contentType: 'text/event-stream', body: `event: answer\ndata: ${JSON.stringify({ ...admitted, state: 'completed', text: 'Synthetic live local result.', savedVersion: '10' })}\n\n` }) })
  await page.getByRole('button', { name: 'Questions', exact: true }).click()
  await page.getByRole('button', { name: /Open thread:/ }).click()
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill(admitted.question)
  await draft.press('Enter')
  await expect(page.locator('.answer-text')).toHaveText([initial.text, 'Synthetic live local result.'])
  await draft.fill('Keep the next draft.')
  data.detail.version = '10'
  data.detail.answers.push({ ...initial, answerID: digest('remote-before-local'), question: 'Synthetic remote predecessor.', text: 'Synthetic remote predecessor result.' }, { ...initial, answerID: admitted.answerID, question: admitted.question, text: 'Synthetic live local result.' })
  await page.getByRole('button', { name: 'Refresh saved thread', exact: true }).click()
  await expect(page.locator('.answer-text')).toHaveText([initial.text, 'Synthetic remote predecessor result.', 'Synthetic live local result.'])
  await expect(draft).toHaveValue('Keep the next draft.')
  await expect(page.locator('.thread-summary [role=alert]')).toHaveCount(0)
})
