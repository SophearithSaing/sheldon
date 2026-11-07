import { test } from '@playwright/test'
import { semanticFlow } from './semantics/flow'

test('development semantic review uses actual Git/API/storage and separate synthetic native context', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await semanticFlow(page, context, info, true)
})
