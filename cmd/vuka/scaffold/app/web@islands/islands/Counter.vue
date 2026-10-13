<script setup lang="ts">
import { ref } from 'vue'

// The props Go declares on the @ui.Island Counter type (islands.d.ts types
// them against this file). Go owns the contract; keep them in step.
const props = defineProps<{ count: number; label: string }>()

const n = ref(props.count)
</script>

<template>
  <button
    type="button"
    class="inline-flex items-center gap-2 rounded-lg bg-brand-600 px-3 py-2 text-sm font-medium text-white hover:bg-brand-700"
    @click="n++"
  >
    {{ label }}: {{ n }}
    <slot />
  </button>
</template>
