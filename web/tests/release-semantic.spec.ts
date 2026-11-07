import { test } from '@playwright/test'
import { semanticFlow } from './semantics/flow'

test('clean release semantic review saves and reopens source-free without application HTTP mocks', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await semanticFlow(page, context, info, false)
})
