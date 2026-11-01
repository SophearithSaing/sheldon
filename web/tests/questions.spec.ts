import { expect, test, type Page } from '@playwright/test'

/**
 * Opens the populated sample's Questions view.
 * @param fixtures Playwright page.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/?sample=review')
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
})

/**
 * Selects one local thread from its full-row question control.
 * @param page Page containing the Questions view.
 * @param number Two-digit thread number.
 */
async function selectQuestion(page: Page, number: string): Promise<void> {
  await page.getByRole('button', { name: new RegExp(`^Open question ${number}:`) }).click()
  await expect(page.getByRole('heading', { name: `Question ${number}`, exact: true })).toBeFocused()
}

/**
 * Exercises sample search, transcript matching, status filters and stable within-group order.
 * @param fixtures Playwright page.
 */
test('question search, filters and sorting use only this PR sample data', async ({ page }): Promise<void> => {
  const questions = page.getByRole('region', { name: 'Questions in this PR', exact: true })
  const filters = page.getByRole('group', { name: 'Filter question status' })
  await expect(filters.getByRole('button', { name: 'All', exact: true })).toHaveText('All 4')
  await expect(filters.getByRole('button', { name: 'Open', exact: true })).toHaveText('Open 3')
  await expect(filters.getByRole('button', { name: 'Understood', exact: true })).toHaveText('Understood 1')
  await filters.getByRole('button', { name: 'Open', exact: true }).click()
  await expect(questions.getByRole('button', { name: /^Open question/ })).toHaveCount(3)
  const search = page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' })
  await search.fill('shares work, not the result')
  await expect(questions.getByRole('button', { name: /^Open question/ })).toHaveCount(1)
  await expect(questions.getByRole('button', { name: /^Open question 02:/ })).toBeVisible()
  await search.fill('no-match')
  await expect(page.getByRole('heading', { name: 'No matching sample questions' })).toBeVisible()
  await page.getByRole('button', { name: 'Clear search', exact: true }).click()
  await search.fill('src/auth/refresh-session.ts')
  await expect(questions.getByRole('button', { name: /^Open question/ })).toHaveCount(2)
  await page.getByLabel('Sort sample questions', { exact: true }).filter({ visible: true }).selectOption('oldest')
  await expect(questions.getByRole('button', { name: /^Open question/ }).first()).toHaveAttribute('aria-label', /^Open question 02:/)
  await search.fill('')
  await filters.getByRole('button', { name: 'Understood', exact: true }).click()
  await expect(questions.getByRole('button', { name: /^Open question/ })).toHaveCount(1)
  await expect(questions.getByRole('button', { name: /^Open question 04:/ })).toBeVisible()
})

