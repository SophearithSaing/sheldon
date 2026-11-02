import { expect, test, type Page } from '@playwright/test'
import { openSampleThread } from './preview'

/**
 * Checks the document fits while allowing bounded source and workspace-link scrolling.
 * @param page Browser page under inspection.
 */
async function expectDocumentFits(page: Page): Promise<void> {
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
}

/**
 * Opens the isolated preview and waits for its local fonts.
 * @param fixtures Playwright page fixture.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/')
  await page.evaluate((): Promise<FontFaceSet> => document.fonts.ready)
})

/**
 * Checks every tab controls its uniquely named panel and retains keyboard focus on activation.
 * @param fixtures Playwright page fixture.
 */
test('four content tabs expose linked panels with no background focus theft', async ({ page }): Promise<void> => {
  const tabs = page.getByRole('tab')
  await expect(tabs).toHaveCount(4)
  for (const tab of await tabs.all()) {
    await tab.focus()
    await tab.press('Enter')
    await expect(tab).toBeFocused()
    await expect(tab).toHaveAttribute('aria-selected', 'true')
    const panelId = await tab.getAttribute('aria-controls')
    const panel = page.locator(`[id="${panelId}"]`)
    await expect(panel).toHaveAttribute('role', 'tabpanel')
    await expect(panel).toHaveAttribute('aria-labelledby', (await tab.getAttribute('id'))!)
    await expect(panel).toHaveAccessibleName((await tab.innerText()).replace(/\s+\d+$/, ''))
    await expect(page.getByRole('tabpanel')).toHaveCount(1)
  }
  await page.getByRole('button', { name: 'Preview semantic generation', exact: true }).click()
  const finish = page.getByRole('button', { name: 'Finish sample grouping', exact: true })
  await finish.focus()
  await page.clock.install()
  await page.clock.runFor(60000)
  await expect(finish).toBeFocused()
  await expect(page.locator('article.semantic-hunk:visible')).toHaveCount(0)
})

/**
 * Proves pane reflow follows the workspace container rather than the desktop viewport.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output location.
 */
