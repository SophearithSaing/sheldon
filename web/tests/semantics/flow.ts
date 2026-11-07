import { expect, type BrowserContext, type Page, type Response, type TestInfo } from '@playwright/test'
import { readFile, rename } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { offlineCLI, type OfflineCLI, type NativeAudit } from '../release/offline'
import { preserveResponseBodies } from '../response-bodies'
import type { SemanticRead, SemanticStatus } from '../../src/runtime/semantic'
import type { SourceResponse } from '../../src/runtime/source'
import type { QuestionAnswer } from '../../src/runtime/questions'

/** Waits for an unmodified actual application response; it installs no request interceptor.
 * @param page Real application browser.
 * @param method Request method.
 * @param suffix Exact endpoint suffix.
 * @returns Actual response.
 */
function response(page: Page, method: string, suffix: string): Promise<Response> { return page.waitForResponse((r: Response): boolean => r.request().method() === method && new URL(r.url()).pathname.endsWith(suffix), { timeout: 15000 }) }

/** Reads only actual semantic prompt dispatches from synthetic native audits.
 * @param fixture Owned offline controller.
 * @returns Semantic dispatches, not status reads.
 */
async function prompts(fixture: OfflineCLI): Promise<NativeAudit[]> { return (await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && !!entry.path?.endsWith('/prompt') && !!entry.body?.includes('purpose-based groups')) }

/** Opens saved semantic state without starting generation.
 * @param page Connected browser.
 * @returns Actual public saved state.
 */
async function enter(page: Page): Promise<SemanticRead> {
  const received = response(page, 'GET', '/semantic-review'); await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  const result = await received; expect(result.status()).toBe(200); expect(result.headers()['cache-control']).toBe('no-store'); return await result.json() as SemanticRead
}

/** Starts one explicit semantic request or confirmed replacement.
 * @param page Actual semantic view.
 * @param replacement Whether a prior complete result exists.
 * @returns Pending application admission.
 */
async function generate(page: Page, replacement: boolean): Promise<SemanticStatus> {
  if (replacement) await page.getByRole('button', { name: 'Regenerate semantic review', exact: true }).click()
  const received = response(page, 'POST', '/semantic-review/generations'); await page.getByRole('button', { name: replacement ? 'Confirm regenerate review' : 'Generate semantic review', exact: true }).click()
  const res = await received; expect(res.status()).toBe(202); const status = await res.json() as SemanticStatus; expect(status.state).toBe('pending'); return status
}

/** Exercises real browser/Git/API/storage with explicitly synthetic native inference only.
 * @param page Actual application browser.
 * @param context Context for a competing browser tab.
 * @param info Artifact destinations.
 * @param development Whether the asset-free CLI uses the protected Vite proxy.
 */