/**
 * Keeps each transcript, draft and native scroll position distinct under shared-session busy state.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('shared revision busy state preserves selected-thread drafts and transcripts', async ({ page }, testInfo): Promise<void> => {
  await selectQuestion(page, '02')
  const panel = page.getByRole('complementary', { name: 'Question 02', exact: true })
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await expect(draft).toHaveValue('Could you walk me through the failure case?')
  await expect(panel).toContainText('It shares work, not the result.')
  await expect(panel.getByRole('button', { name: 'Send — unavailable' })).toBeDisabled()
  await expect(panel.getByRole('button', { name: 'Send — unavailable' })).toHaveAccessibleDescription(/Question 03 is using the shared PR-revision session/)
  await draft.fill('Question 02 local draft')
  await page.screenshot({ path: testInfo.outputPath('questions-busy-1440.png'), fullPage: true })
  const messages = panel.getByRole('region', { name: 'Sample conversation messages' })
  await messages.evaluate((element): void => { element.scrollTop = 90 })
  const scroll = await messages.evaluate((element): number => element.scrollTop)
  await selectQuestion(page, '03')
  await expect.poll(async (): Promise<number> => page.getByRole('region', { name: 'Sample conversation messages' }).evaluate((element): number => element.scrollTop)).toBe(0)
  await page.screenshot({ path: testInfo.outputPath('thread-investigating.png'), fullPage: true })
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('')
  await expect(page.getByRole('complementary', { name: 'Question 03', exact: true })).not.toContainText('It shares work, not the result.')
  await selectQuestion(page, '02')
  await expect(draft).toHaveValue('Question 02 local draft')
  await expect.poll(async (): Promise<number> => messages.evaluate((element): number => element.scrollTop)).toBe(scroll)
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await expect(page.getByRole('button', { name: /^Open question 02:/ })).toBeFocused()
  await selectQuestion(page, '02')
  await expect(draft).toHaveValue('Question 02 local draft')
  await expect.poll(async (): Promise<number> => messages.evaluate((element): number => element.scrollTop)).toBe(scroll)
})

/**
 * Makes understanding a reversible reader decision separate from Viewed or answers.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('mark understood updates only its thread and offers undo and reopen', async ({ page }, testInfo): Promise<void> => {
  await selectQuestion(page, '02')
  const filters = page.getByRole('group', { name: 'Filter question status' })
  await page.getByRole('button', { name: 'Mark understood', exact: true }).click()
  await expect(filters.getByRole('button', { name: 'Understood', exact: true })).toHaveText('Understood 2')
  await expect(filters.getByRole('button', { name: 'Open', exact: true })).toHaveText('Open 2')
  await expect(page.getByRole('complementary', { name: 'Question 02', exact: true })).toContainText('Not PR approval.')
  await expect(page.getByRole('button', { name: 'Undo understood', exact: true })).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('thread-understood.png'), fullPage: true })
  await page.getByRole('button', { name: 'Undo understood', exact: true }).click()
  await expect(filters.getByRole('button', { name: 'Understood', exact: true })).toHaveText('Understood 1')
  await expect(page.getByRole('button', { name: 'Mark understood', exact: true })).toBeFocused()
  await selectQuestion(page, '04')
  await page.getByRole('button', { name: 'Reopen question', exact: true }).click()
  await expect(filters.getByRole('button', { name: 'Understood', exact: true })).toHaveText('Understood 0')
  await page.getByRole('tab', { name: 'Files changed', exact: true }).click()
  await expect(page.getByRole('checkbox', { name: 'Viewed', exact: true })).not.toBeChecked()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '4 sample items')
})

/**
 * Prevents unavailable Send from appending, reopening or dispatching through keyboard input.
 * @param fixtures Playwright page.
 */
test('follow-up Enter is guarded while Shift+Enter and IME retain text', async ({ page }): Promise<void> => {
  await selectQuestion(page, '04')
  const requests: string[] = []
  page.on('request', (request): void => { requests.push(request.url()) })
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Local follow-up')
  await draft.press('Enter')
  await expect(draft).toHaveValue('Local follow-up')
  await draft.press('Shift+Enter')
  await expect(draft).toHaveValue('Local follow-up\n')
  await draft.dispatchEvent('keydown', { key: 'Enter', isComposing: true })
  await expect(draft).toHaveValue('Local follow-up\n')
  await expect(page.getByRole('complementary', { name: 'Question 04', exact: true }).locator('.thread-message')).toHaveCount(4)
  await expect(page.getByRole('button', { name: 'Reopen question', exact: true })).toBeVisible()
  expect(requests).toEqual([])
})

/**
 * Returns source navigation to the selected thread's coordinates and restores list state.
 * @param fixtures Playwright page.
 */
