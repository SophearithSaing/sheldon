import { expect, test } from '@playwright/test'
import { openSampleThread } from './preview'

test.beforeEach(async ({ page }) => {
  await page.goto('/')
  await page.evaluate(() => document.fonts.ready)
})

test('keyboard users can skip directly to the labeled workspace content', async ({ page }) => {
  await page.keyboard.press('Tab')
  const skip = page.getByRole('link', { name: 'Skip to content', exact: true })
  await expect(skip).toBeFocused()
  await expect(skip).toBeInViewport()
  await skip.press('Enter')
  await expect(page.getByRole('main')).toBeFocused()
})

test('uses local Slate typography, foundation geometry and bundled licenses', async ({ page }) => {
  await expect(page.getByText('UI preview', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'UI preview', exact: true }).click()
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
      sansLoaded: document.fonts.check('400 13px "Sheldon Slate Sans"'),
    }
  })
  expect(typography).toMatchObject({
    size: '13px',
    background: 'rgb(22, 26, 34)',
    ligatures: 'none',
    synthesis: 'none',
    loaded: true,
    sansLoaded: true,
  })
  expect(typography.family).toContain('Sheldon Slate Sans')
  await expect(page.getByRole('heading', { level: 1 })).toHaveCSS('font-size', '21px')
  await expect(page.locator('.diff-table').first()).toHaveCSS('font-size', '12px')
  await expect(page.locator('.diff-table').first()).toHaveCSS('line-height', '25px')
  await expect(page.locator('.diff-table').first()).toHaveCSS(
    'font-family',
    '"Sheldon JetBrains Mono", monospace',
  )
  await expect(page.locator('.line-number').first()).toHaveCSS('font-size', '11px')
  const rail = await page.getByRole('complementary', { name: 'Changed files' }).boundingBox()
  expect(rail?.width).toBeCloseTo(240, 0)
  await openSampleThread(page)
  const conversation = await page
    .getByRole('complementary', { name: 'Question 01', exact: true })
    .boundingBox()
  expect(conversation?.width).toBeCloseTo(440, 0)
  await expect(page.locator('.sample-explanation:visible')).toHaveCSS('font-size', '14px')
  await expect(page.locator('.sample-explanation:visible')).toHaveCSS('line-height', '24.5px')
  await expect(page.locator('.sample-prompt:visible')).toHaveCSS('font-size', '15px')
  await expect(page.locator('.sample-prompt:visible')).toHaveCSS('line-height', '24.75px')
  const license = await page
    .getByRole('link', { name: 'JetBrains Mono license' })
    .getAttribute('href')
  expect(license).toBeTruthy()
  const response = await page.request.get(license!)
  expect(await response.text()).toContain('SIL OPEN FONT LICENSE Version 1.1')
  const sansLicense = await page.getByRole('link', { name: 'DM Sans license' }).getAttribute('href')
  expect(sansLicense).toBeTruthy()
  expect(await (await page.request.get(sansLicense!)).text()).toContain('SIL OPEN FONT LICENSE')
})

test('tabs support roving focus and retain editable drafts and scroll position', async ({
  page,
}) => {
  const draft = page.getByRole('textbox', { name: 'Your question', exact: true })
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
  await page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' }).fill('Retained search text')
  await questions.focus()
  await questions.press('End')
  await expect(page.getByRole('tab', { name: 'Semantic review', exact: true })).toBeFocused()
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).press('ArrowLeft')
  await expect(page.getByRole('tab', { name: 'Quiz', exact: true })).toBeFocused()
  await page.getByRole('radio').nth(1).check()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).focus()
  await page.keyboard.press('Home')
  await expect(files).toBeFocused()
  await expect(draft).toHaveValue('A local draft\na')
  expect(await source.evaluate((element) => element.scrollTop)).toBeCloseTo(scrollTop, 0)
  await questions.click()
  await expect(page.getByRole('searchbox', { name: 'Search sample questions, answers, or files' })).toHaveValue('Retained search text')
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(page.getByRole('radio').nth(1)).toBeChecked()
  await expect(page.locator('[role="tab"][tabindex="0"]:visible')).toHaveCount(1)
})

