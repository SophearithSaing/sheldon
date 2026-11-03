import { expect, test, type Page } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { createHash } from 'node:crypto'
import type { RegisteredPR } from '../src/runtime/client'
import type { SourceResponse, SourceFile, SourceLine } from '../src/runtime/source'

const frontend = 'http://127.0.0.1:5175'
test.use({ baseURL: frontend })

/**
 * Hashes explicit browser fixture references, never a real source claim.
 * @param value Fixture identity.
 * @returns Full digest.
 */
function digest(value: string): string { return createHash('sha256').update(value).digest('hex') }

/**
 * Reads only the isolated development fixture's credential.
 * @returns Fixture backend credential.
 */
async function token(): Promise<string> { return JSON.parse(await readFile(process.env.SHELDON_TEST_STATE!, 'utf8')).token }

/**
 * Registers an offline browser fixture and connects its routed page.
 * @param page Browser page.
 * @param number Distinct fixture PR number.
 * @returns Registered fixture identity.
 */
async function registered(page: Page, number: number): Promise<RegisteredPR> {
  const credential = await token()
  const response = await page.request.post(frontend + '/api/open', { headers: { Authorization: `Bearer ${credential}` }, data: { url: `https://github.com/ui/fixture/pull/${number}` } })
  const opened: { prID: string; route: string } = await response.json()
  const metadata = await page.request.get(frontend + '/api/reviews/' + opened.prID, { headers: { Authorization: `Bearer ${credential}` } })
  const pr: RegisteredPR = await metadata.json()
  await page.goto(`${opened.route}#token=${credential}`)
  await expect(page.getByRole('heading', { name: `Pull request #${number}`, exact: true })).toBeVisible()
  return pr
}

/**
 * Builds explicit source-UI fixture data, not a native Git result.
 * @param pr Registered browser fixture PR.
 * @param count Number of fixture changed files.
 * @param baseSHA Fixture base tip.
 * @param headSHA Fixture head commit.
 * @returns Pinned-looking fixture response for renderer testing only.
 */
function sourceFixture(pr: RegisteredPR, count = 40, baseSHA = 'a'.repeat(40), headSHA = 'c'.repeat(40)): SourceResponse {
  const revision = { prID: pr.id, baseBranch: 'release/target', baseSource: 'github' as const, baseSHA, headSHA, mergeBaseSHA: 'b'.repeat(40) }
  const revisionID = digest([pr.id, revision.baseSHA, revision.headSHA, revision.mergeBaseSHA].join('\n'))
  const files: SourceFile[] = []
  for (let index = 0; index < count; index++) {
    const id = digest(`${revisionID}/file/${index}`)
    const lines: SourceLine[] = []
    for (let n = 1; n <= 120; n++) lines.push({ id: digest(`${id}/line/${n}`), sign: n === 1 ? '+' : ' ', text: n === 1 ? '<script>literal source, never HTML</script> ' + 'long source '.repeat(40) : `const fixtureLine${n} = ${n}`, oldLine: n === 1 ? undefined : n - 1, newLine: n, noNewline: n === 120 })
    files.push({ id, status: 'modified', oldPath: `src/file${String(index).padStart(4, '0')}.ts`, newPath: `src/file${String(index).padStart(4, '0')}.ts`, oldMode: '100644', newMode: '100644', oldObject: 'd'.repeat(40), newObject: 'e'.repeat(40), kind: 'text', hunks: [{ id: digest(id + '/hunk'), old: { id: digest(id + '/old'), start: 1, count: 119 }, new: { id: digest(id + '/new'), start: 1, count: 120 }, section: '', lines }] })
  }
  return { state: 'ready', pr: { ...pr, title: `Offline source fixture #${pr.number}`, author: 'fixture-author' }, diff: { revision, revisionID, complete: true, files } }
}

