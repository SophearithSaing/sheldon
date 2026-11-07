import { expect, type BrowserContext, type Page, type Response, type TestInfo } from '@playwright/test'
import { access, readFile, rename } from 'node:fs/promises'
import { dirname, join } from 'node:path'
import { offlineCLI, type OfflineCLI, type NativeAudit } from '../release/offline'
import { preserveResponseBodies } from '../response-bodies'
import type { QuizRead, QuizStatus, QuizChecked, QuizEvidence } from '../../src/runtime/quiz'
import type { SourceResponse } from '../../src/runtime/source'
import type { QuestionAnswer } from '../../src/runtime/questions'

interface PrivateQuiz { schemaVersion: number; prID: string; version: number; quizID: string; revisionID: string; questions: { questionID: string; correctChoiceID: string; explanation: string; evidence: QuizEvidence[] }[] }

/**
 * Waits for an actual finite application response without intercepting requests.
 * @param page Actual browser page.
 * @param method Request method.
 * @param suffix Exact endpoint suffix.
 * @returns Unmodified application response.
 */
function response(page: Page, method: string, suffix: string): Promise<Response> {
  return page.waitForResponse((result: Response): boolean => result.request().method() === method && new URL(result.url()).pathname.endsWith(suffix), { timeout: 15000 })
}

/**
 * Reads only synthetic native prompt dispatch audits, excluding observation.
 * @param fixture Owned offline controller.
 * @returns Actual native fixture dispatches.
 */
async function prompts(fixture: OfflineCLI): Promise<NativeAudit[]> { return (await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && !!entry.path?.endsWith('/prompt')) }

/**
 * Opens the real Quiz view and inspects its unanswered projection.
 * @param page Connected application page.
 * @returns Actual saved public set and runtime feedback.
 */
async function enter(page: Page): Promise<QuizRead> {
  const received = response(page, 'GET', '/quiz')
  await page.getByRole('button', { name: 'Quiz', exact: true }).click()
  const result = await received
  expect(result.status()).toBe(200)
  expect(result.headers()['cache-control']).toBe('no-store')
  const data = await result.json() as QuizRead
  for (const key of ['correctChoiceID', 'explanation', 'evidence', 'surrounding', 'schemaVersion', 'sessionID', 'directory', 'model']) expect(JSON.stringify(data)).not.toContain(`"${key}"`)
  return data
}

/**
 * Explicitly starts generation or confirms replacement of an existing saved set.
 * @param page Real Quiz page.
 * @param replacement Whether a current saved set exists.
 * @returns Actual Pending admission, not saved completion.
 */
async function generate(page: Page, replacement: boolean): Promise<QuizStatus> {
  if (replacement) await page.getByRole('button', { name: 'Regenerate quiz', exact: true }).click()
  const received = response(page, 'POST', '/quiz/generations')
  await page.getByRole('button', { name: replacement ? 'Generate replacement' : 'Generate quiz', exact: true }).click()
  const result = await received
  expect(result.status()).toBe(202)
  const status = await result.json() as QuizStatus
  expect(status.state).toBe('pending')
  expect(status.savedVersion).toBeUndefined()
  return status
}

/**
 * Checks an explicit native-radio choice without another inference request.
 * @param page Current Quiz page.
 * @param question Scoped question number.
 * @param choice Explicit alternative index.
 * @returns Actual single-question keyed response.
 */
async function check(page: Page, question: number, choice: number): Promise<QuizChecked> {
  await page.getByRole('button', { name: new RegExp(`^Quiz question ${question},`) }).click()
  await page.getByRole('radio').nth(choice).check()
  const received = response(page, 'POST', `/questions/q${question}/check`)
  await page.getByRole('button', { name: 'Check answer', exact: true }).click()
  const result = await received
  expect(result.status()).toBe(200)
  const value = await result.json() as QuizChecked
  expect(value.questionID).toBe(`q${question}`)
  expect(value.choiceID).toBe('abcd'[choice])
  await expect(page.getByRole('heading', { name: 'Answer explanation', exact: true })).toBeVisible()
  return value
}

/**
 * Explicitly verifies the existing connection after a document reload.
 * @param page Reloaded application page.
 * @param token Test-owned credential, never put in request URLs or artifacts.
 */
async function connect(page: Page, token: string): Promise<void> {
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Quiz', exact: true })).toBeVisible()
}

/**
 * Probes only a child PID recorded by the disposable native fixture.
 * @param pid Owned child identity.
 * @returns Whether the owned process is still alive.
 */
