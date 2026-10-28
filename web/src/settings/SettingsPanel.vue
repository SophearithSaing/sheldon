<script setup lang="ts">
import { computed, ref, useId } from 'vue'
import UiButton from '../components/ui/UiButton.vue'
import UiField from '../components/ui/UiField.vue'
import type { EnvironmentReport } from '../runtime/environment'
import { copyPreferences, harnesses, harnessNames, modelLabel, selectionError } from './contracts'
import type {
  Binding,
  Confirmation,
  Harness,
  ModelDiscovery,
  ModelSelection,
  SettingsState,
  TestRequest,
  TestRun,
} from './contracts'

const props = defineProps<{
  state: SettingsState
  models: Partial<Record<Harness, ModelDiscovery>>
  environment?: EnvironmentReport
  fixture?: boolean
  binding?: Binding
  testRun?: TestRun
  error?: string
}>()
const emit = defineEmits<{
  models: [harness: Harness]
  confirm: [input: Confirmation]
  test: [input: TestRequest]
  cancelTest: []
}>()
const id = useId()
const draft = ref(copyPreferences(props.state.preferences))
const confirmed = ref(false)
const quota = ref(false)
const selected = computed(() => draft.value.defaultHarness)
const selection = computed(() =>
  selected.value
    ? (draft.value.models[selected.value] ?? ({ mode: 'default' } as const))
    : undefined,
)
const metadata = computed(() => (selected.value ? props.models[selected.value] : undefined))
const tool = computed(() => props.environment?.tools.find((item) => item.id === selected.value))
const noHarness = computed(
  () =>
    props.environment &&
    harnesses.every(
      (h) => props.environment?.tools.find((item) => item.id === h)?.status === 'missing',
    ),
)
const noVerifiedHarness = computed(
  () =>
    props.environment &&
    !props.environment.tools.some(
      (item) =>
        harnesses.includes(item.id as Harness) &&
        item.status === 'available' &&
        item.capabilities?.compatibility.status === 'available',
    ),
)
const activeTest = computed(
  () => props.testRun?.status === 'pending' || props.testRun?.status === 'running',
)
const invalid = computed(() => {
  for (const h of harnesses) {
    const value = draft.value.models[h]
    const error = value ? selectionError(h, value) : ''
    if (error) return `${harnessNames[h]}: ${error}`
  }
  return ''
})
const supported = computed(
  () =>
    tool.value?.capabilities?.compatibility.status === 'available' &&
    (selection.value?.mode === 'default' ||
      metadata.value?.explicitSelection.status === 'available'),
)
const canConfirm = computed(
  () =>
    !!selected.value &&
    !invalid.value &&
    supported.value &&
    props.state.persistence.status === 'available' &&
    confirmed.value,
)
const canTest = computed(
  () =>
    !!selected.value &&
    !invalid.value &&
    supported.value &&
    props.state.testAnswer.status === 'available' &&
    quota.value &&
    !activeTest.value,
)

function resetConsent() {
  confirmed.value = false
  quota.value = false
}

function chooseHarness() {
  resetConsent()
  if (selected.value) {
    if (!draft.value.models[selected.value])
      draft.value.models[selected.value] = { mode: 'default' }
    emit('models', selected.value)
  }
}

function chooseMode(mode: ModelSelection['mode']) {
  if (!selected.value) return
  const previous = draft.value.models[selected.value]
  if (mode === 'default') {
    draft.value.models[selected.value] = { mode }
  } else if (previous?.mode !== 'explicit') {
    draft.value.models[selected.value] = {
      mode: 'explicit',
      model: { id: '', ...(selected.value === 'opencode' ? { providerID: '' } : {}) },
    }
  }
  resetConsent()
}

function confirm() {
  if (!canConfirm.value || !selected.value) return
  emit('confirm', {
    schemaVersion: 1,
    confirm: true,
    defaultHarness: selected.value,
    models: copyPreferences(draft.value).models,
  })
  confirmed.value = false
}

function test() {
  if (!canTest.value || !selected.value || !selection.value) return
  emit('test', {
    clientRequestID: crypto.randomUUID(),
    harness: selected.value,
    selection:
      selection.value.mode === 'explicit'
        ? { mode: 'explicit', model: { ...selection.value.model } }
        : { mode: 'default' },
    confirmQuota: true,
  })
  quota.value = false
}
</script>

