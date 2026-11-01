import { expect, type Page } from '@playwright/test'

/**
 * Creates one local sample thread, or reopens its existing marker.
 * @param page Browser page showing the first-use sample workspace.
 */
export async function openSampleThread(page: Page): Promise<void> {
  const marker = page.getByRole('button', { name: 'Open sample question 1', exact: true })
  if (await marker.isVisible()) {
    await marker.click()
  } else {
    await page.getByRole('textbox', { name: 'Your question', exact: true }).fill('What stops an old refresh from restoring a logged-out session?')
    await page.getByRole('button', { name: 'Preview thread creation', exact: true }).click()
  }
  await expect(page.getByRole('complementary', { name: 'Question 01', exact: true })).toBeVisible()
}
