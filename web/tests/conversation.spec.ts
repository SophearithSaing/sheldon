import { expect, test, type Page } from '@playwright/test'
import type { QuestionAnswer } from '../src/runtime/questions'
import type { SourceResponse } from '../src/runtime/source'
import { digest, registered, sourceFixture, token } from './runtime-fixture'

test.use({ baseURL: 'http://127.0.0.1:5175' })

/**
 * Holds explicitly mocked fetch streams open so browser tests control native-looking snapshots.
 * @param page Browser page using only renderer fixtures.
 */
async function installStreams(page: Page): Promise<void> {
  await page.addInitScript((): void => {
    const streams: Record<string, ReadableStreamDefaultController<Uint8Array>> = {}
    ;(window as unknown as { fixtureStreams: typeof streams }).fixtureStreams = streams
    const original = window.fetch.bind(window)
    window.fetch = async (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const response = await original(input, init)
      const url = new URL(input instanceof Request ? input.url : String(input), location.href)
      const id = /\/answers\/([a-f0-9]{64})\/events$/.exec(url.pathname)?.[1]
      if (!id || response.headers.get('X-Sheldon-Stream-Fixture') !== 'yes') return response
      await response.body?.cancel()
      const signal = init?.signal
      const body = new ReadableStream<Uint8Array>({
        start(controller): void {
          streams[id] = controller
          signal?.addEventListener('abort', (): void => {
            if ((window as unknown as { fixtureIgnoreAbort?: boolean }).fixtureIgnoreAbort) return
            if (streams[id] === controller) { delete streams[id]; controller.error(new Error('Fixture observation aborted.')) }
          }, { once: true })
        },
        cancel(): void { delete streams[id] },
      })
      return new Response(body, { status: 200, headers: { 'Content-Type': 'text/event-stream' } })
    }
  })
}

/**
 * Creates registered source and question routes whose replies are labeled synthetic renderer data.
 * @param page Browser page.
 * @param number Distinct offline fixture PR number.
 * @returns Source descriptors and admitted fixture answers.
 */
async function fixture(page: Page, number: number): Promise<{ source: SourceResponse; answers: QuestionAnswer[]; followups: unknown[] }> {
  await installStreams(page)
  const pr = await registered(page, number)
  const source = sourceFixture(pr, 2)
  const answers: QuestionAnswer[] = []
  const followups: unknown[] = []
  await page.route(`**/api/reviews/${pr.id}/source`, async (route): Promise<void> => { await route.fulfill({ json: source }) })
  await page.route(`**/api/reviews/${pr.id}/questions`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    const answer: QuestionAnswer = { answerID: digest(`answer-${number}-${answers.length}`), threadID: digest(`thread-${number}-${answers.length}`), prID: pr.id, revisionID: source.diff!.revisionID, question: input.question, text: '', state: 'pending', stopRequested: false, executionUnknown: false, reference: { revisionID: source.diff!.revisionID, anchor: input.anchor } }
    answers.push(answer)
    await route.fulfill({ status: 202, json: answer })
  })
  await page.route(`**/api/reviews/${pr.id}/threads/*/answers`, async (route): Promise<void> => {
    const input = route.request().postDataJSON()
    followups.push(input)
    const threadID = new URL(route.request().url()).pathname.split('/')[5]!
    const previous = answers.find((answer): boolean => answer.threadID === threadID)!
    const answer: QuestionAnswer = { ...previous, answerID: digest(`followup-${number}-${answers.length}`), question: input.question }
    answers.push(answer)
    await route.fulfill({ status: 202, json: answer })
  })
  await page.route(`**/api/reviews/${pr.id}/threads/*/answers/*/events`, async (route): Promise<void> => {
    expect(route.request().method()).toBe('POST')
    expect(route.request().headers()['authorization']).toBe(`Bearer ${await token()}`)
    expect(route.request().postDataJSON()).toEqual({ revisionID: source.diff!.revisionID })
    await route.fulfill({ headers: { 'Content-Type': 'text/event-stream', 'X-Sheldon-Stream-Fixture': 'yes' }, body: ': synthetic browser stream\n\n' })
  })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  return { source, answers, followups }
}

