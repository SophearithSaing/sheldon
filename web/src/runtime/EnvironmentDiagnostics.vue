<script setup lang="ts">
import UiButton from '../components/ui/UiButton.vue'
import type { EnvironmentReport } from './environment'

const props = defineProps<{ report?: EnvironmentReport; loading: boolean; error: string }>()
const emit = defineEmits<{ refresh: [] }>()
const labels = {
  protocol: 'Native protocol',
  service: 'Service health',
  policy: 'Read-only policy',
  authentication: 'Login',
  entitlement: 'Model access / quota',
  compatibility: 'Sheldon compatibility',
}

function toggle(event: Event) {
  if (
    (event.currentTarget as HTMLDetailsElement).open &&
    !props.report &&
    !props.loading &&
    !props.error
  ) {
    emit('refresh')
  }
}
</script>

<template>
  <details class="environment" @toggle="toggle">
    <summary>Environment &amp; diagnostics</summary>
    <section aria-label="Environment diagnostics" :aria-busy="loading">
      <p>
        Local version/help checks only. No inference, login, installation, credential inspection or
        service startup.
      </p>
      <p class="muted small">
        Detected does not mean compatible or signed in. Service health, policy enforcement and quota
        may remain unknown. Your existing provider configuration and billing route are unchanged.
      </p>
      <p class="muted small">
        Results are a snapshot, not a live health check. Use only trusted executables on PATH; these
        checks are not a sandbox. Harnesses are alternatives; you do not need all three installed.
      </p>
      <UiButton :disabled="loading" @click="emit('refresh')">Refresh environment</UiButton>
      <p v-if="loading" role="status">Checking local CLI metadata…</p>
      <p v-if="error" role="alert">{{ error }}</p>
      <ul v-if="report" class="environment-tools">
        <li v-for="tool in report.tools" :key="tool.id">
          <article :aria-label="tool.name + ' diagnostics'">
            <h3>
              {{ tool.name }} <span v-if="tool.optional" class="muted small">(optional)</span>
            </h3>
            <p>
              <strong>{{ tool.status === 'available' ? 'Detected' : tool.status }}</strong
              ><span v-if="tool.version"> · {{ tool.version }}</span>
            </p>
            <p class="small">{{ tool.detail }}</p>
            <dl v-if="tool.capabilities">
              <div v-for="(finding, key) in tool.capabilities" :key="key">
                <dt>{{ labels[key] }}</dt>
                <dd>
                  <strong>{{
                    finding.status === 'available' ? 'Advertised' : finding.status
                  }}</strong>
                  — {{ finding.detail }}
                </dd>
              </div>
            </dl>
            <p class="small">{{ tool.guidance }}</p>
            <a :href="tool.documentation" target="_blank" rel="noopener noreferrer"
              >{{ tool.name }} external setup documentation</a
            >
          </article>
        </li>
      </ul>
    </section>
  </details>
</template>

<style scoped>
.environment {
  min-width: 0;
}
.environment summary {
  cursor: pointer;
  padding-block: var(--space-2);
}
.environment section {
  display: grid;
  gap: var(--space-3);
  max-height: 55dvh;
  overflow: auto;
  padding: var(--space-3);
  border: 1px solid var(--color-border-strong);
}
.environment-tools {
  display: grid;
  gap: var(--space-4);
  list-style: none;
  padding: 0;
}
.environment section > .ui-button {
  justify-self: start;
}
.environment article {
  display: grid;
  gap: var(--space-2);
  overflow-wrap: anywhere;
}
.environment h3 {
  font-size: var(--text-body);
}
.environment dl {
  display: grid;
  gap: var(--space-2);
  font-size: var(--text-small);
}
.environment dl > div {
  display: grid;
  grid-template-columns: minmax(8rem, 12rem) 1fr;
  gap: var(--space-3);
}
.environment dt {
  color: var(--color-muted);
}
.environment dd {
  margin: 0;
}
@media (max-width: 600px) {
  .environment dl > div {
    grid-template-columns: 1fr;
    gap: 0;
  }
}
</style>
