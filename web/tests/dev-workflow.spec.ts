import { expect, test } from '@playwright/test'
import { readFile, writeFile } from 'node:fs/promises'
import { join } from 'node:path'
import http from 'node:http'
import net from 'node:net'
import { backendProxy, loopbackOrigin } from '../development/server.ts'

const frontend = 'http://127.0.0.1:5175'
const backend = 'http://127.0.0.1:7332'
test.use({ baseURL: frontend })

test('development targets reject non-loopback and noncanonical configuration', () => {
  for (const value of [
    'https://127.0.0.1:5173',
    'http://localhost:5173',
    'http://0.0.0.0:5173',
    'http://192.0.2.1:7331',
    'http://127.0.0.1:7331/',
    'http://127.0.0.1:7331?token=x',
    'http://user@127.0.0.1:7331',
    'http://127.0.0.1:7331#token=x',
    'http://127.1:7331',
    'http://[::]:5173',
    'http://[::ffff:127.0.0.1]:7331',
    'http://127.0.0.1:0',
  ]) {
    expect(() => loopbackOrigin(value)).toThrow()
    expect(() => backendProxy(value)).toThrow()
  }
  expect(loopbackOrigin(frontend).origin).toBe(frontend)
  expect(loopbackOrigin('http://[::1]:5173').origin).toBe('http://[::1]:5173')
})

/**
 * Reads the backend token written by the development test setup.
 * @returns Backend credentials.
 */
async function credentials(): Promise<{ token: string }> {
  return JSON.parse(await readFile(process.env.SHELDON_TEST_STATE!, 'utf8'))
}

/**
 * Makes a raw HTTP request to the development frontend.
 * @param path Request path.
 * @param headers Request headers.
 * @param method HTTP method.
 * @returns Response status, headers and body.
 */
async function request(
  path: string,
  headers: http.OutgoingHttpHeaders = {},
  method = 'GET',
): Promise<{ status: number; headers: http.IncomingHttpHeaders; body: string }> {
  return new Promise((resolve, reject) => {
    const outgoing = http.request(frontend + path, { method, headers }, (response) => {
      let body = ''
      response.setEncoding('utf8')
      response.on('data', (chunk) => {
        body += chunk
      })
      response.on('end', () =>
        resolve({ status: response.statusCode!, headers: response.headers, body }),
      )
    })
    outgoing.on('error', reject)
    outgoing.end()
  })
}

