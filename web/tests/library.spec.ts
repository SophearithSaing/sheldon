import { expect, test, type Page } from '@playwright/test'
import { openSampleThread } from './preview'

/**
 * Opens the offline library with no backend connection.
 * @param fixtures Playwright page.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/?preview=library')
})

/**
 * Selects an explicit source state without fetching or advancing a completion timer.
 * @param page Browser page with the sample library.
 * @param id Canonical fictional PR identity.
 * @param state Explicit source state to inspect.
 */
async function sourcePreview(page: Page, id: string, state: string): Promise<void> {
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  const tools = page.locator('.library-preview-tools')
  if (!(await tools.evaluate((element): boolean => (element as HTMLDetailsElement).open))) await tools.locator('summary').click()
  await page.getByLabel('PR for source preview', { exact: true }).selectOption(id)
  await page.getByLabel('Source preview state', { exact: true }).selectOption(state)
  await page.getByRole('button', { name: 'Show selected source preview', exact: true }).click()
}

/**
 * Checks disjoint source-status totals, combined filters and separate visit/update ordering.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('eight-row library filters and sorts fictional source records without changing histories', async ({ page }, testInfo): Promise<void> => {
  const table = page.getByRole('table', { name: 'Sample pull requests' })
  await expect(table.getByRole('row')).toHaveCount(9)
  const filters = page.getByRole('group', { name: 'Filter by pull request status' })
  for (const [status, count] of [['All', 8], ['Open', 4], ['Merged', 3], ['Closed', 1]] as const) {
    await expect(filters.getByRole('button', { name: status, exact: true })).toHaveText(`${status} ${count}`)
  }
  await page.screenshot({ path: testInfo.outputPath('library-1440.png'), fullPage: true })
  await filters.getByRole('button', { name: 'Merged', exact: true }).click()
  await expect(table.getByRole('row')).toHaveCount(4)
  await page.getByLabel('Filter by repository', { exact: true }).selectOption('acme/dashboard')
  await expect(table.getByRole('row')).toHaveCount(2)
  await page.getByRole('searchbox', { name: 'Search pull requests by title, number or repository' }).fill(' #82 ')
  await expect(table).toContainText('Add keyboard navigation')
  await page.getByRole('searchbox').fill('no matching sample')
  await expect(page.getByRole('heading', { name: 'No matching sample pull requests' })).toBeVisible()
  await page.getByRole('button', { name: 'Clear filters', exact: true }).click()
  await expect(table.getByRole('row')).toHaveCount(9)
  await page.getByLabel('Sort pull requests', { exact: true }).selectOption('newest')
  await expect(table.getByRole('row').nth(1)).toContainText('acme/platform #251')
  await page.getByLabel('Sort pull requests', { exact: true }).selectOption('updated')
  await expect(table.getByRole('row').nth(1)).toContainText('acme/platform #248')
  await page.getByRole('link', { name: 'Open sample dashboard #86', exact: true }).click()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByLabel('Sort pull requests', { exact: true }).selectOption('visited')
  await expect(table.getByRole('row').nth(1)).toContainText('acme/dashboard #86')
  await expect(filters.getByRole('button', { name: 'Open', exact: true })).toHaveText('Open 4')
})

/**
 * Retains library controls/scroll and existing PR state while preventing duplicate tabs.
 * @param fixtures Playwright page.
 */