test('real PR registration retains input on failure and prevents duplicate submits', async ({ page }): Promise<void> => {
  const credential = await token()
  await page.goto('/#token=' + credential)
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.getByRole('button', { name: 'Open a PR', exact: true }).click()
  const input = page.getByRole('textbox', { name: 'GitHub PR URL', exact: true })
  await input.fill('not a URL')
  await page.getByRole('button', { name: 'Register and open PR' }).click()
  await expect(input).toHaveValue('not a URL')
  await expect(page.getByText('Enter a GitHub pull request URL, not credentials.')).toBeVisible()
  await input.press('Escape')
  await expect(page.getByRole('button', { name: 'Open a PR', exact: true })).toBeFocused()
  await page.getByRole('button', { name: 'Open a PR', exact: true }).click()
  await expect(input).toHaveValue('not a URL')
  let calls = 0
  let release: (() => void) | undefined
  await page.route('**/api/open', async (route): Promise<void> => {
    calls++
    if (calls === 1) { await new Promise<void>((resolve): void => { release = resolve }); await route.fulfill({ status: 503, json: { error: 'private fixture diagnostic' } }) }
    else await route.continue()
  })
  const url = 'https://github.com/ui/fixture/pull/700'
  await input.fill(url)
  await page.getByRole('button', { name: 'Register and open PR' }).click()
  await expect(page.getByRole('button', { name: 'Registering…', exact: true })).toBeDisabled()
  await input.press('Enter')
  expect(calls).toBe(1)
  release!()
  await expect(page.getByText('Cannot register this PR.', { exact: false })).toBeVisible()
  await expect(input).toHaveValue(url)
  expect(await page.content()).not.toContain('private fixture diagnostic')
  await page.getByRole('button', { name: 'Register and open PR' }).click()
  await expect(page.getByRole('heading', { name: 'Pull request #700', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Load pinned source', exact: true })).toBeVisible()
  expect(await page.content()).not.toContain('Deduplicate concurrent session refreshes')
  expect(await page.content()).not.toContain(credential)
})

test('pinned source loads without replacing chrome, navigates files and supports native copying', async ({ page, context }, testInfo): Promise<void> => {
  const pr = await registered(page, 701)
  const fixture = sourceFixture(pr)
  let release: (() => void) | undefined
  let calls = 0
  const api: string[] = []
  page.on('request', (request): void => { if (request.url().includes('/api/')) api.push(new URL(request.url()).pathname) })
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    calls++
    expect(route.request().postDataJSON()).toEqual({ action: 'load' })
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ json: fixture })
  })
  expect(calls).toBe(0)
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Loading pinned source…' })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Open a PR', exact: true })).toBeEnabled()
  await expect(page.getByRole('link', { name: 'Sample UI previews', exact: true })).toBeVisible()
  expect(await page.locator('code').count()).toBe(0)
  await page.screenshot({ path: testInfo.outputPath('real-source-loading.png') })
  release!()
  await expect(page.getByRole('heading', { name: 'Offline source fixture #701' })).toBeVisible()
  const panes = await page.locator('.workspace-panes').boundingBox()
  expect(panes!.y).toBe(160)
  const rail = page.locator('.workspace-files')
  expect((await rail.boundingBox())!.width).toBe(240)
  await expect(page.getByRole('button', { name: 'Ask — unavailable' })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Split — unavailable' })).toBeDisabled()
  const source = page.getByRole('region', { name: 'Unified diff for src/file0000.ts' })
  await expect(source.locator('code').first()).toContainText('<script>literal source, never HTML</script>')
  expect(await source.locator('script').count()).toBe(0)
  expect(await source.evaluate((element): boolean => element.scrollWidth > element.clientWidth)).toBe(true)
  await context.grantPermissions(['clipboard-read', 'clipboard-write'])
  const literal = await source.locator('code').first().textContent()
  await source.locator('code').first().evaluate((element): void => { const range = document.createRange(); range.selectNodeContents(element); const selection = window.getSelection(); selection?.removeAllRanges(); selection?.addRange(range) })
  await page.keyboard.press('Control+c')
  expect(await page.evaluate((): Promise<string> => navigator.clipboard.readText())).toBe(literal)
  await source.focus()
  await page.keyboard.press('PageDown')
  await expect.poll((): Promise<number> => page.locator('.source-pane').evaluate((element): number => element.scrollTop)).toBeGreaterThan(0)
  expect(await page.locator('.files-body').evaluate((element): number => element.scrollTop)).toBe(0)
  await page.getByRole('button', { name: 'src/file0001.ts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/file0001.ts', exact: true })).toBeFocused()
  await page.getByRole('searchbox', { name: 'Find a changed file' }).fill('file0039')
  await expect(page.getByRole('button', { name: 'src/file0039.ts', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'src/file0001.ts', exact: true })).toHaveCount(0)
  await page.getByRole('searchbox', { name: 'Find a changed file' }).fill('')
  await page.locator('.file-disclosure > summary').click()
  expect((await rail.boundingBox())!.width).toBe(48)
  await page.locator('.file-disclosure > summary').press('Enter')
  expect((await rail.boundingBox())!.width).toBe(240)
  await page.screenshot({ path: testInfo.outputPath('real-source-1440.png') })
  await page.setViewportSize({ width: 700, height: 900 })
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('real-source-700.png'), fullPage: true })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.getByRole('heading', { name: 'src/file0001.ts', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('heading', { name: 'src/file0001.ts', exact: true })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('real-source-text-200.png'), fullPage: true })
  expect(calls).toBe(1)
  expect(api).toEqual([`/api/reviews/${pr.id}/source`])
})

