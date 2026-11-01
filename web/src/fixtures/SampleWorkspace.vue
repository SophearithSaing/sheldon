<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import WorkspaceShell from '../components/WorkspaceShell.vue'
import UiBadge from '../components/ui/UiBadge.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import UiTabs from '../components/ui/UiTabs.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import ChangedFileTree from '../components/ChangedFileTree.vue'
import FilesChangedPreview from './FilesChangedPreview.vue'
import {
  sampleChoices,
  sampleExplanation,
  sampleFiles,
  workspaceTabs,
  initialSampleThreads,
  type SampleThread,
} from './workspace'

withDefaults(defineProps<{ workspaceNumber?: number }>(), { workspaceNumber: 248 })
const tabsId = useId()
const activeTab = ref('files')
const activeFile = ref('refresh')
const fileSearch = ref('')
const threads = ref(initialSampleThreads())
const activeThreadId = ref<number | null>(threads.value[0]?.id ?? null)
const genericFollowup = ref('')
const shell = ref<InstanceType<typeof WorkspaceShell>>()
const filesPreview = ref<InstanceType<typeof FilesChangedPreview>>()
const viewed = ref<Record<string, boolean>>({ client: true, docs: true })
const sampleSearch = ref('')
const selectedChoice = ref('')
const conversationOpen = ref(threads.value.length > 0)
const activeThread = computed(() => threads.value.find((thread): boolean => thread.id === activeThreadId.value))
const followupDraft = computed({ get: getFollowup, set: setFollowup })
const conversationTitle = computed((): string => activeThread.value ? `Question ${String(activeThread.value.id).padStart(2, '0')}` : 'Sample conversation')
const tabItems = computed(() => workspaceTabs.map((tab) => ({ ...tab, count: tab.id === 'files' ? sampleFiles.length : tab.id === 'questions' ? threads.value.length : undefined })))
const fileCounts = computed(countQuestions)

/**
 * Counts submitted sample threads, never unsent drafts.
 * @returns Per-file sample question counts.
 */
function countQuestions(): Record<string, number> {
  const result: Record<string, number> = {}
  for (const thread of threads.value) result[thread.anchor.fileId] = (result[thread.anchor.fileId] ?? 0) + 1
  return result
}

/**
 * Reads the selected thread's page-memory follow-up text.
 * @returns Local follow-up draft.
 */
function getFollowup(): string {
  return activeThread.value?.followup ?? genericFollowup.value
}

/**
 * Updates only the selected sample thread's follow-up.
 * @param value Local text to retain.
 */
function setFollowup(value: string): void {
  if (activeThread.value) activeThread.value.followup = value
  else genericFollowup.value = value
}

/**
 * Adds exactly one local sample thread without native dispatch or saving.
 * @param thread Validated sample question supplied by the Files view.
 */
function createThread(thread: SampleThread): void {
  if (!threads.value.some((item): boolean => item.id === thread.id)) threads.value.push(thread)
}

/**
 * Opens the selected local thread with a stable marker as the return-focus target.
 * @param id Existing sample thread identifier.
 * @param trigger Visible marker element.
 */
