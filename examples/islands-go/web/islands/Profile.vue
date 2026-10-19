<script setup lang="ts">
defineProps<{ id: number; name: string }>()
</script>
<template><section id="profile"><h1 class="name">{{ name }}</h1><span class="id">{{ id }}</span></section></template>
