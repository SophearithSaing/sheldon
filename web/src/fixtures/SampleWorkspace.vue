<script setup lang="ts">
import { computed, nextTick, ref, useId, watch } from 'vue'
import WorkspaceShell from '../components/WorkspaceShell.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiTabs from '../components/ui/UiTabs.vue'
import ChangedFileTree from '../components/ChangedFileTree.vue'
import FilesChangedPreview from './FilesChangedPreview.vue'
import QuestionsPreview from './QuestionsPreview.vue'
import ThreadConversation from './ThreadConversation.vue'
import QuizPreview from './QuizPreview.vue'
import SemanticReviewPreview from './SemanticReviewPreview.vue'
import type { QuestionSummary } from './library'
import {
  sampleFiles,
  workspaceTabs,
  initialSampleThreads,
  threadAnchors,
  type SampleThread,
  type SampleAnchor,
} from './workspace'

withDefaults(defineProps<{ workspaceNumber?: number }>(), { workspaceNumber: 248 })
const tabsId = useId()
const activeTab = ref('files')
const activeFile = ref('refresh')
const fileSearch = ref('')
const threads = ref(initialSampleThreads())
const activeThreadId = ref<number | null>(threads.value[0]?.id ?? null)
const shell = ref<InstanceType<typeof WorkspaceShell>>()
const filesPreview = ref<InstanceType<typeof FilesChangedPreview>>()
const viewed = ref<Record<string, boolean>>({ client: true, docs: true })
const conversationOpen = ref(threads.value.length > 0)
const semanticConversation = ref(false)
const visibleConversation = computed({ get: getConversation, set: setConversation })
const activeThread = computed(() => threads.value.find((thread): boolean => thread.id === activeThreadId.value))
const conversationTitle = computed((): string => activeThread.value ? `Question ${String(activeThread.value.id).padStart(2, '0')}` : 'Sample conversation')
const tabItems = computed(() => workspaceTabs.map((tab) => ({ ...tab, count: tab.id === 'files' ? sampleFiles.length : tab.id === 'questions' ? threads.value.length : undefined })))
const fileCounts = computed(countQuestions)
const busyThread = computed(findBusyThread)
const emit = defineEmits<{ summary: [value: QuestionSummary] }>()
const summary = computed(questionSummary)
watch(summary, reportSummary, { immediate: true })

/**
 * Reserves the quiz's right pane for explanations without losing the selected conversation.
 * @returns Whether the ordinary thread panel is visible.
 */
function getConversation(): boolean {
  return activeTab.value !== 'quiz' && (activeTab.value !== 'semantic' || semanticConversation.value) && conversationOpen.value
}

/**
 * Retains normal conversation disclosure state outside the quiz.
 * @param value Requested panel visibility.
 */
function setConversation(value: boolean): void {
  conversationOpen.value = value
}

/**
 * Reveals quiz or semantic evidence without creating or selecting a conversation thread.
 * @param anchor Pinned illustrative source location.
 */
async function showQuizSource(anchor: SampleAnchor): Promise<void> {
  activeTab.value = 'files'
  await nextTick()
  filesPreview.value?.revealAnchor(anchor)
}

/**
 * Returns from semantic navigation to the ordinary file view with its existing draft guards.
 * @param fileId Sample file selected in the semantic tree.
 */
async function selectSemanticFile(fileId: string): Promise<void> {
  await showFiles()
  filesPreview.value?.selectFile(fileId)
}

/**
 * Counts reader-controlled understanding independently from source status.
 * @returns Local thread totals for the sample library.
 */
function questionSummary(): QuestionSummary {
  const understood = threads.value.filter((thread): boolean => thread.status === 'understood').length
  return { open: threads.value.length - understood, understood }
}

/**
 * Reports page-memory question totals without claiming durable saving.
 * @param value Current sample counts.
 */
function reportSummary(value: QuestionSummary): void {
  emit('summary', value)
}

/**
 * Counts submitted sample threads, never unsent drafts.
 * @returns Per-file sample question counts.
 */
