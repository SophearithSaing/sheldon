<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import WorkspaceShell from '../components/WorkspaceShell.vue'
import UiBadge from '../components/ui/UiBadge.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import UiTabs from '../components/ui/UiTabs.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import SampleDiffLines from './SampleDiffLines.vue'
import {
  sampleChoices,
  sampleDiffs,
  sampleExplanation,
  sampleFiles,
  workspaceTabs,
} from './workspace'

withDefaults(defineProps<{ workspaceNumber?: number }>(), { workspaceNumber: 248 })
const tabsId = useId()
const activeTab = ref('files')
const activeFile = ref('refresh')
const fileSearch = ref('')
const lineDraft = ref('')
const followupDraft = ref('')
const sampleSearch = ref('')
const selectedChoice = ref('')
const conversationOpen = ref(false)
const visibleFiles = computed(() =>
  sampleFiles.filter((file) => file.path.includes(fileSearch.value)),
)
const selectedFile = computed(() => sampleFiles.find((file) => file.id === activeFile.value)!)
const diff = computed(() => sampleDiffs[activeFile.value] ?? [])
const draftIndex = computed(() =>
  activeFile.value === 'refresh'
    ? diff.value.findIndex((line) => line.head === 30) + 1
    : diff.value.length,
)
const beforeDraft = computed(() => diff.value.slice(0, draftIndex.value))
const afterDraft = computed(() => diff.value.slice(draftIndex.value))

/**
 * Selects a sample file without changing the native tree disclosure.
 * @param id Sample file identifier.
 */
function selectFile(id: string): void {
  activeFile.value = id
}
</script>

<template>
  <div class="fixture-page">
    <WorkspaceShell
      v-model:conversation-open="conversationOpen"
      :has-file-rail="activeTab === 'files'"
      conversation-title="Sample conversation"
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
          :items="workspaceTabs"
        />
      </template>
      <template #files>
        <UiField
          v-model="fileSearch"
          label="Find a sample file"
          type="search"
          placeholder="Path contains…"
        />
        <nav class="file-list" aria-label="Sample file selection">
          <button
            v-for="file in visibleFiles"
            :key="file.id"
            type="button"
            :aria-current="activeFile === file.id ? 'true' : undefined"
            @click="selectFile(file.id)"
          >
            {{ file.path }}
          </button>
          <p v-if="visibleFiles.length === 0" class="muted" role="status">No sample paths match.</p>
        </nav>
        <p class="file-list-note muted small">
          These paths belong to the design fixture, not your filesystem.
        </p>
      </template>
      <template #default="{ openConversation }">
        <div
          v-show="activeTab === 'files'"
          :id="`${tabsId}-panel-files`"
          role="tabpanel"
          :aria-labelledby="`${tabsId}-tab-files`"
          class="fixture-panel"
        >
          <div class="fixture-toolbar">
            <span class="muted small">Unified diff · sample source</span>
            <UiButton @click="openConversation">Preview sample conversation</UiButton>
          </div>
          <ScrollRegion label="Sample code diff" class="diff-scroll">
            <div class="diff-card">
              <h2 class="diff-file-title">{{ selectedFile.path }}</h2>
              <SampleDiffLines :lines="beforeDraft" label="Sample source before draft" />
              <div v-if="activeFile === 'refresh'" class="inline-draft">
                <div class="fixture-meta">
                  <UiBadge tone="info">Sample inline draft</UiBadge
                  ><span class="muted small">L27–30 · a81c2f4</span>
                </div>
                <UiField
                  v-model="lineDraft"
                  label="Sample line question"
                  multiline
                  hint="Editable local draft only. No question will be created or sent."
                  placeholder="What would you like to understand?"
                />
                <p :id="`${tabsId}-line-unavailable`" class="muted small">
                  Agent requests are not implemented. A real reply will open in the conversation
                  pane.
                </p>
                <div class="fixture-actions">
                  <UiButton variant="quiet" @click="lineDraft = ''">Clear sample draft</UiButton>
                  <UiButton variant="primary" disabled :aria-describedby="`${tabsId}-line-unavailable`"
                    >Ask agent — unavailable</UiButton
                  >
                </div>
              </div>
              <SampleDiffLines
                v-if="afterDraft.length"
                :lines="afterDraft"
                label="Sample source after draft"
              />
            </div>
            <p class="muted small fixture-source-note">
              Source is selectable sample text. Line selection and repository-backed citations are
              not implemented.
            </p>
          </ScrollRegion>
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
        <ScrollRegion label="Sample conversation messages" class="conversation-messages">
          <div class="sample-message">
            <h3>You <span class="muted">· sample message</span></h3>
            <p class="sample-prompt">
              What prevents an old refresh from restoring a logged-out session?
            </p>
          </div>
          <div class="sample-message sample-explanation">
            <h3>Agent <span class="muted">· sample response, not connected</span></h3>
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
.fixture-page {
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
.fixture-meta,
.fixture-actions {
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
.file-list {
  display: grid;
  gap: var(--space-2);
  margin-top: var(--space-5);
}
.file-list button {
  min-width: 0;
  min-height: var(--target-control);
  padding: var(--space-3) var(--space-2);
  border: 1px solid transparent;
  border-left: 2px solid transparent;
  border-radius: var(--radius-control);
  color: var(--color-muted);
  background: transparent;
  text-align: left;
  overflow-wrap: anywhere;
}
.file-list button:hover {
  color: var(--color-text);
  background: var(--color-panel);
}
.file-list button[aria-current] {
  color: var(--color-accent);
  border-left-color: var(--color-accent);
  background: var(--color-accent-soft);
}
.file-list-note {
  margin-top: var(--space-8);
}
.fixture-panel {
  display: flex;
  flex-direction: column;
  flex: 1;
  min-height: 0;
  min-width: 0;
}
.fixture-toolbar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  justify-content: space-between;
  gap: var(--space-2);
  min-height: var(--target-control);
  margin-bottom: var(--space-2);
}
.diff-scroll {
  flex: 1;
  padding-bottom: var(--space-4);
}
.diff-card {
  min-width: 0;
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
}
.diff-file-title {
  font-family: var(--font-code);
  padding: var(--space-4);
  border-bottom: 1px solid var(--color-border);
  background: var(--color-panel);
  font-size: var(--text-code-meta);
  font-weight: 500;
  overflow-wrap: anywhere;
}
.inline-draft {
  display: grid;
  gap: var(--space-3);
  padding: var(--space-4);
  border-block: 1px solid var(--color-accent-border);
  background: var(--color-panel);
}
.fixture-actions {
  justify-content: flex-end;
}
.fixture-source-note {
  margin-top: var(--space-4);
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
