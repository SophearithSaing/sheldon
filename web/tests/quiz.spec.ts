import { expect, test } from '@playwright/test'
import { openSampleThread } from './preview'

/**
 * Opens a fresh offline quiz with no prechecked answers.
 * @param fixtures Playwright page.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
})

/**
 * Keeps the fixed key and explanation hidden until an explicit sample check.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('unchecked quiz uses native radios and does not reveal or dispatch a key on selection', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => {
    if (['fetch', 'xhr'].includes(request.resourceType()) || new URL(request.url()).origin !== 'http://127.0.0.1:5174') requests.push(request.url())
  })
  const check = page.getByRole('button', { name: 'Preview answer check', exact: true })
  await expect(check).toBeDisabled()
  await expect(page.getByText('Correct answer', { exact: false }).filter({ visible: true })).toHaveCount(0)
  await expect(page.getByRole('region', { name: 'Pinned sample quiz evidence' })).toHaveCount(0)
  const radios = page.getByRole('radio')
  await radios.first().focus()
  await radios.first().press('ArrowDown')
  await expect(radios.nth(1)).toBeChecked()
  await expect(check).toBeEnabled()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('0 of 5 checked')
  await expect(page.getByRole('complementary', { name: 'Quiz answer explanation' })).toHaveCSS('width', '440px')
  await page.screenshot({ path: testInfo.outputPath('quiz-unchecked-1440.png'), fullPage: true })
  await check.click()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('1 of 5 checked · 0 to revisit')
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  expect(requests).toEqual([])
})

/**
 * Preserves wrong selection A while labeling C and freezing the original sample attempt.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('checked-wrong sample keeps A selected labels C and pins evidence to the revision', async ({ page }, testInfo): Promise<void> => {
  await page.goto('/?sample=quiz')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  const radios = page.getByRole('radio')
  await expect(radios.nth(0)).toBeChecked()
  await expect(radios.nth(2)).not.toBeChecked()
  await expect(page.locator('.quiz-option.is-wrong:visible')).toContainText('Your answer')
  await expect(page.locator('.quiz-option.is-correct:visible')).toContainText('Correct answer')
  await radios.nth(2).click({ force: true })
  await radios.nth(0).press('ArrowDown')
  await expect(radios.nth(0)).toBeChecked()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('2 of 5 checked · 1 to revisit')
  await expect(page.getByRole('region', { name: 'Pinned sample quiz evidence' })).toContainText('return session;')
  await expect(page.getByRole('complementary', { name: 'Quiz answer explanation' })).toContainText('a81c2f4')
  await page.screenshot({ path: testInfo.outputPath('quiz-checked-wrong-1440.png'), fullPage: true })
  await page.getByRole('button', { name: 'Next question', exact: true }).click()
  await expect(page.locator('legend:visible')).toBeFocused()
  await expect(page.getByRole('region', { name: 'Pinned sample quiz evidence' })).toHaveCount(0)
  await page.getByRole('button', { name: /^Quiz question 2,/ }).click()
  await expect(radios.nth(0)).toBeChecked()
})

/**
 * Keeps factual sample completion separate from understanding, approval and thread creation.
 * @param fixtures Playwright page.
 */
test('five explicit sample checks show results without understanding or thread side effects', async ({ page }): Promise<void> => {
  for (const [index, answer] of [1, 2, 1, 3, 0].entries()) {
    await page.getByRole('radio').nth(answer).check()
    await page.getByRole('button', { name: 'Preview answer check', exact: true }).click()
    if (index < 4) await page.getByRole('button', { name: 'Next question', exact: true }).click()
  }
  await page.getByRole('button', { name: 'Review sample results', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Sample results', exact: true })).toBeVisible()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('5 match the illustrative key · 0 to revisit')
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  await page.getByRole('button', { name: 'Ask in a new thread — unavailable' }).press('Enter')
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  await page.getByRole('button', { name: 'Review checked questions' }).click()
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await expect(page.getByRole('radio').nth(1)).toHaveAttribute('aria-disabled', 'true')
})

/**
 * Preserves quiz attempts, selected threads and drafts across settings, workspaces and source navigation.
 * @param fixtures Playwright page.
 */
test('quiz source and settings navigation retain local attempts and unrelated thread drafts', async ({ page }): Promise<void> => {
  await page.getByRole('tab', { name: 'Files changed', exact: true }).click()
  await openSampleThread(page)
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Unrelated thread draft')
  await page.getByRole('button', { name: 'Select head line 27', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Keep my pinned line draft')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await page.getByRole('radio').nth(1).check()
  await page.getByRole('button', { name: 'Preview answer check', exact: true }).click()
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await page.getByRole('link', { name: /Back to review/ }).click()
  await expect(page.getByRole('tab', { name: 'Quiz', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await page.getByRole('button', { name: 'View in diff', exact: true }).click()
  await expect(page.getByRole('alert', { name: 'Keep your draft context' })).toBeFocused()
  await page.getByRole('button', { name: 'Keep editing', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Keep my pinned line draft')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await page.getByRole('button', { name: 'View in diff', exact: true }).click()
  await page.getByRole('button', { name: 'Keep draft and switch', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Select head line 30', exact: true })).toBeFocused()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Unrelated thread draft')
  await expect(page.getByRole('checkbox', { name: 'Viewed', exact: true })).not.toBeChecked()
  await page.getByRole('link', { name: 'platform #251 — Retry handling', exact: true }).click()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('0 of 5 checked')
  await page.getByRole('link', { name: 'platform #248 — Session refresh', exact: true }).click()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('1 of 5 checked')
})

/**
 * Keeps quiz options, evidence and primary controls reachable when panes stack or text scales.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('quiz narrow and scaled layouts preserve selection and show stacked explanations', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('radio').nth(0).check()
  await page.setViewportSize({ width: 899, height: 1080 })
  await page.getByRole('button', { name: 'Preview answer check', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('quiz-899.png'), fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1080 })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  await expect(page.getByRole('radio').nth(0)).toBeChecked()
  await page.getByRole('button', { name: 'Next question', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('button', { name: 'Next question', exact: true })).toBeInViewport()
  await page.getByRole('button', { name: 'View in diff', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('button', { name: 'View in diff', exact: true })).toBeInViewport()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('quiz-text-scale-200.png') })
})