test('library resume deduplicates tabs and keeps drafts filters and scroll in page memory', async ({ page }): Promise<void> => {
  await page.getByRole('link', { name: 'Open sample platform #248', exact: true }).click()
  await openSampleThread(page)
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Retained only in this page')
  await page.getByRole('button', { name: 'Close sample workspace platform #248', exact: true }).click()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByLabel('Filter by repository', { exact: true }).selectOption('acme/platform')
  await page.getByRole('link', { name: 'Open sample platform #248', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Retained only in this page')
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByRole('link', { name: 'Open sample platform #248', exact: true }).click()
  await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('link')).toHaveCount(2)
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  const library = page.getByRole('main', { name: 'Your pull requests' })
  await library.evaluate((element): void => { element.scrollTop = 100 })
  const position = await library.evaluate((element): number => element.scrollTop)
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await expect(page.getByLabel('Filter by repository', { exact: true })).toHaveValue('acme/platform')
  expect(await library.evaluate((element): number => element.scrollTop)).toBe(position)
  expect(await page.evaluate((): number => localStorage.length + sessionStorage.length)).toBe(0)
})

/**
 * Keeps source-only loading unresolved and global navigation available, without fabricated metadata.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('initial source loading has decorative skeletons no known revision and no automatic work', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => { requests.push(request.url()) })
  await page.clock.install()
  await page.getByRole('link', { name: 'Open sample dashboard #86', exact: true }).click()
  const main = page.getByRole('main', { name: 'Source loading preview' })
  expect((await main.boundingBox())!.y).toBeCloseTo(160, 0)
  await expect(main.getByRole('region', { name: 'Unresolved source' })).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('banner', { name: 'Sheldon navigation' })).not.toHaveAttribute('aria-busy')
  await expect(page.getByText('Waiting for source revision', { exact: true }).filter({ visible: true })).toBeVisible()
  await expect(main.locator('.skeleton-code')).toHaveAttribute('aria-hidden', 'true')
  await expect(page.locator('.line-number:visible')).toHaveCount(0)
  await expect(page.getByRole('region', { name: 'Sample conversation messages' })).toHaveCount(0)
  const unavailable = page.getByRole('navigation', { name: 'Unavailable PR content' })
  await expect(unavailable.getByRole('button', { name: 'Questions', exact: true })).toBeDisabled()
  await unavailable.getByRole('button', { name: 'Questions', exact: true }).press('Enter')
  await page.clock.runFor(60000)
  await expect(main.getByRole('region', { name: 'Unresolved source' })).toHaveAttribute('aria-busy', 'true')
  await page.screenshot({ path: testInfo.outputPath('source-loading-1440.png'), fullPage: true })
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await page.getByRole('link', { name: 'dashboard #86 — Report filters', exact: true }).click()
  await expect(main).toBeVisible()
  await page.getByRole('button', { name: 'Close sample workspace dashboard #86', exact: true }).click()
  expect(requests).toEqual([])
})

/**
 * Makes source failures explicit without adding phantom snapshots or issuing retries.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('offline denied missing and incomplete source previews retain navigation and do not retry', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => { requests.push(request.url()) })
  for (const state of ['offline', 'denied', 'missing', 'unavailable']) {
    await sourcePreview(page, 'github.com/acme/dashboard/86', state)
    const main = page.getByRole('main', { name: 'Source loading preview' })
    await expect(main.getByRole('region', { name: 'Unresolved source' })).toHaveAttribute('aria-busy', 'false')
    await expect(main.getByRole('button', { name: 'Retry source loading — unavailable' })).toBeDisabled()
    await expect(main).toContainText('preview')
    await main.getByRole('button', { name: 'Retry source loading — unavailable' }).press('Enter')
    await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('link')).toHaveCount(3)
    await page.screenshot({ path: testInfo.outputPath(`source-${state}.png`), fullPage: true })
  }
  expect(requests).toEqual([])
})

/**
 * Keeps a coherent sample snapshot and its drafts visible during a cache-refresh failure preview.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('stale cache retains source revision threads and unsent drafts', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('link', { name: 'Open sample platform #248', exact: true }).click()
  await openSampleThread(page)
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Do not discard on refresh')
  await sourcePreview(page, 'github.com/acme/platform/248', 'stale')
  await expect(page.getByText('Cached-refresh failure · sample preview', { exact: true }).filter({ visible: true })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Do not discard on refresh')
  await expect(page.getByRole('region', { name: 'Sample code diff', exact: true })).toContainText('refreshSession')
  await expect(page.getByRole('complementary', { name: 'Question 01', exact: true })).toContainText('a81c2f4')
  await page.screenshot({ path: testInfo.outputPath('cached-refresh-failure.png'), fullPage: true })
  await sourcePreview(page, 'github.com/acme/platform/248', 'offline')
  await page.getByRole('button', { name: 'Return to retained sample snapshot', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Do not discard on refresh')
})

/**
 * Keeps merged/closed histories revisitable with separate source and understanding states.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('merged and closed source records preserve their own historical questions', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('link', { name: 'Open sample platform #239', exact: true }).click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('Move audit events')
  await expect(page.getByRole('region', { name: 'Retained sample source and history' })).toContainText('src/audit/queue.ts')
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Audit-only draft')
  await page.getByRole('button', { name: 'Mark understood', exact: true }).click()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  const row = page.getByRole('table', { name: 'Sample pull requests' }).getByRole('row').filter({ hasText: 'acme/platform #239' })
  await expect(row).toContainText('Merged')
  await expect(row).toContainText('0 open')
  await expect(row).toContainText('4 understood')
  await page.getByRole('link', { name: 'Open sample platform #231', exact: true }).click()
  await expect(page.getByRole('heading', { level: 1 })).toContainText('cookie serializer')
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('')
  await expect(page.getByRole('region', { name: 'Retained sample source and history' })).toContainText('src/session/cookie.ts')
  await page.screenshot({ path: testInfo.outputPath('closed-pr-history.png'), fullPage: true })
  await page.getByRole('button', { name: 'Close sample workspace platform #239', exact: true }).click()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByRole('link', { name: 'Open sample platform #239', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Audit-only draft')
  await expect(page.getByRole('button', { name: 'Undo understood', exact: true })).toBeVisible()
})

/**
 * Retains import text on validation/cancel without manufacturing rows, tabs or network activity.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('Open a PR validates only URL format and never pretends import succeeded', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => { requests.push(request.url()) })
  const trigger = page.getByRole('button', { name: 'Open a PR', exact: true })
  await trigger.click()
  const modal = page.getByRole('dialog')
  const url = modal.getByRole('textbox', { name: 'PR URL', exact: true })
  await expect(url).toBeFocused()
  await url.fill('https://foreign.example/acme/platform/pull/123')
  await modal.getByRole('button', { name: 'Preview URL validation' }).click()
  await expect(url).toHaveAttribute('aria-invalid', 'true')
  await page.screenshot({ path: testInfo.outputPath('import-unavailable-error.png'), fullPage: true })
  await url.fill('https://github.com/acme/platform/pull/248')
  await modal.getByRole('button', { name: 'Preview URL validation' }).click()
  await expect(modal).toContainText('no PR, row or workspace was created')
  await expect(modal.getByRole('button', { name: 'Import PR — unavailable' })).toBeDisabled()
  await modal.getByRole('button', { name: 'Cancel', exact: true }).click()
  await expect(trigger).toBeFocused()
  await trigger.click()
  await expect(url).toHaveValue('https://github.com/acme/platform/pull/248')
  await url.press('Escape')
  await expect(page.getByRole('table', { name: 'Sample pull requests' }).getByRole('row')).toHaveCount(9)
  await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('link')).toHaveCount(2)
  expect(requests).toEqual([])
})

/**
 * Exercises explicit empty/error/stale library snapshots without destroying retained workspaces.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot output location.
 */
test('library feedback states and narrow scaled layouts keep useful controls reachable', async ({ page }, testInfo): Promise<void> => {
  await page.getByText('Sample library controls', { exact: true }).click()
  const state = page.getByLabel('Library preview state', { exact: true })
  for (const value of ['empty', 'error', 'stale']) {
    await state.selectOption(value)
    await expect(page.getByRole('navigation', { name: 'PR workspaces' }).getByRole('link')).toHaveCount(2)
    await page.screenshot({ path: testInfo.outputPath(`library-${value}.png`), fullPage: true })
  }
  await state.selectOption('populated')
  await page.setViewportSize({ width: 700, height: 1080 })
  await expect(page.getByRole('link', { name: 'Open sample platform #231', exact: true })).toBeVisible()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('library-700.png'), fullPage: true })
  await page.setViewportSize({ width: 1440, height: 1080 })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  await expect(page.getByRole('group', { name: 'Filter by pull request status' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('library-text-scale-200.png'), fullPage: true })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '100%' })
  await page.setViewportSize({ width: 700, height: 1080 })
  await sourcePreview(page, 'github.com/acme/dashboard/86', 'loading')
  await expect(page.getByRole('main', { name: 'Source loading preview' })).toBeVisible()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('source-loading-700.png'), fullPage: true })
})
