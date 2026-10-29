import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'

test('first-launch fixture requires a choice and confirmation but never requires a Test answer', async ({
  page,
}, testInfo) => {
  const requests: string[] = []
  page.on('request', (request) => {
    if (['fetch', 'xhr'].includes(request.resourceType())) requests.push(request.url())
  })
  await page.goto('/?fixture=settings')
  await expect(page.getByRole('heading', { name: 'First-launch setup (scaffold)' })).toBeVisible()
  const harness = page.getByLabel('Default harness for new conversations')
  await expect(harness).toHaveValue('')
  const confirm = page.getByRole('button', { name: 'Confirm fixture preferences' })
  await expect(confirm).toBeDisabled()
  await harness.selectOption('opencode')
  await expect(page.getByLabel('Configured/default model')).toHaveText(
    'fixture-provider · fixture-default-a',
  )
  await expect(page.getByLabel('Use harness default', { exact: true })).toBeChecked()
  await expect(confirm).toBeDisabled()
  await page.getByLabel('I confirm this harness/model choice for new conversations only.').check()
  await expect(
    page.getByLabel('I understand a real Test answer may consume quota.'),
  ).not.toBeChecked()
  await confirm.click()
  await expect(
    page.getByRole('heading', { name: 'Settings & Environment', exact: true }),
  ).toBeVisible()
  await expect(
    page.getByRole('status').filter({ hasText: 'Fixture confirmation only' }),
  ).toContainText('not saved')
  await expect(page.getByLabel('New conversation preference preview')).toContainText(
    'OpenCode · harness default',
  )
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0)
  expect(requests).toEqual([])
  await page.screenshot({ path: testInfo.outputPath('settings-first-launch.png'), fullPage: true })
  await page.reload()
  await expect(harness).toHaveValue('')
  await expect(page.getByRole('heading', { name: 'First-launch setup (scaffold)' })).toBeVisible()
})

test('per-harness model preferences never rebind the existing fixture session or convert native IDs', async ({
  page,
}) => {
  await page.goto('/?fixture=settings&scenario=saved')
  const binding = page.getByRole('complementary', { name: 'Existing conversation binding' })
  const original = await binding.textContent()
  const harness = page.getByLabel('Default harness for new conversations')
  await expect(page.getByLabel('Native model ID', { exact: true })).toHaveValue('fixture-model-b')
  await harness.selectOption('codex')
  await page.getByLabel('Explicit native model', { exact: true }).check()
  await page.getByLabel('Native model ID', { exact: true }).fill('fixture/codex-native-id')
  await expect(page.getByLabel('Native provider ID', { exact: true })).toHaveCount(0)
  await harness.selectOption('opencode')
  await expect(page.getByLabel('Native model ID', { exact: true })).toHaveValue('fixture-model-b')
  await expect(page.getByLabel('Native provider ID', { exact: true })).toHaveValue(
    'fixture-provider',
  )
  await harness.selectOption('codex')
  await expect(page.getByLabel('Native model ID', { exact: true })).toHaveValue(
    'fixture/codex-native-id',
  )
  // Editing a draft does not apply even to the new-conversation preview until explicit confirmation.
  await expect(page.getByLabel('New conversation preference preview')).toContainText('OpenCode')
  await page.getByLabel('I confirm this harness/model choice for new conversations only.').check()
  await page.getByRole('button', { name: 'Confirm fixture preferences' }).click()
  await expect(page.getByLabel('New conversation preference preview')).toContainText(
    'Codex · fixture/codex-native-id',
  )
  expect(await binding.textContent()).toBe(original)
  await expect(binding).toContainText('fixture-existing-session')
  await expect(binding).toContainText('fixture-default-a')
  await page.getByLabel('Fixture scenario').selectOption('changed-default')
  await expect(page.getByLabel('Use harness default', { exact: true })).toBeChecked()
  await expect(page.getByLabel('Configured/default model')).toHaveText(
    'fixture-provider · fixture-default-after-change',
  )
  await expect(binding).toContainText('fixture-default-a')
})

test('missing harnesses, unknown catalogs and unsupported existing overrides never silently fall back', async ({
  page,
}) => {
  await page.goto('/?fixture=settings&scenario=no-harness')
  await expect(page.getByRole('status')).toContainText('No harness detected')
  await page.getByLabel('Default harness for new conversations').selectOption('opencode')
  await page.getByLabel('I confirm this harness/model choice for new conversations only.').check()
  await expect(page.getByRole('button', { name: 'Confirm fixture preferences' })).toBeDisabled()
  await expect(page.getByRole('link', { name: 'Return to sample workspace' })).toBeVisible()
  await page.getByLabel('Fixture scenario').selectOption('unknown-models')
  await expect(page.getByLabel('Configured/default model')).toHaveText('Unknown — not resolved')
  await expect(page.getByText('Catalog unknown, not empty.', { exact: false })).toBeVisible()
  await page.getByLabel('Explicit native model', { exact: true }).check()
  await page.getByLabel('Native provider ID', { exact: true }).fill('fixture-provider')
  await page.getByLabel('Native model ID', { exact: true }).fill('fixture-manual-id')
  await page.getByLabel('I confirm this harness/model choice for new conversations only.').check()
  await page.getByRole('button', { name: 'Confirm fixture preferences' }).click()
  await expect(page.getByLabel('New conversation preference preview')).toContainText(
    'fixture-provider · fixture-manual-id',
  )
  await page.getByLabel('I understand a real Test answer may consume quota.').check()
  await page.getByRole('button', { name: 'Simulate fixture Test answer' }).click()
  await expect(page.getByRole('status', { name: 'Test answer state' })).toContainText('failed')
  await expect(page.getByRole('status', { name: 'Test answer state' })).toContainText('No fallback')
  await page.getByLabel('Fixture scenario').selectOption('unsupported-override')
  await expect(page.getByLabel('Explicit native model', { exact: true })).toBeChecked()
  await expect(page.getByLabel('Native model ID', { exact: true })).toHaveValue('fixture-model-b')
  await expect(page.getByLabel('Native model ID', { exact: true })).toBeDisabled()
  await expect(page.getByRole('button', { name: 'Confirm fixture preferences' })).toBeDisabled()
})