test('context and citations select the owning thread and restore source focus', async ({ page }): Promise<void> => {
  await selectQuestion(page, '02')
  await page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' }).fill('promise')
  const panel = page.getByRole('complementary', { name: 'Question 02', exact: true })
  await panel.locator('.thread-citations button').click()
  await expect(page.getByRole('tab', { name: 'Files changed', exact: true })).toHaveAttribute('aria-selected', 'true')
  const end = page.getByRole('button', { name: 'Select head line 35', exact: true })
  await expect(end).toBeFocused()
  await expect(end).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('button', { name: 'Select head line 23', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(panel).toBeVisible()
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await expect(end).toBeFocused()
  await page.getByRole('button', { name: 'Open sample question 2', exact: true }).click()
  await expect(panel).toBeVisible()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await expect(page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' })).toHaveValue('promise')
  await panel.locator('.thread-source').click()
  await expect(end).toBeFocused()
})

/**
 * Guards citations against silently changing the context of an unsent inline draft.
 * @param fixtures Playwright page.
 */
test('citation navigation guards inline drafts before changing pinned coordinates', async ({ page }): Promise<void> => {
  await page.getByRole('tab', { name: 'Files changed', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selected lines', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Keep this range')
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await selectQuestion(page, '04')
  await page.getByRole('button', { name: 'View in diff', exact: true }).click()
  await expect(page.getByRole('alert', { name: 'Keep your draft context' })).toBeFocused()
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Keep this range')
  await page.getByRole('button', { name: 'View in diff', exact: true }).click()
  await page.getByRole('button', { name: 'Keep draft and switch', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Select head line 44', exact: true })).toBeFocused()
  await expect(page.getByRole('complementary', { name: 'Question 04', exact: true })).toBeVisible()
})

/**
 * Exercises recoverable status previews and outdated source without fabricated transcript changes.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('failed interrupted and outdated previews retain transcript and draft', async ({ page }, testInfo): Promise<void> => {
  await selectQuestion(page, '02')
  const panel = page.getByRole('complementary', { name: 'Question 02', exact: true })
  await panel.getByText('Thread state previews', { exact: true }).click()
  const state = page.getByLabel('Sample thread state', { exact: true }).filter({ visible: true })
  await expect(state.locator('option[value="investigating"]')).toBeDisabled()
  await state.selectOption('failed')
  await expect(panel.getByRole('alert')).toContainText('Sample response failed.')
  await page.screenshot({ path: testInfo.outputPath('thread-failed.png'), fullPage: true })
  await state.selectOption('interrupted')
  await expect(panel).toContainText('Interrupted sample response.')
  await page.screenshot({ path: testInfo.outputPath('thread-interrupted.png'), fullPage: true })
  await state.selectOption('outdated')
  await expect(panel).toContainText('Outdated context preview.')
  await expect(panel.locator('.thread-source')).toHaveAttribute('aria-disabled', 'true')
  await panel.locator('.thread-source').click({ force: true })
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(panel).toContainText('It shares work, not the result.')
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Could you walk me through the failure case?')
  await page.screenshot({ path: testInfo.outputPath('thread-outdated.png'), fullPage: true })
  await state.selectOption('answered')
  await expect(panel.getByRole('button', { name: 'Mark understood', exact: true })).toBeVisible()
})

/**
 * Checks reflow without hiding statuses or clipping controls at narrow and scaled sizes.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('questions and conversation remain reachable at narrow and scaled sizes', async ({ page }, testInfo): Promise<void> => {
  await page.setViewportSize({ width: 899, height: 1080 })
  await selectQuestion(page, '02')
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Retained at narrow size')
  await page.screenshot({ path: testInfo.outputPath('questions-899.png'), fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1080 })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Retained at narrow size')
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await expect(page.getByRole('button', { name: /^Open question 02:/ })).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('questions-text-scale-200.png'), fullPage: true })
})

/**
 * Preserves PR-local filters, selection, understanding and drafts across workspace switches.
 * @param fixtures Playwright page.
 */
test('question workspace state is isolated between sample PRs', async ({ page }): Promise<void> => {
  await selectQuestion(page, '02')
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Only PR 248')
  await page.getByRole('button', { name: 'Mark understood', exact: true }).click()
  await page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' }).fill('promise')
  await page.getByRole('group', { name: 'Filter question status' }).getByRole('button', { name: 'Understood', exact: true }).click()
  await page.getByRole('link', { name: 'platform #251 — Retry handling', exact: true }).click()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await expect(page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' })).toHaveValue('')
  await expect(page.getByRole('group', { name: 'Filter question status' }).getByRole('button', { name: 'Understood', exact: true })).toHaveText('Understood 1')
  await selectQuestion(page, '02')
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Could you walk me through the failure case?')
  await page.getByRole('link', { name: 'platform #248 — Session refresh', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Only PR 248')
  await expect(page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' })).toHaveValue('promise')
  await expect(page.getByRole('group', { name: 'Filter question status' }).getByRole('button', { name: 'Understood', exact: true })).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('button', { name: 'Undo understood', exact: true })).toBeVisible()
})
