<script setup lang="ts">
defineProps<{ title: string; points: number[] }>()
</script>
<template><figure id="chart"><figcaption class="title">{{ title }}</figcaption><span class="points">{{ points.join(',') }}</span></figure></template>
