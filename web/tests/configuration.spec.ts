import { expect, test } from '@playwright/test'

/**
 * Opens the clean application-level configuration preview.
 * @param fixtures Playwright page.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/?preview=config')
})

/**
 * Checks the centered panel, native per-harness choices and reversible dirty hints.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('configuration uses three illustrative rows and tracks changes without saving', async ({ page }, testInfo): Promise<void> => {
  await expect(page.getByRole('combobox')).toHaveCount(3)
  expect((await page.locator('.configuration-content').boundingBox())!.width).toBe(1024)
  expect((await page.locator('.application-header').boundingBox())!.height).toBe(48)
  await expect(page.locator('.workspace-summary:visible')).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Save changes — unavailable' })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('configuration-clean-1440.png'), fullPage: true })
  const model = page.getByRole('combobox', { name: 'Model for OpenCode', exact: true })
  await model.focus()
  await model.press('End')
  await expect(model).toHaveValue('sample-gemini')
  await expect(page.getByText('Changed', { exact: true })).toBeVisible()
  await expect(model).toHaveAccessibleDescription(/Previously \(sample\): Claude Sonnet/)
  await model.selectOption('sample-opus')
  await expect(page.getByText('1 unsaved sample change', { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('configuration-dirty-1440.png'), fullPage: true })
  await model.selectOption('sample-sonnet')
  await expect(page.getByText('No sample changes', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Discard changes', exact: true })).toBeDisabled()
})

/**
 * Guards every ordinary global departure without implicitly saving or dropping edits.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('dirty global navigation offers keep editing or explicit discard and leave', async ({ page }, testInfo): Promise<void> => {
  const model = page.getByRole('combobox', { name: 'Model for OpenCode', exact: true })
  await model.selectOption('sample-opus')
  for (const destination of ['All PRs', 'Sheldon — All PRs', 'platform #248 — Session refresh']) {
    await page.getByRole('link', { name: destination, exact: true }).click()
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByRole('link', { name: 'Configuration', exact: true })).toHaveAttribute('aria-current', 'page')
    await page.getByRole('button', { name: 'Keep editing', exact: true }).click()
    await expect(model).toHaveValue('sample-opus')
  }
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('configuration-dirty-guard.png'), fullPage: true })
  await page.getByRole('dialog').press('Escape')
  await expect(model).toHaveValue('sample-opus')
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByRole('button', { name: 'Discard and leave', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Pull requests', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await expect(model).toHaveValue('sample-sonnet')
})

/**
 * Retains unavailable assignments and failed-save drafts without requests or silent fallback.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('unavailable and save-error previews preserve draft and baseline choices without requests', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => {
    if (['fetch', 'xhr'].includes(request.resourceType()) || new URL(request.url()).origin !== 'http://127.0.0.1:5174') requests.push(request.url())
  })
  await page.getByText('Configuration preview controls', { exact: true }).click()
  await page.getByLabel('Configuration availability preview', { exact: true }).selectOption('unavailable')
  const model = page.getByRole('combobox', { name: 'Model for OpenCode', exact: true })
  await expect(model).toHaveValue('sample-sonnet')
  await expect(model.locator('option[value="sample-sonnet"]')).toBeDisabled()
  await expect(model).toHaveAccessibleDescription(/No discovery or fallback ran/)
  await page.screenshot({ path: testInfo.outputPath('configuration-unavailable.png'), fullPage: true })
  await page.getByRole('combobox', { name: 'Model for Claude Code', exact: true }).selectOption('sample-opus')
  await page.getByRole('button', { name: 'Save changes — unavailable' }).press('Enter')
  await page.getByRole('button', { name: 'Preview save failure', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('No save was attempted')
  await expect(page.getByRole('combobox', { name: 'Model for Claude Code', exact: true })).toHaveValue('sample-opus')
  await page.screenshot({ path: testInfo.outputPath('configuration-save-error.png'), fullPage: true })
  await page.getByRole('button', { name: 'Discard changes', exact: true }).click()
  await expect(model).toHaveValue('sample-sonnet')
  await expect(page.getByRole('combobox', { name: 'Model for Claude Code', exact: true })).toHaveValue('sample-sonnet')
  expect(requests).toEqual([])
})

/**
 * Remembers the real origin and reopens a closed tab without resetting its quiz attempt.
 * @param fixtures Playwright page.
 */
test('Back to review restores the originating quiz even when its tab was closed from settings', async ({ page }): Promise<void> => {
  await page.getByRole('link', { name: 'platform #251 — Retry handling', exact: true }).click()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await page.getByRole('radio').nth(1).check()
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await page.getByRole('combobox', { name: 'Model for OpenCode', exact: true }).selectOption('sample-opus')
  await page.getByRole('button', { name: 'Close sample workspace platform #251', exact: true }).click()
  await expect(page.getByRole('dialog')).not.toBeVisible()
  await expect(page.getByText('1 unsaved sample change', { exact: true })).toBeVisible()
  await page.getByRole('link', { name: '← Back to review · platform #251', exact: true }).click()
  await page.getByRole('button', { name: 'Discard and leave', exact: true }).click()
  await expect(page.getByRole('tab', { name: 'Quiz', exact: true })).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('link')).toHaveCount(2)
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz' })).toContainText('0 of 5 checked')
})

/**
 * Keeps fields/actions readable and native at narrow widths and doubled text size.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot location.
 */
test('configuration narrow and scaled layouts retain changed values', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('combobox', { name: 'Model for OpenCode', exact: true }).selectOption('sample-opus')
  await page.setViewportSize({ width: 700, height: 1080 })
  await page.screenshot({ path: testInfo.outputPath('configuration-700.png'), fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1080 })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  await expect(page.getByRole('combobox', { name: 'Model for OpenCode', exact: true })).toHaveValue('sample-opus')
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('configuration-text-scale-200.png'), fullPage: true })
})
