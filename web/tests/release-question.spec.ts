import { test } from '@playwright/test'
import { questionFlow, questionReopenFlow } from './questions/flow'

test('clean-source release runs real Git and synthetic native questions without Node at application runtime', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await questionFlow(page, context, info, false)
})

test('clean-source release continues only the original saved native session after restart', async ({ page }): Promise<void> => {
  test.setTimeout(120000)
  await questionReopenFlow(page, false)
})
