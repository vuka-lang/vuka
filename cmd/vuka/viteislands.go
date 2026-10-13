package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Phase 4 of UI islands: framework islands built through the project's own
// Vite, so they can use a Vite-plugin UI library (Nuxt UI, etc.) whose
// components and Tailwind v4 CSS only exist after its Vite plugin runs — the
// Go/esbuild path can't produce those. It is chosen (over the Phase 2 esbuild
// path) when the island frontend root is a Vite project (has a vite.config.*);
// vanilla .ts islands stay on the pure-Go path.
//
// vuka drives the user's own Vite build (their [vue(), ui(), tailwindcss()]
// plugins intact) through the vuka Vite plugin: vuka writes the island
// entries (a Vue adapter, one `export {default}` per island, a CSS entry) and
// a spec file under a temp dir in the frontend root, then runs
// `node_modules/.bin/vite build` with VUKA_ISLAND_BUILD pointing at the spec.
// The plugin reads it, replaces rollupOptions.input with the island entries,
// redirects the output to the temp dir, and forces the manifest on. vuka then
// reads Vite's .vite/manifest.json, and registers every emitted chunk and the
// CSS under /_vuka/i/ exactly as the Phase-2 path does (the loader + adapter
// contract is unchanged). See docs/design/islands.md, Phase 4.

// hasViteConfig reports whether dir is a Vite project: it has a vite.config
// in one of Vite's recognised extensions.
func hasViteConfig(dir string) bool {
	for _, n := range []string{"vite.config.mts", "vite.config.ts", "vite.config.mjs", "vite.config.js", "vite.config.cts", "vite.config.cjs"} {
		if fileExists(filepath.Join(dir, n)) {
			return true
		}
	}
	return false
}

// islandBuildSpec is the JSON vuka writes and the vuka Vite plugin reads
// (VUKA_ISLAND_BUILD): where to put the build and the entry set (rollup input
// key → path relative to the Vite root).
type islandBuildSpec struct {
	OutDir string            `json:"outDir"`
	Input  map[string]string `json:"input"`
}

// viteManifestEntry is one line of Vite's .vite/manifest.json that vuka reads:
// an entry's or chunk's emitted file and the chunks it imports.
type viteManifestEntry struct {
	File    string   `json:"file"`
	Imports []string `json:"imports"`
	IsEntry bool     `json:"isEntry"`
}

// parseViteManifest reads Vite's manifest into a map keyed by each source's
// path relative to the Vite root (the manifest's own keys).
func parseViteManifest(b []byte) (map[string]viteManifestEntry, error) {
	var m map[string]viteManifestEntry
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	return m, nil
}