function countQuestions(): Record<string, number> {
  const result: Record<string, number> = {}
  for (const thread of threads.value) {
    for (const fileId of new Set(threadAnchors(thread).map((anchor): string => anchor.fileId))) result[fileId] = (result[fileId] ?? 0) + 1
  }
  return result
}

/**
 * Finds the single investigating question in the shared sample context.
 * @returns Its id, or null when the shared session is idle.
 */
function findBusyThread(): number | null {
  return threads.value.find((thread): boolean => thread.status === 'investigating')?.id ?? null
}

/**
 * Marks only the explicitly selected question understood or reopens it.
 * @param id Thread selected by the reader.
 * @param value Whether the reader confirms understanding.
 */
function setUnderstood(id: number, value: boolean): void {
  const thread = threads.value.find((item): boolean => item.id === id)
  if (!thread || (value && thread.status !== 'answered')) return
  thread.status = value ? 'understood' : 'answered'
}

/**
 * Selects a labeled thread-state preview without creating replies or native activity.
 * @param id Thread whose presentation changes.
 * @param state Explicitly selected sample state.
 */
function previewState(id: number, state: string): void {
  const thread = threads.value.find((item): boolean => item.id === id)
  if (!thread) return
  if (state === 'outdated') {
    thread.outdated = true
    return
  }
  if (state === 'investigating' && busyThread.value !== null && busyThread.value !== id) return
  if (state === 'answered' || state === 'investigating' || state === 'failed' || state === 'interrupted') {
    thread.status = state
    thread.outdated = false
  }
}

/**
 * Returns to ordinary source navigation without inventing a whole-PR composer.
 */
async function showFiles(): Promise<void> {
  activeTab.value = 'files'
  await nextTick()
  document.getElementById(`${tabsId}-tab-files`)?.focus()
}

/**
 * Reveals a citation in its pinned sample diff, retaining inline draft guards.
 * @param id Thread that owns the citation.
 * @param anchor Sample source coordinates.
 */
async function showSource(id: number, anchor: SampleAnchor): Promise<void> {
  activeTab.value = 'files'
  await nextTick()
  filesPreview.value?.revealSource(id, anchor)
}

/**
 * Keeps the owning thread selected while returning focus to its source coordinates.
 * @param id Thread whose citation was followed.
 * @param trigger Revealed gutter or whole-file control.
 */
async function sourceRevealed(id: number, trigger: HTMLElement): Promise<void> {
  activeThreadId.value = id
  await shell.value?.openConversation(trigger, false)
  trigger.focus({ preventScroll: true })
}

/**
 * Adds exactly one local sample thread without native dispatch or saving.
 * @param thread Validated sample question supplied by the Files view.
 */
function createThread(thread: SampleThread): void {
  if (!threads.value.some((item): boolean => item.id === thread.id)) threads.value.unshift(thread)
}

/**
 * Opens the selected local thread with a stable marker as the return-focus target.
 * @param id Existing sample thread identifier.
 * @param trigger Visible marker element.
 */
async function openThread(id: number, trigger: HTMLElement): Promise<void> {
  activeThreadId.value = id
  if (activeTab.value === 'semantic') semanticConversation.value = true
  await shell.value?.openConversation(trigger)
}

/**
 * Selects a sample file without changing the native tree disclosure.
 * @param id Sample file identifier.
 */
function selectFile(id: string): void {
  filesPreview.value?.selectFile(id)
}
</script>

