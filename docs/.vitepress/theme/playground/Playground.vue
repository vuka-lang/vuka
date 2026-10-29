<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, shallowRef, watch } from 'vue'
import { useData, withBase } from 'vitepress'
import { basicSetup } from 'codemirror'
import { Compartment, EditorState } from '@codemirror/state'
import { EditorView } from '@codemirror/view'
import { lintGutter, setDiagnostics, type Diagnostic as LintDiagnostic } from '@codemirror/lint'
import { goLanguage, highlighting, language } from './languages'
import { remoteRunner, wasmRunner, type Diagnostic, type Ran, type Runner, type Transpiled } from './runner'
import { decodeProject, encodeProject, type Project } from './share'
import exampleList from '../../../playground/examples.json'

const sources = import.meta.glob('../../../playground/examples/**/*', { query: '?raw', import: 'default', eager: true }) as Record<string, string>

function example(name: string): Project {
  const prefix = `../../../playground/examples/${name}/`
  const files: Record<string, string> = {}
  for (const [path, src] of Object.entries(sources)) if (path.startsWith(prefix)) files[path.slice(prefix.length)] = src
  const entry = exampleList.find((e) => e.name === name)!
  return { files, open: entry.open }
}

const { isDark } = useData()
const files = reactive<Record<string, string>>({})
const current = ref('')
const chosen = ref(exampleList[0].name)
const diagnostics = shallowRef<Diagnostic[]>([])
const generated = shallowRef<Record<string, string>>({})
const status = ref('Loading the compiler…')
const failure = ref('')
const shared = ref(false)
const sourceEl = ref<HTMLElement>()

// Run is there when the docs were built with a runner service's URL.
const runnerURL: string = import.meta.env.VITE_VUKA_RUNNER ?? ''
const canRun = !!runnerURL
const view = ref<'go' | 'run'>('go')
const building = ref(false)
const ran = shallowRef<Ran | null>(null)
const runLog = ref<string[]>([])
const runPath = ref('/')
const frameKey = ref(0)
const frameSrc = computed(() => (ran.value?.url ? ran.value.url + runPath.value.replace(/^\/+/, '') : ''))
const isMac = typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform)
const goEl = ref<HTMLElement>()

const paths = computed(() => Object.keys(files).sort(order))
const counts = computed(() => {
  const n = { error: 0, warning: 0 }
  for (const d of diagnostics.value) n[d.severity]++
  return n
})
const goText = computed(() => {
  const p = current.value
  if (p.endsWith('.go')) return `// ${p} is Go: it compiles as written.\n`
  return generated.value[p] ?? ''
})
const goStale = computed(() => !current.value.endsWith('.go') && diagnostics.value.some((d) => d.severity === 'error' && sameFolder(d.span.file, current.value)))

function order(a: string, b: string): number {
  const depth = (p: string) => (p.includes('/') ? 1 : 0)
  return depth(a) - depth(b) || a.localeCompare(b)
}

function sameFolder(a = '', b = ''): boolean {
  const dir = (p: string) => p.slice(0, Math.max(0, p.lastIndexOf('/')))
  return dir(a) === dir(b)
}

let runner: Runner | undefined
let source: EditorView | undefined
let goView: EditorView | undefined
const states = new Map<string, EditorState>()
const theme = new Compartment()
const goTheme = new Compartment()
const themeOf = (dark: boolean) => EditorView.theme({}, { dark })

function sourceState(path: string): EditorState {
  return EditorState.create({
    doc: files[path],
    extensions: [
      basicSetup,
      language(path),
      highlighting,
      lintGutter(),
      theme.of(themeOf(isDark.value)),
      EditorView.updateListener.of((u) => {
        if (u.docChanged && current.value === path) {
          files[path] = u.state.doc.toString()
          schedule()
        }
      }),
    ],
  })
}

function open(path: string) {
  if (!source) return
  if (current.value && states.has(current.value)) states.set(current.value, source.state)
  current.value = path
  let st = states.get(path)
  if (!st) {
    st = sourceState(path)
    states.set(path, st)
  }
  source.setState(st)
  source.dispatch({ effects: theme.reconfigure(themeOf(isDark.value)) })
  lint()
}

