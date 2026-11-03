import type { Page } from '@playwright/test'

/**
 * Retains real Chromium response bodies for integration assertions without intercepting requests.
 * @param page Chromium page whose finite responses will be inspected.
 */
export async function preserveResponseBodies(page: Page): Promise<void> {
  const cdp = await page.context().newCDPSession(page)
  // Chromium can discard renderer-owned bodies before Playwright reads them on CPU-limited CI.
  await cdp.send('Network.enable', { maxTotalBufferSize: 64 * 1024 * 1024, maxResourceBufferSize: 32 * 1024 * 1024, enableDurableMessages: true })
}
