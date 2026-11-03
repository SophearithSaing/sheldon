import { test } from '@playwright/test'
import { questionFlow } from './questions/flow'

test('development proxy runs the actual managed-source and offline native-question flow', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await questionFlow(page, context, info, true)
})
