package main

import (
	"maps"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

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
const vueAdapterSrc = `import { createApp, reactive, shallowRef, h } from 'vue'
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
  // A page backed by its island: useLive() and useVuka() (vuka:live), $vuka.
  // live's reads follow a version the runtime bumps on every change.
  if (ctx && ctx.live && ctx.live.view) {
    const ver = shallowRef(0)
    ctx.live.subscribe(() => { ver.value++ })
    app.provide('vuka:live', ctx.live.view(() => ver.value))
  } else if (ctx && ctx.live) {
    const live = reactive({ ...ctx.live.methods, pending: false, error: null })
    ctx.live.subscribe((s) => { live.pending = s.pending; live.error = s.error })
    app.provide('vuka:live', live)
  }
  if (ctx && ctx.vuka) {
    app.provide('vuka:vuka', ctx.vuka)
    app.config.globalProperties.$vuka = ctx.vuka
  }
  /*SETUP_CALL*/
  release(el)
  app.mount(el)
  return {
    update: (next) => { Object.assign(state, next || {}) },
    unmount: () => { if (el.__vue_app__ === app) release(el) },
  }
}

// release unmounts the app on el, and forgets it even when its unmount
// fails (its DOM changed under it, as when a navigation already removed
// the nodes it teleported): a stale vnode left on the host would make the
// next app's mount patch against it. The failure is tolerated quietly,
// like the other adapters': debug keeps the trace without alarming every
// console on a routine navigation.
function release(el) {
  if (el.__vue_app__) { try { el.__vue_app__.unmount() } catch (err) { console.debug('vuka: unmounting a Vue app:', err) } }
  if (el._vnode || el.__vue_app__) { el._vnode = null; delete el.__vue_app__; el.textContent = '' }
}
`

// reactAdapterSrc is the React adapter: a root rendering the component with
// the props, again on each update. A state prop gets set<Prop> (taking a
// value or an updater) and on<Prop>Change, chained before any the page gave.
const reactAdapterSrc = `import { createElement, createContext } from 'react'
import { createRoot } from 'react-dom/client'
/*SETUP_IMPORT*/
const VukaContext = globalThis.__vukaReactContext || (globalThis.__vukaReactContext = createContext(null))
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
    if (ctx && (ctx.live || ctx.vuka)) node = createElement(VukaContext.Provider, { value: { live: ctx.live || null, vuka: ctx.vuka || null } }, node)
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
import * as reactivity from 'svelte/reactivity'
const { SvelteMap } = reactivity
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
  const context = new Map()
  if (ctx && ctx.live && ctx.live.view) {
    // live's reads are reactive (createSubscriber), and it is a store too ($l).
    const h = ctx.live
    const read = reactivity.createSubscriber ? reactivity.createSubscriber((update) => h.subscribe(() => update())) : () => {}
    const l = h.view(read)
    l.subscribe = (fn) => { fn(l); return h.subscribe(() => fn(l)) }
    context.set('vuka:live', l)
  } else if (ctx && ctx.live) {
    const h = ctx.live
    const value = () => ({ ...h.methods, pending: h.pending, error: h.error })
    context.set('vuka:live', { ...h.methods, subscribe: (fn) => { fn(value()); return h.subscribe(() => fn(value())) } })
  }
  if (ctx && ctx.vuka) context.set('vuka:vuka', { subscribe: (fn) => { fn(ctx.vuka); return () => {} }, navigate: ctx.vuka.navigate, patch: ctx.vuka.patch })
  el.replaceChildren()
  const inst = smount(Component, { target: el, props: p, context })
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

// linkJS is what each framework's VukaLink shares: the href it renders
// (http and https only, else none) and whether it is the current URL's —
// the same path (and query, when it names one) with exact, else that path
// or one below it ("/" only itself).
const linkJS = `function vukaHref(href) {
  try {
    const u = new URL(String(href), location.href)
    return u.protocol === 'http:' || u.protocol === 'https:' ? String(href) : undefined
  } catch (_) { return undefined }
}
function vukaActive(href, exact) {
  if (href === undefined || typeof location === 'undefined') return false
  const u = new URL(href, location.href)
  if (u.origin !== location.origin) return false
  const at = location.pathname, p = u.pathname
  if (exact) return at === p && (!u.search || u.search === location.search)
  return at === p || (p !== '/' && at.startsWith(p.replace(/\/$/, '') + '/'))
}
function vukaWatchURL(fn) {
  addEventListener('vk:navigate', fn)
  addEventListener('popstate', fn)
  return () => { removeEventListener('vk:navigate', fn); removeEventListener('popstate', fn) }
}
`

// vukaLinkSvelte is Svelte's VukaLink, a component file written beside the
// island build (vuka:live imports it from /*VUKALINK*/).
var vukaLinkSvelte = `<script>
  import { onMount } from 'svelte'
  let { href, patch = false, replace = false, activeClass = 'active', exact = false, class: cls = undefined, children, ...rest } = $props()
  let at = $state(0)
  onMount(() => vukaWatchURL(() => { at++ }))
  const safe = $derived(vukaHref(href))
  const on = $derived((at, vukaActive(safe, exact)))
  const current = $derived((at, vukaActive(safe, true)))
  ` + strings.ReplaceAll(strings.TrimSpace(linkJS), "\n", "\n  ") + `
</script>

<a {...rest} href={safe} data-vk-link={patch ? 'patch' : ''} data-vk-replace={replace ? '' : undefined}
  class={[cls, on && activeClass].filter(Boolean).join(' ') || undefined} aria-current={current ? 'page' : undefined}>{@render children?.()}</a>
`

// liveModules are vuka:live, the module a page-backed island's component
// imports useLive (Svelte: live), useVuka and VukaLink from, per framework:
// what the island's adapter provides (ctx.live, ctx.vuka). React's
// re-renders on each change of the runtime's version.
var liveModules = map[string]string{
	"vue": `import { inject, defineComponent, h, shallowRef, onMounted, onBeforeUnmount } from 'vue'
export function useLive() { return inject('vuka:live', null) }
export function useVuka() { return inject('vuka:vuka', null) }
` + linkJS + `export const VukaLink = defineComponent({
  name: 'VukaLink',
  inheritAttrs: false,
  props: { href: { type: String, required: true }, patch: Boolean, replace: Boolean, activeClass: { type: String, default: 'active' }, exact: Boolean },
  setup(props, { attrs, slots }) {
    const at = shallowRef(0)
    let stop = null
    onMounted(() => { stop = vukaWatchURL(() => { at.value++ }) })
    onBeforeUnmount(() => { if (stop) stop() })
    return () => {
      at.value
      const href = vukaHref(props.href)
      return h('a', {
        ...attrs, href,
        'data-vk-link': props.patch ? 'patch' : '',
        'data-vk-replace': props.replace ? '' : undefined,
        class: [attrs.class, vukaActive(href, props.exact) && props.activeClass],
        'aria-current': vukaActive(href, true) ? 'page' : undefined,
      }, slots.default ? slots.default() : undefined)
    }
  },
})
`,
	"react": `import { createContext, createElement, useContext, useMemo, useSyncExternalStore } from 'react'
const VukaContext = globalThis.__vukaReactContext || (globalThis.__vukaReactContext = createContext(null))
const none = { pending: false, error: null }
const noSub = () => () => {}
export function useLive() {
  const c = useContext(VukaContext)
  const h = c && c.live
  const s = useSyncExternalStore(h ? h.subscribe : noSub, h ? (h.version || h.snapshot) : () => none)
  return useMemo(() => h && h.view ? h.view() : ({ ...(h ? h.methods : {}), pending: s.pending, error: s.error }), [h, s])
}
export function useVuka() { const c = useContext(VukaContext); return c ? c.vuka : null }
` + linkJS + `const vukaURL = () => location.href
export function VukaLink({ href, patch, replace, activeClass = 'active', exact, className, children, ...rest }) {
  useSyncExternalStore(vukaWatchURL, vukaURL, () => '')
  const safe = vukaHref(href)
  const cls = [className, vukaActive(safe, exact) && activeClass].filter(Boolean).join(' ')
  return createElement('a', {
    ...rest, href: safe,
    'data-vk-link': patch ? 'patch' : '',
    'data-vk-replace': replace ? '' : undefined,
    className: cls || undefined,
    'aria-current': vukaActive(safe, true) ? 'page' : undefined,
  }, children)
}
`,
	"svelte": `import { getContext } from 'svelte'
export { default as VukaLink } from /*VUKALINK*/
export function live() { return getContext('vuka:live') }
export const useLive = live
export function useVuka() { return getContext('vuka:vuka') }
`,
}

// liveModulesIn are liveModules for a build in dir: Svelte's VukaLink is
// written there, and its vuka:live imports it.
func liveModulesIn(dir string) (map[string]string, error) {
	path := filepath.Join(dir, "vuka-link.svelte")
	if err := os.WriteFile(path, []byte(vukaLinkSvelte), 0o644); err != nil {
		return nil, err
	}
	out := maps.Clone(liveModules)
	out["svelte"] = strings.Replace(out["svelte"], "/*VUKALINK*/", strconv.Quote(filepath.ToSlash(path)), 1)
	return out, nil
}
