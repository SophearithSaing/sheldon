import { test } from '@playwright/test'
import { quizFlow } from './quizzes/flow'

test('clean release quiz generates, checks and reopens actual saved sets without Node or application mocks', async ({ page, context }, info): Promise<void> => {
  test.setTimeout(120000)
  await quizFlow(page, context, info, false)
})