export async function semanticFlow(page: Page, context: BrowserContext, info: TestInfo, development: boolean): Promise<void> {
  const fixture = await offlineCLI({ development, questions: true, semantics: true })
  const errors: string[] = []; page.on('pageerror', (error: Error): void => { errors.push(error.message) }); await preserveResponseBodies(page)
  try {
    const launch = await fixture.start('https://github.com/fixture/repo/pull/31'); const discovery = await fixture.discovery(); await page.goto(launch)
    expect(await enter(page)).toMatchObject({ version: '0', review: null, generation: null }); expect(await fixture.nativeAudit()).toHaveLength(0); expect(await fixture.audit()).toHaveLength(0)
    await page.getByRole('button', { name: 'Files changed', exact: true }).click(); const sourceResponse = response(page, 'POST', '/source'); await page.getByRole('button', { name: 'Load pinned source', exact: true }).click(); const source = await (await sourceResponse).json() as SourceResponse; const diff = source.diff!
    await page.getByRole('button', { name: 'shared.txt', exact: true }).click(); await page.getByRole('button', { name: 'Select head line 1', exact: true }).click(); await page.getByRole('button', { name: 'Ask about selection', exact: true }).click(); await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Original separate question context; synthetic inference only.')
    const asked = response(page, 'POST', '/questions'); await page.getByRole('button', { name: 'Send question', exact: true }).click(); const question = await (await asked).json() as QuestionAnswer; await expect(page.getByText('Current answer saved locally.', { exact: true })).toBeVisible(); await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
    const base = `${new URL(launch).origin}/api/reviews/${question.prID}`; const headers = { Authorization: `Bearer ${discovery.token}` }
    const quizAdmission = await page.request.post(base + '/quiz/generations', { headers, data: { revision: diff.revision, expectedVersion: '0' } }); expect(quizAdmission.status()).toBe(202)
    await expect.poll(async (): Promise<string> => { const r = await page.request.get(base + '/quiz', { headers }); return String((await r.json()).version) }).toBe('1')
    const directory = join(dirname(fixture.stateFile), 'reviews', question.prID); const threadsPath = join(directory, 'threads.json'); const quizPath = join(directory, 'quiz.json'); const semanticPath = join(directory, 'semantic-review.json')
    let threadBytes = await readFile(threadsPath); const quizBytes = await readFile(quizPath)
    const nativeBefore = (await fixture.nativeAudit()).length; const gitBefore = (await fixture.audit()).length; await enter(page); expect((await fixture.nativeAudit()).length).toBe(nativeBefore); expect((await fixture.audit()).length).toBe(gitBefore)
    const first = await generate(page, false); await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(1)
    const dispatched = (await prompts(fixture))[0]!
    const prompt = JSON.parse(dispatched.body!).text as string
    const instructions = JSON.parse(prompt) as Record<string, unknown>
    expect(Object.keys(instructions).sort()).toEqual(['instructions', 'prURL', 'revision'])
    expect(instructions.revision).toEqual(diff.revision)
    expect(instructions.prURL).toBe('https://github.com/fixture/repo/pull/31')
    expect(prompt.length).toBeLessThan(16384)
    const investigation = (await fixture.nativeAudit()).filter((entry): boolean => !!entry.semanticGit && entry.pid === dispatched.pid)
    expect(investigation.length).toBeGreaterThanOrEqual(3)
    for (const entry of investigation) { expect(entry.pid).toBe(dispatched.pid); expect(entry.semanticGit).toContain(diff.revision.mergeBaseSHA); expect(entry.semanticGit).toContain(diff.revision.headSHA) }
    const sessionCreates = (await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && entry.path === '/api/session')
    const initialQuestion = (await fixture.nativeAudit()).find((entry): boolean => entry.method === 'POST' && !!entry.path?.endsWith('/prompt'))!
    for (const entry of sessionCreates) {
      const permissions = JSON.parse(entry.body!).permissions as { action: string; effect: string }[]
      expect(permissions.some((rule): boolean => rule.action === 'shell' && rule.effect === 'allow')).toBe(entry.pid !== initialQuestion.pid)
    }
    const tab = await context.newPage(); await preserveResponseBodies(tab); await tab.goto(await fixture.open('https://github.com/fixture/repo/pull/31')); expect((await enter(tab)).generation?.generationID).toBe(first.generationID)
    const busy = await tab.request.post(base + '/semantic-review/generations', { headers, data: { revision: diff.revision, expectedVersion: '0' } }); expect(busy.status()).toBe(409); expect(await prompts(fixture)).toHaveLength(1)
    const otherURL = await fixture.open('https://github.com/fixture/repo/pull/32'); const otherID = new URL(otherURL).pathname.split('/').at(-1)!; const other = await tab.request.get(`${new URL(launch).origin}/api/reviews/${otherID}/semantic-review`, { headers }); expect(await other.json()).toMatchObject({ version: '0', review: null })
    await fixture.answer(1, 'succeeded'); await expect(page.locator('.semantic-header')).toContainText('saved version 1')
    const savedBytes = await readFile(semanticPath); const publicResponse = await page.request.get(base + '/semantic-review', { headers }); const read = await publicResponse.json() as SemanticRead
    expect(read.review!.reviewID).toBe(first.reviewID); expect(read.review!.diff.revisionID).toBe(diff.revisionID); expect(read.review!.groups[0]!.ranges).toHaveLength(2); expect(new Set(read.review!.groups[0]!.ranges.map((a): string => a.fileID)).size).toBe(2)
    const dispatches = (await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && !!entry.path?.endsWith('/prompt')); expect(new Set(dispatches.map((entry): number | undefined => entry.pid)).size).toBe(3)
    for (const secret of [fixture.cache, fixture.directory, discovery.token, 'ses_fixture', 'HIDDEN', 'RAW PROVIDER', 'schemaVersion']) expect(JSON.stringify(read)).not.toContain(secret)
    expect(await readFile(threadsPath)).toEqual(threadBytes); expect(await readFile(quizPath)).toEqual(quizBytes)
    await expect(page.locator('.semantic-excerpt')).toHaveCount(2)
    const changedLines = diff.files.reduce((sum, file): number => sum + file.hunks.reduce((n, hunk): number => n + hunk.lines.filter((line): boolean => line.sign !== ' ').length, 0), 0)
    await page.getByRole('button', { name: 'Ungrouped changes', exact: false }).click(); await expect(page.locator('.semantic-excerpt')).toHaveCount(changedLines - 2)
    await page.getByRole('button', { name: 'Synthetic related change', exact: false }).click()
    for (const [width, scale, name] of [[1440, 1, 'native-semantic-1440.png'], [700, 1, 'native-semantic-700.png'], [1440, 2, 'native-semantic-text-200.png']] as const) {
      await page.setViewportSize({ width, height: 1080 }); await page.evaluate((n: number): void => { document.documentElement.style.fontSize = `${n * 16}px` }, scale); await page.locator('.semantic-excerpt').first().scrollIntoViewIfNeeded(); await page.screenshot({ path: info.outputPath(name) }); expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    }
    await page.setViewportSize({ width: 1440, height: 1080 }); await page.evaluate((): void => { document.documentElement.style.fontSize = '' })
    await page.locator('.semantic-excerpt').first().getByRole('button', { name: 'Ask about selected range', exact: true }).click(); await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Explain the grouped original selection.')
    const selectedQuestion = response(page, 'POST', '/questions'); await page.getByRole('button', { name: 'Send question', exact: true }).click(); expect((await selectedQuestion).status()).toBe(202); await expect(page.getByText('Current answer saved locally.', { exact: true })).toBeVisible(); threadBytes = await readFile(threadsPath)
    expect((await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && entry.path === '/api/session')).toHaveLength(3)
    await page.getByRole('button', { name: 'Close conversation', exact: true }).click(); await enter(page)
    const failed = await generate(page, true); await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(2); await fixture.answer(2, 'failed'); await expect(page.locator('.semantic-header')).toContainText('Generation: failed'); expect(await readFile(semanticPath)).toEqual(savedBytes)
    expect(failed.reviewID).not.toBe(first.reviewID)
    await generate(page, true); await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(3); await page.getByRole('button', { name: 'Cancel generation', exact: true }).click(); await expect(page.locator('.semantic-header')).toContainText('Generation: cancelled'); expect(await readFile(semanticPath)).toEqual(savedBytes)
    const replacement = await generate(page, true); await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(4); await fixture.answer(4, 'succeeded'); await expect(page.locator('.semantic-header')).toContainText('saved version 2'); const finalBytes = await readFile(semanticPath); expect(replacement.reviewID).not.toBe(first.reviewID)
    await generate(page, true); await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(5); await fixture.stop(); expect(await readFile(semanticPath)).toEqual(finalBytes); expect(await readFile(threadsPath)).toEqual(threadBytes); expect(await readFile(quizPath)).toEqual(quizBytes)
    await rename(fixture.cache, fixture.cache + '.unavailable'); await rename(join(fixture.directory, 'tools', 'opencode'), join(fixture.directory, 'tools', 'opencode.unavailable'))
    const restarted = await fixture.start('https://github.com/fixture/repo/pull/31'); const fresh = await fixture.discovery(); expect(fresh.token).not.toBe(discovery.token); const nativeStable = (await fixture.nativeAudit()).length; const gitStable = (await fixture.audit()).length
    const reopened = await context.newPage(); await preserveResponseBodies(reopened); await reopened.goto(restarted); expect(await enter(reopened)).toMatchObject({ version: '2', generation: null, review: { reviewID: replacement.reviewID } }); await expect(reopened.getByRole('heading', { name: 'Synthetic related change', exact: true })).toBeVisible()
    await expect(reopened.locator('.semantic-excerpt').first().getByRole('button', { name: 'Ask about selected range', exact: true })).toHaveAttribute('aria-disabled', 'true'); expect((await fixture.nativeAudit()).length).toBe(nativeStable); expect((await fixture.audit()).length).toBe(gitStable)
    expect(await readFile(semanticPath)).toEqual(finalBytes); expect(await readFile(threadsPath)).toEqual(threadBytes); expect(await readFile(quizPath)).toEqual(quizBytes); expect(errors).toEqual([])
  } finally { await fixture.close() }
}
