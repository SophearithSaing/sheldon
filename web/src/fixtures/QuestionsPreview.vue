<script setup lang="ts">
import { computed, ref } from 'vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import ThreadStatus from './ThreadStatus.vue'
import { sampleSourceLabel, type SampleThread } from './workspace'

const props = defineProps<{ threads: SampleThread[]; current: number | null }>()
const emit = defineEmits<{ open: [id: number, trigger: HTMLElement]; files: [] }>()
const search = ref('')
const filter = ref('all')
const sort = ref('recent')
const filters = ['all', 'open', 'understood'] as const
const sortOptions = [{ value: 'recent', label: 'Recent sample activity' }, { value: 'oldest', label: 'Oldest sample activity' }]
const matches = computed(matchingThreads)

/**
 * Counts a status bucket before applying sample text search.
 * @param bucket Status filter to count.
 * @returns Count of local threads, not a backend library total.
 */
function count(bucket: string): number {
  return props.threads.filter((thread): boolean => inBucket(thread, bucket)).length
}

/**
 * Keeps answered, investigating and interrupted/failed threads open until reader action.
 * @param thread Local thread to classify.
 * @param bucket Requested status filter.
 * @returns Whether the thread belongs in the bucket.
 */
function inBucket(thread: SampleThread, bucket: string): boolean {
  return bucket === 'all' || (bucket === 'understood' ? thread.status === 'understood' : thread.status !== 'understood')
}

/**
 * Searches this workspace's sample questions, transcripts and paths with stable activity order.
 * @returns Matching threads sorted by their fixed sample activity order.
 */
function matchingThreads(): SampleThread[] {
  const query = search.value.trim().toLowerCase()
  const result = props.threads.filter((thread): boolean => {
    const text = [thread.question, sampleSourceLabel(thread.anchor), ...thread.messages.flatMap((message): string[] => message.paragraphs)].join(' ').toLowerCase()
    return inBucket(thread, filter.value) && text.includes(query)
  })
  return sort.value === 'oldest' ? result.reverse() : result
}

/**
 * Selects a thread with its row as the panel's focus-return target.
 * @param id Local thread identifier.
 * @param event Row activation.
 */
function open(id: number, event: MouseEvent): void {
  emit('open', id, event.currentTarget as HTMLElement)
}
</script>

<template>
  <ScrollRegion label="Questions in this PR" class="questions-preview">
    <header class="questions-heading">
      <div><h2>Your questions</h2><p>{{ threads.length }} sample threads sharing agent context for this PR revision.</p></div>
      <UiButton variant="primary" @click="emit('files')">Ask in Files changed</UiButton>
    </header>
    <div class="question-tools">
      <UiField v-model="search" type="search" label="Search sample questions, answers, or files" placeholder="Search questions, answers, or files…" />
      <UiField v-model="sort" type="select" label="Sort sample questions" :options="sortOptions" />
    </div>
    <div class="filter-row">
      <div class="question-filters" role="group" aria-label="Filter question status">
        <UiButton v-for="bucket in filters" :key="bucket" :aria-label="bucket === 'all' ? 'All' : bucket === 'open' ? 'Open' : 'Understood'" :aria-pressed="filter === bucket" @click="filter = bucket">{{ bucket === 'all' ? 'All' : bucket === 'open' ? 'Open' : 'Understood' }} <span>{{ count(bucket) }}</span></UiButton>
      </div>
      <p role="status">{{ matches.length }} matching sample questions · This PR only</p>
    </div>
    <section v-for="bucket in ['open', 'understood']" v-show="matches.some((thread) => inBucket(thread, bucket))" :key="bucket" class="question-group" :aria-label="bucket === 'open' ? 'Open questions' : 'Understood questions'">
      <h3>{{ bucket === 'open' ? 'Open questions' : 'Understood' }} <span>{{ matches.filter((thread) => inBucket(thread, bucket)).length }}</span></h3>
      <button v-for="thread in matches.filter((item) => inBucket(item, bucket))" :key="thread.id" type="button" class="question-card" :aria-label="`Open question ${String(thread.id).padStart(2, '0')}: ${thread.question}`" :aria-current="current === thread.id ? 'true' : undefined" @click="open(thread.id, $event)">
        <span class="question-number">{{ String(thread.id).padStart(2, '0') }}</span>
        <span class="question-copy"><strong>{{ thread.question }}</strong><span>{{ sampleSourceLabel(thread.anchor) }} · {{ thread.messages.length }} sample {{ thread.messages.length === 1 ? 'message' : 'messages' }}<span v-if="thread.outdated"> · Outdated context</span></span><span>{{ thread.status === 'understood' ? 'Marked understood by the sample reader' : thread.messages.at(-1)?.paragraphs[0] }}</span></span>
        <ThreadStatus :status="thread.status" />
      </button>
    </section>
    <section v-if="!matches.length" class="question-empty">
      <h3>{{ threads.length ? 'No matching sample questions' : 'No sample questions yet' }}</h3>
      <p v-if="search">No results for “{{ search }}”.</p>
      <p v-else>Choose source in Files changed to compose a local question.</p>
      <UiButton v-if="search" @click="search = ''">Clear search</UiButton>
      <UiButton @click="emit('files')">Back to diff</UiButton>
    </section>
    <p class="questions-note">Only you mark a question understood. An agent reply, Viewed state or quiz result does not close it.</p>
    <p class="questions-note">Search, filters and sorting operate on sample data in this page only. There is no saved question library, agent execution or durable saving. Activity order is illustrative; no live timestamps are shown. Whole-PR question creation is unavailable.</p>
  </ScrollRegion>
