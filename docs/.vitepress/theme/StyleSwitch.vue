<script setup lang="ts">
import { onMounted } from 'vue'
import { initStyle, setStyle, style, type Style } from './style'

const options: { value: Style; label: string }[] = [
  { value: 'vuka', label: 'Vuka' },
  { value: 'go', label: 'Go' },
]

onMounted(initStyle)

function onKey(e: KeyboardEvent) {
  const keys = ['ArrowLeft', 'ArrowRight', 'ArrowUp', 'ArrowDown', 'Home', 'End']
  if (!keys.includes(e.key)) return
  e.preventDefault()
  const next: Style = e.key === 'Home' ? 'vuka' : e.key === 'End' ? 'go' : style.value === 'vuka' ? 'go' : 'vuka'
  setStyle(next)
  const el = (e.currentTarget as HTMLElement).querySelector<HTMLElement>(`[data-style="${next}"]`)
  el?.focus()
}
</script>

<template>
  <div class="style-switch">
    <span class="style-switch-label" aria-hidden="true">Samples in</span>
    <div class="style-switch-group" role="radiogroup" aria-label="Show code samples in" @keydown="onKey">
      <button
        v-for="o in options"
        :key="o.value"
        type="button"
        role="radio"
        class="style-switch-option"
        :data-style="o.value"
        :aria-checked="style === o.value"
        :tabindex="style === o.value ? 0 : -1"
        @click="setStyle(o.value)"
      >
        {{ o.label }}
      </button>
    </div>
    <a class="style-switch-help" href="/guide/two-ways" title="Two ways to write Vuka" aria-label="About this switch">?</a>
  </div>
</template>
