import type { DiffLine, LineRange } from '../components/diff'

export const workspaceTabs = [
  { id: 'files', label: 'Files changed' },
  { id: 'questions', label: 'Questions' },
  { id: 'quiz', label: 'Quiz' },
] as const

export interface SampleAnchor {
  fileId: string
  revision: string
  range: LineRange | null
}

export interface SampleThread {
  id: number
  anchor: SampleAnchor
  question: string
  status: 'answered' | 'investigating' | 'understood' | 'failed' | 'interrupted'
  followup: string
  messages: SampleMessage[]
  outdated?: boolean
}

export interface SampleMessage {
  author: 'You' | 'Agent'
  paragraphs: string[]
  citations?: SampleAnchor[]
}

export const sampleFiles = [
  { id: 'refresh', path: 'src/auth/refresh-session.ts', added: 24, removed: 8, hunk: '@@ −22,12 +22,24 @@ refreshSession' },
  { id: 'store', path: 'src/auth/session-store.ts', added: 8, removed: 2, hunk: '@@ −41,3 +41,4 @@ logout' },
  { id: 'client', path: 'src/http/client.ts', added: 16, removed: 5, hunk: '@@ −12,3 +12,4 @@ authenticatedRequest' },
  { id: 'test', path: 'tests/auth/refresh-session.test.ts', added: 12, removed: 0, hunk: '@@ −12,0 +12,7 @@' },
  { id: 'docs', path: 'docs/sessions.md', added: 4, removed: 1, hunk: '@@ −1,2 +1,4 @@' },
] as const

export const sampleDiffs: Record<string, DiffLine[]> = {
  refresh: [
    { base: 22, head: 22, kind: 'context', text: 'export function refreshSession() {' },
    { base: 23, head: null, kind: 'removed', text: '  return requestSession().then(store.set);' },
    { base: null, head: 23, kind: 'added', text: '  if (inFlight) return inFlight;' },
    { base: null, head: 24, kind: 'added', text: '' },
    { base: null, head: 25, kind: 'added', text: '  const generation = store.generation;' },
    {
      base: null,
      head: 26,
      kind: 'added',
      text: '  inFlight = requestSession().then((session) => {',
    },
    { base: null, head: 27, kind: 'added', text: '    if (generation === store.generation) {' },
    { base: null, head: 28, kind: 'added', text: '      store.set(session);' },
    { base: null, head: 29, kind: 'added', text: '    }' },
    { base: null, head: 30, kind: 'added', text: '    return session;' },
    { base: null, head: 31, kind: 'added', text: '  }).finally(() => {' },
    { base: null, head: 32, kind: 'added', text: '    inFlight = null;' },
    { base: null, head: 33, kind: 'added', text: '  });' },
    { base: null, head: 34, kind: 'added', text: '' },
    { base: null, head: 35, kind: 'added', text: '  return inFlight;' },
    { base: 24, head: 36, kind: 'context', text: '}' },
    { base: 25, head: 37, kind: 'context', text: '' },
    {
      base: 26,
      head: 38,
      kind: 'context',
      text: '// Sample context below the change; no repository has been loaded.',
    },
    {
      base: 27,
      head: 39,
      kind: 'context',
      text: 'export async function getSessionForRequest(request: Request, options: { allowRefresh: boolean; signal?: AbortSignal }) {',
    },
    { base: 28, head: 40, kind: 'context', text: '  const current = store.get();' },
    { base: 29, head: 41, kind: 'context', text: '  if (current) return current;' },
    { base: 30, head: 42, kind: 'context', text: '  if (!options.allowRefresh) return null;' },
    { base: 31, head: 43, kind: 'context', text: '  if (options.signal?.aborted) return null;' },
    { base: 32, head: 44, kind: 'context', text: '  return refreshSession();' },
    { base: 33, head: 45, kind: 'context', text: '}' },
  ],
  store: [
    { base: 41, head: 41, kind: 'context', text: 'export function logout() {' },
    { base: 42, head: 42, kind: 'context', text: '  store.set(null);' },
    { base: null, head: 43, kind: 'added', text: '  store.generation += 1;' },
    { base: 43, head: 44, kind: 'context', text: '}' },
  ],
  client: [
    { base: 12, head: 12, kind: 'context', text: 'export async function authenticatedRequest() {' },
    { base: 13, head: null, kind: 'removed', text: '  return send(store.get());' },
    { base: null, head: 13, kind: 'added', text: '  const session = await getSessionForRequest(request, options);' },
    { base: null, head: 14, kind: 'added', text: '  return send(session);' },
    { base: 14, head: 15, kind: 'context', text: '}' },
  ],
  test: [
    { base: null, head: 12, kind: 'added', text: 'it("ignores refresh after logout", async () => {' },
    { base: null, head: 13, kind: 'added', text: '  const pending = refreshSession();' },
    { base: null, head: 14, kind: 'added', text: '  logout();' },
    { base: null, head: 15, kind: 'added', text: '  resolveSession(oldSession);' },
    { base: null, head: 16, kind: 'added', text: '  await pending;' },
    { base: null, head: 17, kind: 'added', text: '  expect(store.get()).toBe(null);' },
    { base: null, head: 18, kind: 'added', text: '});' },
  ],
  docs: [
    { base: 1, head: 1, kind: 'context', text: '# Session refresh' },
    { base: 2, head: null, kind: 'removed', text: 'Each caller starts a refresh request.' },
    { base: null, head: 2, kind: 'added', text: 'Concurrent callers share the in-flight request.' },
    { base: null, head: 3, kind: 'added', text: 'Logout invalidates the generation before older results can update the store.' },
    { base: null, head: 4, kind: 'added', text: 'Example markup stays literal: <img src=x onerror=alert(1)>.' },
  ],
}

