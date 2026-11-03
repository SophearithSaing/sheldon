import { expect, test, type Page, type Response } from '@playwright/test'
import { readFile, readdir, stat } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { offlineCLI } from './release/offline'
import type { SourceResponse } from '../src/runtime/source'

/**
 * Waits for an actual authenticated source response; no source HTTP route is mocked.
 * @param page Real release page.
 * @returns Next native source response.
 */
function sourceResponse(page: Page): Promise<Response> {
  return page.waitForResponse((response: Response): boolean => new URL(response.url()).pathname.endsWith('/source') && response.request().method() === 'POST')
}

/**
 * Runs a source action and reads the unmodified backend result.
 * @param page Real release page.
 * @param button Explicit source action label.
 * @returns Actual source result from the production source pipeline.
 */
async function source(page: Page, button: string): Promise<SourceResponse> {
  const received = sourceResponse(page)
  await page.getByRole('button', { name: button, exact: true }).click()
  const response = await received
  if (!response.ok()) throw new Error(`Native offline source action failed with HTTP ${response.status()}.`)
  return await response.json() as SourceResponse
}

/**
 * Probes only a PID captured by the owned native Git test fixture.
 * @param pid Owned fixture PID.
 * @returns Whether the fixture process still exists.
 */
function alive(pid: number): boolean {
  try { process.kill(pid, 0); return true } catch { return false }
}

