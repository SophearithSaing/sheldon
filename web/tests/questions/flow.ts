import { expect, type BrowserContext, type Page, type Response, type TestInfo } from '@playwright/test'
import { access, readdir, readFile } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { offlineCLI, type NativeAudit, type OfflineCLI } from '../release/offline'
import type { SourceResponse } from '../../src/runtime/source'
import type { QuestionAnswer } from '../../src/runtime/questions'
import { preserveResponseBodies } from '../response-bodies'

/**
 * Waits for an unmodified application POST response, never a browser-intercepted fixture reply.
 * @param page Actual application page.
 * @param suffix Exact action suffix.
 * @returns Next matching application response.
 */
function response(page: Page, suffix: string): Promise<Response> {
  return page.waitForResponse((result: Response): boolean => result.request().method() === 'POST' && new URL(result.url()).pathname.endsWith(suffix), { timeout: 15000 })
}

/**
 * Runs an explicit real source action through the browser and application-owned Git cache.
 * @param page Connected application page.
 * @param label Source action button.
 * @returns Unmodified managed-source response.
 */
async function source(page: Page, label: string): Promise<SourceResponse> {
  const received = response(page, '/source').then(async (result: Response): Promise<SourceResponse> => {
    if (!result.ok()) throw new Error('Native question integration source action failed.')
    return await result.json() as SourceResponse
  })
  await page.getByRole('button', { name: label, exact: true }).click()
  return received
}

/**
 * Sends literal input for the actual head line, or explicitly retries a retained draft.
 * @param page Real source page.
 * @param text Literal integration question.
 * @returns Actual admission/rejection response.
 */
async function ask(page: Page, text: string): Promise<Response> {
  const form = page.getByRole('form', { name: 'New question', exact: true })
  if (!await form.isVisible()) {
    await page.getByRole('button', { name: 'Select head line 1', exact: true }).click()
    await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  }
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill(text)
  const received = response(page, '/questions')
  await page.getByRole('button', { name: 'Send question', exact: true }).click()
  return received
}

/**
 * Sends one explicit follow-up through the real original-thread endpoint.
 * @param page Owning conversation page.
 * @param text Literal follow-up text.
 * @returns Actual validated backend admission identity.
 */
async function followup(page: Page, text: string): Promise<QuestionAnswer> {
  const received = response(page, '/answers')
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill(text)
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).press('Enter')
  const result = await received
  if (result.status() !== 202) throw new Error('Native question integration follow-up was not admitted.')
  return await result.json() as QuestionAnswer
}

/**
 * Reads actual prompt dispatch records from the synthetic native executable.
 * @param fixture Owned native fixture controller.
 * @returns Prompt records, excluding history/status polling.
 */
async function prompts(fixture: OfflineCLI): Promise<NativeAudit[]> {
  return (await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && !!entry.path?.endsWith('/prompt'))
}

/**
 * Reads actual private admission records independently of browser rendering and native history.
 * @param fixture Owned offline application controller.
 * @param prID Registered PR identity.
 * @returns Saved bindings and ordered admitted turns.
 */
async function savedAdmissions(fixture: OfflineCLI, prID: string): Promise<{ bindings: { state: string }[]; threads: { threadID: string; answers: QuestionAnswer[] }[] }> {
  return JSON.parse(await readFile(join(dirname(fixture.stateFile), 'reviews', prID, 'threads.json'), 'utf8'))
}

/**
 * Probes only a PID recorded by this test's disposable native executable.
 * @param pid Owned native fixture PID.
 * @returns Whether the fixture is still running.
 */
function alive(pid: number): boolean { try { process.kill(pid, 0); return true } catch { return false } }

/**
 * Exercises real browser → application → native Git → synthetic inference, without HTTP mocks.
 * @param page First real PR page.
 * @param context Browser context for separate tab/PR routing.
 * @param info Test artifact destinations.
 * @param development Whether to use the actual asset-free CLI and protected Vite proxy.
 */