<template>
  <section class="settings-panel" aria-label="Settings & Environment">
    <h2>
      {{
        state.preferences.setupConfirmed
          ? 'Settings & Environment'
          : 'First-launch setup (scaffold)'
      }}
    </h2>
    <p v-if="fixture" class="settings-notice">
      Deterministic UI fixture — no preferences are saved to the runtime or disk, and no real test
      answer is sent.
    </p>
    <p v-else class="settings-notice">
      Setup scaffold only. Saving, native model discovery and real test answers are not implemented
      in this build.
    </p>
    <p>
      Preferences apply to <strong>new conversations</strong> and independent quiz/test sessions
      only. Existing conversations retain their harness, native session and effective model.
    </p>
    <p v-if="noHarness" role="status">
      No harness detected. Install and authenticate a chosen harness externally, then return later.
      You do not need all three.
    </p>
    <p v-else-if="noVerifiedHarness" role="status">
      No harness is verified compatible. Installation alone does not establish safe protocol, policy
      or model support; no fallback is selected.
    </p>
    <p v-else-if="!environment" class="muted small">
      Installation and compatibility have not been checked. Use Environment & diagnostics; opening
      settings does not scan CLIs or start a service.
    </p>
    <p v-if="error" role="alert">{{ error }}</p>
    <form class="settings-form" @submit.prevent="confirm">
      <label :for="`${id}-harness`">Default harness for new conversations</label>
      <select :id="`${id}-harness`" v-model="draft.defaultHarness" @change="chooseHarness">
        <option value="">Choose a harness — no automatic fallback</option>
        <option v-for="h in harnesses" :key="h" :value="h">
          {{ harnessNames[h] }} ·
          {{ environment?.tools.find((item) => item.id === h)?.status ?? 'not checked' }}
        </option>
      </select>
      <div v-if="selected" class="settings-selection">
        <p v-if="tool">
          Installation: {{ tool.status }}<span v-if="tool.version"> · {{ tool.version }}</span
          >. Compatibility: {{ tool.capabilities?.compatibility.status ?? 'unknown' }}.
        </p>
        <p v-if="tool?.capabilities" class="small">
          Service: {{ tool.capabilities.service.status }}. Read-only policy:
          {{ tool.capabilities.policy.status }}. Login:
          {{ tool.capabilities.authentication.status }}. Model access / quota:
          {{ tool.capabilities.entitlement.status }}.
        </p>
        <p
          v-if="tool && tool.capabilities?.compatibility.status !== 'available'"
          class="muted small"
        >
          {{ tool.capabilities?.compatibility.detail ?? tool.detail }} Use external setup guidance
          in diagnostics; no harness/model/provider will be substituted.
        </p>
        <p>
          Configured/default model:
          <output aria-label="Configured/default model">{{
            modelLabel(metadata?.defaultModel)
          }}</output>
        </p>
        <p class="muted small">
          {{ metadata?.configuredDefault.detail ?? 'Native model metadata has not been loaded.' }}
        </p>
        <p class="muted small">
          {{
            metadata?.catalog.detail ??
            'No catalog has been queried. Models are not inferred from an installation.'
          }}
        </p>
        <p v-if="metadata?.catalog.status === 'available' && metadata.models.length" class="small">
          Native catalog: {{ metadata.models.map(modelLabel).join(', ') }}
        </p>
        <fieldset>
          <legend>Model choice for {{ harnessNames[selected] }}</legend>
          <label
            ><input
              type="radio"
              :name="`${id}-mode`"
              :checked="selection?.mode === 'default'"
              @change="chooseMode('default')"
            />
            Use harness default</label
          >
          <label
            ><input
              type="radio"
              :name="`${id}-mode`"
              :checked="selection?.mode === 'explicit'"
              :disabled="metadata?.explicitSelection.status !== 'available'"
              @change="chooseMode('explicit')"
            />
            Explicit native model</label
          >
          <p class="muted small">
            {{
              metadata?.explicitSelection.detail ??
              'Explicit selection is unverified and unavailable until adapter capability metadata is known.'
            }}
          </p>
          <template v-if="selection?.mode === 'explicit'">
            <UiField
              v-if="selected === 'opencode'"
              v-model="selection.model.providerID"
              label="Native provider ID"
              hint="Existing OpenCode provider namespace, not a key or provider configuration."
              :disabled="metadata?.explicitSelection.status !== 'available'"
              @input="resetConsent"
            />
            <UiField
              v-model="selection.model.id"
              label="Native model ID"
              hint="A harness-native identifier or supported alias. Do not enter credentials. Slash formats are not converted between harnesses."
              :disabled="metadata?.explicitSelection.status !== 'available'"
              @input="resetConsent"
            />
          </template>
        </fieldset>
        <p v-if="invalid" role="alert">{{ invalid }}</p>
        <p>
          Effective model for a new conversation: <strong>not pinned yet</strong>. The native
          adapter must resolve and verify it when the new session is created; an
          unavailable/mismatched model is an error, not permission to fall back.
        </p>
      </div>
      <label class="settings-check"
        ><input v-model="confirmed" type="checkbox" /> I confirm this harness/model choice for new
        conversations only.</label
      >
      <p class="muted small">
        {{ state.persistence.detail }} Testing is optional, never a requirement to confirm a
        supported preference.
      </p>
      <UiButton type="submit" :disabled="!canConfirm">{{
        fixture ? 'Confirm fixture preferences' : 'Save settings — unavailable'
      }}</UiButton>
    </form>
    <fieldset class="settings-test">
      <legend>Optional Test answer</legend>
      <p>
        A real test sends a model request and may consume subscription/provider quota. It uses a
        separate disposable session, not PR context or an existing conversation.
      </p>
      <p class="muted small">{{ state.testAnswer.detail }}</p>
      <label class="settings-check"
        ><input
          v-model="quota"
          type="checkbox"
          :disabled="state.testAnswer.status !== 'available' || activeTest"
        />
        I understand a real Test answer may consume quota.</label
      >
      <UiButton :disabled="!canTest" @click="test">{{
        fixture ? 'Simulate fixture Test answer' : 'Test answer — unavailable'
      }}</UiButton>
      <div v-if="testRun" role="status" aria-label="Test answer state">
        <p>
          {{ fixture ? 'Fixture test' : 'Test answer' }}: {{ testRun.status }}. Selection:
          {{ harnessNames[testRun.harness] }} ·
          {{
            testRun.selection.mode === 'explicit'
              ? modelLabel(testRun.selection.model)
              : 'harness default'
          }}.
        </p>
        <p v-if="testRun.binding">Pinned test model: {{ modelLabel(testRun.binding.effective) }}</p>
        <p v-if="testRun.error">{{ testRun.error }}</p>
        <p v-if="testRun.status === 'succeeded'">
          Success is evidence for that tested selection only, not a promise of future entitlement.
        </p>
      </div>
      <UiButton v-if="fixture && activeTest" @click="emit('cancelTest')"
        >Stop fixture test</UiButton
      >
    </fieldset>
    <aside v-if="binding" aria-label="Existing conversation binding" class="settings-binding">
      <h3>Existing fixture conversation — unchanged by preferences</h3>
      <p>{{ harnessNames[binding.harness] }} · native session {{ binding.nativeSessionID }}</p>
      <p>
        Requested:
        {{
          binding.requested.mode === 'default'
            ? 'harness default'
            : modelLabel(binding.requested.model)
        }}
      </p>
      <p>Effective pinned model: {{ modelLabel(binding.effective) }}</p>
    </aside>
  </section>