/**
 * Creates an independent set of threads for the populated UI preview.
 * @returns Four fictional threads, or none for the first-use preview.
 */
export function initialSampleThreads(): SampleThread[] {
  if (new URLSearchParams(location.search).get('sample') !== 'review') return []
  return [
    makeSampleThread(1, { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 27, end: 30 } }, 'Can an old refresh restore a session?', 'answered'),
    {
      ...makeSampleThread(2, { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 23, end: 35 } }, 'Is this a cache, or only an in-flight promise?', 'answered'),
      followup: 'Could you walk me through the failure case?',
      messages: [
        { author: 'You', paragraphs: ['Is this caching the session, or just sharing an in-flight request?'] },
        { author: 'Agent', paragraphs: ['It shares work, not the result. While the request is running, every caller receives the same promise.', 'A calls → starts a request. B calls → joins the same promise. Once settled, C starts a new request.', 'That is why cleanup is in finally(): success or failure, the next call can try again.'], citations: [{ fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 23, end: 35 } }] },
      ],
    },
    makeSampleThread(3, { fileId: 'test', revision: 'a81c2f4', range: { side: 'head', start: 12, end: 18 } }, 'What happens when the refresh request fails?', 'investigating'),
    {
      ...makeSampleThread(4, { fileId: 'store', revision: 'a81c2f4', range: { side: 'head', start: 41, end: 44 } }, 'Why increment the session generation on logout?', 'understood'),
      messages: [
        { author: 'You', paragraphs: ['Why increment the session generation on logout?'] },
        { author: 'Agent', paragraphs: ['It invalidates the generation captured by an older refresh, so that result cannot replace the cleared store.'], citations: [{ fileId: 'store', revision: 'a81c2f4', range: { side: 'head', start: 41, end: 44 } }] },
        { author: 'You', paragraphs: ['So this guards the store write rather than cancelling the request?'] },
        { author: 'Agent', paragraphs: ['Yes. In this fictional excerpt, the generation check guards the write. Cancellation is a separate concern.'] },
      ],
    },
  ]
}

/**
 * Creates a local transcript with explicitly illustrative, never generated, content.
 * @param id Local thread number.
 * @param anchor Pinned sample source.
 * @param question Submitted question text.
 * @param status Initial preview status.
 * @returns An independent thread and transcript.
 */
export function makeSampleThread(id: number, anchor: SampleAnchor, question: string, status: SampleThread['status']): SampleThread {
  const messages: SampleMessage[] = [{ author: 'You', paragraphs: [question] }]
  if (status === 'answered' || status === 'understood') {
    messages.push({ author: 'Agent', paragraphs: [...sampleExplanation], citations: [anchor] })
  }
  return { id, anchor, question, status, followup: '', messages }
}

/**
 * Describes a pinned sample source location.
 * @param anchor Source coordinates to describe.
 * @returns Path, side/range and revision without implying verified evidence.
 */
export function sampleSourceLabel(anchor: SampleAnchor): string {
  const path = sampleFiles.find((file): boolean => file.id === anchor.fileId)?.path ?? anchor.fileId
  return `${path} · ${anchor.range ? `${anchor.range.side} L${anchor.range.start}–${anchor.range.end}` : 'Whole file'} · ${anchor.revision}`
}

export const sampleExplanation = [
  'This is sample copy for reviewing typography and scrolling, not an answer from an agent.',
  'In this fictional change, the generation check guards the write to the shared session store. A refresh captures the current generation before starting the request.',
  'Clearing the session increments that generation. An older response can still arrive, but it no longer matches the current store generation and does not replace the stored session.',
  'The return value is a separate concern. The promise still resolves with the response it received. A caller that uses that value directly may need its own check.',
  'A repository-backed explanation would need to inspect the callers and the store implementation before making a stronger claim. These source lines and this explanation are only layout fixtures.',
  'Related sources should remain visible and tied to a revision. A source citation is not evidence until the backend has resolved it against the pinned snapshot.',
  'An answered question is not automatically understood. That is a reader decision, separate from whether a file has been viewed or whether a change should be merged.',
  'Longer conversations must keep native scrolling and keyboard focus available. This final paragraph intentionally makes the sample longer so that behavior can be reviewed without a connected service.',
]

export const sampleChoices = [
  'Every caller immediately receives a new session.',
  'An older response cannot overwrite the newer store generation.',
  'The browser cancels all earlier network requests.',
  'The promise can never resolve after logout.',
]
