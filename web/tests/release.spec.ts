import { expect, test } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import http from 'node:http'
import { createHash } from 'node:crypto'
import { dirname, join } from 'node:path'

/**
 * Reads the release binary state written by the release test setup.
 * @returns Backend origin and token.
 */
async function state(): Promise<{ url: string; token: string }> {
  return JSON.parse((await readFile(process.env.SHELDON_RELEASE_STATE!)).subarray(1).toString())
}

/**
 * Checks standalone assets, credentials and second-instance forwarding after a clean source build.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output location.
 */
test('isolated release binary serves the authenticated shell, fonts and complete licenses', async ({
  page,
}, testInfo): Promise<void> => {
  const { url, token } = await state()
  const urls: string[] = []
  const errors: string[] = []
  page.on('request', (request): void => { urls.push(request.url()) })
  page.on('pageerror', (error): void => { errors.push(error.message) })
  page.on('console', (message): void => {
    if (message.type() === 'error') errors.push(message.text())
  })
  const response = await page.goto(`${url}/?preview=sample#token=${token}`)
  expect(response?.headers()['content-security-policy']).not.toContain('unsafe-inline')
  await expect(page).toHaveURL(url + '/?preview=sample')
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.getByRole('button', { name: 'UI preview', exact: true }).click()
  await expect(page.getByText('No PR loaded. No agent connected.', { exact: false })).toBeVisible()
  await page.evaluate((): Promise<FontFaceSet> => document.fonts.ready)
  expect(await page.evaluate((): boolean => document.fonts.check('400 13px "Sheldon JetBrains Mono"'))).toBe(
    true,
  )
  const font = urls.find((value): boolean => value.endsWith('.ttf'))
  expect(font).toBeTruthy()
  const fontResponse = await page.request.get(font!)
  expect(fontResponse.headers()['content-type']).toBe('font/ttf')
  expect(await fontResponse.body()).toEqual(
    await readFile(new URL('../src/assets/fonts/JetBrainsMono-Variable.ttf', import.meta.url)),
  )
  expect(await page.evaluate((): boolean => document.fonts.check('400 13px "Sheldon Slate Sans"'))).toBe(true)
  const sansFont = urls.find((value): boolean => value.endsWith('.woff2'))
  expect(sansFont).toBeTruthy()
  const sansFontResponse = await page.request.get(sansFont!)
  expect(sansFontResponse.headers()['content-type']).toBe('font/woff2')
  expect(await sansFontResponse.body()).toEqual(
    await readFile(new URL('../src/assets/fonts/DMSans-Latin-Variable.woff2', import.meta.url)),
  )
  const license = await page
    .getByRole('link', { name: 'JetBrains Mono license' })
    .getAttribute('href')
  const licenseResponse = await page.request.get(url + license!)
  expect(licenseResponse.headers()['content-type']).toContain('text/plain')
  const licenseText = await licenseResponse.text()
  expect(licenseText).toContain('SIL OPEN FONT LICENSE Version 1.1')
  expect(licenseText.length).toBeGreaterThan(4000)
  expect(licenseText).toBe(await readFile(new URL('../src/assets/fonts/OFL.txt', import.meta.url), 'utf8'))
  const sansLicense = await page.getByRole('link', { name: 'DM Sans license' }).getAttribute('href')
  const sansLicenseResponse = await page.request.get(url + sansLicense!)
  expect(sansLicenseResponse.headers()['content-type']).toContain('text/plain')
  const sansLicenseText = await sansLicenseResponse.text()
  expect(sansLicenseText).toContain('SIL OPEN FONT LICENSE')
  expect(sansLicenseText.length).toBeGreaterThan(4000)
  expect(sansLicenseText).toBe(await readFile(new URL('../src/assets/fonts/DM-Sans-LICENSE.txt', import.meta.url), 'utf8'))
  expect(urls.every((value): boolean => value.startsWith(url + '/'))).toBe(true)
  expect(urls.some((value): boolean => value.includes(token))).toBe(false)
  expect(await page.content()).not.toContain(token)
  expect(errors).toEqual([])
  await page.getByRole('button', { name: 'UI preview', exact: true }).click()
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
  const registered = JSON.parse(await readFile(join(dirname(process.env.SHELDON_RELEASE_STATE!), 'prs.json'), 'utf8'))
  const pr = registered.prs.find((item: { number: string }): boolean => item.number === '8')
  expect(pr).toBeTruthy()
  await page.goto('about:blank')
  await page.goto(`${url}/reviews/${pr.id}#token=${token}`)
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pull request #8')
  await expect(page.getByText('https://github.com/owner/repo/pull/8', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'PR registered. Source not loaded.' })).toBeVisible()
})

