<script setup lang="ts">
import { useLive } from 'vuka:live'
const live = useLive<any>()
</script>

<template>
  <input v-model="live.q.value" />
</template>
