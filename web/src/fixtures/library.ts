import type { SampleThread } from './workspace'

export type SourcePreviewState = 'ready' | 'loading' | 'offline' | 'denied' | 'missing' | 'unavailable' | 'stale'

export interface SamplePullRequest {
  id: string
  repository: string
  ownerRepository: string
  number: number
  title: string
  tabTitle: string
  author: string
  status: 'Open' | 'Merged' | 'Closed'
  updated: string
  updateOrder: number
  createdOrder: number
  snapshot?: { revision: string; path: string; source: string[]; questions: string[]; understood: number }
}

export interface SampleWorkspaceEntry extends SamplePullRequest {
  open: boolean
  sourceState: SourcePreviewState
  visited: number
}

export interface QuestionSummary {
  open: number
  understood: number
}

export const samplePullRequests: SamplePullRequest[] = [
  { id: 'github.com/acme/platform/248', ownerRepository: 'acme/platform', repository: 'platform', number: 248, title: 'Deduplicate concurrent session refreshes', tabTitle: 'Session refresh', author: 'Jamie Chen', status: 'Open', updated: '24 min ago', updateOrder: 8, createdOrder: 7 },
  { id: 'github.com/acme/platform/251', ownerRepository: 'acme/platform', repository: 'platform', number: 251, title: 'Handle retries after a failed refresh', tabTitle: 'Retry handling', author: 'Sam Rivera', status: 'Open', updated: '1 hour ago', updateOrder: 7, createdOrder: 8 },
  { id: 'github.com/acme/dashboard/86', ownerRepository: 'acme/dashboard', repository: 'dashboard', number: 86, title: 'Keep filters when navigating between reports', tabTitle: 'Report filters', author: 'Priya Shah', status: 'Open', updated: '3 hours ago', updateOrder: 6, createdOrder: 6 },
  { id: 'github.com/acme/platform/239', ownerRepository: 'acme/platform', repository: 'platform', number: 239, title: 'Move audit events to a background queue', tabTitle: 'Audit queue', author: 'Jamie Chen', status: 'Merged', updated: 'Yesterday', updateOrder: 5, createdOrder: 4, snapshot: { revision: 'b47d920', path: 'src/audit/queue.ts', source: ['export function recordAudit(event: AuditEvent) {', '  return queue.enqueue(event);', '}'], questions: ['What happens if the queue is unavailable?', 'Why enqueue instead of writing inline?', 'Where is the event serialized?', 'Does enqueue acknowledge delivery?'], understood: 3 } },
  { id: 'github.com/acme/dashboard/82', ownerRepository: 'acme/dashboard', repository: 'dashboard', number: 82, title: 'Add keyboard navigation to the command menu', tabTitle: 'Command menu', author: 'Alex Morgan', status: 'Merged', updated: '2 days ago', updateOrder: 4, createdOrder: 3, snapshot: { revision: 'c50d271', path: 'src/menu/keyboard.ts', source: ['export function moveSelection(index: number, count: number) {', '  return (index + 1) % count;', '}'], questions: ['Why wrap the selected index?', 'How is an empty menu handled?'], understood: 2 } },
  { id: 'github.com/acme/platform/231', ownerRepository: 'acme/platform', repository: 'platform', number: 231, title: 'Replace the session cookie serializer', tabTitle: 'Cookie serializer', author: 'Sam Rivera', status: 'Closed', updated: '3 days ago', updateOrder: 3, createdOrder: 2, snapshot: { revision: 'd63e284', path: 'src/session/cookie.ts', source: ['export function sessionCookie(value: string) {', '  return serialize("session", value, { httpOnly: true });', '}'], questions: ['Which cookie attributes are preserved?', 'How is expiration represented?', 'Why keep HttpOnly enabled?'], understood: 1 } },
  { id: 'github.com/acme/platform/244', ownerRepository: 'acme/platform', repository: 'platform', number: 244, title: 'Make request timeouts configurable', tabTitle: 'Request timeouts', author: 'Priya Shah', status: 'Open', updated: '4 days ago', updateOrder: 2, createdOrder: 5 },
  { id: 'github.com/acme/platform/224', ownerRepository: 'acme/platform', repository: 'platform', number: 224, title: 'Remove the legacy permissions adapter', tabTitle: 'Permissions adapter', author: 'Alex Morgan', status: 'Merged', updated: 'Last week', updateOrder: 1, createdOrder: 1, snapshot: { revision: 'e74f395', path: 'src/permissions/check.ts', source: ['export function canRead(user: User, resource: Resource) {', '  return policy.allows(user, "read", resource);', '}'], questions: ['Where are permission rules evaluated?', 'Which callers used the old adapter?'], understood: 2 } },
]

/**
 * Creates an isolated tab entry without claiming that source was fetched.
 * @param pr Fictional library record.
 * @param visited Local ordering value.
 * @returns A page-memory workspace with an explicit source-preview state.
 */
export function sampleWorkspaceEntry(pr: SamplePullRequest, visited: number): SampleWorkspaceEntry {
  return { ...pr, open: true, visited, sourceState: pr.id === 'github.com/acme/platform/248' || pr.id === 'github.com/acme/platform/251' ? 'ready' : pr.snapshot ? 'stale' : 'loading' }
}

/**
 * Creates illustrative historical transcripts for exactly the selected sample PR.
 * @param pr Fictional merged or closed source record.
 * @returns Independent reader states, source anchors, transcripts and drafts.
 */
export function historicalSampleThreads(pr: SamplePullRequest): SampleThread[] {
  const snapshot = pr.snapshot
  if (!snapshot) return []
  return snapshot.questions.map((question, index): SampleThread => ({
    id: index + 1,
    anchor: { fileId: snapshot.path, revision: snapshot.revision, range: null },
    question,
    status: index >= snapshot.questions.length - snapshot.understood ? 'understood' : 'answered',
    followup: '',
    messages: [
      { author: 'You', paragraphs: [question] },
      { author: 'Agent', paragraphs: [`Illustrative retained history for ${pr.ownerRepository} #${pr.number}. This quoted sample was not generated by an agent and is not a verified answer.`, 'The historical question stays associated with its original source snapshot, regardless of whether the fictional PR is merged or closed.'] },
    ],
  }))
}
