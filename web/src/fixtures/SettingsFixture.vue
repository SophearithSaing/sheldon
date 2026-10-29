<script setup lang="ts">
import { computed, ref } from 'vue'
import SettingsPanel from '../settings/SettingsPanel.vue'
import { copyPreferences, harnessNames, modelLabel } from '../settings/contracts'
import type { Confirmation, ModelRef, TestRequest, TestRun } from '../settings/contracts'
import { settingsFixture, settingsScenarios } from './settings'
import type { SettingsScenario } from './settings'

const queryScenario = new URLSearchParams(location.search).get('scenario') as SettingsScenario
const scenario = ref<SettingsScenario>(
  settingsScenarios.includes(queryScenario) ? queryScenario : 'first-launch',
)
const fixture = ref(settingsFixture(scenario.value))
const notice = ref('')
const newChoice = computed(() => {
  const p = fixture.value.state.preferences
  if (!p.setupConfirmed || !p.defaultHarness)
    return 'No confirmed preference — nothing will be selected automatically.'
  const s = p.models[p.defaultHarness]
  return `${harnessNames[p.defaultHarness]} · ${s?.mode === 'explicit' ? modelLabel(s.model) : 'harness default (resolve on new session creation)'}`
})

function reset() {
  fixture.value = settingsFixture(scenario.value)
  notice.value = ''
}
function confirm(input: Confirmation) {
  fixture.value.state.preferences = copyPreferences({
    schemaVersion: input.schemaVersion,
    setupConfirmed: true,
    defaultHarness: input.defaultHarness,
    models: input.models,
  })
  notice.value =
    'Fixture confirmation only — not saved to the runtime or disk. Existing conversation binding is unchanged.'
}
function simulate(input: TestRequest) {
  const data = fixture.value.models[input.harness]
  const model = input.selection.mode === 'explicit' ? input.selection.model : data.defaultModel
  const known =
    model &&
    data.models.some((item) => item.id === model.id && item.providerID === model.providerID)
  const run: TestRun = {
    id: 'fixture-test',
    clientRequestID: input.clientRequestID,
    purpose: 'environment_test',
    status: known ? 'running' : 'failed',
    harness: input.harness,
    selection: input.selection,
  }
  if (known)
    run.binding = {
      ...fixture.value.binding,
      harness: input.harness,
      nativeSessionID: 'fixture-independent-test-session',
      requested: input.selection,
      effective: { ...model } as ModelRef,
    }
  else
    run.error =
      'Fixture model/default is unavailable or unresolved. No fallback or real request was attempted.'
  fixture.value.testRun = run
}
function cancel() {
  if (fixture.value.testRun)
    fixture.value.testRun = { ...fixture.value.testRun, status: 'cancelled' }
}
</script>

<template>
  <main class="settings-fixture" aria-label="Settings UI fixture">
    <h1>Settings &amp; Environment · UI fixture</h1>
    <p>Sample states only. No filesystem writes, native discovery, login or inference.</p>
    <label for="settings-fixture-scenario">Fixture scenario</label>
    <select id="settings-fixture-scenario" v-model="scenario" @change="reset">
      <option v-for="item in settingsScenarios" :key="item" :value="item">{{ item }}</option>
    </select>
    <SettingsPanel
      :key="scenario"
      fixture
      :state="fixture.state"
      :models="fixture.models"
      :environment="fixture.environment"
      :binding="fixture.binding"
      :test-run="fixture.testRun"
      @confirm="confirm"
      @test="simulate"
      @cancel-test="cancel"
    />
    <p v-if="notice" role="status">{{ notice }}</p>
    <p aria-label="New conversation preference preview">
      New conversation preference preview: {{ newChoice }}
    </p>
    <a href="/">Return to sample workspace</a>
  </main>
</template>

<style scoped>
.settings-fixture {
  display: grid;
  gap: var(--space-3);
  max-width: 70rem;
  margin: auto;
  padding: var(--space-6);
}
.settings-fixture h1 {
  font-size: var(--text-heading);
}
.settings-fixture select {
  padding: var(--space-2);
  background: var(--color-panel);
  border: 1px solid var(--color-border-strong);
}
.settings-fixture :deep(.settings-panel) {
  max-height: none;
}
</style>