test('fixture test states are quota-gated, independent and explicitly simulated', async ({
  page,
}) => {
  await page.goto('/?fixture=settings')
  await page.getByLabel('Default harness for new conversations').selectOption('opencode')
  const run = page.getByRole('button', { name: 'Simulate fixture Test answer' })
  await expect(run).toBeDisabled()
  await expect(
    page.getByText('may consume subscription/provider quota.', { exact: false }),
  ).toBeVisible()
  await page.getByLabel('I understand a real Test answer may consume quota.').check()
  await run.click()
  await expect(page.getByRole('status', { name: 'Test answer state' })).toContainText('running')
  await page.getByRole('button', { name: 'Stop fixture test' }).click()
  await expect(page.getByRole('status', { name: 'Test answer state' })).toContainText('cancelled')
  for (const status of ['pending', 'running', 'succeeded', 'failed', 'cancelled', 'interrupted']) {
    await page.getByLabel('Fixture scenario').selectOption('test-' + status)
    await expect(page.getByRole('status', { name: 'Test answer state' })).toContainText(
      'Fixture test: ' + status,
    )
  }
  await expect(
    page.getByRole('complementary', { name: 'Existing conversation binding' }),
  ).toContainText('fixture-existing-session')
})

test('invalid model shapes block confirmation and neither testing nor selection happens automatically', async ({
  page,
}) => {
  const api: string[] = []
  page.on('request', (request) => {
    if (request.url().includes('/api/')) api.push(request.url())
  })
  await page.goto('/?fixture=settings')
  await page.getByLabel('Default harness for new conversations').selectOption('opencode')
  await page.getByLabel('Explicit native model', { exact: true }).check()
  await page.getByLabel('Native model ID', { exact: true }).fill('fixture-provider/fixture-model')
  await page.getByLabel('I confirm this harness/model choice for new conversations only.').check()
  await expect(page.getByRole('alert')).toContainText(
    'requires its existing native provider ID separately',
  )
  await expect(page.getByRole('button', { name: 'Confirm fixture preferences' })).toBeDisabled()
  await page.getByLabel('Native provider ID', { exact: true }).fill('fixture-provider')
  await page
    .getByLabel('Native model ID', { exact: true })
    .fill('https://user:secret@example.invalid')
  await expect(page.getByRole('alert')).toContainText('native model ID')
  await expect(page.getByRole('button', { name: 'Confirm fixture preferences' })).toBeDisabled()
  expect(api).toEqual([])
})

test('live settings metadata stays unavailable and causes no CLI, persistence or inference calls', async ({
  page,
  request,
}) => {
  const { token }: { token: string } = JSON.parse(
    await readFile(process.env.SHELDON_TEST_STATE!, 'utf8'),
  )
  const requests: string[] = []
  page.on('request', (item) => requests.push(item.method() + ' ' + item.url()))
  await page.goto(`http://127.0.0.1:5175/#token=${token}`)
  await expect(
    page.getByRole('button', { name: 'Settings & Environment', exact: true }),
  ).toBeVisible()
  expect(requests.some((item) => item.includes('/api/settings'))).toBe(false)
  await page.getByRole('button', { name: 'Settings & Environment', exact: true }).click()
  const panel = page.getByRole('region', { name: 'Settings & Environment', exact: true })
  await expect(panel).toContainText('Setup scaffold only')
  await panel.getByLabel('Default harness for new conversations').selectOption('opencode')
  await expect(
    panel.getByText('Explicit native model selection is not validated', { exact: false }),
  ).toBeVisible()
  await expect(panel.getByLabel('Configured/default model')).toHaveText('Unknown — not resolved')
  await expect(panel.getByLabel('Explicit native model', { exact: true })).toBeDisabled()
  await expect(panel.getByRole('button', { name: 'Save settings — unavailable' })).toBeDisabled()
  await expect(panel.getByRole('button', { name: 'Test answer — unavailable' })).toBeDisabled()
  expect(
    requests.some((item) => item.includes('/api/environment') && !item.includes('/models')),
  ).toBe(false)
  expect(requests.some((item) => item.startsWith('PUT ') || item.startsWith('POST '))).toBe(false)
  expect(await page.content()).not.toContain(token)
  await page.getByRole('button', { name: 'Close settings — return later' }).click()
  await expect(panel).not.toBeVisible()
  const response = await request.put('http://127.0.0.1:5175/api/settings', {
    headers: { Authorization: `Bearer ${token}` },
    data: {
      schemaVersion: 1,
      confirm: true,
      defaultHarness: 'opencode',
      models: { opencode: { mode: 'default' } },
    },
  })
  expect(response.status()).toBe(501)
  const after = await request.get('http://127.0.0.1:5175/api/settings', {
    headers: { Authorization: `Bearer ${token}` },
  })
  expect((await after.json()).preferences.setupConfirmed).toBe(false)
  // Metadata endpoints do not invoke tool probes; this directory may contain prior explicit diagnostic scans only.
  for (const name of ['opencode', 'codex', 'claude']) {
    const log = await readFile(
      join(process.env.SHELDON_TEST_TOOL_LOG!, name + '.jsonl'),
      'utf8',
    ).catch(() => '')
    expect(log).not.toMatch(/test-runs|login|prompt|generate-json-schema/)
  }
})
