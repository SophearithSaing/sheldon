import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { join } from 'node:path'

test.use({ baseURL: 'http://127.0.0.1:5175' })

async function credentials(): Promise<{ token: string }> {
  return JSON.parse(await readFile(process.env.SHELDON_TEST_STATE!, 'utf8'))
}

test('opening diagnostics runs only safe metadata checks and never treats installation as readiness', async ({
  page,
}, testInfo) => {
  const { token } = await credentials()
  const requests: string[] = []
  page.on('request', (request) => requests.push(request.url()))
  await page.goto(`/#token=${token}`)
  await expect(page.getByRole('button', { name: 'Disconnect backend' })).toBeVisible()
  expect(requests.some((url) => url.includes('/api/environment'))).toBe(false)
  await page.getByText('Environment & diagnostics', { exact: true }).click()
  const environment = page.getByRole('region', { name: 'Environment diagnostics' })
  await expect(environment.getByRole('article', { name: 'Git diagnostics' })).toContainText(
    '2.45.1',
  )
  await expect(environment.getByRole('article', { name: 'GitHub CLI diagnostics' })).toContainText(
    '(optional)',
  )
  const opencode = environment.getByRole('article', { name: 'OpenCode diagnostics' })
  await expect(opencode).toContainText('2.1.4')
  await expect(opencode).toContainText('Advertised')
  await expect(opencode).toContainText('No production adapter is validated yet')
  await expect(opencode).toContainText('Login state was not checked')
  await expect(opencode).toContainText('remaining quota were not checked')
  await expect(opencode.getByRole('link')).toHaveAttribute(
    'href',
    'https://opencode.ai/v2/docs/cli/',
  )
  expect(requests.filter((url) => url.includes('/api/environment'))).toHaveLength(1)
  expect(requests.every((url) => url.startsWith('http://127.0.0.1:5175/'))).toBe(true)
  expect(await page.content()).not.toContain('private-provider-secret')
  expect(await page.content()).not.toContain(token)
  const allowed: Record<string, string[]> = {
    git: ['--version'],
    gh: ['--version'],
    opencode: ['--version', 'serve --help'],
    codex: ['--version', 'app-server --help', '--help'],
    claude: ['--version', '--help'],
  }
  for (const [name, commands] of Object.entries(allowed)) {
    const log = await readFile(join(process.env.SHELDON_TEST_TOOL_LOG!, name + '.jsonl'), 'utf8')
    for (const line of log.trim().split('\n')) {
      const call: { args: string[]; stdin: number; dir: string } = JSON.parse(line)
      expect(commands).toContain(call.args.join(' '))
      expect(call.stdin).toBe(0)
      expect(call.dir).toContain('sheldon-environment-')
    }
  }
  await page.screenshot({
    path: testInfo.outputPath('environment-diagnostics.png'),
    fullPage: true,
  })
  await page.getByRole('button', { name: 'Refresh environment' }).click()
  await expect(environment).toHaveAttribute('aria-busy', 'false')
  expect(requests.filter((url) => url.includes('/api/environment'))).toHaveLength(2)
  await page.getByRole('button', { name: 'Disconnect backend' }).click()
  await expect(environment).not.toBeVisible()
})

test('diagnostics errors are actionable, retry is explicit, and disconnect aborts pending checks', async ({
  page,
}) => {
  const { token } = await credentials()
  let attempts = 0
  await page.route('**/api/environment', async (route) => {
    attempts += 1
    await route.fulfill({
      status: 503,
      json: { error: 'untrusted backend diagnostics are not reflected' },
    })
  })
  await page.goto(`/#token=${token}`)
  await expect(page.getByRole('button', { name: 'Disconnect backend' })).toBeVisible()
  await page.getByText('Environment & diagnostics', { exact: true }).click()
  await expect(page.getByRole('alert')).toContainText('Retry after any active check finishes')
  expect(await page.content()).not.toContain('untrusted backend diagnostics')
  expect(attempts).toBe(1)
  await page.getByRole('button', { name: 'Refresh environment' }).click()
  await expect.poll(() => attempts).toBe(2)
  await expect(page.getByRole('region', { name: 'Environment diagnostics' })).toHaveAttribute(
    'aria-busy',
    'false',
  )
  await page.unroute('**/api/environment')
  await page.route('**/api/environment', (route) => route.abort())
  await page.getByRole('button', { name: 'Refresh environment' }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await page.unroute('**/api/environment')
  await page.evaluate(() => {
    const originalFetch = window.fetch
    window.fetch = (input, options) => {
      if (input === '/api/environment') {
        return new Promise<Response>((_resolve, reject) => {
          options?.signal?.addEventListener(
            'abort',
            () => {
              document.body.dataset.environmentAborted = 'yes'
              reject(new DOMException('Aborted', 'AbortError'))
            },
            { once: true },
          )
        })
      }
      return originalFetch(input, options)
    }
  })
  await page.getByRole('button', { name: 'Refresh environment' }).click()
  await expect(page.getByRole('region', { name: 'Environment diagnostics' })).toHaveAttribute(
    'aria-busy',
    'true',
  )
  await page.getByRole('button', { name: 'Disconnect backend' }).click()
  await expect(page.locator('body')).toHaveAttribute('data-environment-aborted', 'yes')
  await expect(page.getByRole('region', { name: 'Environment diagnostics' })).not.toBeVisible()
})
