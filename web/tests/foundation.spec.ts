import { expect, test } from '@playwright/test'

test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => document.fonts.ready)
})

test('keyboard users can skip directly to the labeled workspace content', async ({ page }) => {
  await page.keyboard.press('Tab')
  const skip = page.getByRole('link', { name: 'Skip to workspace', exact: true })
  await expect(skip).toBeFocused()
  await expect(skip).toBeInViewport()
  await skip.press('Enter')
  await expect(page.getByRole('main')).toBeFocused()
})

test('uses local Graphite typography, native geometry and a bundled license', async ({ page }) => {
  await expect(page.getByText('UI fixture', { exact: true })).toBeVisible()
  await expect(page.getByText('No PR loaded. No agent connected.', { exact: false })).toBeVisible()
  const typography = await page.evaluate(() => {
    const style = getComputedStyle(document.documentElement)
    return {
      family: style.fontFamily,
      size: getComputedStyle(document.body).fontSize,
      background: style.backgroundColor,
      ligatures: style.fontVariantLigatures,
      synthesis: style.fontSynthesis,
      loaded: document.fonts.check('400 13px "Sheldon JetBrains Mono"'),
    }
  })
  expect(typography).toMatchObject({
    size: '13px',
    background: 'rgb(23, 25, 29)',
    ligatures: 'none',
    synthesis: 'none',
    loaded: true,
  })
  expect(typography.family).toContain('Sheldon JetBrains Mono')
  await expect(page.getByRole('heading', { level: 1 })).toHaveCSS('font-size', '22px')
  await expect(page.locator('.diff-table').first()).toHaveCSS('font-size', '13px')
  await expect(page.locator('.diff-table').first()).toHaveCSS('line-height', '24px')
  const rail = await page.getByRole('complementary', { name: 'Changed files' }).boundingBox()
  expect(rail?.width).toBeCloseTo(252, 0)
  await page.getByRole('button', { name: 'Preview sample conversation' }).click()
  const conversation = await page
    .getByRole('complementary', { name: 'Sample conversation' })
    .boundingBox()
  expect(conversation?.width).toBeCloseTo(480, 0)
  await expect(page.locator('.sample-explanation')).toHaveCSS('font-size', '14px')
  await expect(page.locator('.sample-explanation')).toHaveCSS('line-height', '23.8px')
  const license = await page
    .getByRole('link', { name: 'JetBrains Mono license' })
    .getAttribute('href')
  expect(license).toBeTruthy()
  const response = await page.request.get(license!)
  expect(await response.text()).toContain('SIL OPEN FONT LICENSE Version 1.1')
})

test('tabs support roving focus and retain editable drafts and scroll position', async ({
  page,
}) => {
  const draft = page.getByLabel('Sample line question', { exact: true })
  await draft.fill('A local draft')
  await draft.press('End')
  await draft.press('Enter')
  await draft.press('a')
  await expect(draft).toHaveValue('A local draft\na')
  const source = page.getByRole('region', { name: 'Sample code diff', exact: true })
  const scrollTop = await source.evaluate((element) => {
    element.scrollTop = 160
    return element.scrollTop
  })
  const files = page.getByRole('tab', { name: 'Files changed' })
  const questions = page.getByRole('tab', { name: 'Questions', exact: true })
  await files.focus()
  await files.press('ArrowRight')
  await expect(questions).toBeFocused()
  await expect(questions).toHaveAttribute('aria-selected', 'true')
  await page.getByLabel('Sample search text').fill('Retained search text')
  await questions.focus()
  await questions.press('End')
  await expect(page.getByRole('tab', { name: 'Quiz', exact: true })).toBeFocused()
  await page.getByRole('radio').nth(1).check()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).focus()
  await page.keyboard.press('Home')
  await expect(files).toBeFocused()
  await expect(draft).toHaveValue('A local draft\na')
  expect(await source.evaluate((element) => element.scrollTop)).toBeCloseTo(scrollTop, 0)
  await questions.click()
  await expect(page.getByLabel('Sample search text')).toHaveValue('Retained search text')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await expect(page.locator('[role="tab"][tabindex="0"]')).toHaveCount(1)
})

test('unfinished actions are labeled, focusable and do not initiate requests', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => {
    if (['fetch', 'xhr'].includes(request.resourceType())) requests.push(request.url())
  })
  const ask = page.getByRole('button', { name: 'Ask agent — unavailable' })
  await expect(ask).toBeDisabled()
  await ask.focus()
  await expect(ask).toBeFocused()
  await expect(ask).toHaveCSS('outline-width', '2px')
  await expect(ask).toHaveAccessibleDescription(/Agent requests are not implemented/)
  await ask.press('Enter')
  await expect(page.getByRole('complementary', { name: 'Sample conversation' })).not.toBeVisible()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await expect(page.getByRole('button', { name: 'New question — unavailable' })).toBeDisabled()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'Check answer — unavailable' }),
  ).toHaveAccessibleDescription(/not implemented/)
  expect(requests).toEqual([])
})

test('file drawer is recoverable at 1180px and restores focus on Escape', async ({ page }) => {
  await page.setViewportSize({ width: 1180, height: 1080 })
  const rail = page.getByRole('complementary', { name: 'Changed files' })
  const trigger = page.getByRole('button', { name: 'Files', exact: true })
  await expect(rail).not.toBeVisible()
  await trigger.click()
  await expect(rail).toBeVisible()
  await expect(rail).toBeFocused()
  await expect(trigger).toHaveAttribute('aria-expanded', 'true')
  await rail.press('Escape')
  await expect(rail).not.toBeVisible()
  await expect(trigger).toBeFocused()
  await trigger.click()
  await page.getByRole('button', { name: 'src/auth/session-store.ts', exact: true }).click()
  await expect(trigger).toBeFocused()
  await expect(rail).not.toBeVisible()
  await expect(
    page.getByRole('heading', { name: 'src/auth/session-store.ts', exact: true }),
  ).toBeVisible()
  await page.setViewportSize({ width: 1440, height: 1080 })
  await expect(rail).toBeVisible()
  await expect(
    page.getByRole('button', { name: 'src/auth/session-store.ts', exact: true }),
  ).toHaveAttribute('aria-current', 'true')
})

