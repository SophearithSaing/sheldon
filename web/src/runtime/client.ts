let launchToken = ''

export interface RegisteredPR {
  id: string
  url: string
  owner: string
  repository: string
  number: string
  title: string | null
  author: string | null
}

export interface ReviewState {
  status: 'disconnected' | 'loading' | 'registered' | 'not-found' | 'failed'
  pr?: RegisteredPR
}

/**
 * Recognizes an exact review route without treating it as authentication.
 * @param path Browser pathname.
 * @returns PR identifier, or an empty string for other routes.
 */
export function reviewID(path: string): string {
  const match = /^\/reviews\/([a-f0-9]{64})$/.exec(path)
  return match && !/^0+$/.test(match[1]!) ? match[1]! : ''
}

/**
 * Captures the launch token from the URL fragment and immediately clears it.
 * Keeps the credential out of component props and Vue diagnostic traces.
 */
export function captureLaunchToken(): void {
  const fragment = new URLSearchParams(location.hash.slice(1))
  launchToken = fragment.get('token') ?? ''
  if (fragment.has('token')) history.replaceState(null, '', location.pathname + location.search)
}

/**
 * Returns and consumes the captured launch token.
 * @returns Launch token, or an empty string if none was captured.
 */
export function takeLaunchToken(): string {
  const token = launchToken
  launchToken = ''
  return token
}

/**
 * Builds an authenticated fetch request initializer.
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
 * Verifies the backend and checks authenticated runtime availability.
 * @param token Bearer token for the local runtime.
 * @param signal Abort signal for cancellation.
 */
export async function inspectBackend(token: string, signal: AbortSignal): Promise<void> {
  await verifyBackend(token, signal)
  const options = authenticatedRequest(token, signal)
  const health = await fetch('/api/health', options)
  if (!health.ok || (await health.json()).status !== 'ok')
    throw new Error('Backend authentication failed.')
}

/**
 * Reads the registered PR for this tab without fetching source.
 * @param token Verified runtime bearer token.
 * @param signal Abort signal for cancellation.
 * @param id PR identifier from the current route.
 * @returns Registered metadata, or null when the PR is not registered.
 */
export async function readRegisteredPR(token: string, signal: AbortSignal, id: string): Promise<RegisteredPR | null> {
  if (reviewID(`/reviews/${id}`) !== id || !id) throw new Error('Invalid review route.')
  const response = await fetch(`/api/reviews/${id}`, authenticatedRequest(token, signal))
  if (response.status === 404) return null
  if (!response.ok) throw new Error('Cannot read registered PR.')
  const pr: Partial<RegisteredPR> | null = await response.json()
  if (!pr || pr.id !== id || typeof pr.owner !== 'string' || typeof pr.repository !== 'string' ||
      typeof pr.number !== 'string' || !/^[1-9][0-9]*$/.test(pr.number) ||
      pr.url !== `https://github.com/${pr.owner}/${pr.repository}/pull/${pr.number}` ||
      !(pr.title === null || typeof pr.title === 'string') || !(pr.author === null || typeof pr.author === 'string')) {
    throw new Error('Unexpected registered PR response.')
  }
  return pr as RegisteredPR
}