test('launch fragments clear before authenticated REST', async ({ page }, testInfo) => {
  const { token } = await credentials()
  const urls: string[] = []
  const messages: string[] = []
  page.on('request', (request) => urls.push(request.url()))
  page.on('console', (message) => messages.push(message.text()))
  await page.goto(`/?preview=sample#token=${token}`)
  await expect(page).toHaveURL(frontend + '/?preview=sample')
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  expect(urls.some((url) => url.includes(token))).toBe(false)
  expect(messages.some((message) => message.includes(token))).toBe(false)
  expect(await page.content()).not.toContain(token)
  await page.getByRole('link', { name: 'Configuration', exact: true }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.getByRole('link', { name: 'platform #248 — Session refresh', exact: true }).click()
  await page.screenshot({ path: testInfo.outputPath('connected-development.png'), fullPage: true })
  await page.getByRole('button', { name: 'Development backend — Backend connected', exact: true }).click()
  await page.getByRole('button', { name: 'Disconnect backend' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Disconnected' })).toBeVisible()
  await page.reload()
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  await expect(page.getByRole('button', { name: 'Connect backend' })).toBeVisible()
  expect(await page.evaluate(() => localStorage.length + sessionStorage.length)).toBe(0)
})

test('manual connection rejects bad credentials without reflecting or saving them', async ({
  page,
}, testInfo) => {
  let bearerSent = false
  page.on('request', (request) => {
    if (request.headers().authorization) bearerSent = true
  })
  await page.goto('/')
  await page.screenshot({ path: testInfo.outputPath('runtime-disconnected.png'), fullPage: true })
  const invalid = 'A'.repeat(43)
  await page.getByLabel('API token', { exact: true }).fill(invalid)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Connection failed' })).toBeVisible()
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  expect(await page.content()).not.toContain(invalid)
  expect(bearerSent).toBe(false)
  await page.screenshot({ path: testInfo.outputPath('runtime-error.png'), fullPage: true })
  const { token } = await credentials()
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
})

test('fixture-only development mode also clears launch fragments without connecting', async ({
  page,
}) => {
  const { token } = await credentials()
  const requests: string[] = []
  page.on('request', (request) => {
    if (['fetch', 'xhr'].includes(request.resourceType())) requests.push(request.url())
  })
  await page.goto(`http://127.0.0.1:5174/#token=${token}`)
  await expect(page).toHaveURL('http://127.0.0.1:5174/')
  await expect(
    page.getByRole('region', { name: 'Development backend connection' }),
  ).not.toBeVisible()
  expect(requests).toEqual([])
  expect(await page.content()).not.toContain(token)
})

test('connection errors are shown without exposing the token', async ({ page }) => {
  await page.goto('/')
  await page.route('**/api/health', (route) => route.abort())
  const { token } = await credentials()
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByRole('status').filter({ hasText: 'Connection failed' })).toBeVisible()
  expect(await page.content()).not.toContain(token)
})

test('proxy rejects hostile authority/origin/metadata and never injects authentication', async () => {
  const { token } = await credentials()
  const auth = { Authorization: `Bearer ${token}` }
  expect((await request('/api/health')).status).toBe(401)
  expect((await request('/api/health', auth)).status).toBe(200)
  for (const headers of [
    { Host: 'attacker.example:5175' },
    { Host: 'localhost:5175' },
    { Host: '127.0.0.1:5176' },
    { Origin: 'https://attacker.example' },
    { Origin: 'null' },
    { Origin: 'http://127.0.0.1:5174' },
    { 'Sec-Fetch-Site': 'cross-site' },
    { 'Sec-Fetch-Site': 'same-site' },
    { Host: 'attacker.example', 'X-Forwarded-Host': '127.0.0.1:5175' },
  ]) {
    const response = await request('/api/health', { ...auth, ...headers })
    expect(response.status).toBe(403)
    expect(response.headers['access-control-allow-origin']).toBeUndefined()
    expect(response.body).not.toContain(token)
  }
  for (const headers of [
    { Authorization: [`Bearer ${token}`, `Bearer ${token}`] },
    { Origin: [frontend, frontend] },
    { 'X-Sheldon-Challenge': ['a', 'a'] },
  ])
    expect((await request('/api/health', { ...auth, ...headers })).status).toBe(400)
  expect((await request('/api/health?token=' + token)).status).toBe(401)
  expect((await request('/api/health', { Cookie: `sheldon_session=${token}` })).status).toBe(401)
  const preflight = await request(
    '/api/open',
    { Origin: 'https://attacker.example', 'Access-Control-Request-Method': 'POST' },
    'OPTIONS',
  )
  expect(preflight.status).toBe(403)
  expect(preflight.headers['access-control-allow-origin']).toBeUndefined()
  const direct = await fetch(backend + '/api/health', {
    headers: { ...auth, Origin: 'http://127.0.0.1:5174' },
  })
  expect(direct.status).toBe(403)
})

test('authenticated browser mutations preserve Origin through Vite', async ({ page }) => {
  const { token } = await credentials()
  await page.goto('/')
  const opened = await page.evaluate(async (token): Promise<{ status: number; prID: string; route: string }> => {
    const response = await fetch('/api/open', {
      method: 'POST',
      credentials: 'omit',
      headers: { Authorization: `Bearer ${token}`, 'Content-Type': 'application/json' },
      body: JSON.stringify({ url: 'https://github.com/owner/repo/pull/7' }),
    })
    return { status: response.status, ...await response.json() }
  }, token)
  expect(opened.status).toBe(202)
  expect(opened.route).toBe(`/reviews/${opened.prID}`)
  const state = await request(`/api/reviews/${opened.prID}`, { Authorization: `Bearer ${token}` })
  expect(JSON.parse(state.body).url).toBe('https://github.com/owner/repo/pull/7')
  expect((await request('/api/open', { Authorization: `Bearer ${token}` })).status).toBe(404)
})

test('HMR updates disposable frontend sources without rebuilding Go or losing connection', async ({
  page,
}) => {
  const { token } = await credentials()
  await page.goto(`/?preview=sample#token=${token}`)
  await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
  await page.evaluate(() => {
    const marker = document.createElement('div')
    marker.id = 'hmr-test-marker'
    marker.hidden = true
    document.body.append(marker)
  })
  const source = join(process.env.SHELDON_TEST_WEB_ROOT!, 'src/fixtures/SampleWorkspace.vue')
  const original = await readFile(source, 'utf8')
  try {
    await writeFile(
      source,
      original.replace('Deduplicate concurrent session refreshes', 'Hot-reloaded sample workspace'),
    )
    await expect(page.getByRole('heading', { level: 1 })).toContainText(
      'Hot-reloaded sample workspace',
    )
    await expect(page.locator('#hmr-test-marker')).toHaveCount(1)
    await expect(page.getByRole('status').filter({ hasText: 'Backend connected' })).toBeVisible()
    expect((await request('/api/health', { Authorization: `Bearer ${token}` })).status).toBe(200)
  } finally {
    await writeFile(source, original)
  }
})

test('HMR upgrades reject foreign origins, and private test state is outside Vite file serving', async ({
  page,
}) => {
  await page.goto('/')
  const response = await new Promise<string>((resolve, reject) => {
    const socket = net.connect(5175, '127.0.0.1')
    let data = ''
    socket.on('connect', () =>
      socket.write(
        'GET / HTTP/1.1\r\nHost: 127.0.0.1:5175\r\nOrigin: https://attacker.example\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nSec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\n\r\n',
      ),
    )
    socket.on('data', (chunk) => {
      data += chunk.toString()
    })
    socket.on('end', () => resolve(data))
    socket.on('error', reject)
    socket.setTimeout(3000, () => {
      socket.destroy()
      reject(new Error('Upgrade did not terminate.'))
    })
  })
  expect(response).toContain('403')
  const denied = await request('/@fs/' + process.env.SHELDON_TEST_STATE!)
  expect(denied.status).toBe(403)
  for (const path of ['/instance.lock', '/%69nstance.lock?raw', '/instance.lock::$DATA', '/prs.json', '/%70rs.json?raw', '/prs.json::$DATA', '/.prs-example.tmp', '/threads.json', '/reviews/private/threads.json?raw', '/%74hreads.json', '/threads.json::$DATA', '/.threads-example.tmp', '/quiz.json', '/reviews/private/quiz.json?raw', '/%71uiz.json', '/quiz.json::$DATA', '/QUIZ.JSON.', '/.quiz-fixture.tmp', '/reviews/private/.quiz-example.tmp', '/%2equiz-fixture.tmp?raw']) {
    const response = await request(path)
    expect(response.status).toBe(403)
    expect(response.body).not.toContain('private fixture value')
    expect(response.body).not.toContain('private quiz answer key')
  }
  const { token } = await credentials()
  expect(denied.body).not.toContain(token)
  const logs = await readFile(process.env.SHELDON_TEST_SERVER_LOG!, 'utf8')
  expect(logs).not.toContain(token)
})

/**
 * Keeps proxied review routes separate from samples and isolates metadata per tab.
 * @param fixtures Browser pages and request client.
 * @param testInfo Screenshot output location.
 */
test('development review routes isolate PRs and ignore cancelled metadata replies', async ({ page, context, request: api }, testInfo): Promise<void> => {
  const { token } = await credentials()
  const headers = { Authorization: `Bearer ${token}` }
  const first = await api.post(frontend + '/api/open', { headers, data: { url: 'https://github.com/dev/routes/pull/201' } })
  const a: { prID: string; route: string } = await first.json()
  await page.goto(frontend + a.route + '#token=' + token)
  await expect(page).toHaveURL(frontend + a.route)
  await expect(page.getByRole('heading', { name: 'Pull request #201' })).toBeVisible()
  const second = await api.post(frontend + '/api/open', { headers, data: { url: 'https://github.com/dev/routes/pull/202' } })
  const b: { prID: string; route: string } = await second.json()
  const other = await context.newPage()
  await other.goto(frontend + b.route + '#token=' + token)
  await expect(other.getByRole('heading', { name: 'Pull request #202' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Pull request #201' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('registered-development.png'), fullPage: true })
  await page.reload()
  let release: (() => void) | undefined
  const gate = new Promise<void>((resolve): void => { release = resolve })
  await page.route(`**/api/reviews/${a.prID}`, async (route): Promise<void> => {
    await gate
    await route.fulfill({ json: { id: a.prID, url: 'https://github.com/dev/routes/pull/201', owner: 'dev', repository: 'routes', number: '201', title: null, author: null } })
  })
  await page.getByLabel('API token', { exact: true }).fill(token)
  await page.getByRole('button', { name: 'Connect backend' }).click()
  await expect(page.getByText('Reading registered PR…', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Disconnect backend' }).click()
  release!()
  await page.unrouteAll({ behavior: 'wait' })
  await expect(page.getByRole('heading', { name: 'Pull request #201' })).toHaveCount(0)
  await expect(page.getByLabel('API token', { exact: true })).toHaveValue('')
  expect(await page.content()).not.toContain(token)
  await other.close()
  for (const path of ['/reviews/invalid', '/reviews/' + 'a'.repeat(64) + '/extra', '/reviews/' + 'A'.repeat(64), '/reviews/' + '0'.repeat(64)]) {
    expect((await api.get(frontend + path)).status()).toBe(404)
  }
})
