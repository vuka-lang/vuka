import { ref } from 'vue'

// The reader's preferred way of writing an app: a .vuka file, or plain Go on
// the library API. The page shows the samples of one; config.mts's head
// script applies it before the first paint.

export type Style = 'vuka' | 'go'

export const storageKey = 'vuka-docs-style'

export const style = ref<Style>('vuka')

function read(): Style {
  return document.documentElement.classList.contains('prefer-go') ? 'go' : 'vuka'
}

function apply(s: Style) {
  const c = document.documentElement.classList
  c.toggle('prefer-go', s === 'go')
  c.toggle('prefer-vuka', s === 'vuka')
}

export function initStyle() {
  if (typeof document === 'undefined') return
  style.value = read()
}

export function setStyle(s: Style) {
  style.value = s
  apply(s)
  try {
    localStorage.setItem(storageKey, s)
  } catch {}
  const url = new URL(location.href)
  if (url.searchParams.get('style') !== s) {
    url.searchParams.set('style', s)
    history.replaceState(history.state, '', url)
  }
}
