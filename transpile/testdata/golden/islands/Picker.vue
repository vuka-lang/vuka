<script setup lang="ts">
type Kind = 'day' | 'week'
interface Props {
  title: string
  kind?: Kind
  max?: number
  days: { label: string; free: boolean }[]
}
const props = withDefaults(defineProps<Props>(), { kind: 'day' })
const open = defineModel<boolean>('open')
const emit = defineEmits<{ select: [day: string]; close: [] }>()
</script>
<template><div><slot /></div></template>