test('explicit fallback requires an unselected fetched base and recovers from moved branches', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 702)
  let selects = 0
  const fixture = sourceFixture(pr, 1)
  fixture.diff!.revision.baseSource = 'user-selected'
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    if (input.action === 'load') await route.fulfill({ json: { state: 'fallback-required', pr } })
    else if (input.action === 'git-only') await route.fulfill({ json: { state: 'base-selection-required', pr, candidates: [{ branch: 'release/target', sha: 'a'.repeat(40) }, { branch: 'main', sha: 'd'.repeat(40) }] } })
    else {
      expect(input).toEqual({ action: 'select-base', branch: 'release/target', sha: 'a'.repeat(40) })
      if (++selects === 1) await route.fulfill({ status: 409, json: { code: 'refresh-required', error: 'raw native secret diagnostic' } })
      else await route.fulfill({ json: fixture })
    }
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(page.getByRole('heading', { name: 'GitHub metadata unavailable' })).toBeVisible()
  await page.getByRole('button', { name: 'Use Git-only fallback' }).click()
  const picker = page.getByLabel('Fetched base branch', { exact: true })
  await expect(picker).toHaveValue('')
  await expect(page.getByRole('button', { name: 'Load selected base', exact: true })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('real-base-picker.png') })
  await picker.selectOption('release/target')
  await page.getByRole('button', { name: 'Load selected base', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('PR or base changed')
  expect(await page.content()).not.toContain('raw native secret diagnostic')
  await page.getByRole('button', { name: 'Fetch branch candidates again' }).click()
  await expect(picker).toHaveValue('')
  await picker.selectOption('release/target')
  await page.getByRole('button', { name: 'Load selected base', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/file0000.ts', exact: true })).toBeVisible()
  await page.getByText('Pinned revision', { exact: false }).click()
  await expect(page.getByText('Explicit Git-only selection; target unverified by GitHub')).toBeVisible()
})

test('source errors, empty and non-text results never borrow sample source', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 703)
  let calls = 0
  const fixture = sourceFixture(pr, 0)
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    if (++calls === 1) await route.fulfill({ status: 502, body: 'private /cache/path synthetic-secret' })
    else await route.fulfill({ json: fixture })
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(page.getByRole('alert')).toContainText('Cannot prepare source')
  expect(await page.content()).not.toContain('synthetic-secret')
  expect(await page.locator('code').count()).toBe(0)
  await page.screenshot({ path: testInfo.outputPath('real-source-error.png') })
  await page.getByRole('button', { name: 'Retry source loading' }).click()
  await expect(page.getByRole('heading', { name: 'No changes in this comparison' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('real-source-empty.png') })
  await page.unrouteAll()
  const other = await registered(page, 704)
  const binary = sourceFixture(other, 1)
  binary.diff!.files[0]!.kind = 'binary'
  binary.diff!.files[0]!.hunks = []
  await page.route(`**/api/reviews/${other.id}/source`, (route): Promise<void> => route.fulfill({ json: binary }))
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(page.getByRole('heading', { name: 'binary change' })).toBeVisible()
  expect(await page.locator('code').count()).toBe(0)
  await page.screenshot({ path: testInfo.outputPath('real-source-binary.png') })
})

test('disconnect cancels source and a late reply cannot populate a different connection', async ({ page }): Promise<void> => {
  const pr = await registered(page, 705)
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ json: sourceFixture(pr, 1) })
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(page.getByRole('heading', { name: 'Loading pinned source…' })).toBeVisible()
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  release!()
  await page.unrouteAll({ behavior: 'wait' })
  await expect(page.getByRole('heading', { name: 'Offline source fixture #705' })).toHaveCount(0)
  await expect(page.getByRole('button', { name: 'Connect backend', exact: true })).toBeVisible()
  expect(await page.locator('code').count()).toBe(0)
})

