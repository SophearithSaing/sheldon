import { computed, onUnmounted, ref, type ComputedRef, type Ref } from 'vue'
import type { RuntimeClient } from './client'
import { useSavedThreads, type SavedThreads, type SavedContext, type SessionStatus, type ThreadDetail } from './history'
import { activeAnswer, questionError, sameAnchor, QuestionAdmissionError, type InitialQuestion, type QuestionAnswer } from './questions'

export interface RuntimeTurn {
  live: boolean
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
  context?: SavedContext
  version?: string
  savedPersistence?: ThreadDetail['persistence']
  session?: SessionStatus
  continuationError?: string
}
interface Conversations {
  history: SavedThreads
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
  openSaved: (prID: string, threadID: string, accept?: () => boolean) => Promise<RuntimeThread | undefined>
  refreshSaved: (thread: RuntimeThread) => Promise<void>
  activate: (thread: RuntimeThread) => void
}

/**
 * Owns page-memory transcripts independently of which source file or panel is visible.
 * @param client Current verified connection, without exposing its captured credential.
 * @returns Draft-safe live/saved thread state and exact-answer operations.
 */
export function useConversations(client: () => RuntimeClient | null): Conversations {
  const history = useSavedThreads(client)
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
    if (scoped.some((thread): boolean => thread.savedPersistence === 'failed' || thread.turns.some((turn): boolean => turn.live && turn.answer.persistence === 'failed'))) return 'An answer is not saved. Reconnect observation or retry saving before continuing; drafts stay editable and no question will be resent.'
    if (scoped.some((thread): boolean => thread.uncertain || thread.session?.state === 'unknown' || thread.turns.some((turn): boolean => turn.live && turn.answer.executionUnknown))) return 'Execution is uncertain in this shared PR-revision session. Drafts stay editable; sending remains unavailable.'
    if (scoped.some((thread): boolean => thread.pending || thread.turns.some((turn): boolean => activeAnswer(turn.answer)))) return 'A question is using this shared PR-revision session. Drafts stay editable until a terminal outcome is confirmed.'
    if (scoped.some((thread): boolean => thread.savedPersistence === 'saving' || thread.turns.some((turn): boolean => turn.answer.persistence === 'saving'))) return 'Saving the latest answer. Drafts stay editable; sending waits for the save to finish.'
    const unavailable = scoped.find((thread): boolean => !!thread.continuationError || thread.session?.state === 'unavailable' || thread.session?.state === 'busy')
    if (unavailable) return unavailable.continuationError || (unavailable.session?.state === 'busy' ? 'This shared revision session is busy in another view. Refresh saved history or reconnect the active answer; drafts stay editable.' : 'Original session continuation is unavailable. Saved history remains readable. Refresh the saved thread after checking the original source/session or local storage; no replacement will be created.')
    return ''
  }

  /**
   * Allocates only presentation state for an already admitted backend answer.
   * @param answer Scoped admission snapshot.
   * @param live Whether this turn belongs to live observation rather than a disk snapshot.
   * @returns Observable runtime turn.
   */
  function newTurn(answer: QuestionAnswer, live = true): RuntimeTurn {
    return { live, answer, observation: live ? 'connecting' : 'done', observationError: '', stopPending: false, stopNotice: '', savePending: false, saveNotice: '' }
  }

  /**
   * Updates only current-answer saving/session evidence, never old historical execution flags.
   * @param turn Latest observed turn.
   */
  function feedback(turn: RuntimeTurn): void {
    const owner = threads.value.find((thread): boolean => thread.id === turn.answer.threadID)
    if (!owner || owner.turns.at(-1) !== turn) return
    owner.savedPersistence = turn.answer.persistence
    if (!turn.live) return
    const status: SessionStatus = turn.answer.persistence === 'failed' ? { state: 'unavailable', reason: 'answer-not-saved' } : turn.answer.executionUnknown ? { state: 'unknown', reason: 'execution-uncertain' } : activeAnswer(turn.answer) || turn.answer.persistence === 'saving' ? { state: 'busy', reason: 'question-active' } : { state: 'ready', reason: 'original-session-verified' }
    for (const thread of threads.value) if (thread.input.revision.prID === turn.answer.prID && thread.turns[0]?.answer.revisionID === turn.answer.revisionID) thread.session = status
  }

  /**
   * Applies exact-answer status updates without regressing terminal text or saved generations.
   * @param turn Original observable runtime turn.
   * @param snapshot Scoped cumulative backend snapshot.
   */
  function update(turn: RuntimeTurn, snapshot: QuestionAnswer): void {
    const current = turn.answer
    if (turn.live && current.savedVersion && (!snapshot.savedVersion || BigInt(snapshot.savedVersion) < BigInt(current.savedVersion))) return
    if (!activeAnswer(current) && !current.executionUnknown && (snapshot.state !== current.state || snapshot.text !== current.text || snapshot.executionUnknown)) return
    if (current.persistence === 'saved' && snapshot.persistence !== 'saved' && !activeAnswer(current) && snapshot.savedVersion === current.savedVersion) return
    turn.answer = { ...snapshot, stopRequested: current.stopRequested || snapshot.stopRequested }
    turn.live = true
    turn.observation = activeAnswer(snapshot) || snapshot.persistence === 'saving' ? 'watching' : 'done'
    if (!activeAnswer(snapshot)) turn.stopNotice = ''
    feedback(turn)
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
   * Merges checked saved answer order without replacing live turns, drafts or original source.
   * @param detail Accepted exact-thread disk snapshot.
   * @returns Existing or newly restored thread, or undefined for stale/conflicting data.
   */
  function mergeSaved(detail: ThreadDetail): RuntimeThread | undefined {
    let thread = threads.value.find((item): boolean => item.id === detail.threadID)
    if (thread) {
      const newest = [thread.version ?? '0', ...thread.turns.map((turn): string => turn.answer.savedVersion ?? '0')].reduce((left: bigint, value: string): bigint => BigInt(value) > left ? BigInt(value) : left, 0n)
      if (BigInt(detail.version) < newest) return thread
      if (thread.input.revision.prID !== detail.prID || thread.turns[0]?.answer.revisionID !== detail.revisionID || thread.input.revision.baseBranch !== detail.context.revision.baseBranch || thread.input.revision.baseSource !== detail.context.revision.baseSource || !sameAnchor(thread.input.anchor, detail.context.anchor) || thread.context && JSON.stringify(thread.context) !== JSON.stringify(detail.context)) { history.error.value = 'Saved history conflicts with the original thread. Existing text and drafts were retained.'; return undefined }
      const local = new Map(thread.turns.map((turn): [string, RuntimeTurn] => [turn.answer.answerID, turn]))
      const savedIDs = new Set(detail.answers.map((answer): string => answer.answerID))
      const currentOrder = thread.turns.filter((turn): boolean => savedIDs.has(turn.answer.answerID)).map((turn): string => turn.answer.answerID)
      const savedOrder = detail.answers.filter((answer): boolean => local.has(answer.answerID)).map((answer): string => answer.answerID)
      if (JSON.stringify(currentOrder) !== JSON.stringify(savedOrder) || thread.turns.some((turn): boolean => !turn.live && !savedIDs.has(turn.answer.answerID))) { history.error.value = 'Saved history conflicts with the original thread. Existing text and drafts were retained.'; return undefined }
      for (const saved of detail.answers) {
        const current = local.get(saved.answerID)?.answer
        if (current && (current.question !== saved.question || !activeAnswer(current) && !current.executionUnknown && !activeAnswer(saved) && !saved.executionUnknown && (current.state !== saved.state || current.text !== saved.text))) { history.error.value = 'Saved history conflicts with the original thread. Existing text and drafts were retained.'; return undefined }
      }
      if (!thread.pending && !thread.uncertain) {
        const merged = detail.answers.map((saved): RuntimeTurn => {
          const turn = local.get(saved.answerID)
          if (!turn) return newTurn(saved, false)
          if (!turn.live) turn.answer = saved
          return turn
        })
        merged.push(...thread.turns.filter((turn): boolean => !savedIDs.has(turn.answer.answerID)))
        thread.turns = merged
      }
    } else {
      thread = { id: detail.threadID, input: { revision: { ...detail.context.revision }, anchor: { ...detail.context.anchor }, question: detail.answers[0]!.question }, path: (detail.context.anchor.kind === 'lines' && detail.context.anchor.side === 'old' ? detail.context.oldPath : detail.context.newPath ?? detail.context.oldPath) ?? 'Original pinned source', turns: detail.answers.map((answer): RuntimeTurn => newTurn(answer, false)), draft: '', pending: false, uncertain: false, error: '' }
      threads.value.push(thread)
      thread = threads.value.at(-1)!
    }
    thread.context = detail.context
    thread.version = detail.version
    const latest = thread.turns.at(-1)!
    thread.savedPersistence = latest.live ? latest.answer.persistence ?? detail.persistence : detail.persistence
    const status = latest.live && (activeAnswer(latest.answer) || latest.answer.executionUnknown || latest.answer.persistence === 'failed' || latest.answer.persistence === 'saving') ? thread.session : detail.session
    if (status) for (const item of threads.value) if (item.turns[0]?.answer.revisionID === detail.revisionID) item.session = status
    thread.continuationError = ''
    return thread
  }

  /**
   * Explicitly reads one saved thread without selecting it or starting observation/inference.
   * @param prID Routed PR.
   * @param threadID Requested saved thread.
   * @param accept Optional current-user-intent guard before restoring visible thread state.
   * @returns Draft-safe restored thread when the response is current.
   */
  async function openSaved(prID: string, threadID: string, accept?: () => boolean): Promise<RuntimeThread | undefined> {
    const epoch = generation
    const connection = client()
    const detail = await history.refreshThread(prID, threadID)
    if (!detail || epoch !== generation || connection !== client() || accept && !accept()) return undefined
    return mergeSaved(detail)
  }

  /**
   * Attaches only the latest accepted active/unsaved answer to its exact read-only observer.
   * @param thread Explicitly opened thread.
   */
  function activate(thread: RuntimeThread): void {
    const turn = thread.turns.at(-1)
    if (turn && (activeAnswer(turn.answer) || thread.savedPersistence === 'saving' || thread.savedPersistence === 'failed') && !observers.has(turn.answer.answerID)) void observe(turn)
  }

  /**
   * Refreshes the current saved transcript while retaining its draft and live observer.
   * @param thread Original visible thread.
   */
  async function refreshSaved(thread: RuntimeThread): Promise<void> {
    const result = await openSaved(thread.input.revision.prID, thread.id)
    if (result && selectedID.value === result.id) activate(result)
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
      thread.savedPersistence = answer.persistence
      for (const item of threads.value) if (item.turns[0]?.answer.revisionID === answer.revisionID) item.session = { state: 'busy', reason: 'question-active' }
      void observe(thread.turns.at(-1)!)
    } catch (failure: unknown) {
      if (controller.signal.aborted || epoch !== generation || client() !== connection) return
      thread.uncertain = !(failure instanceof QuestionAdmissionError) || failure.uncertain
      thread.error = failure instanceof QuestionAdmissionError ? failure.message : 'Follow-up delivery may be uncertain. The draft was retained; do not resend automatically.'
      if (thread.context && failure instanceof QuestionAdmissionError && (failure.status === 410 || failure.status === 503)) thread.continuationError = thread.error
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
    history.clear()
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
  return { threads, selectedID, current, add, busy, followup, observe, stop, save, clear, history, openSaved, refreshSaved, activate }
}