function load(p: Project) {
  for (const k of Object.keys(files)) delete files[k]
  Object.assign(files, p.files)
  states.clear()
  current.value = ''
  generated.value = {}
  diagnostics.value = []
  open(p.files[p.open] !== undefined ? p.open : paths.value[0])
  transpile()
}

// Transpiles wait for typing to pause; a result arriving after a newer
// request was sent is dropped.
let timer: ReturnType<typeof setTimeout> | undefined
let seq = 0
function schedule() {
  clearTimeout(timer)
  timer = setTimeout(transpile, 250)
}

async function transpile() {
  if (!runner) return
  const mine = ++seq
  try {
    const res: Transpiled = await runner.transpile({ ...files })
    if (mine !== seq) return
    failure.value = ''
    generated.value = { ...generated.value, ...res.files }
    diagnostics.value = res.diagnostics
    status.value = `Transpiled in ${Math.round(res.ms)} ms`
    lint()
  } catch (err) {
    if (mine !== seq) return
    failure.value = (err as Error).message
    status.value = 'The compiler failed'
  }
}

// lint shows the open file's diagnostics in its editor.
function lint() {
  if (!source) return
  const doc = source.state.doc
  const out: LintDiagnostic[] = []
  for (const d of diagnostics.value) {
    if (d.span.file !== current.value) continue
    const { from, to } = range(doc, d.span)
    out.push({
      from,
      to,
      severity: d.severity,
      message: d.message + (d.help ? `; ${d.help}` : ''),
      renderMessage: () => message(d),
    })
  }
  source.dispatch(setDiagnostics(source.state, out))
}

function message(d: Diagnostic): HTMLElement {
  const el = document.createElement('div')
  const code = document.createElement('a')
  code.href = withBase(`/errors/${d.code}`)
  code.textContent = d.code
  code.className = 'pg-code'
  el.append(code, ' ', d.message)
  if (d.help) el.append(document.createElement('br'), `help: ${d.help}`)
  if (d.suggest) el.append(document.createElement('br'), `did you mean ${d.suggest}?`)
  return el
}

// range is where a span is in doc: its columns count bytes.
function range(doc: EditorState['doc'], span: Diagnostic['span']): { from: number; to: number } {
  const n = Math.min(Math.max(span.line ?? 1, 1), doc.lines)
  const line = doc.line(n)
  if (!span.col) return { from: line.from, to: line.to }
  const at = (col: number) => line.from + byteColumn(line.text, col)
  const from = Math.min(at(span.col), line.to)
  let to = span.endCol && span.endCol > span.col ? Math.min(at(span.endCol), line.to) : from
  if (to === from) {
    const word = /^[\w.]+|^./.exec(line.text.slice(from - line.from))
    to = from + (word ? word[0].length : 0)
  }
  return { from, to }
}

function byteColumn(text: string, col: number): number {
  const enc = new TextEncoder()
  let bytes = 0
  let i = 0
  for (const ch of text) {
    if (bytes >= col - 1) break
    bytes += enc.encode(ch).length
    i += ch.length
  }
  return i
}

function reveal(d: Diagnostic) {
  const file = d.span.file
  if (!file || !(file in files) || !source) return
  open(file)
  const { from } = range(source.state.doc, d.span)
  source.dispatch({ selection: { anchor: from }, scrollIntoView: true })
  source.focus()
}

async function format() {
  if (!runner || !source) return
  const path = current.value
  const res = await runner.format(path, files[path])
  if (res.error) {
    failure.value = `${res.error.span.file}:${res.error.span.line ?? ''}:${res.error.span.col ?? ''}: ${res.error.message}`
    return
  }
  failure.value = ''
  if (res.src !== undefined && res.src !== files[path] && current.value === path) {
    source.dispatch({ changes: { from: 0, to: source.state.doc.length, insert: res.src } })
  }
}

let runAbort: AbortController | undefined

function stopRun() {
  runAbort?.abort()
  runAbort = undefined
  if (ran.value?.session) runner?.stop?.(ran.value.session)
  ran.value = null
}

