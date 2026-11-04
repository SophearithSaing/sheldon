import { computed, onUnmounted, ref, type ComputedRef, type Ref } from 'vue'
import type { RuntimeClient } from './client'
import { activeAnswer, questionError, QuestionAdmissionError, type InitialQuestion, type QuestionAnswer } from './questions'

export interface RuntimeTurn {
  answer: QuestionAnswer
  observation: 'connecting' | 'watching' | 'lost' | 'done'
  observationError: string
  stopPending: boolean
  stopNotice: string
  savePending: boolean
  saveNotice: string
}
export interface RuntimeThread {
  id: string
  input: InitialQuestion
  path: string
  turns: RuntimeTurn[]
  draft: string
  pending: boolean
  uncertain: boolean
  error: string
}
interface Conversations {
  threads: Ref<RuntimeThread[]>
  selectedID: Ref<string>
  current: ComputedRef<RuntimeThread | undefined>
  add: (answer: QuestionAnswer, input: InitialQuestion, path: string) => void
  busy: (revisionID: string) => string
  followup: (thread: RuntimeThread) => Promise<void>
  observe: (turn: RuntimeTurn) => Promise<void>
  stop: (turn: RuntimeTurn) => Promise<void>
  save: (turn: RuntimeTurn) => Promise<void>
  clear: () => void
}

/**
 * Owns page-memory transcripts independently of which source file or panel is visible.
 * @param client Current verified connection, without exposing its captured credential.
 * @returns Runtime-only thread state and exact-answer operations.
 */
