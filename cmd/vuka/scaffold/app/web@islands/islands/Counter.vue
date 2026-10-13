<script setup lang="ts">
import { ref } from 'vue'

// The props Go declares on the @ui.Island Counter type (islands.d.ts types
// them against this file). Go owns the contract; keep them in step.
const props = defineProps<{ count: number; label: string }>()

const n = ref(props.count)
</script>

<template>
  <!-- UButton comes from Nuxt UI: no import needed — its Vite plugin resolves
       it, and its Tailwind v4 styles ship in the island's CSS. -->
  <UButton color="primary" variant="soft" icon="i-lucide-plus" @click="n++">
    {{ label }}: {{ n }}
    <slot />
  </UButton>
</template>
