import { expect, test } from '@playwright/test'
import { digest, registered, token } from './runtime-fixture'
import { historyFixture, installHistoryHarness, ignoreHistoryAbort, type HistoryHarness } from './history-fixture'

test.use({ baseURL: 'http://127.0.0.1:5175' })

test('saved reads use bearer headers, bounded public DTOs and lossless generations without inference', async ({ page }): Promise<void> => {
  const pr = await registered(page, 760)
  const f = historyFixture(pr)
  const parsed = await page.evaluate(async (detail): Promise<string> => {
    const modulePath = '/src/runtime/history.ts'
    const { parseThreadDetail } = await import(modulePath)
    try { await parseThreadDetail(detail, detail.prID, detail.threadID); return '' } catch (error: unknown) { return String(error) }
  }, f.detail)
  expect(parsed).toBe('')
  const traffic: string[] = []
  page.on('request', (request): void => { traffic.push(request.method() + ' ' + request.url()) })
  await page.route(`**/api/reviews/${pr.id}/threads`, async (route): Promise<void> => {
    expect(route.request().headers()['authorization']).toBe(`Bearer ${await token()}`)
    await route.fulfill({ json: { ...f.list, version: '18446744073709551615', native: { password: 'PRIVATE CANARY' } } })
  })
  await page.route(`**/api/reviews/${pr.id}/threads/${f.detail.threadID}`, async (route): Promise<void> => {
    expect(route.request().method()).toBe('GET')
    expect(route.request().headers()['authorization']).toBe(`Bearer ${await token()}`)
    await route.fulfill({ json: { ...f.detail, version: '18446744073709551615', native: { directory: '/PRIVATE CANARY' } } })
  })
  await installHistoryHarness(page)
  await page.evaluate(async ({ prID, threadID }): Promise<void> => {
    const state = (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state
    await state.refreshList(prID)
    await state.refreshThread(prID, threadID)
  }, { prID: pr.id, threadID: f.detail.threadID })
  const state = await page.evaluate((): string => JSON.stringify((window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.details.value))
  expect(state).toContain('18446744073709551615')
  expect(state).toContain('Synthetic saved answer')
  expect(state).not.toContain('PRIVATE CANARY')
  expect(traffic.some((entry: string): boolean => entry.startsWith('POST ') || entry.includes('/source') || entry.includes('/events') || entry.includes('token='))).toBe(false)
  await expect(page.getByRole('heading', { name: 'Pull request #760', exact: true })).toBeVisible()
})

test('late saved reads cannot replace newer generations, scopes or connections', async ({ page }): Promise<void> => {
  const pr = await registered(page, 761)
  const f = historyFixture(pr)
  await installHistoryHarness(page)
  await ignoreHistoryAbort(page)
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/threads/${f.detail.threadID}`, async (route): Promise<void> => {
    await new Promise<void>((resolve): void => { release = resolve })
    await route.fulfill({ json: f.detail })
  })
  await page.route(`**/api/reviews/${pr.id}/threads`, async (route): Promise<void> => { await route.fulfill({ json: { ...f.list, version: '8' } }) })
  await page.evaluate(({ prID, threadID }): void => { void (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshThread(prID, threadID) }, { prID: pr.id, threadID: f.detail.threadID })
  await expect.poll((): boolean => !!release).toBe(true)
  await page.evaluate(async (prID: string): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshList(prID) }, pr.id)
  release!()
  await expect.poll((): Promise<boolean> => page.evaluate((): boolean => !(window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.pending.value)).toBe(true)
  expect(await page.evaluate((): number => Object.keys((window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.details.value).length)).toBe(0)
  release = undefined
  await page.evaluate(({ prID, threadID }): void => { void (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshThread(prID, threadID) }, { prID: pr.id, threadID: f.detail.threadID })
  await expect.poll((): boolean => !!release).toBe(true)
  await page.evaluate((): void => { (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.replace() })
  release!()
  await expect.poll((): Promise<boolean> => page.evaluate((): boolean => !(window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.pending.value)).toBe(true)
  expect(await page.evaluate((): number => Object.keys((window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.details.value).length)).toBe(0)
  expect(await page.evaluate((): string => (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.list.value!.version)).toBe('8')
  release = undefined
  await page.evaluate(({ prID, threadID }): void => { void (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshThread(prID, threadID) }, { prID: pr.id, threadID: f.detail.threadID })
  await expect.poll((): boolean => !!release).toBe(true)
  const otherID = digest('explicit-other-renderer-PR')
  await page.route(`**/api/reviews/${otherID}/threads`, async (route): Promise<void> => { await route.fulfill({ json: { prID: otherID, version: '1', threads: [] } }) })
  await page.evaluate(async (prID: string): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshList(prID) }, otherID)
  release!()
  await expect.poll((): Promise<boolean> => page.evaluate((): boolean => !(window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.pending.value)).toBe(true)
  expect(await page.evaluate((): string => (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.list.value!.prID)).toBe(otherID)
  expect(await page.evaluate((): number => Object.keys((window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.details.value).length)).toBe(0)
})

test('failed malformed and oversized saved refreshes retain the previous healthy snapshot', async ({ page }): Promise<void> => {
  const pr = await registered(page, 762)
  const f = historyFixture(pr)
  let mode = 'good'
  await page.route(`**/api/reviews/${pr.id}/threads`, async (route): Promise<void> => {
    if (mode === 'good') await route.fulfill({ json: f.list })
    else if (mode === 'error') await route.fulfill({ status: 500, json: { error: 'PRIVATE RAW DIAGNOSTIC' } })
    else if (mode === 'oversized') await route.fulfill({ contentType: 'application/json', body: ' '.repeat((2 << 20) + 1) })
    else await route.fulfill({ json: { ...f.list, prID: 'f'.repeat(64) } })
  })
  await installHistoryHarness(page)
  await page.evaluate(async (prID: string): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshList(prID) }, pr.id)
  for (mode of ['error', 'oversized', 'scope']) {
    await page.evaluate(async (prID: string): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshList(prID) }, pr.id)
    expect(await page.evaluate((): string => (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.list.value!.version)).toBe('7')
    expect(await page.evaluate((): string => (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.error.value)).toBe('Cannot refresh saved question history. Existing transcripts and drafts were retained.')
    expect(await page.content()).not.toContain('PRIVATE RAW DIAGNOSTIC')
  }
})

test('superseded reads cannot clear newer persistence feedback at the same saved generation', async ({ page }): Promise<void> => {
  const pr = await registered(page, 764)
  const f = historyFixture(pr)
  await installHistoryHarness(page)
  await ignoreHistoryAbort(page)
  let calls = 0
  let release: (() => void) | undefined
  await page.route(`**/api/reviews/${pr.id}/threads`, async (route): Promise<void> => {
    calls++
    if (calls === 1) { await new Promise<void>((resolve): void => { release = resolve }); await route.fulfill({ json: f.list }) }
    else await route.fulfill({ json: { ...f.list, threads: [{ ...f.list.threads[0]!, persistence: 'failed' }] } })
  })
  await page.evaluate((prID: string): void => { void (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshList(prID) }, pr.id)
  await expect.poll((): boolean => !!release).toBe(true)
  await page.evaluate(async (prID: string): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshList(prID) }, pr.id)
  release!()
  await expect.poll((): Promise<boolean> => page.evaluate((): boolean => !(window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.pending.value)).toBe(true)
  expect(await page.evaluate((): string => (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.list.value!.threads[0]!.persistence)).toBe('failed')
})

test('saved detail validation rejects private context, inconsistent quotes, wrong scope and oversized bodies', async ({ page }): Promise<void> => {
  const pr = await registered(page, 765)
  const f = historyFixture(pr)
  let mode = 'good'
  await page.route(`**/api/reviews/${pr.id}/threads/${f.detail.threadID}`, async (route): Promise<void> => {
    if (mode === 'directory') await route.fulfill({ json: { ...f.detail, context: { ...f.detail.context, directory: '/PRIVATE HOST PATH' } } })
    else if (mode === 'quotes') await route.fulfill({ json: { ...f.detail, context: { ...f.detail.context, selected: [{ ...f.detail.context.selected[0]!, text: 'PRIVATE inconsistent quote' }] } } })
    else if (mode === 'scope') await route.fulfill({ json: { ...f.detail, answers: [{ ...f.detail.answers[0]!, prID: digest('foreign saved scope') }] } })
    else if (mode === 'oversized') await route.fulfill({ contentType: 'application/json', body: ' '.repeat((8 << 20) + 1) })
    else await route.fulfill({ json: f.detail })
  })
  await installHistoryHarness(page)
  await page.evaluate(async ({ prID, threadID }): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshThread(prID, threadID) }, { prID: pr.id, threadID: f.detail.threadID })
  for (mode of ['directory', 'quotes', 'scope', 'oversized']) {
    await page.evaluate(async ({ prID, threadID }): Promise<void> => { await (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.refreshThread(prID, threadID) }, { prID: pr.id, threadID: f.detail.threadID })
    const snapshot = await page.evaluate((): string => JSON.stringify((window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.details.value))
    expect(snapshot).toContain('Synthetic saved answer')
    expect(snapshot).not.toContain('PRIVATE')
    expect(await page.evaluate((): string => (window as unknown as { fixtureHistory: HistoryHarness }).fixtureHistory.state.error.value)).toBe('Cannot refresh saved question history. Existing transcripts and drafts were retained.')
  }
})
