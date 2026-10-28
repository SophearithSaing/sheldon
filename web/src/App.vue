<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import GraphiteFixture from './fixtures/GraphiteFixture.vue'

const RuntimeConnection =
  import.meta.env.MODE !== 'fixture'
    ? defineAsyncComponent(() => import('./runtime/RuntimeConnection.vue'))
    : undefined
</script>

<template>
  <div class="application" :class="{ 'application--runtime': RuntimeConnection }">
    <RuntimeConnection v-if="RuntimeConnection" />
    <GraphiteFixture />
  </div>
</template>

<style scoped>
.application--runtime {
  display: flex;
  flex-direction: column;
  height: 100dvh;
}
.application--runtime > .fixture-page {
  flex: 1;
  height: auto;
}
</style>