export async function questionFlow(page: Page, context: BrowserContext, info: TestInfo, development: boolean): Promise<void> {
  const fixture = await offlineCLI({ development, questions: true })
  const traffic: string[] = []
  const errors: string[] = []
  /**
   * Preserves Chromium response bodies for assertions and tracks public URLs/browser failures.
   * @param target Application page to observe.
   */
  async function track(target: Page): Promise<void> {
    await preserveResponseBodies(target)
    target.on('request', (request): void => { traffic.push(request.url()) })
    target.on('pageerror', (error: Error): void => { errors.push(error.message) })
  }
  await track(page)
  try {
    const launch = await fixture.start('https://github.com/fixture/repo/pull/31')
    const initialDiscovery = await fixture.discovery()
    await page.goto(launch)
    await expect(page.getByRole('heading', { name: 'PR registered. Source not loaded.', exact: true })).toBeVisible()
    const loaded = await source(page, 'Load pinned source')
    const diff = loaded.diff!
    expect(diff.revision.headSHA).toBe(fixture.firstHead)
    expect(diff.revision.mergeBaseSHA).toBe(fixture.mergeBase)
    await page.getByRole('button', { name: 'shared.txt', exact: true }).click()
    expect(await fixture.nativeAudit()).toHaveLength(0)
    const admitted = await ask(page, 'Explain this actual selected source. Synthetic inference fixture only.')
    expect(admitted.status()).toBe(202)
    const original = await admitted.json() as QuestionAnswer
    const submitted = await savedAdmissions(fixture, original.prID)
    expect(submitted.bindings).toHaveLength(1)
    expect(submitted.bindings[0]!.state).toBe('bound')
    expect(submitted.threads).toHaveLength(1)
    expect(submitted.threads[0]!.answers[0]).toMatchObject({ answerID: original.answerID, threadID: original.threadID, question: original.question })
    expect(original.persistence).toBe('saved')
    expect(original.reference.anchor).toMatchObject({ kind: 'lines', fileID: diff.files.find((file): boolean => file.newPath === 'shared.txt')!.id, side: 'new', start: 1, end: 1 })
    await expect(page.locator('.answer-text')).toHaveText('Synthetic fixture ')
    expect((await savedAdmissions(fixture, original.prID)).threads[0]!.answers[0]).toMatchObject({ state: 'pending', text: '' })
    await expect(page.getByText('Answering', { exact: true })).toBeVisible()
    await expect(page.locator('[data-answer]')).toHaveCount(1)
    await expect(page.locator('[data-answer]')).toBeFocused()
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(1)
    const dispatch = JSON.parse((await prompts(fixture))[0]!.body!) as { text: string }
    for (const literal of [fixture.firstHead, fixture.mergeBase, 'before', 'after PR31', 'shared.txt', original.threadID, original.answerID]) expect(dispatch.text).toContain(literal)
    await page.screenshot({ path: info.outputPath('native-question-streaming.png') })

    const tab = await context.newPage()
    await track(tab)
    await tab.goto(await fixture.open('https://github.com/fixture/repo/pull/31'))
    await source(tab, 'Load pinned source')
    await tab.getByRole('button', { name: 'shared.txt', exact: true }).click()
    const rejected = await ask(tab, 'Retain this second-tab draft while the shared revision is busy.')
    expect(rejected.status()).toBe(409)
    await expect(tab.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Retain this second-tab draft while the shared revision is busy.')
    await expect(tab.locator('[data-answer]')).toHaveCount(0)
    expect((await prompts(fixture)).length).toBe(1)
    expect((await savedAdmissions(fixture, original.prID)).threads).toHaveLength(1)
    await tab.screenshot({ path: info.outputPath('native-question-busy.png') })

    const other = await context.newPage()
    await track(other)
    await other.goto(await fixture.open('https://github.com/fixture/repo/pull/32'))
    expect((await source(other, 'Load pinned source')).state).toBe('fallback-required')
    await source(other, 'Use Git-only fallback')
    await other.getByLabel('Fetched base branch', { exact: true }).selectOption('release/target')
    const otherSource = await source(other, 'Load selected base')
    expect(otherSource.diff!.revision.headSHA).toBe(fixture.secondHead)
    expect((await ask(other, 'Explain this separate PR. Synthetic inference fixture only.')).status()).toBe(202)
    await expect(other.locator('.answer-text')).toHaveText('Synthetic fixture ')
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(2)
    await fixture.answer(1, 'succeeded')
    await expect(page.getByText('Completed', { exact: true })).toBeVisible()
    await expect(page.locator('.answer-text')).toHaveText('Synthetic fixture answer <script>literal</script>.')
    await expect(other.getByText('Answering', { exact: true })).toBeVisible()
    const continued = await followup(page, 'Explain the original before behavior in this same thread.')
    expect(continued.threadID).toBe(original.threadID)
    expect(continued.answerID).not.toBe(original.answerID)
    expect(continued.revisionID).toBe(original.revisionID)
    expect((await savedAdmissions(fixture, original.prID)).threads[0]!.answers).toMatchObject([{ answerID: original.answerID, state: 'completed' }, { answerID: continued.answerID }])
    await expect(page.locator('.answer-text')).toHaveText(['Synthetic fixture answer <script>literal</script>.', 'Synthetic fixture '])
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(3)
    const stop = response(page, '/stop')
    await page.getByRole('button', { name: 'Stop this answer', exact: true }).click()
    expect((await stop).status()).toBe(202)
    await expect(page.getByText('Interrupted — confirmed', { exact: true })).toBeVisible()
    await expect(page.getByText('Stop requested. Termination is not yet confirmed.', { exact: true })).toHaveCount(0)
    await expect(other.getByText('Answering', { exact: true })).toBeVisible()
    const originalPath = (await prompts(fixture))[0]!.path!
    expect((await prompts(fixture))[2]!.path).toBe(originalPath)
    expect((await fixture.nativeAudit()).filter((entry): boolean => !!entry.path?.endsWith('/interrupt')).every((entry): boolean => entry.path === originalPath.replace('/prompt', '/interrupt'))).toBe(true)
    await fixture.answer(2, 'succeeded')
    await expect(other.getByText('Completed', { exact: true })).toBeVisible()

    const retry = await ask(tab, 'Retain this second-tab draft while the shared revision is busy.')
    expect(retry.status()).toBe(202)
    const separate = await retry.json() as QuestionAnswer
    expect(separate.threadID).not.toBe(original.threadID)
    expect(separate.revisionID).toBe(original.revisionID)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(4)
    await fixture.answer(4, 'failed')
    await expect(tab.getByText('Failed', { exact: true })).toBeVisible()
    await expect(tab.locator('.answer-text')).toHaveText('Synthetic fixture ')
    await expect(tab.locator('[data-answer]')).toHaveCount(1)
    await expect(tab.getByRole('form', { name: 'New question', exact: true })).toHaveCount(0)
    await tab.screenshot({ path: info.outputPath('native-question-failed.png') })
    const recovery = await followup(tab, 'Explicit follow-up after the synthetic failure.')
    expect(recovery.threadID).toBe(separate.threadID)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(5)
    await fixture.answer(5, 'succeeded')
    await expect(tab.getByText('Completed', { exact: true })).toBeVisible()
    await expect(tab.locator('.answer-text')).toHaveText(['Synthetic fixture ', 'Synthetic fixture answer <script>literal</script>.'])
    expect((await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && entry.path === '/api/session')).toHaveLength(2)
    for (const index of [2, 3, 4]) expect((await prompts(fixture))[index]!.path).toBe(originalPath)

    const native = await fixture.nativeAudit()
    expect(native.filter((entry): boolean => !!entry.args).every((entry): boolean => entry.restricted === true)).toBe(true)
    const locations = native.filter((entry): boolean => entry.method === 'POST' && entry.path === '/api/session').map((entry): string => (JSON.parse(entry.body!) as { location: { directory: string } }).location.directory)
    expect(locations).toContain(join(fixture.cache, 'worktrees', loaded.pr.id, fixture.firstHead))
    expect(locations).toContain(join(fixture.cache, 'worktrees', otherSource.pr.id, fixture.secondHead))
    expect((await fixture.audit()).every((entry): boolean => entry.Safe)).toBe(true)
    expect((await readdir(join(fixture.directory, 'tools'))).some((name): boolean => name === 'node' || name === 'npm')).toBe(false)
    for (const target of [page, tab, other]) {
      const html = await target.content()
      for (const secret of ['RAW PROVIDER SECRET', 'HIDDEN REASONING', 'RAW TOOL SECRET', 'FOREIGN TEXT', initialDiscovery.token]) expect(html.includes(secret)).toBe(false)
      expect(await target.locator('.question-conversation script').count()).toBe(0)
      expect(new URL(target.url()).hash).toBe('')
    }

    await context.grantPermissions(['clipboard-read', 'clipboard-write'], { origin: new URL(launch).origin })
    const code = page.locator('.line-source code').nth(1)
    const literal = await code.textContent()
    await code.evaluate((element): void => { const range = document.createRange(); range.selectNodeContents(element); const selection = window.getSelection(); selection?.removeAllRanges(); selection?.addRange(range) })
    await page.keyboard.press('Control+c')
    expect(await page.evaluate((): Promise<string> => navigator.clipboard.readText())).toBe(literal)
    const sourceRegion = page.getByRole('region', { name: 'Unified diff for shared.txt', exact: true })
    expect(await sourceRegion.evaluate((element): boolean => element.scrollWidth > element.clientWidth)).toBe(true)
    await sourceRegion.focus()
    await page.keyboard.press('PageDown')
    await expect.poll((): Promise<number> => page.locator('.source-pane').evaluate((element): number => element.scrollTop)).toBeGreaterThan(0)
    const expanded = (await page.locator('.workspace-files').boundingBox())!.width
    await page.locator('.file-disclosure > summary').click()
    expect((await page.locator('.workspace-files').boundingBox())!.width).toBeLessThan(expanded)
    await page.locator('.file-disclosure > summary').press('Enter')
    await page.getByRole('button', { name: 'remove.txt', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'remove.txt', exact: true })).toBeFocused()
    await expect(page.getByText('Interrupted — confirmed', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'shared.txt', exact: true }).click()
    await page.screenshot({ path: info.outputPath('native-question-completed-1440.png') })
    await page.setViewportSize({ width: 700, height: 900 })
    await page.getByRole('button', { name: 'Send follow-up', exact: true }).scrollIntoViewIfNeeded()
    await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeInViewport()
    expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath('native-question-700.png'), fullPage: true })
    await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
    await page.getByRole('button', { name: 'Send follow-up', exact: true }).scrollIntoViewIfNeeded()
    await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeInViewport()
    expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath('native-question-text-200.png'), fullPage: true })

    await followup(tab, 'Remain active until owned application shutdown.')
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(6)
    await expect(tab.getByText('Answering', { exact: true })).toBeVisible()
    const beforeRestart = await savedAdmissions(fixture, original.prID)
    expect(beforeRestart.threads).toHaveLength(2)
    expect(beforeRestart.threads.flatMap((thread): QuestionAnswer[] => thread.answers)).toHaveLength(5)
    await fixture.stop()
    const afterShutdown = await savedAdmissions(fixture, original.prID)
    expect(afterShutdown).toEqual(beforeRestart)
    expect(afterShutdown.threads.flatMap((thread): QuestionAnswer[] => thread.answers).at(-1)).toMatchObject({ state: 'pending', text: '' })
    for (const entry of native.filter((entry): boolean => !!entry.args)) {
      expect(alive(entry.pid!)).toBe(false)
      expect(await access(entry.directory!).then((): boolean => true).catch((): boolean => false)).toBe(false)
    }
    const restarted = await fixture.start('https://github.com/fixture/repo/pull/31')
    const nextDiscovery = await fixture.discovery()
    expect(nextDiscovery.token === initialDiscovery.token).toBe(false)
    const reopenedPage = await context.newPage()
    await track(reopenedPage)
    await reopenedPage.goto(restarted)
    const reopened = await source(reopenedPage, 'Load pinned source')
    expect(reopened.diff!.revisionID).toBe(original.revisionID)
    await expect(reopenedPage.locator('[data-answer]')).toHaveCount(0)
    expect((await prompts(fixture)).length).toBe(6)
    expect((await fixture.nativeAudit()).filter((entry): boolean => !!entry.args)).toHaveLength(native.filter((entry): boolean => !!entry.args).length)
    const missing = await reopenedPage.request.post(`${new URL(restarted).origin}/api/reviews/${original.prID}/threads/${original.threadID}/answers/${original.answerID}/events`, { headers: { Authorization: `Bearer ${nextDiscovery.token}` }, data: { revisionID: original.revisionID } })
    expect(missing.status()).toBe(503)
    expect(await savedAdmissions(fixture, original.prID)).toEqual(afterShutdown)
    await reopenedPage.getByRole('button', { name: 'shared.txt', exact: true }).click()
    const blocked = await ask(reopenedPage, 'Do not replace this revision binding after restart.')
    expect(blocked.status()).toBe(503)
    await expect(reopenedPage.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Do not replace this revision binding after restart.')
    await expect(reopenedPage.locator('[data-answer]')).toHaveCount(0)
    expect((await prompts(fixture)).length).toBe(6)
    expect((await fixture.nativeAudit()).filter((entry): boolean => !!entry.args)).toHaveLength(native.filter((entry): boolean => !!entry.args).length)
    expect(traffic.every((url): boolean => new URL(url).hostname === '127.0.0.1')).toBe(true)
    expect(traffic.some((url): boolean => url.includes(initialDiscovery.token) || url.includes(nextDiscovery.token))).toBe(false)
    expect(errors).toEqual([])
    await tab.close()
    await other.close()
  } finally { await fixture.close() }
}
