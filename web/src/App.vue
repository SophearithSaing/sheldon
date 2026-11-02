<script setup lang="ts">
import { defineAsyncComponent, ref } from 'vue'
import FixtureApplication from './fixtures/FixtureApplication.vue'
import UiButton from './components/ui/UiButton.vue'

const runtimeExpanded = ref(true)
const runtimeState = ref('Disconnected')
const runtimeLabel = import.meta.env.DEV ? 'Development backend' : 'Local runtime'

const ControlsFixture =
  import.meta.env.MODE === 'fixture' &&
  new URLSearchParams(location.search).get('fixture') === 'controls'
    ? defineAsyncComponent(() => import('./fixtures/ControlsFixture.vue'))
    : undefined

const RuntimeConnection =
  import.meta.env.MODE !== 'fixture'
    ? defineAsyncComponent(() => import('./runtime/RuntimeConnection.vue'))
    : undefined
</script>

<template>
  <div class="application">
    <ControlsFixture v-if="ControlsFixture" />
    <FixtureApplication v-else>
      <template v-if="RuntimeConnection" #runtime-trigger>
        <UiButton
          class="runtime-trigger"
          variant="quiet"
          :aria-label="`${runtimeLabel} — ${runtimeState}`"
          :title="`${runtimeLabel} — ${runtimeState}`"
          :aria-expanded="runtimeExpanded"
          aria-controls="runtime-panel"
          @click="runtimeExpanded = !runtimeExpanded"
        >
          <span aria-hidden="true">●</span>
          <span class="runtime-label">{{ runtimeLabel }}</span>
          <span class="runtime-state" role="status">{{ runtimeState }}</span>
        </UiButton>
      </template>
      <template v-if="RuntimeConnection" #runtime-panel>
        <RuntimeConnection v-model:expanded="runtimeExpanded" @state="runtimeState = $event" />
      </template>
    </FixtureApplication>
  </div>
</template>

<style scoped>
.runtime-trigger { white-space: nowrap; font-size: var(--text-small); }
.runtime-state {
  display: block;
  max-width: 10rem;
  overflow: hidden;
  text-overflow: ellipsis;
  color: var(--color-accent);
}
@container application (max-width: 70rem) {
  .runtime-label { display: none; }
}
@container application (max-width: 45rem) {
  .runtime-state { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
}
</style>
