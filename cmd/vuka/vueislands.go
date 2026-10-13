package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Phase 2 of UI islands adds framework islands that Go's esbuild library can't
// compile. A .vue component needs a Node build step (used only when a .vue
// island exists; vanilla islands stay pure-Go). vuka runs Node esbuild +
// esbuild-plugin-vue3 with splitting on: the entry points are a Vue adapter
// plus one per .vue island, and esbuild emits a shared Vue chunk the entries
// import by relative URL. Every emitted file is registered and served under
// /_vuka/i/, so those relative imports resolve; the manifest points each
// island at its entry chunk and at the adapter chunk, matching Phase 1's
// locked loader (Promise.all([import(module), import(adapter)]) then
// adapter.mount(component, el, props, ctx)). See docs/design/islands.md.

// vueAdapterSrc is the Vue adapter the loader imports for a .vue island. It
// mounts the component with its props made reactive (so update() is a merge),
// passes the host's <template data-vk-slot> as the component's default slot,
// and returns the {update, unmount} handle the loader keeps. /*SETUP_IMPORT*/
// and /*SETUP_CALL*/ are replaced when the frontend root has a _setup file
// (its default export runs on the app, the appEntrypoint analogue).
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
  // ctx.emit(name, payload); ctx.emit no-ops off a live page.
  const emitNames = Array.isArray(Component.emits) ? Component.emits : Object.keys(Component.emits || {})
  const listeners = {}
  for (const name of emitNames) {
    const on = 'on' + name.replace(/(^|[-_])(\w)/g, (_, __, c) => c.toUpperCase())
    listeners[on] = (...args) => {
      if (!ctx || !ctx.emit) return
      try { ctx.emit(name, args.length <= 1 ? args[0] : args) } catch (_) {}
    }
  }
  const app = createApp({ render: () => h(Component, { ...state, ...listeners }, slots) })
  /*SETUP_CALL*/
  app.mount(el)
  return {
    update: (next) => { Object.assign(state, next || {}) },
    unmount: () => { try { app.unmount() } catch (_) {} },
  }
}
`

// vueBuildMJS is the Node program vuka generates to build the Vue islands. It
// runs esbuild with splitting, collects every output (entries + shared
// chunks), renames each to a content-hashed name the /_vuka/i/ route accepts
// (lowercase, dotted, matching web's moduleName regex), rewrites the relative
// imports between chunks to the new names, and prints a JSON of the files and
// the per-island / adapter chunk names. /*SPEC*/ is replaced with the build
// spec.
const vueBuildMJS = `import esbuild from 'esbuild'
import vue from 'esbuild-plugin-vue3'
import { createHash } from 'node:crypto'

const SPEC = /*SPEC*/

const r = await esbuild.build({
  entryPoints: SPEC.entries,
  outdir: SPEC.outdir,
  bundle: true,
  format: 'esm',
  splitting: true,
  platform: 'browser',
  target: 'es2020',
  minify: SPEC.minify,
  sourcemap: SPEC.minify ? false : 'inline',
  metafile: true,
  write: false,
  charset: 'utf8',
  absWorkingDir: SPEC.cwd,
  plugins: [vue()],
})