test('unfinished actions are labeled, focusable and do not initiate requests', async ({ page }) => {
  const requests: string[] = []
  page.on('request', (request) => {
    if (['fetch', 'xhr'].includes(request.resourceType())) requests.push(request.url())
  })
  const ask = page.getByRole('button', { name: 'Split — unavailable' })
  await expect(ask).toBeDisabled()
  await ask.focus()
  await expect(ask).toBeFocused()
  await expect(ask).toHaveCSS('outline-width', '2px')
  await expect(ask).toHaveAccessibleDescription(/Split display is not implemented/)
  await ask.press('Enter')
  await expect(page.getByRole('complementary', { name: 'Sample conversation' })).not.toBeVisible()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await expect(page.getByRole('region', { name: 'Questions in this PR', exact: true }).getByText(/Whole-PR question creation is unavailable/)).toBeVisible()
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await expect(
    page.getByRole('button', { name: 'New quiz — unavailable' }),
  ).toHaveAccessibleDescription(/not implemented/)
  expect(requests).toEqual([])
})

test('file disclosure stays available at 1180px and preserves state on collapse', async ({ page }) => {
  await page.setViewportSize({ width: 1180, height: 1080 })
  const rail = page.getByRole('complementary', { name: 'Changed files' })
  const trigger = rail.locator('summary')
  await expect(rail).toBeVisible()
  await page.getByRole('searchbox', { name: 'Find a sample file', exact: true }).fill('session')
  await trigger.focus()
  await rail.press('Escape')
  await expect(rail.locator('details')).not.toHaveAttribute('open')
  await expect(rail).toBeVisible()
  await expect(trigger).toBeFocused()
  await trigger.click()
  await expect(page.getByRole('searchbox', { name: 'Find a sample file', exact: true })).toHaveValue('session')
  await page.getByRole('button', { name: 'src/auth/session-store.ts', exact: true }).click()
  await expect(rail.locator('details')).toHaveAttribute('open', '')
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
  test(`conversation stacks with source and files at ${width}px without losing state`, async ({
    page,
  }, testInfo) => {
    await page.setViewportSize({ width, height: 1080 })
    await openSampleThread(page)
    await page.getByRole('button', { name: 'Ask about selected lines', exact: true }).click()
    await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('Keep the inline draft')
    const trigger = page.getByRole('button', { name: 'Open sample question 1', exact: true })
    await expect(page.getByRole('main')).toBeVisible()
    await expect(page.getByRole('complementary', { name: 'Changed files' })).toBeVisible()
    await expect(
      page.getByRole('heading', { name: 'Question 01', exact: true }),
    ).toBeVisible()
    await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Keep this follow-up too')
    await page.getByRole('button', { name: 'Back to workspace' }).click()
    await expect(trigger).toBeFocused()
    await expect(page.getByRole('textbox', { name: 'Your question', exact: true })).toHaveValue(
      'Keep the inline draft',
    )
    await trigger.click()
    await expect(page.getByRole('textbox', { name: 'Follow up in this thread', exact: true })).toHaveValue(
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
  await openSampleThread(page)
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
  await expect(page.getByRole('complementary', { name: 'Changed files' }).locator('summary')).toBeVisible()
  expect(
    await page.evaluate(
      () => document.documentElement.scrollWidth <= document.documentElement.clientWidth,
    ),
  ).toBe(true)
  await openSampleThread(page)
  const trigger = page.getByRole('button', { name: 'Open sample question 1', exact: true })
  await expect(page.getByRole('button', { name: 'Back to workspace' })).toBeVisible()
  await expect(page.getByRole('main')).toBeVisible()
  await expect(page.getByRole('region', { name: 'Sample conversation messages' })).toBeVisible()
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Still reachable at 200%')
  const send = page.getByRole('button', { name: 'Send — unavailable' })
  await send.scrollIntoViewIfNeeded()
  const sendBounds = await send.boundingBox()
  expect(sendBounds!.height).toBeGreaterThanOrEqual(44)
  await expect(send).toBeInViewport()
  await page.screenshot({ path: testInfo.outputPath('conversation-text-scale-200.png'), fullPage: true })
  await page.getByRole('button', { name: 'Back to workspace' }).click()
  await expect(trigger).toBeFocused()
  expect(
    await page.evaluate(() => {
      const fixtureHeight = document.querySelector('.fixture-application')!.getBoundingClientRect().height
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
  await openSampleThread(page)
  await page.screenshot({ path: testInfo.outputPath('conversation-1440.png'), fullPage: true })
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('questions-1440.png'), fullPage: true })
  await page.getByRole('tab', { name: 'Quiz', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('quiz-1440.png'), fullPage: true })
  expect(unexpected).toEqual([])
})