// run builds the project on the runner and shows the program; a run before
// it is stopped first.
async function run() {
  if (!runner?.run || building.value) return
  stopRun()
  view.value = 'run'
  runLog.value = []
  building.value = true
  const ctl = new AbortController()
  runAbort = ctl
  const push = (line: string) => {
    if (runAbort === ctl) runLog.value = [...runLog.value, line].slice(-500)
  }
  try {
    const res = await runner.run({ ...files }, { log: push, output: push, exit: (st) => push(`· the program ${st}`) }, ctl.signal)
    if (runAbort !== ctl) return
    ran.value = res
    if (res.diagnostics?.length) {
      diagnostics.value = res.diagnostics
      lint()
    }
    for (const line of (res.output ?? '').trimEnd().split('\n')) if (line) push(line)
    if (res.error) push(res.error)
    else if (res.url) status.value = 'Running'
    runPath.value = '/'
    frameKey.value++
  } catch (err) {
    if (runAbort === ctl) push((err as Error).message)
  } finally {
    if (runAbort === ctl) building.value = false
  }
}

function runKey(e: KeyboardEvent) {
  if (canRun && e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
    e.preventDefault()
    e.stopPropagation()
    run()
  }
}

function stopOnLeave() {
  if (ran.value?.session) runner?.stop?.(ran.value.session)
}

async function share() {
  const hash = await encodeProject({ files: { ...files }, open: current.value })
  history.replaceState(null, '', '#' + hash)
  try {
    await navigator.clipboard.writeText(location.href)
  } catch {}
  shared.value = true
  setTimeout(() => (shared.value = false), 2000)
}

function validName(p: string | null): p is string {
  return !!p && /^([\w.-]+\/)*[\w.-]+\.(vuka|vuka\.html|go)$/.test(p) && !p.split('/').includes('..')
}

function addFile() {
  const p = prompt('New file (.vuka, .vuka.html or .go), its folder its package:', 'pages/about.vuka.html')
  if (!validName(p)) return
  if (!(p in files)) {
    const pkg = p.includes('/') ? p.slice(0, p.lastIndexOf('/')).split('/').pop()! : 'main'
    files[p] = `package ${pkg}\n`
  }
  open(p)
  schedule()
}

function renameFile(from: string) {
  const to = prompt('Rename to:', from)
  if (!validName(to) || to === from || to in files) return
  if (current.value === from && source) files[from] = source.state.doc.toString()
  files[to] = files[from]
  delete files[from]
  states.delete(from)
  if (current.value === from) {
    current.value = ''
    open(to)
  }
  schedule()
}

function deleteFile(p: string) {
  if (paths.value.length === 1 || !confirm(`Delete ${p}?`)) return
  delete files[p]
  states.delete(p)
  if (current.value === p) {
    current.value = ''
    open(paths.value[0])
  }
  schedule()
}

function pick() {
  load(example(chosen.value))
  history.replaceState(null, '', location.pathname)
}

async function fromHash() {
  const p = await decodeProject(location.hash)
  if (p) load(p)
  return !!p
}

watch(isDark, (dark) => {
  source?.dispatch({ effects: theme.reconfigure(themeOf(dark)) })
  goView?.dispatch({ effects: goTheme.reconfigure(themeOf(dark)) })
})

watch(goText, (text) => {
  if (goView && goView.state.doc.toString() !== text) {
    goView.dispatch({ changes: { from: 0, to: goView.state.doc.length, insert: text } })
  }
})

onMounted(async () => {
  source = new EditorView({ parent: sourceEl.value! })
  goView = new EditorView({
    parent: goEl.value!,
    state: EditorState.create({
      extensions: [basicSetup, goLanguage, highlighting, EditorState.readOnly.of(true), EditorView.editable.of(false), goTheme.of(themeOf(isDark.value))],
    }),
  })
  runner = wasmRunner(withBase('/playground/'))
  if (canRun) {
    runner = remoteRunner(runner, runnerURL)
    addEventListener('keydown', runKey, true)
    addEventListener('pagehide', stopOnLeave)
  }
  if (!(await fromHash())) load(example(chosen.value))
  addEventListener('hashchange', fromHash)
})

