import { test, expect, type Page } from '@playwright/test'
import { registered, sourceFixture, token } from './runtime-fixture'
import { semanticFixture } from './semantic-fixture'
import type { SemanticRead } from '../src/runtime/semantic'

test.use({ baseURL: 'http://127.0.0.1:5175' })

/** Delivers delayed renderer replies despite abort, exercising ownership rather than transport alone.
 * @param page Fixture browser page.
 */
async function ignoreAbort(page: Page): Promise<void> {
  await page.evaluate((): void => {
    const original = window.fetch.bind(window)
    /** Removes cancellation only for explicit semantic renderer fixtures.
     * @param input Fetch target.
     * @param init Request options.
     * @returns Original transport without semantic cancellation.
     */
    window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const path = new URL(input instanceof Request ? input.url : String(input), location.href).pathname
      return original(input, path.includes('/semantic-review') ? { ...init, signal: undefined } : init)
    }
  })
}

for (const kind of ['base', 'head']) test(`semantic ${kind} refresh labels old groups and never navigates into new source`, async ({ page }): Promise<void> => {
  const pr = await registered(page, kind === 'base' ? 710 : 711); const f = semanticFixture(pr); let refreshed = false
  await page.route('**/source', async (route): Promise<void> => { await route.fulfill({ json: refreshed ? { ...sourceFixture(pr, 3, kind === 'base' ? 'f'.repeat(40) : 'a'.repeat(40), kind === 'head' ? 'f'.repeat(40) : 'c'.repeat(40)), requestedRevisionID: f.source.diff!.revisionID } : f.source }) })
  await page.route('**/semantic-review', async (route): Promise<void> => { await route.fulfill({ json: f.read }) })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click(); await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await expect(page.locator('.semantic-excerpt').first().getByRole('button', { name: 'View in original diff', exact: true })).not.toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('button', { name: 'Files changed', exact: false }).click(); refreshed = true; await page.getByRole('button', { name: 'Refresh source', exact: true }).click(); await expect(page.getByText('Refresh complete.', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click(); await expect(page.getByText('Older revision; source was not retargeted.', { exact: false })).toBeVisible()
  await expect(page.locator('.semantic-excerpt').first().getByRole('button', { name: 'View in original diff', exact: true })).toHaveAttribute('aria-disabled', 'true')
})

test('late semantic saved read cannot replace a later view visit or move focus even when abort is ignored', async ({ page }): Promise<void> => {
  const pr = await registered(page, 712); const f = semanticFixture(pr); let count = 0; let release: (() => void) | undefined; let delivered = false
  await page.route('**/semantic-review', async (route): Promise<void> => {
    count++; const first = count === 1
    if (first) await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ json: first ? f.read : { ...f.read, version: '2', review: { ...f.read.review!, reviewID: f.status.reviewID, groups: [{ ...f.read.review!.groups[0]!, title: 'Later saved grouping' }] } } }); if (first) delivered = true
  })
  await ignoreAbort(page); await page.getByRole('button', { name: 'Semantic review', exact: true }).click(); await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Files changed', exact: false }).click(); await page.getByRole('button', { name: 'Semantic review', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Later saved grouping', exact: true })).toBeVisible()
  const trigger = page.getByRole('button', { name: 'Ungrouped changes', exact: false }); await trigger.click(); release!(); await expect.poll((): boolean => delivered).toBe(true)
  await expect(trigger).toBeFocused(); await expect(page.getByRole('heading', { name: 'Ungrouped changes', exact: true })).toBeVisible(); await expect(page.getByText('saved version 2', { exact: false })).toBeVisible()
})

test('semantic high-water checks reject regressing and mutable max-uint64 saved records', async ({ page }): Promise<void> => {
  const pr = await registered(page, 713); const f = semanticFixture(pr); const initial = { ...f.read, version: '18446744073709551615' }; let current: SemanticRead = initial
  await page.route('**/semantic-review', async (route): Promise<void> => { await route.fulfill({ json: current }) })
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  for (const next of [{ ...initial, version: '18446744073709551614' }, { ...initial, review: { ...initial.review!, reviewID: f.status.reviewID } }, { ...initial, review: { ...initial.review!, groups: [{ ...initial.review!.groups[0]!, title: 'Mutable same-version group' }] } }]) {
    current = next; await page.getByRole('button', { name: 'Refresh saved review', exact: true }).click(); await expect(page.getByRole('alert')).toContainText('Cannot read saved'); await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  }
})

test('uncertain semantic admission is not automatically retried and keeps prior complete groups', async ({ page }): Promise<void> => {
  const pr = await registered(page, 714); const f = semanticFixture(pr); let posts = 0
  await page.route('**/source', async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route('**/semantic-review', async (route): Promise<void> => { await route.fulfill({ json: f.read }) })
  await page.route('**/semantic-review/generations', async (route): Promise<void> => { posts++; await route.abort('failed') })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click(); await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await page.getByRole('button', { name: 'Regenerate semantic review', exact: true }).click(); await page.getByRole('button', { name: 'Confirm regenerate review', exact: true }).click(); await expect(page.getByRole('alert')).toContainText('could not be confirmed')
  await expect(page.getByRole('button', { name: 'Regenerate semantic review', exact: true })).toHaveAttribute('aria-disabled', 'true'); await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible(); expect(posts).toBe(1)
})

for (const action of ['generate', 'status', 'cancel']) test(`late semantic ${action} cannot mutate a new verified connection when cancellation is ignored`, async ({ page }): Promise<void> => {
  const pr = await registered(page, 715 + ['generate', 'status', 'cancel'].indexOf(action)); const f = semanticFixture(pr)
  let current: SemanticRead = { ...f.read, generation: action === 'generate' ? null : f.status }
  let release: (() => void) | undefined; let delivered = false
  await page.route('**/source', async (route): Promise<void> => { await route.fulfill({ json: f.source }) })
  await page.route('**/semantic-review', async (route): Promise<void> => { await route.fulfill({ json: current }) })
  await page.route('**/semantic-review/generations**', async (route): Promise<void> => {
    const post = route.request().method() === 'POST'
    if (action === 'generate' && !route.request().url().endsWith('/cancel') && post || action === 'status' && !post || action === 'cancel' && route.request().url().endsWith('/cancel')) { await new Promise<void>((resolve): void => { release = resolve }); delivered = true }
    await route.fulfill({ status: post ? 202 : 200, json: f.status })
  })
  await ignoreAbort(page)
  if (action === 'generate') await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  if (action === 'generate') { await page.getByRole('button', { name: 'Regenerate semantic review', exact: true }).click(); await page.getByRole('button', { name: 'Confirm regenerate review', exact: true }).click() }
  if (action === 'cancel') await page.getByRole('button', { name: 'Cancel generation', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  current = { ...f.read, version: '2', review: { ...f.read.review!, reviewID: f.status.reviewID }, generation: null }
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click(); await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  await page.getByRole('textbox', { name: 'API token', exact: true }).fill(await token()); await page.getByRole('button', { name: 'Connect backend', exact: true }).click()
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click(); await expect(page.getByText('saved version 2', { exact: false })).toBeVisible()
  const navigation = page.getByRole('button', { name: 'Ungrouped changes', exact: false }); await navigation.click(); release!(); await expect.poll((): boolean => delivered).toBe(true)
  await expect(navigation).toBeFocused(); await expect(page.getByText('saved version 2', { exact: false })).toBeVisible(); await expect(page.getByText('Generation: pending', { exact: false })).toHaveCount(0)
})
