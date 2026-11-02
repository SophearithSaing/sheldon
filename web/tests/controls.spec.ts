import { expect, test } from '@playwright/test'

/**
 * Opens the fixture-only control gallery.
 * @param fixtures Playwright page fixture.
 */
test.beforeEach(async ({ page }): Promise<void> => {
  await page.goto('/?fixture=controls')
  await page.evaluate((): Promise<FontFaceSet> => document.fonts.ready)
})

/**
 * Checks focusable unavailable/busy buttons cannot activate or submit a form.
 * @param fixtures Playwright page fixture.
 */
test('unavailable and busy actions guard pointer, keyboard and form submission', async ({ page }): Promise<void> => {
  for (const name of ['Unavailable submit', 'Sending sample…']) {
    const button = page.getByRole('button', { name, exact: true })
    await expect(button).toBeDisabled()
    await expect(button).toHaveAttribute('type', 'button')
    await button.focus()
    await expect(button).toBeFocused()
    await expect(button).toHaveCSS('outline-width', '2px')
    await expect(button).toHaveCSS('outline-color', 'rgb(138, 180, 255)')
    await button.click({ force: true })
    await button.press('Enter')
    await button.press('Space')
    await button.evaluate((element): void => (element as HTMLButtonElement).click())
  }
  await expect(page.getByRole('button', { name: 'Unavailable submit' })).toHaveAccessibleDescription(/not implemented/)
  await expect(page.getByRole('button', { name: 'Sending sample…' })).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('button', { name: 'Sending sample…' })).toHaveAccessibleDescription(/shared PR-revision session/)
  await expect(page.getByRole('status')).toHaveText('Local actions: 0 · Form submissions: 0')
  await page.getByRole('button', { name: 'Toggle sample busy state' }).click()
  const ready = page.getByRole('button', { name: 'Ready sample' })
  await expect(ready).toBeEnabled()
  await expect(ready).not.toHaveAttribute('aria-busy')
  await ready.click()
  await expect(page.getByRole('status')).toHaveText('Local actions: 1 · Form submissions: 1')
})

/**
 * Checks Slate target sizes, selected styling and hover feedback.
 * @param fixtures Playwright page fixture.
 */
test('Slate controls expose readable hover, focus and selected states at usable sizes', async ({ page }): Promise<void> => {
  const secondary = page.getByRole('button', { name: 'Secondary sample' })
  await expect(secondary).toHaveCSS('font-size', '13px')
  await expect(secondary).toHaveCSS('border-radius', '8px')
  await expect(secondary).toHaveCSS('background-color', 'rgb(30, 35, 46)')
  await secondary.hover()
  await expect(secondary).toHaveCSS('background-color', 'rgb(22, 26, 34)')
  const selected = page.getByRole('button', { name: 'Selected sample' })
  await selected.click()
  await expect(selected).toHaveCSS('background-color', 'rgb(38, 50, 71)')
  await page.mouse.move(0, 0)
  await expect(selected).toHaveAttribute('aria-pressed', 'true')
  await expect(selected).toHaveCSS('background-color', 'rgb(38, 50, 71)')
  await expect(selected).toHaveCSS('color', 'rgb(138, 180, 255)')
  await expect(page.getByText('Failed', { exact: true })).toHaveCSS('color', 'rgb(232, 174, 181)')
  for (const control of await page.getByRole('button').all()) {
    const box = await control.boundingBox()
    expect(box!.height).toBeGreaterThanOrEqual(44)
    expect(box!.width).toBeGreaterThanOrEqual(44)
  }
})

/**
 * Checks validation feedback retains caller descriptions and editable values.
 * @param fixtures Playwright page fixture.
 */
test('fields associate hints, caller descriptions and dynamic text errors', async ({ page }): Promise<void> => {
  const question = page.getByRole('textbox', { name: 'Sample question', exact: true })
  await expect(question).toHaveAccessibleDescription(/Nothing is sent to an agent.*Local text only/)
  await page.getByRole('button', { name: 'Validate sample' }).click()
  await expect(question).toHaveAttribute('aria-invalid', 'true')
  await expect(question).toHaveAccessibleDescription(/Nothing is sent to an agent.*Local text only.*Error: enter a sample question/)
  await expect(question).toHaveCSS('border-color', 'rgb(232, 174, 181)')
  await question.fill('A local sample')
  await page.getByRole('button', { name: 'Validate sample' }).click()
  await expect(question).not.toHaveAttribute('aria-invalid')
  await expect(question).toHaveValue('A local sample')
  await expect(question).toHaveAccessibleDescription(/Nothing is sent to an agent.*Local text only/)
  const draft = page.getByRole('textbox', { name: 'Sample draft', exact: true })
  await expect(draft).toHaveAttribute('aria-invalid', 'true')
  await expect(draft).toHaveAccessibleDescription('Error: source is unavailable.')
  await draft.fill('Retained through an error')
  await expect(draft).toHaveValue('Retained through an error')
})

