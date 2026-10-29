import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import http from 'node:http'
import { dirname } from 'node:path'

async function state(): Promise<{ url: string; token: string }> {
  return JSON.parse((await readFile(process.env.SHELDON_RELEASE_STATE!)).subarray(1).toString())
}

test('isolated release binary serves the authenticated shell, font and complete license', async ({
  page,
}, testInfo) => {
  const { url, token } = await state()
  const urls: string[] = []
  const errors: string[] = []
  page.on('request', (request) => urls.push(request.url()))
  page.on('pageerror', (error) => errors.push(error.message))
  page.on('console', (message) => {
    if (message.type() === 'error') errors.push(message.text())
  })
  const response = await page.goto(`${url}/#token=${token}`)
  expect(response?.headers()['content-security-policy']).not.toContain('unsafe-inline')
  await expect(page).toHaveURL(url + '/')
  const runtime = page.getByRole('region', { name: 'Runtime connection' })
  await expect(runtime.getByRole('status').first()).toContainText('Backend connected')
  await expect(page.getByText('No PR loaded. No agent connected.', { exact: false })).toBeVisible()
  await page.evaluate(() => document.fonts.ready)
  expect(await page.evaluate(() => document.fonts.check('400 13px "Sheldon JetBrains Mono"'))).toBe(
    true,
  )
  const font = urls.find((value) => value.endsWith('.ttf'))
  expect(font).toBeTruthy()
  expect((await page.request.get(font!)).headers()['content-type']).toBe('font/ttf')
  const license = await page
    .getByRole('link', { name: 'JetBrains Mono license' })
    .getAttribute('href')
  const licenseResponse = await page.request.get(url + license!)
  expect(licenseResponse.headers()['content-type']).toContain('text/plain')
  const licenseText = await licenseResponse.text()
  expect(licenseText).toContain('SIL OPEN FONT LICENSE Version 1.1')
  expect(licenseText.length).toBeGreaterThan(4000)
  expect(urls.every((value) => value.startsWith(url + '/'))).toBe(true)
  expect(urls.some((value) => value.includes(token))).toBe(false)
  expect(await page.content()).not.toContain(token)
  expect(errors).toEqual([])
  await page.screenshot({ path: testInfo.outputPath('release-shell.png'), fullPage: true })
  const binary = process.env.SHELDON_RELEASE_BINARY!
  const result = await promisify(execFile)(
    binary,
    ['open', 'https://github.com/owner/repo/pull/8'],
    {
      cwd: dirname(binary),
      env: JSON.parse(process.env.SHELDON_RELEASE_ENV!),
      timeout: 5000,
    },
  )
  expect(result.stdout + result.stderr).not.toContain(token)
  await page.goto('about:blank')
  await page.goto(`${url}/#token=${token}`)
  await expect(page.getByText('Pending open request:', { exact: false })).toContainText(
    'https://github.com/owner/repo/pull/8',
  )
})

test('release paths retain Host/Origin protection, bearer auth and no source or SPA fallback', async ({
  request,
}) => {
  const { url, token } = await state()
  const hostileHeaders: Record<string, string>[] = [
    { Origin: 'https://attacker.example' },
    { Origin: 'http://127.0.0.1:5173' },
    { Host: 'attacker.example' },
  ]
  for (const path of ['/', '/index.html']) {
    for (const headers of hostileHeaders) {
      expect((await request.get(url + path, { headers })).status()).toBe(403)
    }
  }
  for (const path of ['/api/health', '/api/open', '/api/health?token=' + token]) {
    expect((await request.get(url + path)).status()).toBe(401)
  }
  expect(
    (
      await request.get(url + '/api/health', { headers: { Cookie: `sheldon_session=${token}` } })
    ).status(),
  ).toBe(401)
  for (const path of [
    '/api/unknown',
    '/src/main.ts',
    '/instance.lock',
    '/assets/missing.js',
    '/route',
    '/assets/',
  ]) {
    const response = await request.get(url + path, {
      headers: { Authorization: `Bearer ${token}` },
      maxRedirects: 0,
    })
    expect(response.status()).toBe(404)
    expect(await response.text()).not.toContain('<!doctype')
  }
  // Preserve raw dot segments; browser URL normalization must not conceal server behavior.
  for (const path of ['/assets/../index.html', '/%2e%2e/index.html', '//index.html']) {
    const status = await new Promise<number>((resolve, reject) => {
      const outgoing = http.get(url, { path }, (response) => {
        response.resume()
        resolve(response.statusCode!)
      })
      outgoing.on('error', reject)
    })
    expect(status).toBe(404)
  }
})

test('release reload requires explicit reconnecting and wrong proof never receives a bearer', async ({
  page,
}) => {
  const { url, token } = await state()
  await page.goto(`${url}/#token=${token}`)
  await expect(page.getByRole('button', { name: 'Disconnect backend' })).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0)
  let bearerSent = false
  page.on('request', (request) => {
    if (request.headers().authorization) bearerSent = true
  })
  await page.route('**/instance-proof', (route) =>
    route.fulfill({ json: { origin: url, proof: 'A'.repeat(43) } }),
  )
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Connection failed' })).toBeVisible()
  expect(bearerSent).toBe(false)
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  expect(await page.content()).not.toContain(token)
})