test('standalone CLI opens two native offline PRs, refreshes independently and reuses source across restart', async ({ page, context }, testInfo): Promise<void> => {
  test.setTimeout(120000)
  const fixture = await offlineCLI()
  const traffic: string[] = []
  const messages: string[] = []
  page.on('request', (request): void => { traffic.push(request.url()) })
  page.on('pageerror', (failure: Error): void => { messages.push(failure.message) })
  try {
    const firstLaunch = await fixture.start('https://github.com/fixture/repo/pull/31')
    const firstURL = new URL(firstLaunch)
    const firstRoute = firstURL.pathname
    const firstID = firstRoute.slice('/reviews/'.length)
    const firstDiscovery = await fixture.discovery()
    await page.goto(firstLaunch)
    await expect(page).toHaveURL(firstDiscovery.url + firstRoute)
    await expect(page.getByRole('heading', { name: 'PR registered. Source not loaded.' })).toBeVisible()
    expect((await fixture.audit()).filter((entry): boolean => entry.Operation === 'fetch')).toHaveLength(0)
    const first = await source(page, 'Load pinned source')
    expect(first.state).toBe('ready')
    expect(first.pr.id).toBe(firstID)
    expect(first.diff!.revision).toMatchObject({ baseSHA: fixture.baseTip, mergeBaseSHA: fixture.mergeBase, headSHA: fixture.firstHead, baseBranch: 'release/target', baseSource: 'github' })
    expect(first.diff!.files.map((file): string | undefined => file.newPath ?? file.oldPath)).toEqual(['remove.txt', 'shared.txt'])
    await page.getByRole('button', { name: 'shared.txt', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Offline CLI PR 31', exact: true })).toBeVisible()
    await expect(page.locator('.line-source code')).toHaveText(['before', 'after PR31 <script>literal</script>'])
    expect(await page.locator('.line-source script').count()).toBe(0)
    await page.screenshot({ path: testInfo.outputPath('native-cli-first-pr.png') })

    const repositories = join(fixture.cache, 'repositories')
    const cacheNames = await readdir(repositories)
    expect(cacheNames).toHaveLength(1)
    const bare = join(repositories, cacheNames[0]!)
    const repositoryBefore = await stat(bare)
    const oldWorktree = join(fixture.cache, 'worktrees', firstID, fixture.firstHead)
    const sourceBefore = await readFile(join(oldWorktree, 'shared.txt'), 'utf8')
    const firstAlias = new URL(await fixture.open('https://github.com/FIXTURE/REPO/pull/31/?tab=files#discussion'))
    expect(firstAlias.origin + firstAlias.pathname).toBe(firstDiscovery.url + firstRoute)
    expect((await fixture.discovery()).url).toBe(firstDiscovery.url)

    const secondLaunch = await fixture.open('https://github.com/fixture/repo/pull/32')
    const second = await context.newPage()
    await second.goto(secondLaunch)
    const unresolved = await source(second, 'Load pinned source')
    expect(unresolved.state).toBe('fallback-required')
    await expect(second.getByRole('heading', { name: 'GitHub metadata unavailable', exact: true })).toBeVisible()
    const choices = await source(second, 'Use Git-only fallback')
    expect(choices.state).toBe('base-selection-required')
    await expect(second.getByLabel('Fetched base branch')).toHaveValue('')
    await expect(second.getByRole('button', { name: 'Load selected base', exact: true })).toBeDisabled()
    await second.getByLabel('Fetched base branch').selectOption('release/target')
    const secondSource = await source(second, 'Load selected base')
    expect(secondSource.pr.id).not.toBe(firstID)
    expect(secondSource.diff!.revision).toMatchObject({ baseSHA: fixture.baseTip, mergeBaseSHA: fixture.mergeBase, headSHA: fixture.secondHead, baseBranch: 'release/target', baseSource: 'user-selected' })
    expect(secondSource.diff!.files.map((file): string | undefined => file.newPath ?? file.oldPath)).toEqual(['notes.txt'])
    await expect(second.locator('.line-source code')).toHaveText(['only PR32'])
    await expect(page.getByText(`Pinned revision ${first.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
    await second.screenshot({ path: testInfo.outputPath('native-cli-manual-second-pr.png') })
    expect(await readdir(repositories)).toEqual(cacheNames)
    expect((await stat(bare)).ino).toBe(repositoryBefore.ino)
    const records: { prs: { id: string; number: string }[] } = JSON.parse(await readFile(join(dirname(fixture.stateFile), 'prs.json'), 'utf8'))
    expect(records.prs.map((pr): string => pr.number).sort()).toEqual(['31', '32'])

    const replacement = await fixture.head(31, 'after PR31 refreshed\n')
    const refreshed = await source(page, 'Refresh source')
    expect(refreshed.requestedRevisionID).toBe(first.diff!.revisionID)
    expect(refreshed.diff!.revision.headSHA).toBe(replacement)
    expect(refreshed.diff!.revisionID).not.toBe(first.diff!.revisionID)
    await expect(page.locator('.line-source code')).toHaveText(['before', 'after PR31 refreshed'])
    await expect(second.getByText(`Pinned revision ${secondSource.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
    expect(await readFile(join(oldWorktree, 'shared.txt'), 'utf8')).toBe(sourceBefore)
    expect(await fixture.git(oldWorktree, '', 'rev-parse', 'HEAD')).toBe(fixture.firstHead)
    await fixture.git(fixture.upstream, '', 'update-ref', '-d', 'refs/pull/31/head')
    const failed = sourceResponse(page)
    await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
    expect((await failed).status()).toBe(502)
    await expect(page.getByRole('alert')).toContainText('Cannot prepare source')
    await expect(page.getByText(`Pinned revision ${refreshed.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
    await expect(page.locator('.line-source code')).toHaveText(['before', 'after PR31 refreshed'])
    await expect(second.locator('.line-source code')).toHaveText(['only PR32'])
    await page.screenshot({ path: testInfo.outputPath('native-cli-refresh-failure.png') })
    await fixture.git(fixture.upstream, '', 'update-ref', 'refs/pull/31/head', replacement)
    const recovered = await source(page, 'Retry source loading')
    expect(recovered.diff!.revisionID).toBe(refreshed.diff!.revisionID)
    await expect(page.getByRole('button', { name: 'Open original snapshot', exact: true })).toHaveCount(0)
    await expect(page.getByRole('button', { name: 'Verify pinned cache', exact: true })).toHaveCount(0)

    await fixture.stop()
    expect((await readdir(fixture.cache)).filter((name): boolean => name.startsWith('.git-controls-'))).toHaveLength(0)
    const restartedLaunch = await fixture.start('https://github.com/FIXTURE/REPO/pull/31')
    const restartedDiscovery = await fixture.discovery()
    expect(restartedDiscovery.token === firstDiscovery.token).toBe(false)
    await page.goto(restartedLaunch)
    const reopened = await source(page, 'Load pinned source')
    expect(reopened.pr.id).toBe(firstID)
    expect(reopened.diff!.revisionID).toBe(refreshed.diff!.revisionID)
    expect((await stat(bare)).ino).toBe(repositoryBefore.ino)
    expect((await fixture.audit()).filter((entry): boolean => entry.Operation === 'init')).toHaveLength(1)
    expect((await fixture.audit()).every((entry): boolean => entry.Safe)).toBe(true)
    const reopenedRecords: { prs: unknown[] } = JSON.parse(await readFile(join(dirname(fixture.stateFile), 'prs.json'), 'utf8'))
    expect(reopenedRecords.prs).toHaveLength(2)
    expect(await page.content()).not.toContain(restartedDiscovery.token)
    expect(traffic.every((url): boolean => new URL(url).hostname === '127.0.0.1')).toBe(true)
    expect(traffic.some((url): boolean => url.includes(firstDiscovery.token) || url.includes(restartedDiscovery.token))).toBe(false)
    expect(messages).toEqual([])

    await fixture.stop()
    const hangingLaunch = await fixture.start('https://github.com/fixture/repo/pull/31', true)
    await page.goto(hangingLaunch)
    const fetches = (await fixture.audit()).filter((entry): boolean => entry.Operation === 'fetch').length
    await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
    await expect.poll(async (): Promise<number> => (await fixture.audit()).filter((entry): boolean => entry.Operation === 'fetch').length).toBe(fetches + 1)
    const pending = (await fixture.audit()).filter((entry): boolean => entry.Operation === 'fetch').at(-1)!
    expect(alive(pending.PID)).toBe(true)
    await fixture.stop()
    await expect.poll((): boolean => alive(pending.PID)).toBe(false)
    expect((await readdir(fixture.cache)).filter((name): boolean => name.startsWith('.git-controls-'))).toHaveLength(0)
    await second.close()
  } finally { await fixture.close() }
})