onBeforeUnmount(() => {
  removeEventListener('hashchange', fromHash)
  removeEventListener('keydown', runKey, true)
  removeEventListener('pagehide', stopOnLeave)
  stopRun()
  clearTimeout(timer)
  runner?.dispose()
  source?.destroy()
  goView?.destroy()
})
</script>

<template>
  <div class="pg">
    <div class="pg-bar">
      <label class="pg-examples">
        <span>Example</span>
        <select v-model="chosen" @change="pick">
          <option v-for="e in exampleList" :key="e.name" :value="e.name">{{ e.title }}</option>
        </select>
      </label>
      <button v-if="canRun" type="button" class="pg-button pg-run" :disabled="building" :title="`Run (${isMac ? '⌘' : 'Ctrl'}+Enter)`" data-testid="run-button" @click="run">
        {{ building ? 'Building…' : 'Run' }}
      </button>
      <button type="button" class="pg-button" @click="format">Format</button>
      <button type="button" class="pg-button" @click="share">{{ shared ? 'Link copied' : 'Share' }}</button>
      <span class="pg-status" data-testid="status">{{ status }}</span>
    </div>
    <p v-if="failure" class="pg-failure" role="alert">{{ failure }}</p>
    <div class="pg-panes">
      <section class="pg-pane">
        <div class="pg-tabs" role="tablist">
          <span v-for="p in paths" :key="p" class="pg-tab" :class="{ active: p === current }">
            <button type="button" role="tab" :aria-selected="p === current" @click="open(p)" @dblclick="renameFile(p)" title="Double-click to rename">{{ p }}</button>
            <button type="button" class="pg-x" :aria-label="`Delete ${p}`" @click="deleteFile(p)">×</button>
          </span>
          <button type="button" class="pg-tab pg-add" aria-label="Add a file" @click="addFile">+</button>
        </div>
        <div ref="sourceEl" class="pg-editor" data-testid="source"></div>
      </section>
      <section class="pg-pane">
        <div class="pg-tabs" :role="canRun ? 'tablist' : undefined">
          <template v-if="canRun">
            <span class="pg-tab" :class="{ active: view === 'go' }">
              <button type="button" role="tab" :aria-selected="view === 'go'" @click="view = 'go'">Generated Go</button>
            </span>
            <span class="pg-tab" :class="{ active: view === 'run' }">
              <button type="button" role="tab" :aria-selected="view === 'run'" data-testid="run-tab" @click="view = 'run'">Program</button>
            </span>
            <span v-if="view === 'go' && goStale" class="pg-label">last good, the package has errors</span>
          </template>
          <span v-else class="pg-label">Generated Go<template v-if="goStale"> · last good, the package has errors</template></span>
        </div>
        <div v-show="view === 'go'" ref="goEl" class="pg-editor pg-go" :class="{ stale: goStale }" data-testid="go"></div>
        <div v-if="canRun" v-show="view === 'run'" class="pg-editor pg-program" data-testid="program">
          <form v-if="ran?.url" class="pg-url" @submit.prevent="frameKey++">
            <input v-model="runPath" aria-label="Path" spellcheck="false" autocomplete="off" data-testid="path" />
            <button type="submit" class="pg-button" title="Reload" aria-label="Reload">↻</button>
            <a class="pg-button" :href="frameSrc" target="_blank" rel="noopener">Open ↗</a>
          </form>
          <iframe
            v-if="ran?.url"
            :key="frameKey"
            :src="frameSrc"
            class="pg-frame"
            sandbox="allow-scripts allow-forms allow-same-origin"
            title="The running program"
            data-testid="frame"
          ></iframe>
          <p v-else-if="!building && !runLog.length" class="pg-none pg-hint">Run builds the project on a server and shows the program here.</p>
          <pre v-if="runLog.length" class="pg-log" :class="{ alone: !ran?.url }" data-testid="log">{{ runLog.join('\n') }}</pre>
        </div>
      </section>
    </div>
    <section class="pg-diags" data-testid="diagnostics">
      <h2>
        Diagnostics
        <small>{{ counts.error }} errors · {{ counts.warning }} warnings</small>
      </h2>
      <p v-if="!diagnostics.length" class="pg-none">vuka build and vet have nothing to say.</p>
      <ul v-else>
        <li v-for="(d, i) in diagnostics" :key="i" :class="d.severity">
          <a class="pg-code" :href="withBase(`/errors/${d.code}`)">{{ d.code }}</a>
          <button type="button" class="pg-where" @click="reveal(d)">{{ d.span.file }}<template v-if="d.span.line">:{{ d.span.line }}<template v-if="d.span.col">:{{ d.span.col }}</template></template></button>
          <span class="pg-msg">{{ d.message }}</span>
          <span v-if="d.help" class="pg-help">help: {{ d.help }}</span>
          <span v-if="d.suggest" class="pg-help">did you mean {{ d.suggest }}?</span>
        </li>
      </ul>
    </section>
  </div>