/**
 * Checks release query parameters cannot enable control simulations.
 * @param fixtures Playwright page fixture.
 */
test('release cannot enable the fixture-only control gallery', async ({ page }): Promise<void> => {
  const { url } = await state()
  await page.goto(`${url}/?fixture=controls`)
  await expect(page.getByRole('heading', { name: 'Slate control fixtures' })).toHaveCount(0)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Open a pull request')
  await expect(page.getByRole('button', { name: 'Connect backend' })).toBeVisible()
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
    '/prs.json',
    '/reviews/invalid',
    '/reviews/' + '0'.repeat(64),
    '/reviews/' + 'a'.repeat(64) + '/extra',
    '/reviews/' + 'A'.repeat(64),
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
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.getByRole('button', { name: 'Local runtime — Backend connected', exact: true }).click()
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

/**
 * Verifies manual recovery and disconnect across Slate destinations with no credential persistence.
 * @param fixtures Playwright page fixture.
 * @param testInfo Screenshot output location.
 */
test('release reconnects manually and disconnects without rebinding sample workspaces', async ({ page }, testInfo): Promise<void> => {
  const { url, token } = await state()
  await page.goto(`${url}/?preview=sample#token=${token}`)
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await expect(page.getByLabel('API token', { exact: true })).toHaveCount(0)
  const draft = page.getByRole('textbox', { name: 'Your question', exact: true })
  await draft.fill('Keep the sample draft across runtime recovery')
  await page.getByRole('tab', { name: 'Semantic review', exact: true }).click()
  await expect(page.getByRole('button', { name: 'Preview semantic generation', exact: true })).toBeVisible()
  await expect(page.getByRole('button', { name: 'Finish sample grouping', exact: true })).toHaveCount(0)
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.getByRole('link', { name: 'All PRs', exact: true }).click()
  await page.getByRole('button', { name: 'Local runtime — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Disconnected' })).toBeVisible()
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  await page.getByRole('link', { name: 'platform #248 — Session refresh', exact: true }).click()
  await page.getByRole('tab', { name: 'Files changed', exact: true }).click()
  await expect(draft).toHaveValue('Keep the sample draft across runtime recovery')
  await expect(page.getByRole('tab', { name: 'Questions', exact: true })).toHaveAttribute('aria-description', '0 sample items')
  expect(await page.evaluate((): number => localStorage.length + sessionStorage.length)).toBe(0)
  expect(await page.content()).not.toContain(token)
  await page.screenshot({ path: testInfo.outputPath('release-disconnected-draft.png') })
})

/**
 * Opens registered PRs independently and distinguishes missing PRs from source readiness.
 * @param fixtures Browser pages and request client.
 * @param testInfo Screenshot output location.
 */
test('registered release routes isolate PRs, reuse aliases and require reconnecting', async ({ page, context, request }, testInfo): Promise<void> => {
  const { url, token } = await state()
  const headers = { Authorization: `Bearer ${token}` }
  const first = await request.post(url + '/api/open', { headers, data: { url: 'https://github.com/route/repo/pull/101' } })
  expect(first.status()).toBe(202)
  const a: { prID: string; route: string } = await first.json()
  const alias = await request.post(url + '/api/open', { headers, data: { url: 'https://github.com/ROUTE/REPO/pull/101/?tab=files#discussion' } })
  expect(await alias.json()).toEqual(a)
  await page.goto(url + a.route + '#token=' + token)
  await expect(page).toHaveURL(url + a.route)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pull request #101')
  const second = await request.post(url + '/api/open', { headers, data: { url: 'https://github.com/route/repo/pull/102' } })
  const b: { prID: string; route: string } = await second.json()
  expect(a.prID).not.toBe(b.prID)
  const other = await context.newPage()
  await other.goto(url + b.route + '#token=' + token)
  await expect(other.getByRole('heading', { level: 1 })).toHaveText('Pull request #102')
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Pull request #101')
  await expect(page).toHaveURL(url + a.route)
  expect(await page.content()).not.toContain('Deduplicate concurrent session refreshes')
  expect(await page.content()).not.toContain(token)
  expect((await request.get(url + '/api/open', { headers })).status()).toBe(404)
  await page.screenshot({ path: testInfo.outputPath('registered-review-1440.png'), fullPage: true })
  await page.reload()
  await expect(page.getByRole('button', { name: 'Connect backend' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Pull request #101' })).toHaveCount(0)
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByRole('heading', { name: 'Pull request #101' })).toBeVisible()
  expect(await page.evaluate((): number => localStorage.length + sessionStorage.length)).toBe(0)
  await page.setViewportSize({ width: 700, height: 900 })
  await page.screenshot({ path: testInfo.outputPath('registered-review-700.png'), fullPage: true })
  await page.evaluate((): void => { document.documentElement.style.fontSize = '200%' })
  expect(await page.evaluate((): boolean => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: testInfo.outputPath('registered-review-text-scale-200.png'), fullPage: true })
  await other.goto(url + '/reviews/' + 'e'.repeat(64) + '#token=' + token)
  await expect(other.getByRole('heading', { name: 'PR not found' })).toBeVisible()
  await other.screenshot({ path: testInfo.outputPath('registered-review-not-found.png'), fullPage: true })
  await other.close()
})

/**
 * Keeps lookup failures and proof rejection honest on registered routes.
 * @param fixtures Browser page and request client.
 * @param testInfo Screenshot output location.
 */
test('registered release route errors hide raw details and verify before lookup', async ({ page, request }, testInfo): Promise<void> => {
  const { url, token } = await state()
  const response = await request.post(url + '/api/open', { headers: { Authorization: `Bearer ${token}` }, data: { url: 'https://github.com/error/route/pull/303' } })
  const opened: { prID: string; route: string } = await response.json()
  await page.route(`**/api/reviews/${opened.prID}`, (route): Promise<void> => route.fulfill({ status: 500, json: { error: 'raw private fixture details' } }))
  await page.goto(url + opened.route + '#token=' + token)
  await expect(page.getByText('Cannot read this PR.', { exact: false })).toBeVisible()
  expect(await page.content()).not.toContain('raw private fixture details')
  expect(await page.content()).not.toContain(token)
  await page.screenshot({ path: testInfo.outputPath('registered-review-error.png'), fullPage: true })
  await page.unrouteAll()
  await page.reload()
  let bearerSent = false
  page.on('request', (request): void => { if (request.headers().authorization) bearerSent = true })
  await page.route('**/instance-proof', (route): Promise<void> => route.fulfill({ json: { origin: url, proof: 'A'.repeat(43) } }))
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByText('Cannot read this PR.', { exact: false })).toBeVisible()
  expect(bearerSent).toBe(false)
  await expect(page.getByRole('heading', { name: 'Pull request #303' })).toHaveCount(0)
})

/**
 * Exercises embedded source controls using explicit offline HTTP response fixtures.
 * @param fixtures Browser page and protected API client.
 * @param testInfo Screenshot output location.
 */
test('release source UI distinguishes metadata fallback, explicit selection and pinned empty source', async ({ page, request }, testInfo): Promise<void> => {
  const { url, token } = await state()
  const headers = { Authorization: `Bearer ${token}` }
  const opened: { prID: string; route: string } = await (await request.post(url + '/api/open', { headers, data: { url: 'https://github.com/release/fixture/pull/801' } })).json()
  const pr = await (await request.get(url + '/api/reviews/' + opened.prID, { headers })).json()
  const revision = { prID: opened.prID, baseBranch: 'release/target', baseSource: 'user-selected', baseSHA: 'a'.repeat(40), headSHA: 'c'.repeat(40), mergeBaseSHA: 'b'.repeat(40) }
  const revisionID = createHash('sha256').update([opened.prID, revision.baseSHA, revision.headSHA, revision.mergeBaseSHA].join('\n')).digest('hex')
  let calls = 0
  await page.route(`**/api/reviews/${opened.prID}/source`, async (route): Promise<void> => {
    calls++
    const input = route.request().postDataJSON()
    if (input.action === 'load') await route.fulfill({ json: { state: 'fallback-required', pr } })
    else if (input.action === 'git-only') await route.fulfill({ json: { state: 'base-selection-required', pr, candidates: [{ branch: revision.baseBranch, sha: revision.baseSHA }] } })
    else {
      expect(input).toEqual({ action: 'select-base', branch: revision.baseBranch, sha: revision.baseSHA })
      await route.fulfill({ json: { state: 'ready', pr, diff: { revision, revisionID, complete: true, files: [] } } })
    }
  })
  await page.goto(url + opened.route + '#token=' + token)
  await expect(page.getByRole('heading', { name: 'PR registered. Source not loaded.' })).toBeVisible()
  expect(calls).toBe(0)
  await page.getByRole('button', { name: 'Load pinned source' }).click()
  await page.getByRole('button', { name: 'Use Git-only fallback' }).click()
  await expect(page.getByLabel('Fetched base branch')).toHaveValue('')
  await page.getByLabel('Fetched base branch').selectOption(revision.baseBranch)
  await page.getByRole('button', { name: 'Load selected base', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'No changes in this comparison' })).toBeVisible()
  expect(calls).toBe(3)
  await expect(page.getByRole('button', { name: 'Questions — unavailable' })).toBeDisabled()
  expect(await page.content()).not.toContain(token)
  await page.screenshot({ path: testInfo.outputPath('release-pinned-empty-source.png') })
})
