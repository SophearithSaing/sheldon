import { test } from '@playwright/test'
import { quizFlow } from './quizzes/flow'

test('development quiz uses actual Git, protected APIs, private saves and source-free restart without application mocks', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await quizFlow(page, context, info, true)
})