test('opening another PR from pinned source preserves the original tab', async ({ page }): Promise<void> => {
  const pr = await registered(page, 706)
  await page.route(`**/api/reviews/${pr.id}/source`, (route): Promise<void> => route.fulfill({ json: sourceFixture(pr, 1) }))
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(page.getByRole('heading', { name: 'Offline source fixture #706' })).toBeVisible()
  const originalURL = page.url()
  await page.getByRole('button', { name: 'Open a PR', exact: true }).click()
  await page.getByRole('textbox', { name: 'GitHub PR URL', exact: true }).fill('https://github.com/ui/fixture/pull/707')
  const created = page.waitForEvent('popup')
  await page.getByRole('button', { name: 'Register and open PR' }).click()
  const other = await created
  await expect(other.getByRole('heading', { name: 'Pull request #707', exact: true })).toBeVisible()
  await expect(other.getByRole('button', { name: 'Load pinned source', exact: true })).toBeVisible()
  expect(await other.evaluate((): unknown => window.opener)).toBe(null)
  await expect(page).toHaveURL(originalURL)
  await expect(page.getByRole('heading', { name: 'Offline source fixture #706' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'src/file0000.ts', exact: true })).toBeVisible()
  await other.close()
})

test('refresh isolates two tabs, preserves file navigation and reopens exact original source', async ({ page, context }, testInfo): Promise<void> => {
  const pr = await registered(page, 710)
  const initial = sourceFixture(pr, 2)
  const baseOnly = sourceFixture(pr, 2, 'd'.repeat(40))
  const headOnly = sourceFixture(pr, 2, 'd'.repeat(40), 'e'.repeat(40))
  let refreshed = 0
  const requests: string[] = []
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    requests.push(input.action)
    if (input.action === 'load') await route.fulfill({ json: initial })
    else if (input.action === 'snapshot') {
      expect(input.revision).toEqual(initial.diff!.revision)
      await route.fulfill({ json: { ...initial, requestedRevisionID: initial.diff!.revisionID } })
    } else {
      const expected = refreshed === 0 ? initial : baseOnly
      expect(input.revision).toEqual(expected.diff!.revision)
      const next = ++refreshed === 1 ? baseOnly : headOnly
      await route.fulfill({ json: { ...next, requestedRevisionID: expected.diff!.revisionID } })
    }
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(page.getByRole('heading', { name: 'src/file0000.ts', exact: true })).toBeVisible()
  const other = await context.newPage()
  await other.route(`**/api/reviews/${pr.id}/source`, (route): Promise<void> => route.fulfill({ json: initial }))
  await other.goto(`/reviews/${pr.id}#token=${await token()}`)
  await other.getByRole('button', { name: 'Load pinned source' }).click()
  await expect(other.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'src/file0001.ts', exact: true }).click()
  await page.getByRole('searchbox', { name: 'Find a changed file' }).fill('file0001')
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByText(`Pinned revision ${baseOnly.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'src/file0001.ts', exact: true })).toBeVisible()
  await expect(page.getByRole('searchbox', { name: 'Find a changed file' })).toHaveValue('file0001')
  await expect(other.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByText(`Pinned revision ${headOnly.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('refreshed-pinned-source.png') })
  await page.getByRole('button', { name: 'Open original snapshot', exact: true }).click()
  await expect(page.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await expect(page.getByText('Exact pinned source verified in the cache; no latest source was fetched.')).toBeVisible()
  await expect(other.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  expect(requests).toEqual(['load', 'refresh', 'refresh', 'snapshot'])
  await other.close()
})

test('failed and cancelled refreshes retain readable source and ignore stale responses', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 711)
  const initial = sourceFixture(pr, 2)
  const updated = sourceFixture(pr, 2, 'd'.repeat(40))
  let calls = 0
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    if (input.action === 'load') { await route.fulfill({ json: initial }); return }
    calls++
    if (calls === 1) await route.fulfill({ status: 502, json: { code: 'source-unavailable', error: 'native private diagnostic' } })
    else if (calls === 2) {
      await new Promise<void>((resolve): void => { release = resolve })
      await route.fulfill({ json: { ...updated, requestedRevisionID: initial.diff!.revisionID } })
    } else await route.fulfill({ json: { ...initial, requestedRevisionID: initial.diff!.revisionID } })
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Cannot prepare source')
  await expect(page.getByText('Stale / refresh needs attention.', { exact: false })).toBeVisible()
  await expect(page.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  expect(await page.content()).not.toContain('native private diagnostic')
  await page.screenshot({ path: testInfo.outputPath('refresh-failure-retains-source.png') })
  await page.getByRole('button', { name: 'Retry source loading', exact: true }).click()
  await expect(page.getByText('Refreshing source…', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'src/file0001.ts', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/file0001.ts', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Cancel source request', exact: true }).click()
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByText('Source unchanged. The displayed revision is still pinned.')).toBeVisible()
  release!()
  await page.unrouteAll({ behavior: 'wait' })
  await expect(page.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await expect(page.getByText(`Pinned revision ${updated.diff!.revisionID.slice(0, 8)}`, { exact: true })).toHaveCount(0)
})

