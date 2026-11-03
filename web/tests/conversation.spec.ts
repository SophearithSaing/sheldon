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
          signal?.addEventListener('abort', (): void => { if (streams[id] === controller) { delete streams[id]; controller.error(new Error('Fixture observation aborted.')) } }, { once: true })
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
