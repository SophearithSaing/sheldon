let launchToken = ''

/**
 * Captures the launch token from the URL fragment and immediately clears it.
 */
export function captureLaunchToken(): void {
  const fragment = new URLSearchParams(location.hash.slice(1))
  launchToken = fragment.get('token') ?? ''
  if (fragment.has('token')) history.replaceState(null, '', location.pathname + location.search)
}

/**
 * Returns and consumes the captured launch token.
 *
 * @returns Launch token, or an empty string if none was captured.
 */
export function takeLaunchToken(): string {
  const token = launchToken
  launchToken = ''
  return token
}

/**
 * Builds an authenticated fetch request initializer.
 *
 * @param token Bearer token for the local runtime.
 * @param signal Abort signal for cancellation.
 * @returns Request options with the bearer header.
 */
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

/**
 * Verifies the backend identity before transmitting the bearer token.
 *
 * @param token Bearer token for the local runtime.
 * @param signal Abort signal for cancellation.
 */
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

/**
 * Verifies the backend and reads the pending open URL.
 *
 * @param token Bearer token for the local runtime.
 * @param signal Abort signal for cancellation.
 * @returns Pending PR URL, or an empty string if none is queued.
 */
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