function alive(pid: number): boolean { try { process.kill(pid, 0); return true } catch { return false } }

/**
 * Exercises browser → actual Git/API/storage → synthetic native quiz, without application HTTP mocks.
 * @param page Initial real PR page.
 * @param context Browser context for a sibling tab.
 * @param info Owned artifact destinations.
 * @param development Whether to use the asset-free CLI/protected Vite proxy instead of embedded assets.
 */
export async function quizFlow(page: Page, context: BrowserContext, info: TestInfo, development: boolean): Promise<void> {
  const fixture = await offlineCLI({ development, questions: true, quizzes: true })
  const traffic: string[] = []
  const errors: string[] = []
  await preserveResponseBodies(page)
  page.on('request', (request): void => { traffic.push(request.url()) })
  page.on('pageerror', (error: Error): void => { errors.push(error.message) })
  try {
    const launch = await fixture.start('https://github.com/fixture/repo/pull/31')
    for (const unavailable of [join(fixture.directory, 'refs'), join(fixture.directory, 'tools', 'node')]) expect(await access(unavailable).then((): boolean => true).catch((): boolean => false)).toBe(false)
    const initial = await fixture.discovery()
    await page.goto(launch)
    const empty = await enter(page)
    expect(empty).toMatchObject({ version: '0', quiz: null, generation: null })
    expect(await fixture.nativeAudit()).toHaveLength(0)
    expect(await fixture.audit()).toHaveLength(0)
    await page.getByRole('button', { name: 'Files changed', exact: true }).click()
    const sourceResponse = response(page, 'POST', '/source')
    await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
    const source = await (await sourceResponse).json() as SourceResponse
    const diff = source.diff!
    expect(diff.revision.headSHA).toBe(fixture.firstHead)
    expect(diff.revision.mergeBaseSHA).toBe(fixture.mergeBase)
    await page.getByRole('button', { name: 'shared.txt', exact: true }).click()
    await page.getByRole('button', { name: 'Select head line 1', exact: true }).click()
    await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
    await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Separate question context; offline synthetic inference only.')
    const asked = response(page, 'POST', '/questions')
    await page.getByRole('button', { name: 'Send question', exact: true }).click()
    const question = await (await asked).json() as QuestionAnswer
    await expect(page.getByText('Current answer saved locally.', { exact: true })).toBeVisible()
    const reviewDirectory = join(dirname(fixture.stateFile), 'reviews', question.prID)
    const threadPath = join(reviewDirectory, 'threads.json')
    const quizPath = join(reviewDirectory, 'quiz.json')
    const threads = await readFile(threadPath)
    await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
    const nativeBefore = (await fixture.nativeAudit()).length
    const gitBefore = (await fixture.audit()).length
    await enter(page)
    expect((await fixture.nativeAudit()).length).toBe(nativeBefore)
    expect((await fixture.audit()).length).toBe(gitBefore)
    const first = await generate(page, false)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(2)
    expect(await access(quizPath).then((): boolean => true).catch((): boolean => false)).toBe(false)
    const dispatches = await prompts(fixture)
    expect(dispatches[0]!.pid).not.toBe(dispatches[1]!.pid)
    const nativeQuiz = JSON.parse(dispatches[1]!.body!) as { text: string }
    for (const literal of [fixture.firstHead, fixture.mergeBase]) expect(nativeQuiz.text).toContain(literal)
    const compact = JSON.parse(nativeQuiz.text) as Record<string, unknown>
    expect(Object.keys(compact).sort()).toEqual(['instructions', 'prURL', 'revision'])
    expect(compact.revision).toEqual(diff.revision)
    expect(compact.prURL).toBe('https://github.com/fixture/repo/pull/31')
    expect(nativeQuiz.text.length).toBeLessThan(16384)
    expect(nativeQuiz.text).not.toContain('after PR31')
    const investigation = (await fixture.nativeAudit()).filter((entry): boolean => !!entry.semanticGit && entry.pid === dispatches[1]!.pid)
    expect(investigation.length).toBeGreaterThanOrEqual(3)
    for (const entry of investigation) { expect(entry.semanticGit).toContain(fixture.mergeBase); expect(entry.semanticGit).toContain(fixture.firstHead) }
    const sessions = (await fixture.nativeAudit()).filter((entry): boolean => entry.method === 'POST' && entry.path === '/api/session')
    for (const entry of sessions) {
      const permissions = JSON.parse(entry.body!).permissions as { action: string; effect: string }[]
      expect(permissions.some((rule): boolean => rule.action === 'shell' && rule.effect === 'allow')).toBe(entry.pid === dispatches[1]!.pid)
    }
    expect(nativeQuiz.text).not.toContain(question.threadID)
    const tab = await context.newPage()
    await preserveResponseBodies(tab)
    await tab.goto(await fixture.open('https://github.com/fixture/repo/pull/31'))
    expect((await enter(tab)).generation?.generationID).toBe(first.generationID)
    const busy = await tab.request.post(`${new URL(launch).origin}/api/reviews/${question.prID}/quiz/generations`, { headers: { Authorization: `Bearer ${initial.token}` }, data: { revision: diff.revision, expectedVersion: '0' } })
    expect(busy.status()).toBe(409)
    expect((await prompts(fixture)).length).toBe(2)
    await fixture.answer(1, 'succeeded')
    await expect(page.locator('.quiz-revision')).toContainText('Saved version 1')
    const bytes = await readFile(quizPath)
    const saved = JSON.parse(bytes.toString()) as PrivateQuiz
    expect(saved).toMatchObject({ schemaVersion: 1, version: 1, quizID: first.quizID, revisionID: diff.revisionID })
    expect(saved.questions).toHaveLength(5)
    expect(await readFile(threadPath)).toEqual(threads)
    const stableNative = (await fixture.nativeAudit()).length
    const stableGit = (await fixture.audit()).length
    const reread = response(page, 'GET', '/quiz')
    await page.getByRole('button', { name: 'Refresh saved quiz', exact: true }).click()
    const publicRead = await (await reread).json() as QuizRead
    for (const key of ['correctChoiceID', 'explanation', 'evidence', 'schemaVersion', 'sessionID', 'directory']) expect(JSON.stringify(publicRead)).not.toContain(`"${key}"`)
    const wrong = await check(page, 1, 0)
    expect(wrong).toMatchObject({ quizID: saved.quizID, version: '1', correct: false, correctChoiceID: saved.questions[0]!.correctChoiceID, explanation: saved.questions[0]!.explanation })
    expect(wrong.evidence).toEqual(saved.questions[0]!.evidence)
    await expect(page.getByRole('radio').first()).toBeChecked()
    expect((await check(page, 2, 1)).correct).toBe(true)
    await page.getByRole('button', { name: /^Quiz question 1,/ }).click()
    await expect(page.getByRole('radio').first()).toBeChecked()
    await page.screenshot({ path: info.outputPath('native-quiz-checked-1440.png'), fullPage: true })
    await page.setViewportSize({ width: 700, height: 1000 })
    await page.getByRole('heading', { name: 'Answer explanation', exact: true }).scrollIntoViewIfNeeded()
    await page.screenshot({ path: info.outputPath('native-quiz-700.png'), fullPage: true })
    await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
    await page.getByRole('button', { name: 'Next question', exact: true }).scrollIntoViewIfNeeded()
    await expect(page.getByRole('button', { name: 'Next question', exact: true })).toBeInViewport()
    expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.screenshot({ path: info.outputPath('native-quiz-200-text.png'), fullPage: true })
    await page.evaluate((): void => { document.documentElement.style.fontSize = '100%' })
    await page.setViewportSize({ width: 1440, height: 1080 })
    await page.getByRole('button', { name: 'View in original diff', exact: true }).click()
    await expect(page.getByRole('heading', { name: wrong.evidence[0]!.newPath ?? wrong.evidence[0]!.oldPath!, exact: true })).toBeFocused()
    expect((await enter(page)).quiz?.quizID).toBe(saved.quizID)
    await expect(page.getByRole('radio').first()).toBeChecked()
    expect((await fixture.nativeAudit()).length).toBe(stableNative)
    expect((await fixture.audit()).length).toBe(stableGit)
    expect(await readFile(quizPath)).toEqual(bytes)
    await tab.close()
    await page.reload()
    await connect(page, initial.token)
    expect((await enter(page)).quiz?.quizID).toBe(saved.quizID)
    await expect(page.getByText('0 of 5 checked in this page', { exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Before you check', exact: true })).toBeVisible()
    expect((await fixture.nativeAudit()).length).toBe(stableNative)
    expect((await fixture.audit()).length).toBe(stableGit)
    await page.getByRole('button', { name: 'Files changed', exact: true }).click()
    const reloadSource = response(page, 'POST', '/source')
    await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
    await reloadSource
    await enter(page)
    await page.getByRole('radio').nth(2).check()
    await generate(page, true)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(3)
    await fixture.answer(2, 'failed')
    await expect(page.getByText('Generation: failed', { exact: true })).toBeVisible()
    await expect(page.getByRole('radio').nth(2)).toBeChecked()
    expect(await readFile(quizPath)).toEqual(bytes)
    await generate(page, true)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(4)
    await page.getByRole('button', { name: 'Cancel generation', exact: true }).click()
    await expect(page.getByText('Generation cancelled. The previously saved set is retained.', { exact: true })).toBeVisible()
    expect(await readFile(quizPath)).toEqual(bytes)
    await generate(page, true)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(5)
    await fixture.answer(4, 'succeeded')
    await expect(page.locator('.quiz-revision')).toContainText('Saved version 2')
    const replacementBytes = await readFile(quizPath)
    const replacement = JSON.parse(replacementBytes.toString()) as PrivateQuiz
    expect(replacement.quizID).not.toBe(saved.quizID)
    const stale = await page.request.post(`${new URL(launch).origin}/api/reviews/${question.prID}/quiz/${saved.quizID}/questions/q1/check`, { headers: { Authorization: `Bearer ${initial.token}` }, data: { revisionID: saved.revisionID, choiceID: 'b' } })
    expect(stale.status()).toBe(409)
    expect((await prompts(fixture)).length).toBe(5)
    expect(await readFile(threadPath)).toEqual(threads)
    await generate(page, true)
    await expect.poll(async (): Promise<number> => (await prompts(fixture)).length).toBe(6)
    const native = await fixture.nativeAudit()
    const services = native.filter((entry): boolean => !!entry.args?.includes('serve'))
    expect(services).toHaveLength(2)
    expect(services.every((entry): boolean => entry.restricted === true)).toBe(true)
    await fixture.stop()
    expect(await readFile(quizPath)).toEqual(replacementBytes)
    expect(await readFile(threadPath)).toEqual(threads)
    for (const service of services) {
      expect(alive(service.pid!)).toBe(false)
      expect(await access(service.directory!).then((): boolean => true).catch((): boolean => false)).toBe(false)
    }
    await rename(fixture.cache, fixture.cache + '.unavailable')
    await rename(join(fixture.directory, 'tools', 'opencode'), join(fixture.directory, 'tools', 'opencode.unavailable'))
    const beforeRestartNative = (await fixture.nativeAudit()).length
    const beforeRestartGit = (await fixture.audit()).length
    const restarted = await fixture.start('https://github.com/fixture/repo/pull/31')
    const next = await fixture.discovery()
    expect(next.token === initial.token).toBe(false)
    await page.goto('about:blank')
    await page.goto(restarted)
    const reopened = await enter(page)
    expect(reopened).toMatchObject({ version: '2', quiz: { quizID: replacement.quizID, revisionID: saved.revisionID }, generation: null })
    await expect(page.getByRole('button', { name: 'Regenerate quiz', exact: true })).toBeDisabled()
    expect((await check(page, 1, 1)).correct).toBe(true)
    await page.getByRole('button', { name: 'Questions', exact: true }).click()
    await page.getByRole('button', { name: `Open thread: ${question.question}`, exact: true }).click()
    await expect(page.locator('.answer-text')).toHaveText('Synthetic fixture answer <script>literal</script>.')
    expect((await fixture.nativeAudit()).length).toBe(beforeRestartNative)
    expect((await fixture.audit()).length).toBe(beforeRestartGit)
    expect(await readFile(quizPath)).toEqual(replacementBytes)
    expect(await readFile(threadPath)).toEqual(threads)
    const visible = await page.locator('body').innerText()
    for (const secret of [initial.token, next.token, fixture.directory, 'DO NOT DISPLAY HIDDEN REASONING', 'DO NOT DISPLAY FOREIGN TEXT', 'RAW TOOL SECRET']) expect(visible.includes(secret) || JSON.stringify(publicRead).includes(secret) || JSON.stringify(reopened).includes(secret)).toBe(false)
    expect(traffic.every((url): boolean => new URL(url).hostname === '127.0.0.1')).toBe(true)
    expect(traffic.some((url): boolean => url.includes(initial.token) || url.includes(next.token))).toBe(false)
    expect(errors).toEqual([])
  } finally { await fixture.close() }
}
