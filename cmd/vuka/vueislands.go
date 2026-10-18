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

	"github.com/vuka-lang/vuka/diag"
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

// vueBuildMJS is the Node program vuka generates to build the Vue islands. It
// runs esbuild with splitting, collects every output (entries + shared
// chunks), renames each to a content-hashed name the /_vuka/i/ route accepts
// (lowercase, dotted, matching web's moduleName regex), rewrites the relative
// imports between chunks to the new names, and prints a JSON of the files and
// the per-island / adapter chunk names. /*SPEC*/ is replaced with the build
// spec.
const vueBuildMJS = `import esbuild from 'esbuild'
import { createHash } from 'node:crypto'
import { realpathSync } from 'node:fs'
import { resolve as resolvePath } from 'node:path'

const SPEC = /*SPEC*/
const plugins = [{
  name: 'vuka-live',
  setup(b) {
    b.onResolve({ filter: /^vuka:live$/ }, (a) => ({ path: /\.vue/.test(a.importer) ? 'vue' : /\.svelte/.test(a.importer) ? 'svelte' : 'react', namespace: 'vuka-live' }))
    b.onLoad({ filter: /.*/, namespace: 'vuka-live' }, (a) => ({ contents: SPEC.live[a.path], loader: 'js', resolveDir: SPEC.cwd }))
  },
}]
if (SPEC.plugins.includes('vue')) {
  // Vue's compiler resolves defineProps<T>() of a type imported from a
  // module (vuka:live, through tsconfig's paths) with TypeScript, when the
  // project has it.
  try { const sfc = await import('vue/compiler-sfc'); const ts = await import('typescript'); sfc.registerTS(() => ts.default || ts) } catch {}
  plugins.push((await import('esbuild-plugin-vue3')).default())
}
if (SPEC.plugins.includes('svelte')) plugins.push((await import('esbuild-svelte')).default({ compilerOptions: { css: 'injected' } }))

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
  jsx: 'automatic',
  conditions: ['svelte', 'browser'],
  mainFields: ['svelte', 'browser', 'module', 'main'],
  plugins,
})

const base = (p) => p.split('/').pop()
const files = new Map()
for (const f of r.outputFiles) files.set(base(f.path), { contents: Buffer.from(f.contents), imports: [], key: '' })
const entryOf = {}
const real = (p) => { try { return realpathSync(p) } catch { return p } }
for (const [k, v] of Object.entries(SPEC.entries)) entryOf[real(v)] = k
for (const [out, info] of Object.entries(r.metafile.outputs)) {
  const e = files.get(base(out))
  if (!e) continue
  e.imports = (info.imports || []).filter((i) => i.kind === 'import-statement').map((i) => base(i.path))
  if (info.entryPoint) {
    const ep = info.entryPoint.replace(/^[a-z-]+:/, '')
    e.key = entryOf[real(resolvePath(SPEC.cwd, ep))] || entryOf[real(resolvePath(real(SPEC.cwd), ep))] || ''
  }
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
const out = { files: {}, adapters: {}, islands: {} }
for (const [bn, e] of files) out.files[newName.get(bn)] = e.final.toString('base64')
for (const [fw, key] of Object.entries(SPEC.adapters)) for (const [bn, e] of files) if (e.key === key) out.adapters[fw] = newName.get(bn)
for (const [name, key] of Object.entries(SPEC.islands)) for (const [bn, e] of files) if (e.key === key) out.islands[name] = newName.get(bn)
process.stdout.write(JSON.stringify(out))
`

// vueBuildSpec is the build spec the generated .mjs reads: esbuild entry
// points (output key → source file), which key is the adapter, each island's
// entry key, and build options.
type vueBuildSpec struct {
	Entries  map[string]string `json:"entries"`
	Adapters map[string]string `json:"adapters"` // framework → its adapter's entry key
	Islands  map[string]string `json:"islands"`
	Plugins  []string          `json:"plugins"` // vue, svelte: the esbuild plugins to load
	Live     map[string]string `json:"live"`    // vuka:live, per framework
	Minify   bool              `json:"minify"`
	Outdir   string            `json:"outdir"`
	Cwd      string            `json:"cwd"`
}

// vueBuildResult is what the generated .mjs prints: every emitted file by its
// served name (base64 contents), the adapter chunk's name, and each island's
// entry chunk name.
type vueBuildResult struct {
	Files    map[string]string `json:"files"`
	Adapters map[string]string `json:"adapters"`
	Islands  map[string]string `json:"islands"`
}

var jsKeyUnsafe = regexp.MustCompile(`[^a-z0-9]`)

// frameworkDeps are the packages a framework's islands build with.
var frameworkDeps = map[string][]string{
	"vue":    {"vue", "esbuild", "esbuild-plugin-vue3"},
	"react":  {"react", "react-dom", "esbuild"},
	"svelte": {"svelte", "esbuild", "esbuild-svelte"},
}

// findSetup is the import and call of the frontend root's _setup file for a
// framework's adapter: Vue's runs on the app, React's wraps the element.
func findSetup(frontendRoot, framework string) (string, string) {
	for _, s := range []string{"_setup.ts", "_setup.mts", "_setup.js", "_setup.mjs"} {
		if _, e := os.Stat(filepath.Join(frontendRoot, s)); e == nil {
			imp := fmt.Sprintf("import __vukaSetup from %s", strconv.Quote(filepath.Join(frontendRoot, s)))
			switch framework {
			case "vue":
				return imp, "try { if (typeof __vukaSetup === 'function') __vukaSetup(app) } catch (e) { console.error('vuka island _setup:', e) }"
			case "react":
				return imp, "try { if (typeof __vukaSetup === 'function') node = __vukaSetup(node) || node } catch (e) { console.error('vuka island _setup:', e) }"
			}
			return "", ""
		}
	}
	return "", ""
}

