import { expect, test } from '@playwright/test'
import { openSampleThread } from './preview'

/**
 * Opens the offline first-use preview.
 * @param fixtures Playwright page.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/')
})

/**
 * Checks exact local creation counts and marker handoff without requests.
 * @param fixtures Playwright page.
 */
test('one submission creates one thread and reopening never increments counts', async ({ page }): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => { requests.push(request.url()) })
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  await page.getByRole('button', { name: 'Preview thread creation', exact: true }).click()
  await expect(page.getByText('Enter a question before previewing thread creation.')).toBeVisible()
  await openSampleThread(page)
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '1 sample items')
  await expect(page.getByRole('checkbox', { name: 'Viewed', exact: true })).not.toBeChecked()
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  const marker = page.getByRole('button', { name: 'Open sample question 1', exact: true })
  await expect(marker).toBeFocused()
  await marker.click()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '1 sample items')
  expect(requests).toEqual([])
})

/**
 * Keeps native text selection inert while direct gutter clicks compose and retain range origins.
 * @param fixtures Playwright page.
 */
test('source text is inert and direct line selection opens a question with Shift extension', async ({ page }): Promise<void> => {
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.locator('.line-source code').first().evaluate((element): void => {
    const range = document.createRange()
    range.selectNodeContents(element)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
  })
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).not.toBeVisible()
  await expect(page.getByRole('button', { name: 'Ask about selected lines', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Select base line 23', exact: true }).click()
  await expect(page.getByRole('form', { name: 'New sample question' })).toContainText('base L23–23')
  await page.getByRole('button', { name: 'Select head line 30', exact: true }).click()
  await page.getByRole('button', { name: 'Select head line 27', exact: true }).click({ modifiers: ['Shift'] })
  await page.getByRole('button', { name: 'Select head line 32', exact: true }).press('Shift+Enter')
  await expect(page.getByRole('form', { name: 'New sample question' })).toContainText('head L30–32')
})

/**
 * Selects a multi-line range by dragging, composes on release and draws one continuous outer border.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('dragging line numbers selects multiple lines and opens their question on release', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  const start = page.getByRole('button', { name: 'Select head line 27', exact: true })
  const end = page.getByRole('button', { name: 'Select head line 31', exact: true })
  await start.hover()
  await page.mouse.down()
  await end.hover()
  await expect(end).toHaveAttribute('aria-pressed', 'true')
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).not.toBeVisible()
  await page.mouse.up()
  await expect(page.getByRole('form', { name: 'New sample question' })).toContainText('head L27–31')
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toBeFocused()
  const selectedRows = page.locator('.drag-selectable tr.is-selected:visible')
  await expect(selectedRows).toHaveCount(5)
  await expect(selectedRows.first().locator('td').first()).toHaveCSS('border-top-width', '1px')
  await expect(selectedRows.last().locator('td').first()).toHaveCSS('border-bottom-width', '1px')
  await expect(selectedRows.nth(1)).toHaveCSS('outline-style', 'none')
  await expect(selectedRows.nth(1).locator('td').first()).toHaveCSS('border-top-width', '0px')
  await expect(selectedRows.nth(1).locator('td').first()).toHaveCSS('border-bottom-width', '0px')
  await page.screenshot({ path: testInfo.outputPath('direct-drag-question.png'), fullPage: true })
})

/**
 * Exercises IME, newlines, creation failure and response failure separately.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('shortcuts respect IME and failed creation retains text without a thread', async ({ page }, testInfo): Promise<void> => {
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await input.fill('Why?')
  await input.press('Enter')
  await expect(input).toHaveValue('Why?\n')
  await input.dispatchEvent('keydown', { key: 'Enter', ctrlKey: true, isComposing: true })
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  await page.locator('summary:visible').filter({ hasText: 'Sample preview controls' }).click()
  await page.getByLabel('Preview creation outcome').filter({ visible: true }).selectOption('creation-error')
  await input.press('Control+Enter')
  await expect(input).toHaveValue('Why?\n')
  await expect(page.getByText('Sample creation failed. No thread was created; your draft is retained.')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('creation-error.png'), fullPage: true })
  await page.getByLabel('Preview creation outcome').filter({ visible: true }).selectOption('response-error')
  await input.press('Meta+Enter')
  await expect(page.getByRole('complementary', { name: 'Question 01', exact: true })).toContainText('Sample response failed.')
})

/**
 * Guards pending creation against duplicate submission and context changes.
 * @param fixtures Playwright page.
 */
test('manual pending creation blocks duplicates and finishes exactly once', async ({ page }): Promise<void> => {
  await page.locator('summary:visible').filter({ hasText: 'Sample preview controls' }).click()
  await page.getByLabel('Preview creation outcome').filter({ visible: true }).selectOption('pending')
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await input.fill('Pending question')
  await input.press('Control+Enter')
  await input.press('Control+Enter')
  await page.getByRole('button', { name: 'src/auth/session-store.ts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/auth/refresh-session.ts', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  await page.getByRole('button', { name: 'Finish sample creation', exact: true }).click()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '1 sample items')
})

/**
 * Requires explicit retention/discard and supports whole-file anchors and escaped source.
 * @param fixtures Playwright page.
 */
test('draft guards, recovery and whole-file questions preserve pinned context', async ({ page }): Promise<void> => {
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await input.fill('Retain original anchor')
  await page.getByRole('button', { name: 'docs/sessions.md', exact: true }).click()
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(input).toHaveValue('Retain original anchor')
  await page.getByRole('button', { name: 'docs/sessions.md', exact: true }).click()
  await page.getByRole('button', { name: 'Keep draft and switch', exact: true }).click()
  await expect(page.locator('.line-source')).toContainText(['# Session refresh'])
  await expect(page.locator('.line-source img')).toHaveCount(0)
  await expect(page.getByText('Example markup stays literal: <img src=x onerror=alert(1)>.', { exact: true })).toBeVisible()
  await page.locator('summary:visible').filter({ hasText: 'Recover local drafts' }).click()
  await page.getByRole('button', { name: /Recover src\/auth\/refresh-session.ts/ }).click()
  await expect(input).toHaveValue('Retain original anchor')
  await page.getByRole('button', { name: 'Ask about this file ↗', exact: true }).click()
  await page.getByRole('button', { name: 'Discard draft and switch', exact: true }).click()
  await expect(input).toHaveValue('')
  await input.fill('Whole-file question')
  await input.press('Control+Enter')
  await expect(page.getByRole('complementary', { name: 'Question 01', exact: true })).toContainText('Whole file · a81c2f4')
})

/**
 * Keeps the populated sample separate and enforces shared-revision busy state.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('four-thread sample retains counts and blocks creation during investigating', async ({ page }, testInfo): Promise<void> => {
  await page.goto('/?sample=review')
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '4 sample items')
  await page.getByRole('button', { name: 'Select head line 27', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Waiting for shared context')
  await expect(page.getByRole('button', { name: 'Preview thread creation', exact: true })).toBeDisabled()
  await expect(page.getByText('Another sample question is investigating in this PR revision. Creation is unavailable.')).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('four-thread-review.png'), fullPage: true })
})
