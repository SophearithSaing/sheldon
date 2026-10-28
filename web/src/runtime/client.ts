import type { EnvironmentReport } from './environment'
import type { Harness, ModelDiscovery, SettingsState } from '../settings/contracts'

let launchToken = ''

// Keep the credential out of component props (and Vue's diagnostic prop traces).
export function captureLaunchToken(): void {
  const fragment = new URLSearchParams(location.hash.slice(1))
  launchToken = fragment.get('token') ?? ''
  if (fragment.has('token')) history.replaceState(null, '', location.pathname + location.search)
}

export function takeLaunchToken(): string {
  const token = launchToken
  launchToken = ''
  return token
}

function authenticatedRequest(token: string, signal: AbortSignal): RequestInit {
  return {
    headers: { Authorization: `Bearer ${token}` },
    signal,
    credentials: 'omit',
    mode: 'same-origin',
    redirect: 'error',
    cache: 'no-store',
  }
}

// Verify the runtime before transmitting a bearer, including after manual token entry.
// The origin is public proof metadata, never a destination for browser requests.
async function verifyBackend(token: string, signal: AbortSignal): Promise<void> {
  const challengeBytes = crypto.getRandomValues(new Uint8Array(32))
  const challenge = btoa(String.fromCharCode(...challengeBytes))
    .replaceAll('+', '-')
    .replaceAll('/', '_')
    .replaceAll('=', '')
  const response = await fetch('/instance-proof', {
    signal,
    credentials: 'omit',
    mode: 'same-origin',
    redirect: 'error',
    cache: 'no-store',
    headers: { 'X-Sheldon-Challenge': challenge },
  })
  if (!response.ok) throw new Error('Backend verification failed.')
  const proof: { origin?: unknown; proof?: unknown } = await response.json()
  if (
    typeof proof.origin !== 'string' ||
    proof.origin.length > 128 ||
    typeof proof.proof !== 'string' ||
    !/^[A-Za-z0-9_-]{43}$/.test(proof.proof)
  ) {
    throw new Error('Backend verification failed.')
  }
  const signature = Uint8Array.from(
    atob(proof.proof.replaceAll('-', '+').replaceAll('_', '/') + '='),
    (value) => value.charCodeAt(0),
  )
  const encoder = new TextEncoder()
  const key = await crypto.subtle.importKey(
    'raw',
    encoder.encode(token),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['verify'],
  )
  const message = encoder.encode(`sheldon-instance-v1\n${proof.origin}\n${challenge}`)
  if (!(await crypto.subtle.verify('HMAC', key, signature, message)))
    throw new Error('Backend verification failed.')
}

export async function inspectBackend(token: string, signal: AbortSignal): Promise<string> {
  await verifyBackend(token, signal)
  const options = authenticatedRequest(token, signal)
  const health = await fetch('/api/health', options)
  if (!health.ok || (await health.json()).status !== 'ok')
    throw new Error('Backend authentication failed.')
  const open = await fetch('/api/open', options)
  if (!open.ok) throw new Error('Backend request failed.')
  const state: { url?: unknown } = await open.json()
  if (typeof state.url !== 'string') throw new Error('Unexpected backend response.')
  return state.url
}

// Called only when diagnostics are opened/refreshed, never during startup or streaming.
export async function inspectEnvironment(
  token: string,
  signal: AbortSignal,
): Promise<EnvironmentReport> {
  await verifyBackend(token, signal)
  const response = await fetch('/api/environment', authenticatedRequest(token, signal))
  if (!response.ok) throw new Error('Environment checks unavailable.')
  return response.json()
}

// Settings metadata is static scaffold data; neither operation starts native discovery.
export async function inspectSettings(token: string, signal: AbortSignal): Promise<SettingsState> {
  await verifyBackend(token, signal)
  const response = await fetch('/api/settings', authenticatedRequest(token, signal))
  if (!response.ok) throw new Error('Settings unavailable.')
  return response.json()
}

export async function inspectModels(
  token: string,
  signal: AbortSignal,
  harness: Harness,
): Promise<ModelDiscovery> {
  await verifyBackend(token, signal)
  const response = await fetch(
    `/api/environment/harnesses/${encodeURIComponent(harness)}/models`,
    authenticatedRequest(token, signal),
  )
  if (!response.ok) throw new Error('Model metadata unavailable.')
  return response.json()
}

// Live-only fixture SSE. No EventSource, query credentials, replay or agent dispatch.
export async function* fixtureEvents(
  token: string,
  signal: AbortSignal,
): AsyncGenerator<'connected' | 'heartbeat'> {
  const response = await fetch('/api/fixture/events', authenticatedRequest(token, signal))
  if (
    !response.ok ||
    !response.headers.get('Content-Type')?.startsWith('text/event-stream') ||
    !response.body
  ) {
    await response.body?.cancel()
    throw new Error('Fixture stream unavailable.')
  }
  const reader = response.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  try {
    for (;;) {
      const chunk = await reader.read()
      if (chunk.done) throw new Error('Fixture stream closed. Reconnect explicitly.')
      buffer += decoder.decode(chunk.value, { stream: true })
      if (buffer.length > 8192) throw new Error('Fixture frame exceeded its limit.')
      let boundary = /\r?\n\r?\n/.exec(buffer)
      while (boundary) {
        const frame = buffer.slice(0, boundary.index).split(/\r?\n/)
        buffer = buffer.slice(boundary.index + boundary[0].length)
        const event = frame
          .find((line) => line.startsWith('event:'))
          ?.slice(6)
          .trim()
        if (event === 'fixture') {
          const data = frame
            .filter((line) => line.startsWith('data:'))
            .map((line) => line.slice(5).trimStart())
            .join('\n')
          const value: { fixture?: unknown } = JSON.parse(data)
          if (value.fixture !== 'connected' && value.fixture !== 'heartbeat')
            throw new Error('Unexpected fixture event.')
          yield value.fixture
        }
        boundary = /\r?\n\r?\n/.exec(buffer)
      }
    }
  } finally {
    await reader.cancel().catch(() => {})
    reader.releaseLock()
  }
}
