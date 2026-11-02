<script setup lang="ts">
import UiBadge from '../components/ui/UiBadge.vue'
import type { SampleThread } from './workspace'

defineProps<{ status: SampleThread['status'] }>()
const labels = { answered: 'Answered', investigating: 'Investigating', understood: 'Understood', failed: 'Failed', interrupted: 'Interrupted' } as const
const tones = { answered: 'info', investigating: 'warning', understood: 'success', failed: 'error', interrupted: 'warning' } as const
</script>

<template>
  <UiBadge :tone="tones[status]">{{ labels[status] }}</UiBadge>
</template>