const base = (p) => p.split('/').pop()
const files = new Map()
for (const f of r.outputFiles) files.set(base(f.path), { contents: Buffer.from(f.contents), imports: [], key: '' })
const entryOf = {}
for (const [k, v] of Object.entries(SPEC.entries)) entryOf[base(v)] = k
for (const [out, info] of Object.entries(r.metafile.outputs)) {
  const e = files.get(base(out))
  if (!e) continue
  e.imports = (info.imports || []).filter((i) => i.kind === 'import-statement').map((i) => base(i.path))
  if (info.entryPoint) e.key = entryOf[base(info.entryPoint)] || ''
}
const stem = (e) => (e.key ? e.key.toLowerCase().replace(/[^a-z0-9]/g, '') : 'chunk') || 'chunk'
const hash8 = (b) => createHash('sha256').update(b).digest('hex').slice(0, 8)
const order = []
const seen = new Set()
const visit = (bn) => {
  if (seen.has(bn)) return
  seen.add(bn)
  for (const d of files.get(bn)?.imports || []) visit(d)
  order.push(bn)
}
for (const bn of files.keys()) visit(bn)
const newName = new Map()
for (const bn of order) {
  const e = files.get(bn)
  let txt = e.contents.toString('utf8')
  for (const dep of e.imports) {
    const nn = newName.get(dep)
    if (nn) txt = txt.split('./' + dep).join('./' + nn)
  }
  const buf = Buffer.from(txt, 'utf8')
  e.final = buf
  newName.set(bn, stem(e) + '.' + hash8(buf) + '.js')
}
const out = { files: {}, adapter: '', islands: {} }
for (const [bn, e] of files) out.files[newName.get(bn)] = e.final.toString('base64')
for (const [bn, e] of files) if (e.key === SPEC.adapterKey) out.adapter = newName.get(bn)
const keyName = {}
for (const [name, key] of Object.entries(SPEC.islands)) keyName[key] = name
for (const [bn, e] of files) if (e.key && keyName[e.key]) out.islands[keyName[e.key]] = newName.get(bn)
process.stdout.write(JSON.stringify(out))
`

// vueBuildSpec is the build spec the generated .mjs reads: esbuild entry
// points (output key → source file), which key is the adapter, each island's
// entry key, and build options.
type vueBuildSpec struct {
	Entries    map[string]string `json:"entries"`
	AdapterKey string            `json:"adapterKey"`
	Islands    map[string]string `json:"islands"`
	Minify     bool              `json:"minify"`
	Outdir     string            `json:"outdir"`
	Cwd        string            `json:"cwd"`
}

// vueBuildResult is what the generated .mjs prints: every emitted file by its
// served name (base64 contents), the adapter chunk's name, and each island's
// entry chunk name.
type vueBuildResult struct {
	Files   map[string]string `json:"files"`
	Adapter string            `json:"adapter"`
	Islands map[string]string `json:"islands"`
}

var jsKeyUnsafe = regexp.MustCompile(`[^a-z0-9]`)

// buildVueIslands compiles the given .vue islands with one Node esbuild build
// rooted at the frontend root (where vue / esbuild / esbuild-plugin-vue3 are
// installed). It returns every emitted chunk (entries + the shared Vue chunk)
// as islandBundles to register under /_vuka/i/, and a manifestIsland per
// island pointing at its entry chunk and the shared adapter chunk.
func buildVueIslands(frontendRoot string, islands []*islandComp, dev bool) ([]*islandBundle, map[string]*manifestIsland, error) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return nil, nil, fmt.Errorf("vue islands need node on PATH (install Node.js 20+): %w", err)
	}
	nm := filepath.Join(frontendRoot, "node_modules")
	for _, dep := range []string{"vue", "esbuild", "esbuild-plugin-vue3"} {
		if fi, e := os.Stat(filepath.Join(nm, filepath.FromSlash(dep))); e != nil || !fi.IsDir() {
			return nil, nil, fmt.Errorf("vue islands need `vue` and `esbuild-plugin-vue3` in %s; run: npm i -D esbuild esbuild-plugin-vue3 vue", frontendRoot)
		}
	}

	// The build runs inside a temp dir under the frontend root so Node's ESM
	// resolver and esbuild reach its node_modules.
	tmp, err := os.MkdirTemp(frontendRoot, ".vuka-island-build-")
	if err != nil {
		return nil, nil, fmt.Errorf("vue islands: %w", err)
	}
	defer os.RemoveAll(tmp)

	setupImport, setupCall := "", ""
	for _, s := range []string{"_setup.ts", "_setup.mts", "_setup.js", "_setup.mjs"} {
		if _, e := os.Stat(filepath.Join(frontendRoot, s)); e == nil {
			setupImport = fmt.Sprintf("import __vukaSetup from %s", strconv.Quote(filepath.Join(frontendRoot, s)))
			setupCall = "try { if (typeof __vukaSetup === 'function') __vukaSetup(app) } catch (e) { console.error('vuka island _setup:', e) }"
			break
		}
	}
	adapter := strings.NewReplacer("/*SETUP_IMPORT*/", setupImport, "/*SETUP_CALL*/", setupCall).Replace(vueAdapterSrc)
	adapterPath := filepath.Join(tmp, "vue-adapter.ts")
	if err := os.WriteFile(adapterPath, []byte(adapter), 0o644); err != nil {
		return nil, nil, fmt.Errorf("vue islands: %w", err)
	}

	const adapterKey = "vue"
	spec := vueBuildSpec{
		Entries:    map[string]string{adapterKey: adapterPath},
		AdapterKey: adapterKey,
		Islands:    map[string]string{},
		Minify:     !dev,
		Outdir:     filepath.Join(tmp, "o"),
		Cwd:        frontendRoot,
	}
	used := map[string]bool{adapterKey: true}
	for _, ic := range islands {
		key := jsKeyUnsafe.ReplaceAllString(strings.ToLower(ic.name), "")
		if key == "" {
			key = "island"
		}
		for b, i := key, 2; used[key]; i++ {
			key = fmt.Sprintf("%s%d", b, i)
		}
		used[key] = true
		spec.Entries[key] = ic.src
		spec.Islands[ic.name] = key
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, err
	}
	mjs := strings.Replace(vueBuildMJS, "/*SPEC*/", string(specJSON), 1)
	mjsPath := filepath.Join(tmp, "build.mjs")
	if err := os.WriteFile(mjsPath, []byte(mjs), 0o644); err != nil {
		return nil, nil, fmt.Errorf("vue islands: %w", err)
	}

	var stdout, stderr bytes.Buffer
	cmd := exec.Command(nodeBin, mjsPath)
	cmd.Dir = tmp
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		names := make([]string, 0, len(islands))
		for _, ic := range islands {
			names = append(names, ic.rel)
		}
		return nil, nil, fmt.Errorf("vue islands (%s): %s", strings.Join(names, ", "), msg)
	}

	var res vueBuildResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return nil, nil, fmt.Errorf("vue islands: unreadable build output: %w", err)
	}
	if res.Adapter == "" {
		return nil, nil, fmt.Errorf("vue islands: build emitted no adapter chunk")
	}

	var bundles []*islandBundle
	for name, b64 := range res.Files {
		js, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, nil, fmt.Errorf("vue islands: chunk %s: %w", name, err)
		}
		bundles = append(bundles, &islandBundle{name: name, js: js})
	}
	// Stable order so the generated Go embedding is deterministic.
	slices.SortFunc(bundles, func(a, b *islandBundle) int { return strings.Compare(a.name, b.name) })

	entries := map[string]*manifestIsland{}
	for _, ic := range islands {
		mod := res.Islands[ic.name]
		if mod == "" {
			return nil, nil, fmt.Errorf("vue islands: no chunk emitted for island %s", ic.name)
		}
		entries[ic.name] = &manifestIsland{Name: ic.name, Module: mod, Adapter: res.Adapter, Framework: "vue"}
	}
	return bundles, entries, nil
}