/**
 * Pushes fragmented UTF-8 fixture snapshots through the real client SSE parser.
 * @param page Browser page.
 * @param answer Original routed answer.
 * @param changes Explicit synthetic snapshot changes.
 * @param close Whether to end observation after this frame.
 */
async function snapshot(page: Page, answer: QuestionAnswer, changes: Partial<QuestionAnswer>, close = false): Promise<void> {
  await expect.poll((): Promise<boolean> => page.evaluate((id: string): boolean => !!(window as unknown as { fixtureStreams: Record<string, unknown> }).fixtureStreams[id], answer.answerID)).toBe(true)
  await page.evaluate(({ id, value, close }): void => {
    const stream = (window as unknown as { fixtureStreams: Record<string, ReadableStreamDefaultController<Uint8Array>> }).fixtureStreams[id]!
    const bytes = new TextEncoder().encode(`event: answer\r\ndata: ${JSON.stringify(value)}\r\n\r\n`)
    for (let offset = 0; offset < bytes.length; offset += 7) stream.enqueue(bytes.slice(offset, offset + 7))
    if (close) { stream.close(); delete (window as unknown as { fixtureStreams: Record<string, unknown> }).fixtureStreams[id] }
  }, { id: answer.answerID, value: { ...answer, ...changes }, close })
}

/**
 * Submits explicit line-scoped fixture input through the real inline composer.
 * @param page Browser page.
 * @param line Selected head coordinate.
 * @param question Literal fixture question.
 */