export function useConversations(client: () => RuntimeClient | null): Conversations {
  const threads = ref<RuntimeThread[]>([])
  const selectedID = ref('')
  const current = computed((): RuntimeThread | undefined => threads.value.find((thread): boolean => thread.id === selectedID.value))
  const observers = new Map<string, AbortController>()
  const requests = new Set<AbortController>()
  let generation = 0

  /**
   * Keeps the shared revision unavailable until every known execution is confirmed terminal.
   * @param revisionID Exact comparison identity.
   * @returns Send-unavailable explanation, or an empty string when no local work blocks it.
   */
  function busy(revisionID: string): string {
    const scoped = threads.value.filter((thread): boolean => thread.turns[0]?.answer.revisionID === revisionID)
    if (scoped.some((thread): boolean => thread.turns.some((turn): boolean => turn.answer.persistence === 'failed'))) return 'An answer is not saved. Retry saving before continuing; drafts stay editable and no question will be resent.'
    if (scoped.some((thread): boolean => thread.uncertain || thread.turns.some((turn): boolean => turn.answer.executionUnknown))) return 'Execution is uncertain in this shared PR-revision session. Drafts stay editable; sending remains unavailable.'
    if (scoped.some((thread): boolean => thread.pending || thread.turns.some((turn): boolean => activeAnswer(turn.answer)))) return 'A question is using this shared PR-revision session. Drafts stay editable until a terminal outcome is confirmed.'
    if (scoped.some((thread): boolean => thread.turns.some((turn): boolean => turn.answer.persistence === 'saving'))) return 'Saving the latest answer. Drafts stay editable; sending waits for the save to finish.'
    return ''
  }

  /**
   * Allocates only presentation state for an already admitted backend answer.
   * @param answer Scoped admission snapshot.
   * @returns Observable runtime turn.
   */
  function newTurn(answer: QuestionAnswer): RuntimeTurn {
    return { answer, observation: 'connecting', observationError: '', stopPending: false, stopNotice: '', savePending: false, saveNotice: '' }
  }

  /**
   * Applies exact-answer status updates without regressing terminal text or saved generations.
   * @param turn Original observable runtime turn.
   * @param snapshot Scoped cumulative backend snapshot.
   */
  function update(turn: RuntimeTurn, snapshot: QuestionAnswer): void {
    const current = turn.answer
    if (current.savedVersion && (!snapshot.savedVersion || BigInt(snapshot.savedVersion) < BigInt(current.savedVersion))) return
    if (!activeAnswer(current) && !current.executionUnknown && (snapshot.state !== current.state || snapshot.text !== current.text || snapshot.executionUnknown)) return
    if (current.persistence === 'saved' && snapshot.persistence !== 'saved' && !activeAnswer(current) && snapshot.savedVersion === current.savedVersion) return
    turn.answer = { ...snapshot, stopRequested: current.stopRequested || snapshot.stopRequested }
    turn.observation = activeAnswer(snapshot) || snapshot.persistence === 'saving' ? 'watching' : 'done'
    if (!activeAnswer(snapshot)) turn.stopNotice = ''
  }

  /**
   * Observes one original answer, replacing snapshots instead of appending duplicate text.
   * @param turn Exact admitted turn, even if its thread is not selected.
   */
  async function observe(turn: RuntimeTurn): Promise<void> {
    const connection = client()
    if (!connection) return
    const id = turn.answer.answerID
    observers.get(id)?.abort()
    const controller = new AbortController()
    const epoch = generation
    observers.set(id, controller)
    turn.observation = 'connecting'
    turn.observationError = ''
    try {
      await connection.observeAnswer(turn.answer, controller.signal, (snapshot: QuestionAnswer): void => {
        if (controller.signal.aborted || epoch !== generation || client() !== connection || observers.get(id) !== controller) return
        update(turn, snapshot)
      })
      if (!controller.signal.aborted && epoch === generation && client() === connection) turn.observation = 'done'
    } catch {
      if (!controller.signal.aborted && epoch === generation && client() === connection) {
        turn.observation = 'lost'
        turn.observationError = 'Answer observation stopped. Execution may still be active. Reconnect observation to check; no question will be resent.'
      }
    } finally {
      if (observers.get(id) === controller) observers.delete(id)
    }
  }

  /**
   * Adds one new UI thread only after successful admission and starts its observer.
   * @param answer Validated original admission.
   * @param input Immutable source selection and literal question.
   * @param path Original file label from the displayed pinned source.
   */
  function add(answer: QuestionAnswer, input: InitialQuestion, path: string): void {
    if (threads.value.some((thread): boolean => thread.id === answer.threadID)) return
    threads.value.push({ id: answer.threadID, input, path, turns: [newTurn(answer)], draft: '', pending: false, uncertain: false, error: '' })
    selectedID.value = answer.threadID
    const thread = current.value!
    void observe(thread.turns[0]!)
  }

  /**
   * Admits one follow-up while retaining the thread's original source and failed draft.
   * @param thread Submitting thread, independent of the current panel selection.
   */
  async function followup(thread: RuntimeThread): Promise<void> {
    const previous = thread.turns.at(-1)?.answer
    const connection = client()
    if (!previous || !connection || thread.pending || thread.uncertain) return
    thread.error = questionError(thread.draft) || busy(previous.revisionID)
    if (thread.error) return
    const input: InitialQuestion = { ...thread.input, question: thread.draft }
    const controller = new AbortController()
    const epoch = generation
    requests.add(controller)
    thread.pending = true
    try {
      const answer = await connection.askFollowup(previous, input, controller.signal)
      if (controller.signal.aborted || epoch !== generation || client() !== connection) return
      if (thread.turns.some((turn): boolean => turn.answer.answerID === answer.answerID)) throw new QuestionAdmissionError('Unexpected follow-up admission. Delivery may be uncertain; do not resend.', true)
      thread.turns.push(newTurn(answer))
      thread.draft = ''
      void observe(thread.turns.at(-1)!)
    } catch (failure: unknown) {
      if (controller.signal.aborted || epoch !== generation || client() !== connection) return
      thread.uncertain = !(failure instanceof QuestionAdmissionError) || failure.uncertain
      thread.error = failure instanceof QuestionAdmissionError ? failure.message : 'Follow-up delivery may be uncertain. The draft was retained; do not resend automatically.'
    } finally {
      requests.delete(controller)
      if (epoch === generation && client() === connection) thread.pending = false
    }
  }

  /**
   * Requests Stop for only this answer without letting an acknowledgement regress streamed text.
   * @param turn Exact turn associated with the Stop control.
   */
  async function stop(turn: RuntimeTurn): Promise<void> {
    const connection = client()
    if (!connection || turn.stopPending || !activeAnswer(turn.answer)) return
    const controller = new AbortController()
    const epoch = generation
    requests.add(controller)
    turn.stopPending = true
    try {
      const snapshot = await connection.stopAnswer(turn.answer, AbortSignal.any([controller.signal, AbortSignal.timeout(10000)]))
      if (controller.signal.aborted || epoch !== generation || client() !== connection) return
      if (activeAnswer(turn.answer) || turn.answer.persistence === 'saving') {
        if (!activeAnswer(snapshot)) {
          update(turn, snapshot)
          turn.observationError = ''
          if (turn.answer.persistence !== 'saving') observers.get(turn.answer.answerID)?.abort()
        } else turn.answer = { ...turn.answer, stopRequested: turn.answer.stopRequested || snapshot.stopRequested }
      }
      turn.stopNotice = activeAnswer(turn.answer) ? turn.answer.stopRequested ? 'Stop requested. Termination is not yet confirmed.' : 'Stop was not confirmed. Execution may still be active.' : turn.answer.executionUnknown ? 'Execution remains unknown; the shared session is unavailable.' : 'Stop check returned a terminal answer snapshot.'
    } catch {
      if (!controller.signal.aborted && epoch === generation && client() === connection) turn.stopNotice = 'Cannot confirm the Stop request. Execution may still be active; check observation before sending again.'
    } finally {
      requests.delete(controller)
      if (epoch === generation && client() === connection) turn.stopPending = false
    }
  }

  /**
   * Retries persistence only, retaining drafts and never sending the original question again.
   * @param turn Exact turn whose current text was not saved.
   */
  async function save(turn: RuntimeTurn): Promise<void> {
    const connection = client()
    if (!connection || turn.savePending || turn.answer.persistence !== 'failed') return
    const controller = new AbortController()
    const epoch = generation
    requests.add(controller)
    turn.savePending = true
    turn.saveNotice = ''
    try {
      const snapshot = await connection.saveAnswer(turn.answer, controller.signal)
      if (controller.signal.aborted || epoch !== generation || client() !== connection) return
      update(turn, snapshot)
      if (activeAnswer(turn.answer) || snapshot.persistence === 'saving') void observe(turn)
    } catch {
      if (!controller.signal.aborted && epoch === generation && client() === connection) turn.saveNotice = 'Saving failed. Text is retained in this runtime; no question was resent.'
    } finally {
      requests.delete(controller)
      if (epoch === generation && client() === connection) turn.savePending = false
    }
  }

  /** Aborts browser observation/requests only; accepted native work remains app-owned. */
  function clear(): void {
    generation++
    for (const observer of observers.values()) observer.abort()
    for (const request of requests) request.abort()
    observers.clear()
    requests.clear()
    threads.value = []
    selectedID.value = ''
  }

  /**
   * Warns before losing any thread's unsent draft or uncertain admission.
   * @param event Browser document-navigation event.
   */
  function beforeLeave(event: BeforeUnloadEvent): void {
    if (threads.value.some((thread): boolean => !!thread.draft || thread.pending || thread.uncertain || thread.turns.some((turn): boolean => turn.answer.persistence === 'failed' || turn.answer.persistence === 'saving'))) { event.preventDefault(); event.returnValue = '' }
  }
  window.addEventListener('beforeunload', beforeLeave)
  onUnmounted((): void => { clear(); window.removeEventListener('beforeunload', beforeLeave) })
  return { threads, selectedID, current, add, busy, followup, observe, stop, save, clear }
}
