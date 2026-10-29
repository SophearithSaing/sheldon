// Deterministic design samples only. None of these records came from a repository or agent.
export const workspaceTabs = [
  { id: 'files', label: 'Files changed' },
  { id: 'questions', label: 'Questions' },
  { id: 'quiz', label: 'Quiz' },
] as const

export interface DiffLine {
  base: number | null
  head: number | null
  kind: 'context' | 'added' | 'removed'
  text: string
}

export const sampleFiles = [
  { id: 'refresh', path: 'src/auth/refresh-session.ts' },
  { id: 'store', path: 'src/auth/session-store.ts' },
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
    {
      base: 1,
      head: 1,
      kind: 'context',
      text: '// Sample session store, not a loaded application file.',
    },
    { base: 2, head: 2, kind: 'context', text: 'let session: Session | null = null;' },
    { base: null, head: 3, kind: 'added', text: 'let generation = 0;' },
    { base: 3, head: 4, kind: 'context', text: '' },
    { base: 4, head: 5, kind: 'context', text: 'export function clearSession() {' },
    { base: null, head: 6, kind: 'added', text: '  generation += 1;' },
    { base: 5, head: 7, kind: 'context', text: '  session = null;' },
    { base: 6, head: 8, kind: 'context', text: '}' },
  ],
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