async function openThread(id: number, trigger: HTMLElement): Promise<void> {
  activeThreadId.value = id
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
  <div class="sample-workspace">
    <WorkspaceShell
      ref="shell"
      v-model:conversation-open="conversationOpen"
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
      <template #default="{ openConversation }">
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
          <ScrollRegion label="Question control samples" class="fixture-body">
            <div class="fixture-stack">
              <h2>Question controls</h2>
              <p class="muted">Component samples, not a saved question library.</p>
              <UiField
                v-model="sampleSearch"
                label="Sample search text"
                type="search"
                hint="This field demonstrates input styling. Search is not connected."
                placeholder="Search questions…"
              />
              <section class="sample-card" :aria-labelledby="`${tabsId}-status-samples`">
                <h3 :id="`${tabsId}-status-samples`">Status samples</h3>
                <div class="fixture-meta">
                  <UiBadge tone="warning">Investigating · sample</UiBadge>
                  <UiBadge tone="info">Answered · sample</UiBadge>
                  <UiBadge tone="success">Understood · sample</UiBadge>
                  <UiBadge tone="error">Failed · sample</UiBadge>
                </div>
                <p class="muted small">
                  Answered and reader-confirmed Understood are different states. None of these
                  samples represents a running service.
                </p>
              </section>
              <section class="sample-card" :aria-labelledby="`${tabsId}-sample-question-title`">
                <UiBadge>Sample question</UiBadge>
                <h3 :id="`${tabsId}-sample-question-title`">Can an older refresh restore a session?</h3>
                <p class="muted">src/auth/refresh-session.ts · L27–30 · sample revision</p>
                <UiButton @click="openConversation">Preview sample conversation</UiButton>
              </section>
              <p :id="`${tabsId}-questions-unavailable`" class="muted small">
                Creating and saving questions is not implemented.
              </p>
              <UiButton variant="primary" disabled :aria-describedby="`${tabsId}-questions-unavailable`"
                >New question — unavailable</UiButton
              >
            </div>
          </ScrollRegion>
        </div>

        <div
          v-show="activeTab === 'quiz'"
          :id="`${tabsId}-panel-quiz`"
          role="tabpanel"
          :aria-labelledby="`${tabsId}-tab-quiz`"
          class="fixture-panel"
        >
          <ScrollRegion label="Quiz control samples" class="fixture-body">
            <div class="fixture-stack">
              <h2>Quiz controls</h2>
              <p class="muted">An ungraded radio-control fixture, not an agent-generated quiz.</p>
              <fieldset class="sample-choices">
                <legend>In this fictional example, what does the generation check protect?</legend>
                <label
                  v-for="(choice, index) in sampleChoices"
                  :key="choice"
                  :class="{ 'is-selected': selectedChoice === choice }"
                >
                  <input
                    v-model="selectedChoice"
                    type="radio"
                    :name="`${tabsId}-sample-answer`"
                    :value="choice"
                  />
                  <span class="muted" aria-hidden="true">{{
                    String.fromCharCode(65 + index)
                  }}</span>
                  <span>{{ choice }}</span>
                </label>
              </fieldset>
              <p :id="`${tabsId}-quiz-unavailable`" class="muted small">
                Selection stays in this page. Generation, grading and saved attempts are not
                implemented.
              </p>
              <UiButton variant="primary" disabled :aria-describedby="`${tabsId}-quiz-unavailable`"
                >Check answer — unavailable</UiButton
              >
            </div>
          </ScrollRegion>
        </div>
      </template>
      <template #conversation>
        <div v-if="activeThread" class="thread-context">
          <UiBadge :tone="activeThread.status === 'failed' ? 'error' : 'info'">{{ activeThread.status }} · sample</UiBadge>
          <p>{{ sampleFiles.find((item) => item.id === activeThread!.anchor.fileId)?.path }}</p>
          <p>{{ activeThread.anchor.range ? `${activeThread.anchor.range.side} L${activeThread.anchor.range.start}–${activeThread.anchor.range.end}` : 'Whole file' }} · {{ activeThread.anchor.revision }}</p>
          <p>Shared agent context · This PR revision</p>
        </div>
        <ScrollRegion label="Sample conversation messages" class="conversation-messages">
          <div class="sample-message">
            <h3>You <span class="muted">· sample message</span></h3>
            <p class="sample-prompt">
              {{ activeThread?.question ?? 'What prevents an old refresh from restoring a logged-out session?' }}
            </p>
          </div>
          <p v-if="activeThread?.status === 'failed'" role="alert">Sample response failed. No agent request ran. The thread and question are retained.</p>
          <p v-else-if="activeThread?.status === 'investigating'" role="status">Investigating preview only — no agent is running.</p>
          <div v-else class="sample-message sample-explanation">
            <h3>Agent <span class="muted">· sample response, not connected</span></h3>
            <p>Fixed sample text, not an answer generated for your question.</p>
            <p v-for="paragraph in sampleExplanation" :key="paragraph">{{ paragraph }}</p>
          </div>
        </ScrollRegion>
        <div class="followup-sample">
          <UiField
            v-model="followupDraft"
            label="Sample follow-up"
            multiline
            hint="Local text only. Closing the preview keeps this draft until page reload."
          />
          <p :id="`${tabsId}-followup-unavailable`" class="muted small">
            No harness is connected; sending is unavailable.
          </p>
          <UiButton variant="primary" disabled :aria-describedby="`${tabsId}-followup-unavailable`"
            >Send — unavailable</UiButton
          >
        </div>
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
.thread-context { display: grid; gap: var(--space-1); padding: var(--space-3) var(--space-6); border-bottom: 1px solid var(--color-border); color: var(--color-muted); font-size: var(--text-code-meta); }
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
.fixture-body {
  flex: 1;
  padding: var(--space-6) var(--space-8);
}
.fixture-stack {
  display: grid;
  justify-items: start;
  gap: var(--space-5);
  width: min(100%, 48.75rem);
  margin-inline: auto;
}
.fixture-stack > * {
  max-width: 100%;
}
.fixture-stack > .ui-field {
  width: 100%;
}
.fixture-stack h2 {
  font-size: var(--text-heading);
  font-weight: 500;
}
.fixture-stack h3 {
  font-size: var(--text-prose);
  font-weight: 550;
}
.sample-card {
  display: grid;
  justify-items: start;
  gap: var(--space-3);
  width: 100%;
  padding: var(--space-5);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-panel);
}
.sample-choices {
  display: grid;
  gap: var(--space-3);
  width: 100%;
  margin: 0;
  padding: 0;
  border: 0;
}
.sample-choices legend {
  margin-bottom: var(--space-5);
  font-size: var(--text-prose);
  line-height: 1.7;
}
.sample-choices label {
  display: flex;
  align-items: baseline;
  gap: var(--space-3);
  min-height: var(--target-control);
  padding: var(--space-4);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
  background: var(--color-panel);
  cursor: pointer;
}
.sample-choices label:hover {
  background: var(--color-surface);
}
.sample-choices label.is-selected {
  border-color: var(--color-accent);
  background: var(--color-accent-soft);
}
.sample-choices input {
  flex-shrink: 0;
  accent-color: var(--color-accent);
}
.conversation-messages {
  flex: 1;
  min-height: 10rem;
  padding: var(--space-5) var(--space-6);
}
.sample-message {
  font-family: var(--font-prose);
  font-size: var(--text-prose);
  line-height: 1.75;
  overflow-wrap: anywhere;
}
.sample-message h3 {
  margin-bottom: var(--space-3);
  font-size: var(--text-small);
  font-weight: 600;
}
.sample-prompt {
  font-size: var(--text-prompt);
  line-height: 1.65;
  padding: var(--space-3);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
  background: var(--color-canvas);
}
.sample-explanation {
  margin-top: var(--space-6);
}
.sample-explanation p + p {
  margin-top: var(--space-4);
}
.followup-sample {
  flex-shrink: 0;
  display: grid;
  justify-items: end;
  gap: var(--space-3);
  padding: var(--space-4) var(--space-6);
  border-top: 1px solid var(--color-border);
}
.followup-sample > .ui-field {
  width: 100%;
}
</style>
