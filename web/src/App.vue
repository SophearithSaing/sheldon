<script setup lang="ts">
import { defineAsyncComponent } from 'vue'
import GraphiteFixture from './fixtures/GraphiteFixture.vue'

const DevelopmentConnection =
  import.meta.env.DEV && import.meta.env.MODE === 'development'
    ? defineAsyncComponent(() => import('./development/DevelopmentConnection.vue'))
    : undefined
</script>

<template>
  <div class="application" :class="{ 'application--development': DevelopmentConnection }">
    <DevelopmentConnection v-if="DevelopmentConnection" />
    <GraphiteFixture />
  </div>
</template>

<style scoped>
.application--development {
  display: flex;
  flex-direction: column;
  height: 100dvh;
}
.application--development > .fixture-page {
  flex: 1;
  height: auto;
}
</style>