<template>
  <div class="sample-workspace" :class="{ 'is-full-page': activeTab === 'quiz' || (activeTab === 'semantic' && !visibleConversation) }">
    <WorkspaceShell
      ref="shell"
      v-model:conversation-open="visibleConversation"
      :has-file-rail="activeTab === 'files'"
      :conversation-title="conversationTitle"
    >
      <template #summary>
        <h1 class="fixture-title">
          <template v-if="workspaceNumber === 251">Handle retries after a failed refresh</template>
          <template v-else>Deduplicate concurrent session refreshes</template>
          <span class="muted"> #{{ workspaceNumber }} · sample PR</span>
        </h1>
        <div class="fixture-meta">
          <span>Sample source</span>
          <code class="context-chip">fix/session-refresh → main</code>
          <span>Sample revision <code>a81c2f4</code></span>
          <span class="muted">Nothing is posted to GitHub.</span>
        </div>
        <UiButton class="github-sample" disabled :aria-describedby="`${tabsId}-github-unavailable`">
          Open in GitHub <span aria-hidden="true">↗</span>
        </UiButton>
        <p :id="`${tabsId}-github-unavailable`" class="sr-only">Unavailable: this is a sample PR, not a loaded GitHub pull request.</p>
      </template>
      <template #tabs>
        <UiTabs
          :id="tabsId"
          v-model="activeTab"
          label="Workspace previews"
          :items="tabItems"
        />
      </template>
      <template #files>
        <ChangedFileTree
          v-model="fileSearch"
          :files="sampleFiles"
          :current="activeFile"
          :counts="fileCounts"
          :viewed="viewed"
          @select="selectFile"
        />
      </template>
      <template #default>
        <div
          v-show="activeTab === 'files'"
          :id="`${tabsId}-panel-files`"
          role="tabpanel"
          :aria-labelledby="`${tabsId}-tab-files`"
          class="fixture-panel"
        >
          <FilesChangedPreview
            ref="filesPreview"
            v-model:file="activeFile"
            :threads="threads"
            :viewed="viewed"
            @create="createThread"
            @open="openThread"
            @reveal="sourceRevealed"
            @viewed="(id, value) => viewed[id] = value"
          />
        </div>

        <div
          v-show="activeTab === 'questions'"
          :id="`${tabsId}-panel-questions`"
          role="tabpanel"
          :aria-labelledby="`${tabsId}-tab-questions`"
          class="fixture-panel"
        >
          <QuestionsPreview :threads="threads" :current="conversationOpen ? activeThreadId : null" @open="openThread" @files="showFiles" />
        </div>

        <div
          v-show="activeTab === 'quiz'"
          :id="`${tabsId}-panel-quiz`"
          role="tabpanel"
          :aria-labelledby="`${tabsId}-tab-quiz`"
          class="fixture-panel"
        >
          <QuizPreview @source="showQuizSource" />
        </div>
        <div v-show="activeTab === 'semantic'" :id="`${tabsId}-panel-semantic`" role="tabpanel" :aria-labelledby="`${tabsId}-tab-semantic`" class="fixture-panel semantic-panel">
          <SemanticReviewPreview :threads="threads" :counts="fileCounts" :viewed="viewed" :file="activeFile" :busy="busyThread !== null" @file="selectSemanticFile" @source="showQuizSource" @files="showFiles" @create="createThread" @open="openThread" />
        </div>
      </template>
      <template #conversation>
        <KeepAlive><ThreadConversation v-if="activeThread" :key="activeThread.id" v-model:followup="activeThread.followup" :thread="activeThread" :busy-thread="busyThread" @source="showSource" @understood="setUnderstood" @preview="previewState" /></KeepAlive>
      </template>
    </WorkspaceShell>
  </div>
</template>

<style scoped>
.sample-workspace {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  min-width: 0;
}
.fixture-title {
  grid-column: 1;
  grid-row: 1;
  font-size: var(--text-heading);
  font-weight: 550;
  line-height: 1.3;
  letter-spacing: -0.5px;
  overflow-wrap: anywhere;
}
.fixture-meta {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: var(--space-2) var(--space-3);
}
.fixture-meta {
  font-size: var(--text-small);
}
.github-sample { grid-column: 2; grid-row: 1 / 3; align-self: center; }
.fixture-meta code {
  font-size: var(--text-code-meta);
}
.context-chip {
  font-size: var(--text-code-meta);
  overflow-wrap: anywhere;
}
.fixture-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  min-width: 0;
}
.sample-workspace :deep(.file-disclosure) { position: relative; }
.sample-workspace :deep(.files-body) { position: absolute; inset: var(--target-control) 0 0; height: auto; min-height: 0; }
@container workspace (max-width: 56.25rem) {
  .is-full-page :deep(.workspace-content) { flex: 1; min-height: 0; }
}
.semantic-panel { container: semantic-panel / inline-size; }
</style>