</template>

<style>
.pg {
  --pg-keyword: #a626a4;
  --pg-string: #50a14f;
  --pg-comment: #8a8f98;
  --pg-number: #986801;
  --pg-type: #c18401;
  --pg-func: #4078f2;
  --pg-tag: #c4501f;
  --pg-attr: #986801;
  --pg-brace: #c4501f;
  --pg-builtin: #0184bc;
  max-width: 1440px;
  margin: 0 auto;
  padding: 24px 24px 48px;
}

.dark .pg {
  --pg-keyword: #c678dd;
  --pg-string: #98c379;
  --pg-comment: #7f848e;
  --pg-number: #d19a66;
  --pg-type: #e5c07b;
  --pg-func: #61afef;
  --pg-tag: #f08a55;
  --pg-attr: #d19a66;
  --pg-brace: #f08a55;
  --pg-builtin: #56b6c2;
}

.pg-bar {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 12px;
  margin-bottom: 12px;
}

.pg-examples {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 14px;
  color: var(--vp-c-text-2);
}

.pg select,
.pg-button {
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-1);
  font-size: 14px;
  padding: 4px 10px;
}

.pg-button:hover {
  border-color: var(--vp-c-brand-1);
}

.pg-run {
  border-color: var(--vp-c-brand-1);
  background: var(--vp-c-brand-1);
  color: var(--vp-c-white);
  font-weight: 600;
}

.pg-run:hover {
  background: var(--vp-c-brand-2);
}

.pg-run:disabled {
  opacity: 0.7;
  cursor: progress;
}

.pg-status {
  margin-left: auto;
  font-size: 13px;
  color: var(--vp-c-text-2);
}

.pg-failure {
  margin: 0 0 12px;
  padding: 8px 12px;
  border-radius: 6px;
  background: var(--vp-c-danger-soft);
  color: var(--vp-c-danger-1);
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
}

.pg-panes {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
  gap: 16px;
}

@media (max-width: 960px) {
  .pg {
    padding: 16px 12px 32px;
  }
  .pg-panes {
    grid-template-columns: minmax(0, 1fr);
  }
}

.pg-pane {
  display: flex;
  flex-direction: column;
  min-width: 0;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  overflow: hidden;
  background: var(--vp-c-bg);
}

.pg-tabs {
  display: flex;
  flex-wrap: wrap;
  align-items: stretch;
  min-height: 36px;
  border-bottom: 1px solid var(--vp-c-divider);
  background: var(--vp-c-bg-soft);
  font-family: var(--vp-font-family-mono);
  font-size: 12.5px;
}

.pg-tab {
  display: inline-flex;
  align-items: center;
  border-right: 1px solid var(--vp-c-divider);
  color: var(--vp-c-text-2);
}

.pg-tab button {
  padding: 6px 4px 6px 12px;
}

.pg-tab.active {
  background: var(--vp-c-bg);
  color: var(--vp-c-text-1);
  box-shadow: inset 0 -2px 0 var(--vp-c-brand-1);
}

.pg-tab .pg-x {
  padding: 6px 10px 6px 4px;
  color: var(--vp-c-text-3);
}

.pg-tab .pg-x:hover {
  color: var(--vp-c-danger-1);
}

.pg-add {
  padding: 6px 12px;
}

.pg-label {
  padding: 8px 12px;
  color: var(--vp-c-text-2);
}

.pg-editor {
  height: min(64vh, 640px);
  overflow: hidden;
}