</template>

<style scoped>
.questions-preview { flex: 1; min-height: 0; padding: var(--space-3) var(--space-2); }
.questions-heading { display: flex; align-items: start; flex-wrap: wrap; justify-content: space-between; gap: var(--space-3); }
.questions-heading h2 { font-size: var(--text-heading); font-weight: 500; letter-spacing: -0.4px; }
.questions-heading p { margin-top: var(--space-1); color: var(--color-muted); font-size: var(--text-small); }
.question-tools { display: flex; align-items: end; flex-wrap: wrap; gap: var(--space-3); margin-top: var(--space-5); }
.question-tools > :first-child { flex: 1 1 18rem; }
.question-tools > :last-child { flex: 0 1 12rem; }
.filter-row { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: var(--space-3); margin: var(--space-4) 0 var(--space-5); }
.filter-row p { color: var(--color-muted); font-size: var(--text-code-meta); }
.question-filters { display: flex; flex-wrap: wrap; gap: var(--space-1); }
.question-filters .ui-button { background: transparent; border-color: transparent; color: var(--color-muted); }
.question-filters [aria-pressed='true'] { background: var(--color-panel); border-color: var(--color-border-strong); color: var(--color-text); }
.question-filters span { margin-left: var(--space-2); color: var(--color-muted); }
.question-group + .question-group { margin-top: var(--space-6); }
.question-group h3 { margin-bottom: var(--space-3); color: var(--color-muted); font-size: var(--text-small); font-weight: 550; }
.question-group h3 span { margin-left: var(--space-2); font-weight: 400; }
.question-card { position: relative; display: grid; grid-template-columns: 1.75rem minmax(0, 1fr) auto; align-items: start; gap: var(--space-3); width: 100%; min-height: var(--target-control); margin-top: var(--space-3); padding: var(--space-4); border: 1px solid var(--color-border); border-radius: var(--radius-panel); background: var(--color-panel); text-align: left; }
.question-card:hover { border-color: var(--color-border-strong); }
.question-card[aria-current] { background: var(--color-accent-soft); border-color: var(--color-accent-border); }
.question-card[aria-current]::before { content: ''; position: absolute; left: -1px; top: var(--space-3); bottom: var(--space-3); width: 3px; background: var(--color-accent); }
.question-number { display: grid; place-items: center; min-height: 1.625rem; border: 1px solid var(--color-border); border-radius: var(--radius-control); background: var(--color-canvas); color: var(--color-muted); font-size: var(--text-code-meta); }
.question-card[aria-current] .question-number { background: var(--color-accent); color: var(--color-on-accent); }
.question-copy { display: grid; gap: var(--space-2); min-width: 0; overflow-wrap: anywhere; }
.question-copy strong { font-size: var(--text-prompt); font-weight: 500; }
.question-copy > span { color: var(--color-muted); font-size: var(--text-small); }
.question-copy > span:last-child { overflow: hidden; display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; }
.questions-note { margin-top: var(--space-5); color: var(--color-muted); font-size: var(--text-small); }
.question-empty { display: grid; justify-items: start; gap: var(--space-3); padding: var(--space-6); border: 1px solid var(--color-border); border-radius: var(--radius-panel); }
@media (max-width: 1180px) { .question-card { grid-template-columns: 1.75rem minmax(0, 1fr); } .question-card > .ui-badge { grid-column: 2; justify-self: start; } }
</style>