test('a narrow container inside a desktop viewport keeps source files and conversation reachable', async ({ page }, testInfo): Promise<void> => {
  await openSampleThread(page)
  await page.locator('.workspace-container:visible').evaluate((element): void => { (element as HTMLElement).style.width = '899px' })
  expect(page.viewportSize()!.width).toBe(1440)
  const files = page.getByRole('complementary', { name: 'Changed files', exact: true })
  const source = page.getByRole('main')
  const panel = page.getByRole('complementary', { name: 'Question 01', exact: true })
  expect((await source.boundingBox())!.y).toBeGreaterThan((await files.boundingBox())!.y)
  expect((await panel.boundingBox())!.y).toBeGreaterThan((await source.boundingBox())!.y)
  const fileList = files.getByRole('region', { name: 'Changed file list', exact: true })
  const fileBounds = (await files.boundingBox())!
  const listBounds = (await fileList.boundingBox())!
  expect(listBounds.y + listBounds.height).toBeLessThanOrEqual(fileBounds.y + fileBounds.height)
  expect(await fileList.evaluate((element): boolean => element.scrollHeight > element.clientHeight)).toBe(true)
  const panes = page.getByRole('region', { name: 'Review panes', exact: true })
  await page.keyboard.press('Tab')
  await panes.focus()
  await expect(panes).toHaveCSS('outline-width', '2px')
  await expect(panes).toHaveCSS('outline-color', 'rgb(138, 180, 255)')
  await panes.press('PageDown')
  await expect.poll((): Promise<number> => panes.evaluate((element): number => element.scrollTop)).toBeGreaterThan(0)
  await page.getByRole('textbox', { name: 'Follow up in this thread', exact: true }).fill('Container-local draft')
  await page.getByRole('button', { name: 'Back to workspace', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Open sample question 1', exact: true })).toBeFocused()
  await expect(source).toBeInViewport()
  await expectDocumentFits(page)
  await page.screenshot({ path: testInfo.outputPath('container-899-in-1440.png') })
})

/**
 * Checks status-card layout responds to its own available width with the conversation retained.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output location.
 */
test('question cards reflow by panel width rather than viewport width', async ({ page }, testInfo): Promise<void> => {
  await page.goto('/?sample=review')
  await page.getByRole('tab', { name: 'Questions', exact: true }).click()
  await page.locator('.workspace-container:visible').evaluate((element): void => { (element as HTMLElement).style.width = '1100px' })
  const card = page.getByRole('button', { name: /^Open question 01:/ })
  await expect(card.locator('.ui-badge')).toHaveCSS('grid-column-start', '2')
  await page.keyboard.press('Tab')
  await card.focus()
  await expect(card).toHaveCSS('outline-width', '2px')
  await expectDocumentFits(page)
  await page.screenshot({ path: testInfo.outputPath('questions-container-1100.png') })
  await page.getByRole('button', { name: 'Close conversation', exact: true }).click()
  await expect(card.locator('.ui-badge')).toHaveCSS('grid-column-start', 'auto')
})

/**
 * Associates actual composer validation and manual pending feedback with the field and actions.
 * @param fixtures Playwright page fixture.
 */
test('composer errors and pending feedback have accessible descriptions and retained focus', async ({ page }): Promise<void> => {
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await input.fill('')
  await input.press('Control+Enter')
  await expect(input).toBeFocused()
  await expect(input).toHaveAttribute('aria-invalid', 'true')
  await expect(input).toHaveAccessibleDescription(/Enter a question/)
  await page.getByText('Sample preview controls', { exact: true }).filter({ visible: true }).click()
  await page.getByLabel('Preview creation outcome', { exact: true }).filter({ visible: true }).selectOption('pending')
  await input.fill('A manually pending sample')
  await input.press('Control+Enter')
  await expect(input).toBeFocused()
  await expect(input).toHaveAttribute('readonly', '')
  await expect(input).toHaveAccessibleDescription(/Sample creation is pending.*finish or cancel/)
  const pending = page.getByRole('button', { name: 'Sample creation pending…', exact: true })
  await expect(pending).toHaveAttribute('aria-busy', 'true')
  await expect(pending).toHaveAccessibleDescription(/Nothing is sent/)
  await expect(page.getByRole('button', { name: 'Finish sample creation', exact: true })).toHaveAccessibleDescription(/finish or cancel/)
})

/**
 * Connects shared-session and outdated reasons to the group draft without stealing editable focus.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output location.
 */
test('busy and outdated group reasons describe both the draft and unavailable creation', async ({ page }, testInfo): Promise<void> => {
  await page.goto('/?sample=review')
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await page.getByRole('button', { name: 'Preview semantic generation', exact: true }).click()
  await page.getByRole('button', { name: 'Finish sample grouping', exact: true }).click()
  await page.getByRole('button', { name: 'Ask about this group', exact: true }).click()
  const input = page.getByRole('textbox', { name: 'Your question', exact: true })
  await expect(input).toBeFocused()
  await expect(input).toHaveAccessibleDescription(/Another sample question is investigating/)
  const create = page.getByRole('button', { name: 'Preview thread creation', exact: true })
  await expect(create).toHaveAccessibleDescription(/Creation is unavailable/)
  await page.getByText('Semantic preview controls', { exact: true }).filter({ visible: true }).click()
  await page.getByRole('checkbox', { name: 'Preview groups outdated', exact: true }).check()
  await expect(input).toHaveAccessibleDescription(/Outdated grouping preview/)
  await expect(create).toHaveAccessibleDescription(/Outdated grouping preview/)
  await page.getByRole('heading', { name: 'Outdated grouping · preview', exact: true }).scrollIntoViewIfNeeded()
  await page.screenshot({ path: testInfo.outputPath('semantic-outdated-feedback-1440.png') })
})

/**
 * Stresses real user text and test-only long path labels without adding application test hooks.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output location.
 */
test('long question text and path labels remain bounded at narrow and scaled sizes', async ({ page }, testInfo): Promise<void> => {
  const question = `Explain this boundary: ${'unbroken_identifier_'.repeat(24)}`
  await page.getByRole('textbox', { name: 'Your question', exact: true }).fill(question)
  await page.getByRole('button', { name: 'Preview thread creation', exact: true }).click()
  const path = `src/${'deeply-nested-package/'.repeat(12)}refresh-session.ts`
  await page.locator('.thread-source strong:visible').evaluate((element, label): void => { element.textContent = label }, path)
  for (const scaled of [false, true]) {
    await page.setViewportSize({ width: scaled ? 1440 : 899, height: 1080 })
    await page.evaluate((large): void => { document.documentElement.style.fontSize = large ? '200%' : '100%' }, scaled)
    const messages = page.getByRole('region', { name: 'Sample conversation messages', exact: true })
    await page.keyboard.press('Tab')
    await messages.focus()
    await expect(messages).toHaveCSS('outline-width', '2px')
    await expect(messages).toHaveCSS('overflow-y', 'auto')
    await expect(messages).toContainText(question)
    expect(await messages.evaluate((element): boolean => element.scrollWidth <= element.clientWidth)).toBe(true)
    await expectDocumentFits(page)
    await page.screenshot({ path: testInfo.outputPath(scaled ? 'long-content-text-scale-200.png' : 'long-content-899.png') })
    const send = page.getByRole('button', { name: 'Send — unavailable', exact: true })
    await send.focus()
    await expect(send).toBeInViewport()
    await expect(send).toHaveAccessibleDescription(/No harness is connected/)
  }
})