@media (max-width: 960px) {
  .pg-editor {
    height: 50vh;
  }
}

.pg-editor .cm-editor {
  height: 100%;
  font-size: 13px;
  background: var(--vp-c-bg);
  color: var(--vp-c-text-1);
}

.pg-editor .cm-scroller {
  font-family: var(--vp-font-family-mono);
  line-height: 1.6;
}

.pg-editor .cm-gutters {
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-3);
  border-right: 1px solid var(--vp-c-divider);
}

.pg-editor .cm-activeLine,
.pg-editor .cm-activeLineGutter {
  background: var(--vp-c-default-soft);
}

.pg-editor .cm-cursor {
  border-left-color: var(--vp-c-text-1);
}

.pg-program {
  display: flex;
  flex-direction: column;
  background: var(--vp-c-bg);
}

.pg-url {
  display: flex;
  gap: 6px;
  padding: 6px 8px;
  border-bottom: 1px solid var(--vp-c-divider);
}

.pg-url input {
  flex: 1;
  min-width: 0;
  border: 1px solid var(--vp-c-divider);
  border-radius: 6px;
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-1);
  font-family: var(--vp-font-family-mono);
  font-size: 13px;
  padding: 4px 8px;
}

.pg-url a.pg-button {
  text-decoration: none;
}

.pg-frame {
  flex: 1;
  width: 100%;
  border: 0;
  background: #fff;
}

.pg-log {
  flex: 0 0 auto;
  max-height: 30%;
  margin: 0;
  overflow: auto;
  border-top: 1px solid var(--vp-c-divider);
  padding: 8px 12px;
  background: var(--vp-c-bg-soft);
  color: var(--vp-c-text-2);
  font-family: var(--vp-font-family-mono);
  font-size: 12px;
  line-height: 1.5;
  white-space: pre-wrap;
}

.pg-log.alone {
  flex: 1;
  max-height: none;
  border-top: 0;
}

.pg-hint {
  padding: 16px;
}

.pg-go.stale .cm-content {
  opacity: 0.55;
}

.pg-editor .cm-tooltip {
  background: var(--vp-c-bg-elv);
  color: var(--vp-c-text-1);
  border: 1px solid var(--vp-c-divider);
  max-width: 560px;
  font-size: 13px;
}

.pg-code {
  font-family: var(--vp-font-family-mono);
  font-weight: 600;
  color: var(--vp-c-brand-1);
  text-decoration: none;
}

.pg-code:hover {
  text-decoration: underline;
}

.pg-diags {
  margin-top: 16px;
  border: 1px solid var(--vp-c-divider);
  border-radius: 8px;
  padding: 12px 16px;
}

.pg-diags h2 {
  display: flex;
  align-items: baseline;
  gap: 12px;
  margin: 0 0 8px;
  font-size: 15px;
  font-weight: 600;
}

.pg-diags h2 small {
  font-weight: 400;
  color: var(--vp-c-text-2);
}

.pg-diags ul {
  margin: 0;
  padding: 0;
  list-style: none;
  font-size: 13.5px;
}

.pg-diags li {
  display: flex;
  flex-wrap: wrap;
  gap: 2px 10px;
  padding: 6px 0 6px 10px;
  border-left: 3px solid var(--vp-c-danger-1);
  margin-bottom: 4px;
}

.pg-diags li.warning {
  border-left-color: var(--vp-c-warning-1);
}

.pg-where {
  font-family: var(--vp-font-family-mono);
  color: var(--vp-c-text-2);
}

.pg-where:hover {
  color: var(--vp-c-brand-1);
}

.pg-msg {
  flex-basis: 100%;
  white-space: pre-wrap;
}

.pg-help {
  flex-basis: 100%;
  color: var(--vp-c-text-2);
}

.pg-notes {
  max-width: 1440px;
  margin: 0 auto;
  padding: 0 24px 48px;
  color: var(--vp-c-text-2);
  font-size: 14px;
}

.pg-notes h2 {
  margin: 0 0 8px;
  font-size: 15px;
  font-weight: 600;
  color: var(--vp-c-text-1);
}

.pg-none {
  margin: 0;
  color: var(--vp-c-text-2);
  font-size: 13.5px;
}
</style>
