import { test } from '@playwright/test'
import { questionCrashFlow, questionFlow, questionReopenFlow } from './questions/flow'

test('development proxy runs the actual managed-source and offline native-question flow', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await questionFlow(page, context, info, true)
})

for (const loss of ['source', 'native', 'busy'] as const) test(`development crash keeps history readable with ${loss} original state unavailable`, async ({ page }, info): Promise<void> => {
  test.setTimeout(120000)
  await questionCrashFlow(page, info, true, loss)
})

test('development continuation reopens the original saved native session without replay', async ({ page }): Promise<void> => {
  test.setTimeout(120000)
  await questionReopenFlow(page, true)
})
