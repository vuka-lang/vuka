package main

// The framework adapters the island loader imports: mount(Component, el,
// props, ctx) → {update, unmount}. props are resolved by the loader: a
// function prop is a function (it pushes a live event or runs commands), a
// state prop the state's value; ctx.bound names the state props the
// component may write, and ctx.set(name, v) writes one — the adapter wires
// each framework's two-way convention to it: Vue's update:<prop> (a
// defineModel), React's set<Prop> and on<Prop>Change, Svelte's $bindable.
// Each reads the host's <template data-vk-slot> as the component's children.

// vueAdapterSrc is the Vue adapter. /*SETUP_IMPORT*/ and /*SETUP_CALL*/ are
// replaced when the frontend root has a _setup file (its default export runs
// on the app, the appEntrypoint analogue).
const vueAdapterSrc = `import { createApp, reactive, h } from 'vue'
/*SETUP_IMPORT*/
export function mount(Component, el, props, ctx) {
  const state = reactive({ ...(props || {}) })
  let slotHTML = null
  try {
    const tpl = el && el.querySelector && el.querySelector('template[data-vk-slot]')
    if (tpl) slotHTML = tpl.innerHTML
  } catch (_) {}
  const slots = slotHTML != null && slotHTML !== ''
    ? { default: () => h('span', { innerHTML: slotHTML }) }
    : undefined
  // Forward the component's declared emits to the server handlers bound on the
  // tag (on<Emit>): each emit becomes an on<Event> listener that calls
  // ctx.emit(name, payload); ctx.emit no-ops off a live page. A function
  // prop of the same name (in state) wins.
  const emitNames = Array.isArray(Component.emits) ? Component.emits : Object.keys(Component.emits || {})
  const listeners = {}
  for (const name of emitNames) {
    if (/^update:/.test(name)) continue
    const on = 'on' + name.replace(/(^|[-_])(\w)/g, (_, __, c) => c.toUpperCase())
    listeners[on] = (...args) => {
      if (!ctx || !ctx.emit) return
      try { const r = ctx.emit(name, args.length <= 1 ? args[0] : args); if (r && r.catch) r.catch(() => {}) } catch (_) {}
    }
  }
  // A state prop's model writes go to the state: update:<prop>.
  for (const name of (ctx && ctx.bound) || []) {
    listeners['onUpdate:' + name] = (v) => { state[name] = v; ctx.set(name, v) }
  }
  const app = createApp({ render: () => h(Component, { ...listeners, ...state }, slots) })
  /*SETUP_CALL*/
  app.mount(el)
  return {
    update: (next) => { Object.assign(state, next || {}) },
    unmount: () => { try { app.unmount() } catch (_) {} },
  }
}
`

// reactAdapterSrc is the React adapter: a root rendering the component with
// the props, again on each update. A state prop gets set<Prop> (taking a
// value or an updater) and on<Prop>Change, chained before any the page gave.
const reactAdapterSrc = `import { createElement } from 'react'
import { createRoot } from 'react-dom/client'
/*SETUP_IMPORT*/
export function mount(Component, el, props, ctx) {
  let slotHTML = null
  try {
    const tpl = el && el.querySelector && el.querySelector('template[data-vk-slot]')
    if (tpl) slotHTML = tpl.innerHTML
  } catch (_) {}
  let cur = { ...(props || {}) }
  const root = createRoot(el)
  const cap = (s) => s.charAt(0).toUpperCase() + s.slice(1)
  const render = () => {
    const p = { ...cur }
    for (const name of (ctx && ctx.bound) || []) {
      const set = (v) => {
        const next = typeof v === 'function' ? v(cur[name]) : v
        cur = { ...cur, [name]: next }
        ctx.set(name, next)
        render()
      }
      for (const key of ['set' + cap(name), 'on' + cap(name) + 'Change']) {
        const given = p[key]
        p[key] = given ? (v) => { set(v); return given(v) } : set
      }
    }
    const kids = slotHTML ? createElement('span', { dangerouslySetInnerHTML: { __html: slotHTML } }) : undefined
    let node = createElement(Component, p, kids)
    /*SETUP_CALL*/
    root.render(node)
  }
  render()
  return {
    update: (next) => { cur = { ...(next || {}) }; render() },
    unmount: () => { try { root.unmount() } catch (_) {} },
  }
}
`

// svelteAdapterSrc is the Svelte 5 adapter: the props an object of accessors
// over a SvelteMap (so the component follows updates), a $bindable state
// prop's accessor writing to the state too.
const svelteAdapterSrc = `import { mount as smount, unmount as sunmount, createRawSnippet } from 'svelte'
import { SvelteMap } from 'svelte/reactivity'
export function mount(Component, el, props, ctx) {
  let slotHTML = null
  try {
    const tpl = el && el.querySelector && el.querySelector('template[data-vk-slot]')
    if (tpl) slotHTML = tpl.innerHTML
  } catch (_) {}
  const values = new SvelteMap(Object.entries(props || {}))
  const bound = new Set((ctx && ctx.bound) || [])
  const p = {}
  const define = (k) => Object.defineProperty(p, k, {
    enumerable: true, configurable: true,
    get: () => values.get(k),
    set: bound.has(k) ? (v) => { values.set(k, v); ctx.set(k, v) } : undefined,
  })
  for (const k of values.keys()) define(k)
  if (slotHTML) p.children = createRawSnippet(() => ({ render: () => '<span>' + slotHTML + '</span>' }))
  el.replaceChildren()
  const inst = smount(Component, { target: el, props: p })
  return {
    update: (next) => { for (const [k, v] of Object.entries(next || {})) { if (!(k in p)) define(k); values.set(k, v) } },
    unmount: () => { try { sunmount(inst) } catch (_) {} },
  }
}
`

// adapterSrc is a framework's adapter source, with the app's _setup wired in
// for Vue (setupImport/setupCall).
func adapterSrc(framework, setupImport, setupCall string) string {
	switch framework {
	case "react":
		return replaceSetup(reactAdapterSrc, setupImport, setupCall)
	case "svelte":
		return svelteAdapterSrc
	}
	return replaceSetup(vueAdapterSrc, setupImport, setupCall)
}