test('historical cache loss and mismatched snapshot replies never substitute latest source', async ({ page }, testInfo): Promise<void> => {
  const pr = await registered(page, 712)
  const initial = sourceFixture(pr, 1)
  const updated = sourceFixture(pr, 1, 'd'.repeat(40))
  let calls = 0
  let checks = 0
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    calls++
    if (input.action === 'load') await route.fulfill({ json: initial })
    else if (input.action === 'snapshot') {
      if (++checks === 1) await route.fulfill({ status: 410, json: { code: 'snapshot-unavailable' } })
      else await route.fulfill({ json: { ...updated, requestedRevisionID: initial.diff!.revisionID } })
    }
    else await route.fulfill({ json: { ...updated, requestedRevisionID: 'f'.repeat(64) } })
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await page.getByRole('button', { name: 'Verify pinned cache' }).click()
  await expect(page.getByRole('alert')).toContainText('exact pinned source is unavailable')
  await expect(page.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('missing-pinned-cache.png') })
  await page.getByRole('button', { name: 'Verify pinned cache' }).click()
  await expect(page.getByRole('alert')).toHaveText('Source response does not match the requested snapshot.')
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByRole('alert')).toHaveText('Source response does not match the requested snapshot.')
  await expect(page.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  expect(calls).toBe(4)
})

test('metadata failure during refresh keeps the old diff through manual base selection', async ({ page }): Promise<void> => {
  const pr = await registered(page, 713)
  const initial = sourceFixture(pr, 1)
  const updated = sourceFixture(pr, 1, 'd'.repeat(40))
  updated.diff!.revision.baseSource = 'user-selected'
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    if (input.action === 'load') await route.fulfill({ json: initial })
    else if (input.action === 'refresh') await route.fulfill({ json: { state: 'fallback-required', pr, requestedRevisionID: initial.diff!.revisionID } })
    else if (input.action === 'git-only') await route.fulfill({ json: { state: 'base-selection-required', pr, candidates: [{ branch: 'release/target', sha: 'd'.repeat(40) }] } })
    else await route.fulfill({ json: updated })
  })
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'GitHub metadata unavailable' })).toBeVisible()
  await expect(page.getByText(`Pinned revision ${initial.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Use Git-only fallback' }).click()
  await expect(page.getByLabel('Fetched base branch')).toHaveValue('')
  await page.getByLabel('Fetched base branch').selectOption('release/target')
  await page.getByRole('button', { name: 'Load selected base', exact: true }).click()
  await expect(page.getByText(`Pinned revision ${updated.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
})
