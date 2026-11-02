import { expect, test, type Page } from '@playwright/test'
import { semanticGroups, semanticHunks, ungrouped } from '../src/fixtures/semantic'

/**
 * Opens the optional view without starting a grouping simulation.
 * @param fixtures Playwright page.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/')
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
})

/**
 * Explicitly starts and manually finishes the fixed complete grouping.
 * @param page Browser showing the semantic start preview.
 */
async function completeGrouping(page: Page): Promise<void> {
  await page.getByRole('button', { name: 'Preview semantic generation', exact: true }).click()
  await page.getByRole('button', { name: 'Finish sample grouping', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Prevent stale sessions after logout', exact: true })).toBeVisible()
}

/**
 * Opens local simulation controls without relying on hidden neighboring workspaces.
 * @param page Browser showing the semantic preview.
 */
async function controls(page: Page): Promise<void> {
  const details = page.locator('.semantic-tools:visible')
  if (!(await details.evaluate((element): boolean => (element as HTMLDetailsElement).open))) await details.locator('summary').click()
}

/**
 * Checks manifest partition and source coordinates, not real repository coverage.
 */
test('sample manifest accounts for eight unique hunks as 3 plus 2 plus 2 and one ungrouped', (): void => {
  expect(semanticGroups.map((group): number => group.hunks.length)).toEqual([3, 2, 2])
  const grouped = semanticGroups.flatMap((group): string[] => group.hunks)
  const all = [...grouped, ...ungrouped.hunks]
  expect(new Set(grouped).size).toBe(7)
  expect(new Set(all).size).toBe(8)
  expect([...all].sort()).toEqual(semanticHunks.map((hunk): string => hunk.id).sort())
  for (const hunk of semanticHunks) {
    expect(hunk.anchor.revision).toBe('a81c2f4')
    expect(hunk.anchor.range).not.toBeNull()
    expect(hunk.lines.some((line): boolean => line.kind !== 'context')).toBe(true)
    const range = hunk.anchor.range!
    const numbers = hunk.lines.flatMap((line): number[] => line[range.side] === null ? [] : [line[range.side]!])
    expect(numbers[0]).toBe(range.start)
    expect(numbers.at(-1)).toBe(range.end)
    expect(numbers).toEqual([...numbers].sort((left, right): number => left - right))
  }
})

/**
 * Keeps entry inert and ordinary file review reachable while a manual simulation is pending.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot locations.
 */
test('entering and restoring Semantic review starts no work and pending grouping never auto-completes', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => {
    if (['fetch', 'xhr'].includes(request.resourceType()) || new URL(request.url()).origin !== 'http://127.0.0.1:5174') requests.push(request.url())
  })
  await expect(page.getByRole('tab')).toHaveCount(4)
  await expect(page.getByText('Not started', { exact: true }).filter({ visible: true })).toBeVisible()
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('semantic-start-1440.png') })
  await page.clock.install()
  await page.clock.runFor(60000)
  await expect(page.getByRole('button', { name: 'Finish sample grouping' })).toHaveCount(0)
  await page.getByRole('button', { name: 'Preview semantic generation', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Preview semantic generation', exact: true })).toBeDisabled()
  await page.clock.runFor(60000)
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(0)
  await page.getByRole('button', { name: 'Continue in Files changed', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Sample code diff', exact: true })).toBeVisible()
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Finish sample grouping', exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('semantic-generating.png') })
  await page.getByRole('button', { name: 'Cancel sample grouping', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Sample grouping cancelled', exact: true })).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  expect(requests).toEqual([])
})

/**
 * Renders separate ranged excerpts and reachable ungrouped source without interpreting counts as scores.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot locations.
 */
test('complete sample has three groups and a reachable ungrouped hunk with literal source', async ({ page }, testInfo): Promise<void> => {
  await completeGrouping(page)
  const navigation = page.getByRole('region', { name: 'Semantic review navigation', exact: true })
  await expect(navigation).toContainText('7 of 8 changed hunks grouped')
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(3)
  await expect(page.getByRole('region', { name: 'Semantic excerpt: generation', exact: true })).toContainText('  const generation = store.generation;')
  await page.screenshot({ path: testInfo.outputPath('semantic-review-1440.png') })
  await page.getByRole('button', { name: /^Group 02:/ }).click()
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(2)
  const shared = page.getByRole('region', { name: 'Semantic excerpt: shared-promise', exact: true })
  await expect(shared).toContainText('return requestSession().then(store.set);')
  await expect(shared.locator('.diff-line--removed .line-number').first()).toHaveText('23')
  await expect(shared.locator('.diff-line--added .line-number').nth(1)).toHaveText('23')
  await page.getByRole('button', { name: /^Group 03:/ }).click()
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(2)
  await expect(page.getByRole('region', { name: 'Semantic excerpt: failure-test', exact: true })).toContainText('omitted sample source')
  await page.getByRole('button', { name: 'Next: ungrouped changes', exact: true }).click()
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(1)
  await expect(page.getByRole('region', { name: 'Semantic excerpt: documentation', exact: true })).toContainText('Example markup stays literal: <img src=x onerror=alert(1)>.')
  await expect(page.locator('article.semantic-hunk:visible img')).toHaveCount(0)
  await page.screenshot({ path: testInfo.outputPath('semantic-ungrouped.png') })
  await expect(navigation).toContainText('7 of 8 changed hunks grouped')
})

/**
 * Expands adjacent context without changing group ranges, hunk coverage or viewed state.
 * @param fixtures Playwright page.
 */
test('expanded context preserves original anchors and source navigation reaches the separate failure hunk', async ({ page }): Promise<void> => {
  await completeGrouping(page)
  await page.getByRole('button', { name: 'Expand context', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Semantic excerpt: generation', exact: true })).toContainText('  return inFlight;')
  await expect(page.getByRole('region', { name: 'Semantic review navigation', exact: true })).toContainText('7 of 8 changed hunks grouped')
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Pinned group question draft', exact: true })).toContainText('head L25–30')
  await expect(page.getByRole('region', { name: 'Pinned group question draft', exact: true })).not.toContainText('head L22–36')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await page.getByRole('button', { name: /^Group 03:/ }).click()
  await page.getByRole('article', { name: 'Exercise another request after failure', exact: true }).getByRole('button', { name: 'View in diff', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Select head line 36', exact: true })).toBeFocused()
  await expect(page.getByRole('region', { name: 'Sample code diff', exact: true })).toContainText('omitted sample source')
  await expect(page.getByRole('button', { name: 'Select head line 19', exact: true })).toHaveCount(0)
  await expect(page.getByRole('checkbox', { name: 'Viewed', exact: true })).not.toBeChecked()
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await page.getByRole('button', { name: /^Group 01:/ }).click()
  await expect(page.getByRole('button', { name: 'Collapse context', exact: true })).toBeVisible()
})

/**
 * Preserves complete grouping, source expansion, selected group and earlier drafts through replacement previews.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot locations.
 */
test('rebuild confirmation cancel failure and success retain the previous complete grouping and pinned drafts', async ({ page }, testInfo): Promise<void> => {
  await completeGrouping(page)
  await page.getByRole('button', { name: /^Group 02:/ }).click()
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Retain the original two ranges')
  await page.getByRole('button', { name: 'Cancel', exact: true }).click()
  await controls(page)
  await page.getByLabel('Sample grouping outcome', { exact: true }).filter({ visible: true }).selectOption('error')
  await page.getByRole('button', { name: 'Rebuild groups · preview', exact: true }).click()
  await page.getByRole('button', { name: 'Keep current groups', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Finish sample grouping', exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: 'Rebuild groups · preview', exact: true }).click()
  await page.getByRole('button', { name: 'Confirm preview rebuild', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Share concurrent refresh work', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Finish sample grouping', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Sample generation failed', exact: true })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('semantic-generation-error.png') })
  await page.getByLabel('Sample grouping outcome', { exact: true }).filter({ visible: true }).selectOption('success')
  await page.getByRole('button', { name: 'Rebuild groups · preview', exact: true }).click()
  await page.getByRole('button', { name: 'Confirm preview rebuild', exact: true }).click()
  await page.getByRole('button', { name: 'Cancel sample grouping', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Share concurrent refresh work', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Rebuild groups · preview', exact: true }).click()
  await page.getByRole('button', { name: 'Confirm preview rebuild', exact: true }).click()
  await page.getByRole('button', { name: 'Finish sample grouping', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Retain the original two ranges')
  await expect(page.getByRole('region', { name: 'Pinned group question draft', exact: true })).toContainText('Retained from an earlier grouping')
  await expect(page.getByRole('region', { name: 'Pinned group question draft', exact: true })).toContainText('src/http/client.ts · head L12–15 · a81c2f4')
})

/**
 * Creates one multi-range UI thread with the revision's shared question context and existing markers.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot locations.
 */
test('group creation is one thread with all three pinned ranges not a whole-file or new-session context', async ({ page }, testInfo): Promise<void> => {
  const requests: string[] = []
  page.on('request', (request): void => {
    if (['fetch', 'xhr'].includes(request.resourceType()) || new URL(request.url()).origin !== 'http://127.0.0.1:5174') requests.push(request.url())
  })
  await completeGrouping(page)
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  const groupDraft = page.getByRole('region', { name: 'Pinned group question draft', exact: true })
  await expect(groupDraft.locator('li')).toHaveCount(3)
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('How do logout and the refresh guard interact?')
  await page.screenshot({ path: testInfo.outputPath('semantic-group-draft.png') })
  await page.getByRole('button', { name: 'Preview thread creation', exact: true }).click()
  const panel = page.getByRole('complementary', { name: 'Question 01', exact: true })
  await expect(panel).toBeVisible()
  await expect(panel).toContainText('Shared agent context · This PR revision')
  const context = panel.getByRole('region', { name: 'Pinned multi-range question context', exact: true })
  await expect(context).toContainText('3 pinned source ranges · One UI thread')
  await expect(context).toContainText('src/auth/refresh-session.ts · head L25–30 · a81c2f4')
  await expect(context).toContainText('src/auth/session-store.ts · head L41–44 · a81c2f4')
  await expect(context).toContainText('tests/auth/refresh-session.test.ts · head L12–18 · a81c2f4')
  await expect(context).not.toContainText('Whole file')
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '1 sample items')
  await page.screenshot({ path: testInfo.outputPath('semantic-thread-panel.png') })
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Open sample question 1', exact: true })).toBeFocused()
  await page.getByRole('button', { name: 'Open sample question 1', exact: true }).click()
  await context.getByRole('button').nth(1).click()
  await expect(page.getByRole('button', { name: 'Select head line 44', exact: true })).toBeFocused()
  await expect(page.getByRole('checkbox', { name: 'Viewed', exact: true })).not.toBeChecked()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await page.getByRole('searchbox').fill('tests/auth/refresh-session.test.ts')
  await expect(page.getByRole('button', { name: /^Open question 01:/ })).toBeVisible()
  expect(requests).toEqual([])
})

/**
 * Keeps creation failures local and pending creation single-shot while response failure belongs to its created thread.
 * @param fixtures Playwright page.
 */
test('creation errors pending completion and response errors retain the correct group context', async ({ page }): Promise<void> => {
  await completeGrouping(page)
  await controls(page)
  const outcome = page.getByLabel('Group thread creation outcome', { exact: true }).filter({ visible: true })
  await outcome.selectOption('creation-error')
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await input.fill('Retained group draft')
  await input.press('Control+Enter')
  await expect(page.getByText(/Sample creation failed\. No thread was created; the draft and all pinned ranges are retained\./)).toBeVisible()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  await outcome.selectOption('pending')
  await input.press('Control+Enter')
  await input.press('Control+Enter')
  await page.getByRole('button', { name: /^Group 02:/ }).click()
  await expect(page.getByRole('heading', { name: 'Prevent stale sessions after logout', exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Finish sample creation', exact: true }).click()
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '1 sample items')
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await page.getByRole('button', { name: /^Group 02:/ }).click()
  await outcome.selectOption('response-error')
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('A second group question')
  await page.getByRole('button', { name: 'Preview thread creation', exact: true }).click()
  await expect(page.getByRole('complementary', { name: 'Question 02', exact: true })).toContainText('Sample response failed.')
  await expect(page.getByRole('complementary', { name: 'Question 02', exact: true })).toContainText('2 pinned source ranges')
})

/**
 * Separates generation from shared-session busy state and blocks creation from outdated context.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot locations.
 */
test('busy questions and outdated previews retain editable anchored drafts without claiming live work', async ({ page }, testInfo): Promise<void> => {
  await page.goto('/?sample=review')
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await completeGrouping(page)
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Wait for the shared revision context')
  await expect(page.getByRole('button', { name: 'Preview thread creation', exact: true })).toBeDisabled()
  await expect(page.getByText('Another sample question is investigating in this PR revision. Creation is unavailable.').filter({ visible: true })).toBeVisible()
  await controls(page)
  await page.getByRole('checkbox', { name: 'Preview groups outdated', exact: true }).check()
  await expect(page.getByRole('button', { name: 'Rebuild for latest — unavailable', exact: true })).toBeDisabled()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('Wait for the shared revision context')
  await page.screenshot({ path: testInfo.outputPath('semantic-outdated-busy.png') })
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '4 sample items')
})

/**
 * Restores PR-local grouping/drafts across settings and close/reopen without rerunning or resetting the quiz.
 * @param fixtures Playwright page.
 * @param testInfo Screenshot locations.
 */
test('semantic state is PR-local and remains usable at narrow and scaled sizes', async ({ page }, testInfo): Promise<void> => {
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await page.getByRole('radio').nth(1).check()
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await completeGrouping(page)
  await page.getByRole('button', { name: /^Group 03:/ }).click()
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('PR 248 semantic draft')
  const source = page.getByRole('region', { name: 'Semantic review source', exact: true })
  await source.evaluate((element): void => { element.scrollTop = 120 })
  const position = await source.evaluate((element): number => element.scrollTop)
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await page.getByRole('link', { name: /Back to review/ }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('PR 248 semantic draft')
  expect(await source.evaluate((element): number => element.scrollTop)).toBe(position)
  await page.getByRole('link', { name: 'platform #251 — Retry handling', exact: true }).click()
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await expect(page.getByText('Not started', { exact: true }).filter({ visible: true })).toBeVisible()
  await page.getByRole('button', { name: 'Close sample workspace platform #248', exact: true }).click()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByRole('link', { name: 'Open sample platform #248', exact: true }).click()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue('PR 248 semantic draft')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await expect(page.getByRole('region', { name: 'Sample comprehension quiz', exact: true })).toContainText('0 of 5 checked')
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await page.setViewportSize({ width: 899, height: 1080 })
  await page.getByRole('textbox', { name: 'Your question', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('semantic-899.png') })
  await page.setViewportSize({ width: 1440, height: 1080 })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  await page.getByRole('button', { name: 'Continue in Files changed', exact: true }).scrollIntoViewIfNeeded()
  await expect(page.getByRole('button', { name: 'Continue in Files changed', exact: true })).toBeInViewport()
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('semantic-text-scale-200.png') })
})