func replaceSetup(src, imp, call string) string {
	return strings.NewReplacer("/*SETUP_IMPORT*/", imp, "/*SETUP_CALL*/", call).Replace(src)
}

// buildVueIslands compiles the given framework islands (.vue, .tsx/.jsx,
// .svelte) with one Node esbuild build rooted at the frontend root (where
// the frameworks, esbuild and its Vue/Svelte plugins are installed). It
// returns every emitted chunk (entries + shared chunks) as islandBundles to
// register under /_vuka/i/, and a manifestIsland per island pointing at its
// entry chunk and its framework's adapter chunk.
func buildVueIslands(frontendRoot string, islands []*islandComp, dev bool) ([]*islandBundle, map[string]*manifestIsland, error) {
	nodeBin, err := exec.LookPath("node")
	if err != nil {
		return nil, nil, diag.New("E0602", "framework islands need node on PATH: %v", err).WithHelp("install Node.js 20+")
	}
	nm := filepath.Join(frontendRoot, "node_modules")
	frameworks := map[string]bool{}
	for _, ic := range islands {
		frameworks[ic.framework] = true
	}
	fws := make([]string, 0, len(frameworks))
	for fw := range frameworks {
		fws = append(fws, fw)
	}
	slices.Sort(fws)
	for _, fw := range fws {
		for _, dep := range frameworkDeps[fw] {
			if fi, e := os.Stat(filepath.Join(nm, filepath.FromSlash(dep))); e != nil || !fi.IsDir() {
				return nil, nil, diag.New("E0603", "%s islands need %s in %s", fw, strings.Join(frameworkDeps[fw], ", "), frontendRoot).WithHelp("run: npm i -D %s", strings.Join(frameworkDeps[fw], " "))
			}
		}
	}

	// The build runs inside a temp dir under the frontend root so Node's ESM
	// resolver and esbuild reach its node_modules.
	tmp, err := os.MkdirTemp(frontendRoot, ".vuka-island-build-")
	if err != nil {
		return nil, nil, fmt.Errorf("islands: %w", err)
	}
	defer os.RemoveAll(tmp)

	live, err := liveModulesIn(tmp)
	if err != nil {
		return nil, nil, fmt.Errorf("islands: %w", err)
	}
	spec := vueBuildSpec{
		Entries:  map[string]string{},
		Adapters: map[string]string{},
		Islands:  map[string]string{},
		Minify:   !dev,
		Outdir:   filepath.Join(tmp, "o"),
		Cwd:      frontendRoot,
		Live:     live,
	}
	used := map[string]bool{}
	for _, fw := range fws {
		imp, call := findSetup(frontendRoot, fw)
		ext := ".ts"
		if fw == "svelte" {
			ext = ".js"
		}
		path := filepath.Join(tmp, fw+"-adapter"+ext)
		if err := os.WriteFile(path, []byte(adapterSrc(fw, imp, call)), 0o644); err != nil {
			return nil, nil, fmt.Errorf("islands: %w", err)
		}
		key := fw + "adapter"
		used[key] = true
		spec.Entries[key] = path
		spec.Adapters[fw] = key
		if fw == "vue" || fw == "svelte" {
			spec.Plugins = append(spec.Plugins, fw)
		}
	}
	bySrc := map[string]string{} // islands sharing a component share its entry
	for _, ic := range islands {
		if key, ok := bySrc[ic.src]; ok {
			spec.Islands[ic.name] = key
			continue
		}
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
		bySrc[ic.src] = key
	}

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, err
	}
	mjs := strings.Replace(vueBuildMJS, "/*SPEC*/", string(specJSON), 1)
	mjsPath := filepath.Join(tmp, "build.mjs")
	if err := os.WriteFile(mjsPath, []byte(mjs), 0o644); err != nil {
		return nil, nil, fmt.Errorf("islands: %w", err)
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
		return nil, nil, fmt.Errorf("islands (%s): %s", strings.Join(names, ", "), msg)
	}

	var res vueBuildResult
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil {
		return nil, nil, fmt.Errorf("islands: unreadable build output: %w", err)
	}
	for _, fw := range fws {
		if res.Adapters[fw] == "" {
			return nil, nil, fmt.Errorf("islands: build emitted no %s adapter chunk", fw)
		}
	}

	var bundles []*islandBundle
	for name, b64 := range res.Files {
		js, err := base64.StdEncoding.DecodeString(b64)
		if err != nil {
			return nil, nil, fmt.Errorf("islands: chunk %s: %w", name, err)
		}
		bundles = append(bundles, &islandBundle{name: name, js: js})
	}
	// Stable order so the generated Go embedding is deterministic.
	slices.SortFunc(bundles, func(a, b *islandBundle) int { return strings.Compare(a.name, b.name) })

	entries := map[string]*manifestIsland{}
	for _, ic := range islands {
		mod := res.Islands[ic.name]
		if mod == "" {
			return nil, nil, fmt.Errorf("islands: no chunk emitted for island %s", ic.name)
		}
		entries[ic.name] = &manifestIsland{Name: ic.name, Module: mod, Adapter: res.Adapters[ic.framework], Framework: ic.framework}
	}
	return bundles, entries, nil
}