</template>

<style scoped>
.settings-panel {
  display: grid;
  gap: var(--space-3);
  max-height: 65dvh;
  overflow: auto;
  padding: var(--space-4);
  border: 1px solid var(--color-border-strong);
}
.settings-panel h2,
.settings-panel h3 {
  font-size: var(--text-body);
}
.settings-notice {
  color: var(--color-muted);
  border-left: 2px solid var(--color-accent);
  padding-left: var(--space-3);
}
.settings-form,
.settings-selection,
.settings-test,
.settings-binding {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
}
.settings-panel fieldset {
  display: grid;
  gap: var(--space-3);
  min-width: 0;
  padding: var(--space-3);
  border: 1px solid var(--color-border-strong);
}
.settings-panel select {
  width: 100%;
  min-width: 0;
  min-height: var(--target-control);
  padding: var(--space-2);
  background: var(--color-canvas);
  border: 1px solid var(--color-border-strong);
  border-radius: var(--radius-control);
}
.settings-panel label {
  display: flex;
  align-items: center;
  gap: var(--space-2);
}
.settings-panel input[type='checkbox'],
.settings-panel input[type='radio'] {
  flex-shrink: 0;
}
.settings-panel p {
  overflow-wrap: anywhere;
}
.settings-panel .ui-button {
  justify-self: start;
}
</style>
