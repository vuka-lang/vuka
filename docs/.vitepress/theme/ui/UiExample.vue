<script setup lang="ts">
import { onMounted, ref } from 'vue'

// An example of a component: its preview (a page of docs/public/ui/previews,
// drawn with the component's own CSS in the docs' theme) and, under it in
// the same card, its markup: three lines until View Code opens it.
const props = defineProps<{ src: string; hero?: boolean; height?: number }>()
const open = ref(false)
const h = props.height ?? (props.hero ? 220 : 180)
const code = ref<HTMLElement>()

// Code that fits in the collapsed card needs no View Code.
onMounted(() => {
  const block = code.value?.querySelector<HTMLElement>('div[class*="language-"]')
  if (block && block.scrollHeight <= block.clientHeight + 1) open.value = true
})
</script>

<template>
  <div class="ui-example" :class="{ hero }">
    <div class="ui-example-preview">
      <iframe :src="src" :style="{ height: h + 'px' }" loading="lazy" title="Preview"></iframe>
    </div>
    <div ref="code" class="ui-example-code" :class="{ open }">
      <slot />
      <div v-if="!open" class="ui-example-fade">
        <button type="button" @click="open = true">View Code</button>
      </div>
    </div>
  </div>
</template>