// buildViteIslands builds the given framework islands through the frontend
// root's own Vite. It returns every emitted JS chunk and the CSS as bundles to
// register under /_vuka/i/, a manifestIsland per island (its entry chunk + the
// shared adapter chunk), and the CSS chunks' served names.
func buildViteIslands(root, frontendRoot string, islands []*islandComp, dev bool, warn io.Writer) ([]*islandBundle, map[string]*manifestIsland, []string, error) {
	if warn == nil {
		warn = io.Discard
	}
	// Install the frontend's dependencies if needed and sync the vuka Vite
	// plugin into it (idempotent — a no-op when already up to date).
	if err := prepareFrontend(root, frontendRoot, warn); err != nil {
		return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
	}
	if !fileExists(viteBinary(frontendRoot)) {
		return nil, nil, nil, fmt.Errorf("vite islands: no Vite in %s (run its package manager, or add vite)", rel(root, frontendRoot))
	}
	if !importsPlugin(frontendRoot) {
		return nil, nil, nil, fmt.Errorf("vite islands: %s's vite.config must import and use the vuka plugin (add vuka() to plugins) so the island entries are injected", rel(root, frontendRoot))
	}

	tmp, err := os.MkdirTemp(frontendRoot, ".vuka-vite-isl-")
	if err != nil {
		return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
	}
	defer os.RemoveAll(tmp)
	outDir := filepath.Join(tmp, "o")

	// The Vue adapter, with the app's optional _setup default export wired in.
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
		return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
	}

	toRel := func(p string) string {
		r, err := filepath.Rel(frontendRoot, p)
		if err != nil {
			return filepath.ToSlash(p)
		}
		return filepath.ToSlash(r)
	}

	const adapterKey = "vue"
	spec := islandBuildSpec{OutDir: outDir, Input: map[string]string{adapterKey: toRel(adapterPath)}}
	adapterRel := spec.Input[adapterKey]

	// One `export {default}` entry per island; its manifest key is its path
	// relative to the Vite root.
	islandRel := map[string]string{} // island name → entry source rel path
	used := map[string]bool{adapterKey: true, "styles": true}
	for _, ic := range islands {
		key := jsKeyUnsafe.ReplaceAllString(strings.ToLower(ic.name), "")
		if key == "" {
			key = "island"
		}
		for b, i := key, 2; used[key]; i++ {
			key = fmt.Sprintf("%s%d", b, i)
		}
		used[key] = true
		entryPath := filepath.Join(tmp, key+".entry.ts")
		src := fmt.Sprintf("export { default } from %s\n", strconv.Quote(ic.src))
		if err := os.WriteFile(entryPath, []byte(src), 0o644); err != nil {
			return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
		}
		spec.Input[key] = toRel(entryPath)
		islandRel[ic.name] = spec.Input[key]
	}

	// The CSS entry: the app's own app.css when it has one, else a Tailwind +
	// Nuxt UI starter written into the temp dir.
	cssRel := ""
	if appCSS := filepath.Join(frontendRoot, "app.css"); fileExists(appCSS) {
		cssRel = toRel(appCSS)
	} else {
		cssPath := filepath.Join(tmp, "app.css")
		if err := os.WriteFile(cssPath, []byte("@import \"tailwindcss\";\n@import \"@nuxt/ui\";\n"), 0o644); err != nil {
			return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
		}
		cssRel = toRel(cssPath)
	}
	spec.Input["styles"] = cssRel

	specJSON, err := json.Marshal(spec)
	if err != nil {
		return nil, nil, nil, err
	}
	specPath := filepath.Join(tmp, "islands.json")
	if err := os.WriteFile(specPath, specJSON, 0o644); err != nil {
		return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
	}

	fmt.Fprintf(warn, "vuka: building %d Vue island(s) with Vite in %s\n", len(islands), rel(root, frontendRoot))
	var stdout bytes.Buffer
	c := exec.Command(viteBinary(frontendRoot), "build", "--logLevel", "warn")
	c.Dir = frontendRoot
	env := append(os.Environ(), "VUKA_ISLAND_BUILD="+specPath)
	if dev {
		env = append(env, "NODE_ENV=development")
	} else {
		env = append(env, "NODE_ENV=production")
	}
	c.Env = env
	w := &prefixed{w: warn, prefix: "vite: "}
	c.Stdout, c.Stderr = &stdout, w
	err = c.Run()
	w.flush()
	if err != nil {
		names := make([]string, 0, len(islands))
		for _, ic := range islands {
			names = append(names, ic.rel)
		}
		return nil, nil, nil, fmt.Errorf("vite islands (%s): vite build: %v\n%s", strings.Join(names, ", "), err, stdout.String())
	}

	manPath := filepath.Join(outDir, ".vite", "manifest.json")
	manBytes, err := os.ReadFile(manPath)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("vite islands: no manifest at %s: %w (does vite.config use the vuka plugin?)", rel(frontendRoot, manPath), err)
	}
	man, err := parseViteManifest(manBytes)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("vite islands: unreadable manifest: %w", err)
	}

	fileOf := func(relKey string) (string, bool) {
		e, ok := man[relKey]
		if !ok || e.File == "" {
			return "", false
		}
		return filepath.Base(e.File), true
	}
	adapterChunk, ok := fileOf(adapterRel)
	if !ok {
		return nil, nil, nil, fmt.Errorf("vite islands: manifest has no adapter entry (%s)", adapterRel)
	}

	// Register every emitted JS and CSS file under its own basename, so the
	// relative imports between chunks resolve under /_vuka/i/.
	entriesDir, err := os.ReadDir(outDir)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
	}
	var bundles []*islandBundle
	var cssNames []string
	for _, de := range entriesDir {
		if de.IsDir() {
			continue
		}
		name := de.Name()
		if !strings.HasSuffix(name, ".js") && !strings.HasSuffix(name, ".css") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			return nil, nil, nil, fmt.Errorf("vite islands: %w", err)
		}
		bundles = append(bundles, &islandBundle{name: name, js: data})
		if strings.HasSuffix(name, ".css") {
			cssNames = append(cssNames, name)
		}
	}
	slices.SortFunc(bundles, func(a, b *islandBundle) int { return strings.Compare(a.name, b.name) })
	slices.Sort(cssNames)

	entries := map[string]*manifestIsland{}
	for _, ic := range islands {
		chunk, ok := fileOf(islandRel[ic.name])
		if !ok {
			return nil, nil, nil, fmt.Errorf("vite islands: no chunk emitted for island %s", ic.name)
		}
		entries[ic.name] = &manifestIsland{Name: ic.name, Module: chunk, Adapter: adapterChunk, Framework: "vue"}
	}
	return bundles, entries, cssNames, nil
}
