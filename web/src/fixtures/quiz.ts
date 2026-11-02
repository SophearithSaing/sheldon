import type { SampleAnchor } from './workspace'

export interface SampleQuizQuestion {
  prompt: string
  topic: string
  choices: string[]
  correct: number
  explanation: string[]
  anchor: SampleAnchor
}

export const sampleQuiz: SampleQuizQuestion[] = [
  { prompt: 'In this fictional example, what does the generation check protect?', topic: 'Store boundary', choices: ['Every caller immediately receives a new session.', 'An older response cannot overwrite the newer store generation.', 'The browser cancels all earlier network requests.', 'The promise can never resolve after logout.'], correct: 1, explanation: ['The captured generation is compared with the current store generation before writing. This guards the store update, not every caller of the promise.'], anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 25, end: 30 } } },
  { prompt: 'A user logs out while a session refresh is in flight. What happens when the old request resolves?', topic: 'Logout boundary', choices: ['It cancels the pending network request.', 'It rejects every caller awaiting that promise.', 'It skips the store write, but still returns the session.', 'It starts a new refresh with the latest generation.'], correct: 2, explanation: ['The store write is skipped. The generation changes on logout, so the old response cannot update the store.', 'That does not cancel the request. The promise still resolves with the returned session. The return is outside the generation guard.'], anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 27, end: 30 } } },
  { prompt: 'What does a concurrent caller receive while a refresh is already in flight?', topic: 'Shared work', choices: ['A new request for each caller.', 'The same in-flight promise.', 'A cached session forever.', 'An immediate cancellation error.'], correct: 1, explanation: ['The early return reuses inFlight. This shares pending work rather than retaining a settled result indefinitely.'], anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 23, end: 26 } } },
  { prompt: 'Why clear inFlight in finally()?', topic: 'Failure boundary', choices: ['To prevent later refresh attempts.', 'To cancel every caller.', 'To increment the store generation.', 'To allow another request after success or failure.'], correct: 3, explanation: ['The sample clears the shared promise when it settles, regardless of whether it fulfilled or rejected. Later calls can begin another refresh.'], anchor: { fileId: 'refresh', revision: 'a81c2f4', range: { side: 'head', start: 31, end: 35 } } },
  { prompt: 'Which logout change invalidates an earlier refresh generation?', topic: 'Logout state', choices: ['Incrementing store.generation.', 'Returning the old session.', 'Starting a new HTTP request.', 'Checking a quiz answer.'], correct: 0, explanation: ['Logout clears the session and increments the generation. The fictional source status, quiz and reader understanding are independent.'], anchor: { fileId: 'store', revision: 'a81c2f4', range: { side: 'head', start: 41, end: 44 } } },
]
