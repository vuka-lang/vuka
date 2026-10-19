<script setup lang="ts">
defineProps<{ label: string; options: string[]; picked: string }>()
defineEmits<{ pick: [value: string] }>()
</script>
<template>
  <fieldset class="picker">
    <legend>{{ label }}</legend>
    <button v-for="o in options" :key="o" :class="{ on: o === picked }" @click="$emit('pick', o)">{{ o }}</button>
  </fieldset>
</template>
