<script setup lang="ts">
import { ref } from 'vue'
import UiBadge from '../components/ui/UiBadge.vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import UiNavigation from '../components/ui/UiNavigation.vue'
import UiTabs from '../components/ui/UiTabs.vue'
import ScrollRegion from '../components/ui/ScrollRegion.vue'

const actions = ref(0)
const submissions = ref(0)
const busy = ref(true)
const selected = ref(false)
const question = ref('')
const error = ref('')
const draft = ref('')
const model = ref('default')
const unavailableModel = ref('default')
const answer = ref('')
const destination = ref('library')
const tab = ref('first')
const destinations = [
  { id: 'library', label: 'All PRs preview', href: '#library' },
  { id: 'settings', label: 'Configuration preview', href: '#settings' },
] as const
const panels = [
  { id: 'first', label: 'First panel' },
  { id: 'second', label: 'Second panel' },
] as const
const models = [
  { value: 'default', label: 'Harness default — sample' },
  { value: 'a', label: 'Sample model A' },
  { value: 'b', label: 'Sample model B' },
] as const

/**
 * Validates only the local sample field without submitting a question.
 */
function validate(): void {
  error.value = question.value.trim() ? '' : 'Error: enter a sample question.'
}

/**
 * Switches the local navigation specimen without leaving the gallery.
 * @param id Fixture destination identifier.
 * @param event Link activation event.
 */
function navigate(id: string, event: MouseEvent): void {
  event.preventDefault()
  destination.value = id
}
</script>

<template>
  <main class="controls-fixture">
    <header>
      <h1>Slate control fixtures</h1>
      <p>Local previews only. No PR loading, agent requests, model discovery or persistence.</p>
    </header>
    <section aria-labelledby="buttons-title">
      <h2 id="buttons-title">Buttons and feedback</h2>
      <div class="control-row">
        <UiButton variant="primary" @click="actions++">Preview action</UiButton>
        <UiButton>Secondary sample</UiButton>
        <UiButton variant="quiet">Quiet sample</UiButton>
        <UiButton variant="soft">Soft sample</UiButton>
        <UiButton :aria-pressed="selected" @click="selected = !selected">Selected sample</UiButton>
      </div>
      <form class="control-row" @submit.prevent="submissions++">
        <UiButton type="submit" disabled aria-describedby="unavailable-reason" @click="actions++">
          Unavailable submit
        </UiButton>
        <UiButton type="submit" :busy="busy" aria-describedby="busy-reason" @click="actions++">
          {{ busy ? 'Sending sample…' : 'Ready sample' }}
        </UiButton>
        <UiButton @click="busy = !busy">Toggle sample busy state</UiButton>
      </form>
      <p id="unavailable-reason">Unavailable: submissions are not implemented.</p>
      <p id="busy-reason">Busy preview: another question is using the shared PR-revision session.</p>
      <p role="status">Local actions: {{ actions }} · Form submissions: {{ submissions }}</p>
      <div class="control-row">
        <UiBadge>Sample status</UiBadge>
        <UiBadge tone="info">Answered</UiBadge>
        <UiBadge tone="warning">Investigating</UiBadge>
        <UiBadge tone="success">Understood</UiBadge>
        <UiBadge tone="error">Failed</UiBadge>
      </div>
    </section>
    <section aria-labelledby="fields-title">
      <h2 id="fields-title">Fields and native choices</h2>
      <div class="field-grid">
        <div>
          <UiField
            v-model="question"
            label="Sample question"
            hint="Local text only."
            :error="error"
            aria-describedby="external-description"
          />
          <p id="external-description">Nothing is sent to an agent.</p>
          <UiButton @click="validate">Validate sample</UiButton>
        </div>
        <UiField
          v-model="draft"
          label="Sample draft"
          multiline
          error="Error: source is unavailable."
        />
        <UiField
          v-model="model"
          label="Sample harness model"
          type="select"
          :options="models"
          hint="Illustrative choices, not an installed catalog."
        />
        <UiField
          v-model="unavailableModel"
          label="Unavailable models"
          type="select"
          :options="models"
          disabled
          hint="Unavailable: model discovery is not implemented."
        />
        <UiField
          v-model="unavailableModel"
          label="Loading models"
          type="select"
          :options="models"
          busy
          hint="Loading preview only; no discovery is running."
        />
        <UiField
          v-model="unavailableModel"
          label="Invalid model"
          type="select"
          :options="models"
          error="Error: choose an available replacement."
        />
      </div>
      <fieldset class="sample-radios">
        <legend>Sample answer — selection does not grade</legend>
        <label>
          <input v-model="answer" type="radio" name="control-answer" value="a" /> Sample answer A
        </label>
        <label>
          <input v-model="answer" type="radio" name="control-answer" value="b" /> Sample answer B
        </label>
      </fieldset>
    </section>
    <section aria-labelledby="navigation-title">
      <h2 id="navigation-title">Destinations and in-page panels</h2>
      <UiNavigation
        label="Sample destinations"
        :current="destination"
        :items="destinations"
        @navigate="navigate"
      />
      <UiTabs id="controls" v-model="tab" label="Sample panels" :items="panels" />
      <div
        v-show="tab === 'first'"
        id="controls-panel-first"
        role="tabpanel"
        aria-labelledby="controls-tab-first"
      >
        First panel content
      </div>
      <div
        v-show="tab === 'second'"
        id="controls-panel-second"
        role="tabpanel"
        aria-labelledby="controls-tab-second"
      >
        Second panel content
      </div>
      <ScrollRegion label="Sample scroll region" class="sample-scroll">
        <p v-for="line in 20" :key="line">Sample content line {{ line }} — keyboard scrolling stays local.</p>
      </ScrollRegion>
    </section>
  </main>
</template>

<style scoped>
.controls-fixture {
  container: controls / inline-size;
  max-width: 70rem;
  margin-inline: auto;
  padding: var(--space-6);
}
.controls-fixture header,
.controls-fixture section,
.field-grid > div {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}
.controls-fixture section {
  margin-top: var(--space-6);
  padding: var(--space-4);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-panel);
  background: var(--color-panel);
}
.controls-fixture h1 {
  font-size: var(--text-heading);
}
.controls-fixture h2 {
  font-size: var(--text-prose);
  font-weight: 500;
}
.controls-fixture p {
  color: var(--color-muted);
}
.control-row {
  display: flex;
  flex-wrap: wrap;
  gap: var(--space-3);
}
.field-grid {
  display: grid;
  gap: var(--space-4);
}
.sample-radios {
  margin: 0;
  padding: var(--space-3);
  border: 1px solid var(--color-border);
  border-radius: var(--radius-control);
}
.sample-radios label {
  display: flex;
  align-items: center;
  gap: var(--space-2);
  min-height: var(--target-control);
}
.sample-radios input {
  accent-color: var(--color-accent);
}
.sample-scroll {
  max-height: 8rem;
  padding: var(--space-2);
  border: 1px solid var(--color-border);
}
@container controls (min-width: 45rem) {
  .field-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
</style>
