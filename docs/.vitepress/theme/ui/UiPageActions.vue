<script setup lang="ts">
import { ref } from 'vue'
import { withBase } from 'vitepress'

// The actions beside a component page's title: Copy Page (the page as
// Markdown, from docs/public/ui/pages) and the previous and next components.
const props = defineProps<{ copy: string; prev?: string; prevTitle?: string; next?: string; nextTitle?: string }>()
const copied = ref(false)

async function copyPage() {
  try {
    const res = await fetch(withBase(props.copy))
    await navigator.clipboard.writeText(await res.text())
    copied.value = true
    setTimeout(() => (copied.value = false), 1500)
  } catch {}
}
</script>

<template>
  <div class="ui-page-actions">
    <button type="button" class="ui-copy-page" @click="copyPage">{{ copied ? 'Copied' : 'Copy Page' }}</button>
    <a v-if="prev" :href="withBase(prev)" :title="prevTitle" :aria-label="'Previous: ' + prevTitle">&#8592;</a>
    <a v-if="next" :href="withBase(next)" :title="nextTitle" :aria-label="'Next: ' + nextTitle">&#8594;</a>
  </div>
</template>
