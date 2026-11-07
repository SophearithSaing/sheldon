import { test, expect } from '@playwright/test'
import { registered } from './runtime-fixture'
import { semanticFixture } from './semantic-fixture'

test.use({ baseURL: 'http://127.0.0.1:5175' })

test('semantic entry reads saved literal cross-file groups without source loading or generation', async ({ page }): Promise<void> => {
  const pr = await registered(page, 701); const fixture = semanticFixture(pr)
  let posts = 0
  await page.route('**/semantic-review**', async (route): Promise<void> => { if (route.request().method() === 'POST') posts++; await route.fulfill({ json: fixture.read }) })
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  await expect(page.locator('.semantic-excerpt')).toHaveCount(2)
  await expect(page.getByText('<script>Literal organizing hypothesis</script>', { exact: false })).toBeVisible()
  await expect(page.getByText('Currentness unverified; source is not loaded.', { exact: false })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Regenerate semantic review', exact: true })).toHaveAttribute('aria-disabled', 'true')
  expect(posts).toBe(0)
  await page.getByRole('button', { name: 'Ungrouped changes', exact: false }).click()
  await expect(page.locator('.semantic-excerpt')).toHaveCount(1)
  for (const [width, scale, name] of [[1440, 1, 'semantic-saved-1440.png'], [700, 1, 'semantic-saved-700.png'], [1440, 2, 'semantic-saved-text-200.png']] as const) {
    await page.setViewportSize({ width, height: 1080 }); await page.evaluate((n: number): void => { document.documentElement.style.fontSize = `${n * 16}px` }, scale)
    await page.screenshot({ path: test.info().outputPath(name) })
    expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  }
})

test('semantic generation is explicit and publishes only a read acknowledged complete result', async ({ page }): Promise<void> => {
  const pr = await registered(page, 702); const fixture = semanticFixture(pr); let generated = false; let posts = 0
  await page.route('**/source', async (route): Promise<void> => { await route.fulfill({ json: fixture.source }) })
  await page.route('**/semantic-review**', async (route): Promise<void> => {
    const path = new URL(route.request().url()).pathname
    if (route.request().method() === 'POST') { posts++; generated = true; await route.fulfill({ status: 202, json: fixture.status }); return }
    if (path.includes('/generations/')) { await route.fulfill({ json: { ...fixture.status, state: 'completed', sequence: '2', savedVersion: '1' } }); return }
    await route.fulfill({ json: generated ? { ...fixture.read, review: { ...fixture.read.review!, reviewID: fixture.status.reviewID }, generation: { ...fixture.status, state: 'completed', sequence: '2', savedVersion: '1' } } : { prID: pr.id, version: '0', review: null, generation: null } })
  })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await expect(page.getByText('No saved semantic review.', { exact: false })).toBeVisible(); expect(posts).toBe(0)
  await page.getByRole('button', { name: 'Generate semantic review', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  expect(posts).toBe(1)
})

test('failed semantic replacement preserves complete groups and requires explicit regeneration confirmation', async ({ page }): Promise<void> => {
  const pr = await registered(page, 703); const fixture = semanticFixture(pr); let generated = false; let posts = 0
  await page.route('**/source', async (route): Promise<void> => { await route.fulfill({ json: fixture.source }) })
  await page.route('**/semantic-review**', async (route): Promise<void> => {
    if (route.request().method() === 'POST') { posts++; generated = true; await route.fulfill({ status: 202, json: fixture.status }); return }
    if (new URL(route.request().url()).pathname.includes('/generations/')) { await route.fulfill({ json: { ...fixture.status, state: 'failed', sequence: '2', failureCode: 'invalid-output' } }); return }
    await route.fulfill({ json: { ...fixture.read, generation: generated ? { ...fixture.status, state: 'failed', sequence: '2', failureCode: 'invalid-output' } : null } })
  })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click(); await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Regenerate semantic review', exact: true }).click(); expect(posts).toBe(0)
  await page.getByRole('button', { name: 'Keep current review', exact: true }).click(); expect(posts).toBe(0)
  await page.getByRole('button', { name: 'Regenerate semantic review', exact: true }).click(); await page.getByRole('button', { name: 'Confirm regenerate review', exact: true }).click()
  await expect(page.getByText('Generation: failed', { exact: false })).toBeVisible(); await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible(); expect(posts).toBe(1)
})

test('semantic parsing rejects overlapping, foreign and private result fields while retaining healthy saved groups', async ({ page }): Promise<void> => {
  const pr = await registered(page, 704); const fixture = semanticFixture(pr); let response: unknown = fixture.read
  await page.route('**/semantic-review', async (route): Promise<void> => { await route.fulfill({ json: response }) })
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click(); await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  for (const broken of [
    { ...fixture.read, nativeID: 'PRIVATE' },
    { ...fixture.read, review: { ...fixture.read.review!, groups: [{ ...fixture.read.review!.groups[0]!, ranges: [fixture.read.review!.groups[0]!.ranges[0]!, fixture.read.review!.groups[0]!.ranges[0]!] }] } },
    { ...fixture.read, review: { ...fixture.read.review!, groups: [{ ...fixture.read.review!.groups[0]!, ranges: [{ ...fixture.read.review!.groups[0]!.ranges[0]!, fileID: 'f'.repeat(64) }] }] } },
  ]) {
    response = broken; await page.getByRole('button', { name: 'Refresh saved review', exact: true }).click(); await expect(page.getByRole('alert')).toContainText('Cannot read saved semantic review'); await expect(page.getByRole('heading', { name: 'Guard and caller', exact: true })).toBeVisible()
  }
  await expect(page.getByText('PRIVATE', { exact: true })).toHaveCount(0)
})

test('semantic gutter selection opens the existing original-source composer and rejected questions retain that exact draft', async ({ page }): Promise<void> => {
  const pr = await registered(page, 705); const fixture = semanticFixture(pr); const questions: unknown[] = []
  await page.route('**/source', async (route): Promise<void> => { await route.fulfill({ json: fixture.source }) })
  await page.route('**/semantic-review', async (route): Promise<void> => { await route.fulfill({ json: fixture.read }) })
  await page.route('**/questions', async (route): Promise<void> => { questions.push(route.request().postDataJSON()); await route.fulfill({ status: 503, json: { code: 'persistence-unavailable' } }) })
  await page.getByRole('button', { name: 'Load pinned source', exact: true }).click()
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  const first = page.locator('.semantic-excerpt').first()
  await first.getByRole('button', { name: 'View in original diff', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'src/file0000.ts', exact: true })).toBeVisible()
  expect(questions).toHaveLength(0)
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await first.getByRole('button', { name: 'Select head line 1', exact: true }).click()
  expect(questions).toHaveLength(0)
  await first.getByRole('button', { name: 'Ask about selected range', exact: true }).click()
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await expect(input).toBeFocused(); await input.fill('Explain this selected boundary')
  await page.getByRole('button', { name: 'Send question', exact: true }).click()
  await expect(input).toHaveValue('Explain this selected boundary')
  expect(questions).toHaveLength(1)
  expect(questions[0]).toEqual({ revision: fixture.source.diff!.revision, anchor: { kind: 'lines', ...fixture.read.review!.groups[0]!.ranges[0]! }, question: 'Explain this selected boundary' })
  await page.screenshot({ path: test.info().outputPath('semantic-question-draft.png') })
  await page.getByRole('button', { name: 'Semantic review', exact: true }).click()
  await expect(first.getByRole('button', { name: 'Ask about selected range', exact: true })).toHaveAttribute('aria-disabled', 'true')
  await page.getByRole('button', { name: 'Files changed', exact: false }).click(); await expect(input).toHaveValue('Explain this selected boundary')
})