/**
 * Checks native select/radio keyboard behavior and unavailable field feedback.
 * @param fixtures Playwright page fixture.
 */
test('native model selects and radio choices keep their keyboard semantics', async ({ page }): Promise<void> => {
  const model = page.getByRole('combobox', { name: 'Sample harness model' })
  await model.focus()
  await expect(model).toHaveCSS('outline-width', '2px')
  await model.press('ArrowDown')
  await model.press('Enter')
  await expect(model).toHaveValue('a')
  expect((await model.boundingBox())!.height).toBeGreaterThanOrEqual(48)
  const unavailable = page.getByRole('combobox', { name: 'Unavailable models' })
  await expect(unavailable).toBeDisabled()
  await expect(unavailable).toHaveAccessibleDescription(/model discovery is not implemented/)
  await expect(unavailable).toHaveCSS('opacity', '1')
  await expect(page.getByRole('combobox', { name: 'Loading models' })).toHaveAttribute('aria-busy', 'true')
  await expect(page.getByRole('combobox', { name: 'Loading models' })).toBeDisabled()
  await expect(page.getByRole('combobox', { name: 'Invalid model' })).toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByRole('combobox', { name: 'Invalid model' })).toHaveAccessibleDescription(/choose an available replacement/)
  const answer = page.getByRole('radio', { name: 'Sample answer A' })
  await answer.focus()
  await answer.press('Space')
  await expect(answer).toBeChecked()
  await answer.press('ArrowDown')
  await expect(page.getByRole('radio', { name: 'Sample answer B' })).toBeChecked()
})

/**
 * Checks destination links stay distinct from roving in-page tabs and native scrolling.
 * @param fixtures Playwright page fixture.
 */
test('destination navigation, panel tabs and scroll regions keep distinct semantics', async ({ page }): Promise<void> => {
  const navigation = page.getByRole('navigation', { name: 'Sample destinations' })
  await expect(navigation.getByRole('tab')).toHaveCount(0)
  const library = navigation.getByRole('link', { name: 'All PRs preview' })
  await expect(library).toHaveAttribute('aria-current', 'page')
  await library.focus()
  await expect(library).toHaveCSS('outline-width', '2px')
  expect((await library.boundingBox())!.height).toBeGreaterThanOrEqual(44)
  await library.press('Tab')
  const config = navigation.getByRole('link', { name: 'Configuration preview' })
  await expect(config).toBeFocused()
  await config.press('Enter')
  await expect(config).toHaveAttribute('aria-current', 'page')
  await expect(library).not.toHaveAttribute('aria-current')
  await expect(page).toHaveURL(/\?fixture=controls$/)
  const first = page.getByRole('tab', { name: 'First panel' })
  await first.focus()
  await first.press('End')
  await expect(page.getByRole('tab', { name: 'Second panel' })).toBeFocused()
  await expect(page.getByRole('tabpanel')).toHaveText('Second panel content')
  await page.getByRole('tab', { name: 'Second panel' }).press('Home')
  await expect(first).toBeFocused()
  await expect(page.getByRole('tabpanel')).toHaveText('First panel content')
  const scroll = page.getByRole('region', { name: 'Sample scroll region' })
  await scroll.focus()
  await expect(scroll).toHaveCSS('outline-width', '2px')
  await scroll.press('PageDown')
  await expect.poll(async (): Promise<number> => scroll.evaluate((element): number => element.scrollTop)).toBeGreaterThan(0)
})

/**
 * Captures local control specimens at desktop, narrow and enlarged-text sizes.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output locations.
 */
test('control gallery stays local and reflows without clipping fields or feedback', async ({ page }, testInfo): Promise<void> => {
  const unexpected: string[] = []
  page.on('request', (request): void => {
    const url = new URL(request.url())
    if (url.origin !== 'http://127.0.0.1:5174' || url.pathname.startsWith('/api/')) unexpected.push(request.url())
  })
  await page.reload()
  await page.evaluate((): Promise<FontFaceSet> => document.fonts.ready)
  await page.getByRole('button', { name: 'Validate sample' }).click()
  await page.getByRole('button', { name: 'Selected sample' }).click()
  await page.screenshot({ path: testInfo.outputPath('controls-desktop.png'), fullPage: true })
  await page.setViewportSize({ width: 600, height: 900 })
  await page.screenshot({ path: testInfo.outputPath('controls-narrow.png'), fullPage: true })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  const question = page.getByRole('textbox', { name: 'Sample question', exact: true })
  await question.focus()
  await question.fill('Editable at 200%')
  await expect(question).toHaveValue('Editable at 200%')
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= document.documentElement.clientWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('controls-scaled.png'), fullPage: true })
  expect(unexpected).toEqual([])
})