for (const width of [900, 899]) {
  test(`conversation switches panes at ${width}px without losing draft or focus`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1080 })
    await page.getByLabel('Sample line question', { exact: true }).fill('Keep the inline draft')
    const trigger = page.getByRole('button', { name: 'Preview sample conversation' })
    await trigger.click()
    await expect(page.getByRole('main')).not.toBeVisible()
    await expect(
      page.getByRole('heading', { name: 'Sample conversation', exact: true }),
    ).toBeFocused()
    await page.getByLabel('Sample follow-up', { exact: true }).fill('Keep this follow-up too')
    await page.getByRole('button', { name: 'Back to workspace' }).click()
    await expect(trigger).toBeFocused()
    await expect(page.getByLabel('Sample line question', { exact: true })).toHaveValue(
      'Keep the inline draft',
    )
    await trigger.click()
    await expect(page.getByLabel('Sample follow-up', { exact: true })).toHaveValue(
      'Keep this follow-up too',
    )
    expect(
      await page.evaluate(
        () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
      ),
    ).toBe(true)
    await page.screenshot({
      path: testInfo.outputPath(`conversation-${width}.png`),
      fullPage: true,
    })
  })
}

test('code and conversation keep independent, keyboard-accessible native scrolling', async ({
  page,
}) => {
  await page.getByRole('button', { name: 'Preview sample conversation' }).click()
  const source = page.getByRole('region', { name: 'Sample code diff', exact: true })
  const messages = page.getByRole('region', { name: 'Sample conversation messages' })
  await source.evaluate((element) => {
    element.scrollTop = 0
  })
  await messages.focus()
  await messages.press('PageDown')
  await expect.poll(() => messages.evaluate((element) => element.scrollTop)).toBeGreaterThan(0)
  expect(await source.evaluate((element) => element.scrollTop)).toBe(0)
  const lines = page.getByRole('region', { name: 'Sample source after draft' })
  await lines.focus()
  await lines.press('ArrowRight')
  await expect.poll(() => lines.evaluate((element) => element.scrollLeft)).toBeGreaterThan(0)
  const text = await lines
    .locator('code')
    .first()
    .evaluate((element) => {
      const range = document.createRange()
      range.selectNodeContents(element)
      const selection = window.getSelection()!
      selection.removeAllRanges()
      selection.addRange(range)
      return selection.toString()
    })
  expect(text).toBe('  }).finally(() => {')
})

test('200% text scaling reflows the shell instead of clipping controls', async ({
  page,
}, testInfo) => {
  await page.evaluate(() => {
    document.documentElement.style.fontSize = '200%'
  })
  await expect(page.locator('body')).toHaveCSS('font-size', '26px')
  await expect(page.getByRole('button', { name: 'Files', exact: true })).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true)
  const trigger = page.getByRole('button', { name: 'Preview sample conversation' })
  await trigger.click()
  await expect(page.getByRole('button', { name: 'Back to workspace' })).toBeVisible()
  await expect(page.getByRole('main')).not.toBeVisible()
  await expect(page.getByRole('region', { name: 'Sample conversation messages' })).toBeVisible()
  await page.getByLabel('Sample follow-up', { exact: true }).fill('Still reachable at 200%')
  const send = page.getByRole('button', { name: 'Send — unavailable' })
  await send.scrollIntoViewIfNeeded()
  const paneBounds = await page.getByRole('complementary', { name: 'Sample conversation' }).boundingBox()
  const sendBounds = await send.boundingBox()
  expect(sendBounds!.y + sendBounds!.height).toBeLessThanOrEqual(paneBounds!.y + paneBounds!.height)
  await page.screenshot({ path: testInfo.outputPath('conversation-text-scale-200.png'), fullPage: true })
  await page.getByRole('button', { name: 'Back to workspace' }).click()
  await expect(trigger).toBeFocused()
  expect(
    await page.evaluate(() => {
      const fixtureHeight = document.querySelector('.fixture-page')!.getBoundingClientRect().height
      return document.documentElement.scrollHeight <= Math.ceil(fixtureHeight) + 1
    }),
  ).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('text-scale-200.png'), fullPage: true })
})

test('captures deterministic foundation previews without external or API traffic', async ({
  page,
}, testInfo) => {
  const unexpected: string[] = []
  page.on('request', (request) => {
    const url = new URL(request.url())
    if (url.origin !== 'http://127.0.0.1:5174' || url.pathname.startsWith('/api/'))
      unexpected.push(request.url())
  })
  // Reload so the network check includes the document, styles and bundled font.
  await page.reload()
  await page.evaluate(() => document.fonts.ready)
  await page.screenshot({ path: testInfo.outputPath('files-1440.png'), fullPage: true })
  await page.getByRole('button', { name: 'Preview sample conversation' }).click()
  await page.screenshot({ path: testInfo.outputPath('conversation-1440.png'), fullPage: true })
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('questions-1440.png'), fullPage: true })
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('quiz-1440.png'), fullPage: true })
  expect(unexpected).toEqual([])
})
