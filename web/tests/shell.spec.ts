import { expect, test } from '@playwright/test'
import { openSampleThread } from './preview'

/**
 * Opens the local workspace shell without a backend connection.
 * @param fixtures Playwright page fixture.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/')
  await page.evaluate((): Promise<FontFaceSet> => document.fonts.ready)
})

/**
 * Checks compact desktop chrome and explicit tree/panel reflow.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output locations.
 */
test('desktop chrome is 160px and native tree collapse returns 192px to source', async ({ page }, testInfo): Promise<void> => {
  const header = await page.locator('.application-header').boundingBox()
  const summary = await page.locator('.workspace-summary:visible').boundingBox()
  const navigation = await page.locator('.workspace-navigation:visible').boundingBox()
  expect(header!.height).toBeCloseTo(48, 0)
  expect(summary!.height).toBeCloseTo(67, 0)
  expect(navigation!.height).toBeCloseTo(45, 0)
  expect(navigation!.y + navigation!.height).toBeCloseTo(160, 0)
  await expect(page.locator('.workspace-footer')).toHaveCount(0)
  await openSampleThread(page)
  const files = page.getByRole('complementary', { name: 'Changed files' })
  const source = page.getByRole('main')
  const conversation = page.getByRole('complementary', { name: 'Question 01', exact: true })
  expect((await files.boundingBox())!.width).toBeCloseTo(240, 0)
  expect((await conversation.boundingBox())!.width).toBeCloseTo(440, 0)
  expect((await files.boundingBox())!.y).toBeCloseTo(172, 0)
  const expanded = await source.boundingBox()
  const pane = await conversation.boundingBox()
  expect(pane!.x - expanded!.x - expanded!.width).toBeCloseTo(16, 0)
  await page.getByRole('button', { name: 'Ask about selected lines', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Keep the selected source draft')
  const scroll = page.getByRole('region', { name: 'Sample code diff', exact: true })
  const position = await scroll.evaluate((element): number => {
    element.scrollTop = 100
    return element.scrollTop
  })
  await files.locator('summary').click()
  expect((await files.boundingBox())!.width).toBeCloseTo(48, 0)
  expect((await source.boundingBox())!.width - expanded!.width).toBeCloseTo(192, 0)
  await expect(files.locator('summary')).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('shell-collapsed-1440.png'), fullPage: true })
  await files.locator('summary').press('Enter')
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Keep the selected source draft')
  expect(await scroll.evaluate((element): number => element.scrollTop)).toBeCloseTo(position, 0)
  await page.screenshot({ path: testInfo.outputPath('shell-expanded-1440.png'), fullPage: true })
})

/**
 * Checks destination selection and shell-free global pages.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output locations.
 */
test('global destinations are links and library/configuration omit PR chrome', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Pull requests', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'All PRs', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('.workspace-summary:visible')).toHaveCount(0)
  await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('tab')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('library-shell.png'), fullPage: true })
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Configuration', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'Configuration', exact: true })).toHaveAttribute('aria-current', 'page')
  await expect(page.locator('.workspace-links [aria-current]')).toHaveCount(0)
  await expect(page.locator('.workspace-navigation:visible')).toHaveCount(0)
  expect((await page.locator('.application-header').boundingBox())!.height).toBeCloseTo(48, 0)
  await page.screenshot({ path: testInfo.outputPath('configuration-shell.png'), fullPage: true })
  await page.goto('/?preview=config')
  await expect(page.getByRole('heading', { name: 'Configuration', exact: true })).toBeVisible()
  await page.goto('/?preview=github.com%2Facme%2Fplatform%2F251')
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Handle retries')
})

/**
 * Checks local PR state stays isolated across navigation and sample-tab close/reopen.
 * @param fixtures Playwright page fixture.
 */
test('workspace switches and close/reopen preserve page-memory drafts without sharing them', async ({ page }): Promise<void> => {
  await openSampleThread(page)
  await page.getByRole('button', { name: 'Ask about selected lines', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('PR 248 draft')
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('PR 248 follow-up')
  const source = page.getByRole('region', { name: 'Sample code diff', exact: true })
  const position = await source.evaluate((element): number => {
    element.scrollTop = 80
    return element.scrollTop
  })
  await page.getByRole('complementary', { name: 'Changed files' }).locator('summary').click()
  await page.getByRole('link', { name: 'platform #251 — Retry handling', exact: true }).click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Handle retries')
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('')
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('PR 251 draft')
  await page.getByRole('link', { name: 'platform #248 — Session refresh', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('PR 248 draft')
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('PR 248 follow-up')
  await expect(page.getByRole('complementary', { name: 'Changed files' }).locator('details')).not.toHaveAttribute('open')
  expect(await source.evaluate((element): number => element.scrollTop)).toBeCloseTo(position, 0)
  await page.getByRole('button', { name: 'Close sample workspace platform #248' }).click()
  await expect(page.getByRole('link', { name: 'platform #251 — Retry handling', exact: true })).toBeFocused()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('PR 251 draft')
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByRole('link', { name: 'Open sample platform #248' }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('PR 248 draft')
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('PR 248 follow-up')
  await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('link')).toHaveCount(2)
  expect(await page.evaluate((): number => localStorage.length + sessionStorage.length)).toBe(0)
  const duplicateIds = await page.evaluate((): boolean => {
    const ids = Array.from(document.querySelectorAll('[id]'), (element): string => element.id)
    return new Set(ids).size !== ids.length
  })
  expect(duplicateIds).toBe(false)
})

/**
 * Checks closing inactive tabs does not navigate and closing the last tab returns home.
 * @param fixtures Playwright page fixture.
 */
test('closing tabs preserves global destinations and returns to All PRs when none remain', async ({ page }): Promise<void> => {
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await page.getByRole('button', { name: 'Close sample workspace platform #248' }).click()
  await expect(page.getByRole('heading', { name: 'Configuration', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Close sample workspace platform #251' }).click()
  await expect(page.getByRole('heading', { name: 'Configuration', exact: true })).toBeVisible()
  await page.getByRole('link', { name: 'Sheldon — All PRs', exact: true }).click()
  await page.getByRole('link', { name: 'Open sample platform #248' }).click()
  await page.getByRole('button', { name: 'Close sample workspace platform #248' }).click()
  await expect(page.getByRole('heading', { name: 'Pull requests', exact: true })).toBeVisible()
  await expect(page.getByRole('link', { name: 'All PRs', exact: true })).toBeFocused()
})

/**
 * Checks many local workspaces scroll inside the same global row without eviction.
 * @param fixtures Playwright page fixture.
 */
test('many sample workspaces overflow horizontally without growing chrome or evicting tabs', async ({ page }): Promise<void> => {
  for (let index = 0; index < 8; index++) {
    await page.getByRole('link', { name: 'All PRs', exact: true }).click()
    if (index === 0) await page.getByText('Sample library controls', { exact: true }).click()
    await page.getByRole('button', { name: 'Open another sample workspace' }).click()
  }
  const navigation = page.getByRole('navigation', { name: 'PR workspaces' })
  await expect(navigation.getByRole('link')).toHaveCount(10)
  expect(await navigation.evaluate((element): boolean => element.scrollWidth > element.clientWidth)).toBe(true)
  await navigation.getByRole('link').last().focus()
  await expect(navigation.getByRole('link').last()).toBeInViewport()
  expect((await page.locator('.application-header').boundingBox())!.height).toBeCloseTo(48, 0)
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
})
