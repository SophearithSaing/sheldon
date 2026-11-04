import { test } from '@playwright/test'
import { questionCrashFlow, questionFlow, questionReopenFlow } from './questions/flow'

test('clean-source release runs real Git and synthetic native questions without Node at application runtime', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await questionFlow(page, context, info, false)
})

for (const loss of ['source', 'native', 'busy'] as const) test(`clean release crash keeps history readable with ${loss} original state unavailable`, async ({ page }, info): Promise<void> => {
  test.setTimeout(120000)
  await questionCrashFlow(page, info, false, loss)
})

test('clean-source release continues only the original saved native session after restart', async ({ page }): Promise<void> => {
  test.setTimeout(120000)
  await questionReopenFlow(page, false)
})