async function ask(page: Page, line: number, question: string): Promise<void> {
  await page.getByRole('button', { name: `Select head line ${line}`, exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill(question)
  await page.getByRole('button', { name: 'Send question', exact: true }).click()
  await expect(page.getByRole('form', { name: 'New question', exact: true })).toHaveCount(0)
}

/**
 * Explicitly reconnects the same renderer without reloading or recovering lost question state.
 * @param page Disconnected fixture page.
 */
async function reconnect(page: Page): Promise<void> {
  await page.getByLabel('API token', { exact: true }).fill(await token())
  await page.getByRole('button', { name: 'Connect backend', exact: true }).click()
  await expect(page.getByText('Previously accepted questions may still be running', { exact: false })).toBeVisible()
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/file0000.ts', exact: true })).toBeVisible()
}

test('real streamed snapshots stay escaped and do not steal source selection, focus or drafts', async ({ page }, testInfo): Promise<void> => {
  const f = await fixture(page, 720)
  await ask(page, 2, 'Synthetic first question.')
  const answer = f.answers[0]!
  const panel = page.getByRole('region', { name: 'Question conversation', exact: true })
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic é <script>literal</script>.' })
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic é <script>literal</script>.' })
  await expect(panel.locator('.answer-text')).toHaveText('Synthetic é <script>literal</script>.')
  await expect(panel.locator('script')).toHaveCount(0)
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Keep this draft')
  await draft.press('Shift+Enter')
  await expect(draft).toHaveValue('Keep this draft\n')
  await draft.dispatchEvent('keydown', { key: 'Enter', isComposing: true })
  await draft.press('Enter')
  expect(f.followups).toHaveLength(0)
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'src/file0001.ts', exact: true }).click()
  const heading = page.getByRole('heading', { name: 'src/file0001.ts', exact: true })
  await expect(heading).toBeFocused()
  await snapshot(page, answer, { state: 'completed', text: 'Synthetic final answer. src/file0000.ts:2' }, true)
  await expect(panel.locator('.answer-text')).toHaveText('Synthetic final answer. src/file0000.ts:2')
  await expect(heading).toBeFocused()
  await expect(draft).toHaveValue('Keep this draft\n')
  await expect(panel).toContainText('src/file0000.ts · head L2–2')
  await expect(panel.getByText('Completed', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Questions — unavailable', exact: true })).toBeDisabled()
  expect(await panel.textContent()).not.toContain('Sample transcript')
  expect(await panel.textContent()).not.toContain('Saved')
  expect(f.answers).toHaveLength(1)
  await page.screenshot({ path: testInfo.outputPath('real-conversation-completed-1440.png') })
  await page.setViewportSize({ width: 700, height: 900 })
  await page.getByRole('button', { name: 'Send follow-up', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeInViewport()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('real-conversation-700.png'), fullPage: true })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  await page.getByRole('button', { name: 'Send follow-up', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeInViewport()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('real-conversation-text-200.png'), fullPage: true })
})

test('thread-local follow-ups route background answers without clearing another thread draft', async ({ page }): Promise<void> => {
  const f = await fixture(page, 721)
  await ask(page, 2, 'Synthetic thread A.')
  await snapshot(page, f.answers[0]!, { state: 'completed', text: 'Synthetic original A.' }, true)
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('A follow-up\nsecond line')
  await ask(page, 4, 'Synthetic thread B.')
  await snapshot(page, f.answers[1]!, { state: 'completed', text: 'Synthetic original B.' }, true)
  await draft.fill('Retain B draft')
  await page.getByRole('button', { name: 'Thread 1 · Synthetic thread A.', exact: true }).click()
  await expect(draft).toHaveValue('A follow-up\nsecond line')
  await draft.dispatchEvent('keydown', { key: 'Enter', isComposing: true })
  expect(f.followups).toHaveLength(0)
  await draft.press('Enter')
  await expect.poll((): number => f.followups.length).toBe(1)
  expect(f.followups[0]).toEqual({ revisionID: f.source.diff!.revisionID, question: 'A follow-up\nsecond line' })
  const follow = f.answers[2]!
  expect(follow.threadID).toBe(f.answers[0]!.threadID)
  await page.getByRole('button', { name: 'Thread 2 · Synthetic thread B.', exact: true }).click()
  await expect(draft).toHaveValue('Retain B draft')
  await expect(draft).toBeEditable()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  await snapshot(page, follow, { state: 'streaming', text: 'Synthetic A partial.' })
  await expect(page.locator('.answer-text')).toHaveText('Synthetic original B.')
  await snapshot(page, follow, { state: 'failed', text: 'Synthetic A partial.', failureCode: 'execution-failed' }, true)
  await expect(draft).toHaveValue('Retain B draft')
  await page.getByRole('button', { name: 'Thread 1 · Synthetic thread A.', exact: true }).click()
  await expect(page.locator('.answer-text')).toHaveText(['Synthetic original A.', 'Synthetic A partial.'])
  await expect(draft).toHaveValue('')
  await expect(page.getByText('Failed', { exact: true })).toBeVisible()
  await expect(page.locator('[data-answer]')).toHaveCount(2)
  await expect(page.getByRole('form', { name: 'New question', exact: true })).toHaveCount(0)
})

test('lost observation reconnects read-only and Stop requires a terminal snapshot', async ({ page }, testInfo): Promise<void> => {
  const f = await fixture(page, 722)
  await ask(page, 2, 'Synthetic loss fixture.')
  const answer = f.answers[0]!
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic partial.' }, true)
  await expect(page.getByText('Answer observation stopped.', { exact: false })).toBeVisible()
  expect(f.answers).toHaveLength(1)
  await page.getByRole('button', { name: 'Select head line 4', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  const initialDraft = page.getByRole('textbox', { name: 'Your question', exact: true })
  await initialDraft.fill('Draft while another question is active.')
  await expect(initialDraft).toBeEditable()
  await expect(page.getByRole('button', { name: 'Send question', exact: true })).toBeDisabled()
  await page.getByRole('button', { name: 'Discard draft', exact: true }).click()
  await page.getByRole('button', { name: 'Reconnect answer observation', exact: true }).click()
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic partial.' })
  await expect(page.locator('.answer-text')).toHaveText('Synthetic partial.')
  await page.route('**/threads/*/answers/*/stop', async (route): Promise<void> => { await route.fulfill({ status: 202, json: { ...answer, state: 'streaming', text: 'Synthetic partial.', stopRequested: true } }) })
  await page.getByRole('button', { name: 'Stop this answer', exact: true }).click()
  await expect(page.getByText('Stop requested — awaiting confirmation', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('real-conversation-stopping.png') })
  await snapshot(page, answer, { state: 'interrupted', text: 'Synthetic partial.', stopRequested: true }, true)
  await expect(page.getByText('Interrupted — confirmed', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeEnabled()
  expect(f.answers).toHaveLength(1)
})

test('late Stop acknowledgements cannot regress a completed answer', async ({ page }): Promise<void> => {
  const f = await fixture(page, 725)
  await ask(page, 2, 'Synthetic Stop race.')
  const answer = f.answers[0]!
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic partial.' })
  let release: (() => void) | undefined
  await page.route('**/threads/*/answers/*/stop', async (route): Promise<void> => {
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ status: 202, json: { ...answer, stopRequested: true } })
  })
  await page.getByRole('button', { name: 'Stop this answer', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  await snapshot(page, answer, { state: 'completed', text: 'Synthetic completed before Stop acknowledgement.' }, true)
  release!()
  await expect(page.getByText('Stop check returned a terminal answer snapshot.', { exact: true })).toBeVisible()
  await expect(page.locator('.answer-text')).toHaveText('Synthetic completed before Stop acknowledgement.')
  await expect(page.getByText('Completed', { exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeEnabled()
})

test('pending and uncertain follow-up admission retains one draft without duplicate requests', async ({ page }): Promise<void> => {
  const f = await fixture(page, 726)
  await ask(page, 2, 'Synthetic admission fixture.')
  await snapshot(page, f.answers[0]!, { state: 'completed', text: 'Synthetic original.' }, true)
  let calls = 0
  let release: (() => void) | undefined
  await page.route('**/threads/*/answers', async (route): Promise<void> => {
    calls++
    await new Promise<void>((resolve): void => { release = resolve })
    await route.abort('failed')
  })
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Retain uncertain follow-up.')
  await draft.press('Enter')
  await expect(page.getByRole('button', { name: 'Sending follow-up…', exact: true })).toBeDisabled()
  await expect(draft).toHaveAttribute('readonly', '')
  await draft.press('Enter')
  await page.getByRole('form', { name: 'Thread follow-up', exact: true }).dispatchEvent('submit')
  expect(calls).toBe(1)
  release!()
  await expect(page.getByText('Follow-up admission is uncertain.', { exact: false })).toBeVisible()
  await expect(draft).toHaveValue('Retain uncertain follow-up.')
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  await expect(page.locator('.turn')).toHaveCount(1)
  expect(f.answers).toHaveLength(1)
})

test('oversized SSE frames stop observation without replacing retained text', async ({ page }): Promise<void> => {
  const f = await fixture(page, 727)
  await ask(page, 2, 'Synthetic bounded-frame fixture.')
  const answer = f.answers[0]!
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic bounded partial.' })
  await page.evaluate((id: string): void => {
    const stream = (window as unknown as { fixtureStreams: Record<string, ReadableStreamDefaultController<Uint8Array>> }).fixtureStreams[id]!
    stream.enqueue(new TextEncoder().encode('data: ' + 'x'.repeat(2 * 1024 * 1024 + 1)))
  }, answer.answerID)
  await expect(page.getByText('Answer observation stopped.', { exact: false })).toBeVisible()
  await expect(page.locator('.answer-text')).toHaveText('Synthetic bounded partial.')
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  expect(f.answers).toHaveLength(1)
})

test('remote busy rejection retains a follow-up and explicit recovery never creates phantom turns', async ({ page }): Promise<void> => {
  const f = await fixture(page, 723)
  await ask(page, 2, 'Synthetic busy fixture.')
  await snapshot(page, f.answers[0]!, { state: 'completed', text: 'Synthetic initial answer.' }, true)
  let attempts = 0
  await page.route('**/threads/*/answers', async (route): Promise<void> => {
    attempts++
    if (attempts === 1) await route.fulfill({ status: 409, json: { error: 'RAW PRIVATE BUSY DIAGNOSTIC' } })
    else await route.fallback()
  })
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Retry only explicitly.')
  await draft.press('Enter')
  await expect(page.getByText('This shared revision session is busy or unavailable.', { exact: false })).toBeVisible()
  await expect(draft).toHaveValue('Retry only explicitly.')
  expect(f.answers).toHaveLength(1)
  expect(await page.content()).not.toContain('RAW PRIVATE')
  await draft.press('Enter')
  await expect.poll((): number => f.answers.length).toBe(2)
  await snapshot(page, f.answers[1]!, { state: 'completed', text: 'Synthetic explicit recovery.' }, true)
  await expect(page.locator('.answer-text')).toHaveText(['Synthetic initial answer.', 'Synthetic explicit recovery.'])
  await expect(page.locator('[data-answer]')).toHaveCount(1)
  expect(attempts).toBe(2)
})

test('unknown execution stays busy and wrong-scope stream text is never rendered', async ({ page }, testInfo): Promise<void> => {
  const f = await fixture(page, 724)
  await ask(page, 2, 'Synthetic unknown fixture.')
  const answer = f.answers[0]!
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic retained text.' })
  await snapshot(page, answer, { state: 'completed', threadID: digest('foreign-thread'), text: 'WRONG SCOPE SECRET' }, true)
  await expect(page.getByText('Answer observation stopped.', { exact: false })).toBeVisible()
  await expect(page.locator('.answer-text')).toHaveText('Synthetic retained text.')
  expect(await page.content()).not.toContain('WRONG SCOPE SECRET')
  await page.getByRole('button', { name: 'Reconnect answer observation', exact: true }).click()
  await snapshot(page, answer, { state: 'interrupted', text: 'Synthetic retained text.', executionUnknown: true, failureCode: 'execution-uncertain' }, true)
  await expect(page.getByText('Interrupted / execution unknown', { exact: false })).toBeVisible()
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Editable but not sent.')
  await expect(draft).toBeEditable()
  await expect(page.getByRole('button', { name: 'Send follow-up', exact: true })).toBeDisabled()
  await page.screenshot({ path: testInfo.outputPath('real-conversation-unknown.png') })
  expect(f.answers).toHaveLength(1)
})

for (const change of ['base-only', 'head']) test(`${change} refresh preserves old follow-up bindings and never moves a pinned inline draft`, async ({ page }, testInfo): Promise<void> => {
  const f = await fixture(page, change === 'base-only' ? 730 : 731)
  await ask(page, 2, 'Synthetic original revision.')
  await snapshot(page, f.answers[0]!, { state: 'completed', text: 'Synthetic original transcript.' }, true)
  const original = f.source.diff!
  const next = sourceFixture(f.source.pr, 2, change === 'base-only' ? 'd'.repeat(40) : original.revision.baseSHA, change === 'head' ? 'e'.repeat(40) : original.revision.headSHA)
  next.requestedRevisionID = original.revisionID
  let refreshes = 0
  await page.route(`**/api/reviews/${original.revision.prID}/source`, async (route): Promise<void> => {
    refreshes++
    expect(route.request().postDataJSON()).toEqual({ action: 'refresh', revision: original.revision })
    await route.fulfill({ json: next })
  })
  const followup = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await followup.fill('Continue the old source after refresh.')
  await page.getByRole('button', { name: 'Select head line 4', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  const initial = page.getByRole('textbox', { name: 'Your question', exact: true })
  await initial.fill('Do not move this draft.')
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  expect(refreshes).toBe(0)
  await expect(initial).toHaveValue('Do not move this draft.')
  await expect(page.getByRole('form', { name: 'New question', exact: true })).toContainText(original.revisionID.slice(0, 8))
  await page.getByRole('button', { name: 'Discard draft', exact: true }).click()
  await page.getByRole('button', { name: 'Refresh source', exact: true }).click()
  await expect(page.getByText('Older context.', { exact: false })).toBeVisible()
  await expect(page.getByText(`Pinned revision ${next.diff!.revisionID.slice(0, 8)}`, { exact: true })).toBeVisible()
  await expect(page.locator('[data-answer]')).toHaveCount(0)
  await expect(followup).toHaveValue('Continue the old source after refresh.')
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await page.getByRole('button', { name: 'Open runtime conversation', exact: true }).click()
  await expect(page.getByText('Older context.', { exact: false })).toBeVisible()
  await expect(followup).toHaveValue('Continue the old source after refresh.')
  await followup.press('Enter')
  await expect.poll((): number => f.followups.length).toBe(1)
  expect(f.followups[0]).toEqual({ revisionID: original.revisionID, question: 'Continue the old source after refresh.' })
  expect(f.answers[1]!.reference.anchor).toEqual(f.answers[0]!.reference.anchor)
  await snapshot(page, f.answers[1]!, { state: 'completed', text: 'Synthetic old-context follow-up.' }, true)
  await expect(page.locator('.answer-text')).toHaveText(['Synthetic original transcript.', 'Synthetic old-context follow-up.'])
  await page.screenshot({ path: testInfo.outputPath(`real-question-${change}-refresh.png`) })
})

test('disconnect requires a discard decision and late streams cannot populate a reconnected workspace', async ({ page }, testInfo): Promise<void> => {
  const f = await fixture(page, 732)
  await ask(page, 2, 'Synthetic connection-bound answer.')
  const old = f.answers[0]!
  await snapshot(page, old, { state: 'streaming', text: 'Synthetic before disconnect.' })
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Keep this local draft.')
  let stops = 0
  await page.route('**/threads/*/answers/*/stop', async (route): Promise<void> => { stops++; await route.fulfill({ status: 202, json: old }) })
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: 'Disconnect this question workspace?', exact: true })
  await expect(dialog).toContainText('does not stop accepted execution')
  await page.screenshot({ path: testInfo.outputPath('real-question-disconnect-guard.png') })
  await dialog.getByRole('button', { name: 'Keep connected', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue('Keep this local draft.')
  await expect(page.getByRole('button', { name: 'Disconnect backend', exact: true })).toBeFocused()
  await page.evaluate((): void => { (window as unknown as { fixtureIgnoreAbort: boolean }).fixtureIgnoreAbort = true })
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  await dialog.getByRole('button', { name: 'Discard page state and disconnect', exact: true }).click()
  await reconnect(page)
  await snapshot(page, old, { state: 'completed', text: 'STALE STREAM MUST NOT APPEAR' }, true)
  await expect(page.locator('[data-answer]')).toHaveCount(0)
  await expect(page.locator('.answer-text')).toHaveCount(0)
  expect(await page.content()).not.toContain('STALE STREAM MUST NOT APPEAR')
  expect(stops).toBe(0)
  await ask(page, 4, 'Synthetic new connection answer.')
  await snapshot(page, f.answers[1]!, { state: 'completed', text: 'Synthetic new connection result.' }, true)
  await expect(page.locator('.answer-text')).toHaveText('Synthetic new connection result.')
  expect(f.answers).toHaveLength(2)
})

for (const action of ['followup', 'stop']) test(`late ${action} replies cannot mutate a new verified connection`, async ({ page }): Promise<void> => {
  const f = await fixture(page, action === 'followup' ? 733 : 734)
  await ask(page, 2, 'Synthetic old request scope.')
  const old = f.answers[0]!
  await snapshot(page, old, { state: action === 'followup' ? 'completed' : 'streaming', text: 'Synthetic original scope.' }, action === 'followup')
  await page.evaluate((action: string): void => {
    const original = window.fetch.bind(window)
    let armed = true
    window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      const path = new URL(input instanceof Request ? input.url : String(input), location.href).pathname
      if (armed && (action === 'stop' ? path.endsWith('/stop') : /\/threads\/[^/]+\/answers$/.test(path))) {
        armed = false
        return original(input, { ...init, signal: undefined })
      }
      return original(input, init)
    }
  }, action)
  let release: (() => void) | undefined
  await page.route(action === 'stop' ? '**/threads/*/answers/*/stop' : '**/threads/*/answers', async (route): Promise<void> => {
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ status: 202, json: action === 'stop' ? { ...old, state: 'interrupted', text: 'STALE STOP RESPONSE', stopRequested: true } : { ...old, answerID: digest('stale-followup'), question: 'Delayed old follow-up.' } })
  })
  if (action === 'stop') await page.getByRole('button', { name: 'Stop this answer', exact: true }).click()
  else {
    await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Delayed old follow-up.')
    await page.getByRole('button', { name: 'Send follow-up', exact: true }).click()
  }
  await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  await page.getByRole('button', { name: 'Discard page state and disconnect', exact: true }).click()
  await reconnect(page)
  await ask(page, 4, 'Synthetic new request scope.')
  release!()
  await snapshot(page, f.answers[1]!, { state: 'completed', text: 'Synthetic current transcript.' }, true)
  await expect(page.locator('.answer-text')).toHaveText('Synthetic current transcript.')
  await expect(page.locator('[data-answer]')).toHaveCount(1)
  expect(await page.content()).not.toContain('STALE STOP RESPONSE')
  expect(await page.content()).not.toContain('Delayed old follow-up.')
})

test('two tabs share backend busy feedback but never share drafts or a Questions library', async ({ page, context }): Promise<void> => {
  const first = await fixture(page, 735)
  const secondPage = await context.newPage()
  const second = await fixture(secondPage, 735)
  await ask(page, 2, 'Synthetic tab A.')
  await snapshot(page, first.answers[0]!, { state: 'streaming', text: 'Synthetic A active.' })
  let allowed = false
  await secondPage.route('**/api/reviews/*/questions', async (route): Promise<void> => {
    if (!allowed) { await route.fulfill({ status: 409, json: { error: 'synthetic remote busy' } }); return }
    const answer: QuestionAnswer = { ...first.answers[0]!, answerID: digest('second-tab-answer'), threadID: digest('second-tab-thread'), question: route.request().postDataJSON().question, reference: { revisionID: first.source.diff!.revisionID, anchor: route.request().postDataJSON().anchor } }
    second.answers.push(answer)
    await route.fulfill({ status: 202, json: answer })
  })
  await secondPage.getByRole('button', { name: 'Select head line 4', exact: true }).click()
  await secondPage.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  await secondPage.getByRole('textbox', { name: 'Your question', exact: true }).fill('Synthetic tab B retained draft.')
  await secondPage.getByRole('button', { name: 'Send question', exact: true }).click()
  await expect(secondPage.getByText('This shared revision session is busy or unavailable.', { exact: false })).toBeVisible()
  await expect(secondPage.locator('[data-answer]')).toHaveCount(0)
  await expect(page.locator('[data-answer]')).toHaveCount(1)
  const otherPage = await context.newPage()
  const other = await fixture(otherPage, 736)
  await ask(otherPage, 2, 'Synthetic independent PR.')
  await snapshot(otherPage, other.answers[0]!, { state: 'completed', text: 'Synthetic independent result.' }, true)
  await expect(page.locator('.answer-text')).toHaveText('Synthetic A active.')
  await snapshot(page, first.answers[0]!, { state: 'completed', text: 'Synthetic A finished.' }, true)
  allowed = true
  await expect(secondPage.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Synthetic tab B retained draft.')
  await secondPage.getByRole('button', { name: 'Send question', exact: true }).click()
  await expect.poll((): number => second.answers.length).toBe(1)
  await snapshot(secondPage, second.answers[0]!, { state: 'completed', text: 'Synthetic B result.' }, true)
  await expect(page.locator('.answer-text')).toHaveText('Synthetic A finished.')
  await expect(secondPage.locator('.answer-text')).toHaveText('Synthetic B result.')
  for (const tab of [page, secondPage, otherPage]) {
    await expect(tab.getByRole('button', { name: 'Questions — unavailable', exact: true })).toBeDisabled()
    const credential = await token()
    expect(await tab.content()).not.toContain(credential)
    expect(await tab.evaluate((): string => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))).not.toContain(credential)
  }
})

test('oversized admission responses are bounded and cannot invent a thread', async ({ page }): Promise<void> => {
  const f = await fixture(page, 737)
  await page.route('**/api/reviews/*/questions', async (route): Promise<void> => { await route.fulfill({ status: 202, body: 'x'.repeat(2 * 1024 * 1024 + 1) }) })
  await page.getByRole('button', { name: 'Select head line 2', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Keep the bounded draft.')
  await page.getByRole('button', { name: 'Send question', exact: true }).click()
  await expect(page.getByText('Admission is uncertain.', { exact: false })).toBeVisible()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Keep the bounded draft.')
  await expect(page.locator('[data-answer]')).toHaveCount(0)
  expect(f.answers).toHaveLength(0)
})

test('document navigation warns before discarding a follow-up draft without stopping observation', async ({ page }): Promise<void> => {
  const f = await fixture(page, 738)
  await ask(page, 2, 'Synthetic guarded navigation.')
  const answer = f.answers[0]!
  await snapshot(page, answer, { state: 'streaming', text: 'Synthetic still executing.' })
  const draft = page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })
  await draft.fill('Keep this draft on cancelled navigation.')
  const originalURL = page.url()
  let warned = false
  page.once('dialog', async (dialog): Promise<void> => { warned = dialog.type() === 'beforeunload'; await dialog.dismiss() })
  await page.getByRole('link', { name: 'Sample UI previews', exact: true }).click()
  expect(warned).toBe(true)
  expect(page.url()).toBe(originalURL)
  await expect(draft).toHaveValue('Keep this draft on cancelled navigation.')
  await snapshot(page, answer, { state: 'completed', text: 'Synthetic completed after navigation was cancelled.' }, true)
  await expect(page.locator('.answer-text')).toHaveText('Synthetic completed after navigation was cancelled.')
})

test('a late initial admission cannot clear the next connection draft or create a ghost marker', async ({ page }): Promise<void> => {
  const f = await fixture(page, 739)
  await page.evaluate((): void => {
    const original = window.fetch.bind(window)
    let armed = true
    window.fetch = (input: RequestInfo | URL, init?: RequestInit): Promise<Response> => {
      if (armed && String(input).endsWith('/questions')) { armed = false; return original(input, { ...init, signal: undefined }) }
      return original(input, init)
    }
  })
  let release: (() => void) | undefined
  await page.route('**/api/reviews/*/questions', async (route): Promise<void> => {
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fallback()
  })
  await page.getByRole('button', { name: 'Select head line 2', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Synthetic delayed admission.')
  await page.getByRole('button', { name: 'Send question', exact: true }).click()
  await expect.poll((): boolean => !!release).toBe(true)
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  await page.getByRole('button', { name: 'Discard page state and disconnect', exact: true }).click()
  await reconnect(page)
  await page.getByRole('button', { name: 'Select head line 4', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about selection', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('New connection draft must survive.')
  release!()
  await expect.poll((): number => f.answers.length).toBe(1)
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('New connection draft must survive.')
  await expect(page.locator('[data-answer]')).toHaveCount(0)
  await expect(page.locator('.answer-text')).toHaveCount(0)
})
