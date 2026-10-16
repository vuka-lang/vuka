package main

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/evanw/esbuild/pkg/api"
)

// What vuka run watches for the islands: a vanilla island's files are what
// its esbuild bundle loaded; a framework island's build (Vite, or esbuild on
// Node) reads anything under the frontend root — its config, stylesheets,
// shared components, package.json and lockfile — and the directories its
// stylesheets name with @source, so those trees are watched, as the island
// cache's key (islandInputs) hashes them.

// recordInputs is an esbuild plugin noting each file the build loads.
func recordInputs(into *[]string) api.Plugin {
	*into = nil
	return api.Plugin{Name: "vuka-inputs", Setup: func(b api.PluginBuild) {
		b.OnLoad(api.OnLoadOptions{Filter: ".*"}, func(a api.OnLoadArgs) (api.OnLoadResult, error) {
			if a.Namespace == "file" || a.Namespace == "" {
				*into = append(*into, a.Path)
			}
			return api.OnLoadResult{}, nil
		})
	}}
}

// islandTrees are the directories a change in which rebuilds pa's framework
// islands.
func islandTrees(pa *pageAssets) []string {
	var trees []string
	add := func(d string) {
		if !slices.Contains(trees, d) {
			trees = append(trees, d)
		}
	}
	for _, ic := range pa.islands {
		if ic.framework == "vanilla" || ic.src == "" {
			continue
		}
		if pa.frontendRoot != "" {
			add(pa.frontendRoot)
			if !strings.HasPrefix(ic.src, pa.frontendRoot+string(filepath.Separator)) {
				add(filepath.Dir(ic.src))
			}
		} else {
			add(filepath.Dir(ic.src))
		}
	}
	if pa.frontendRoot != "" && len(trees) > 0 {
		for _, d := range cssSources(pa.frontendRoot) {
			add(d)
		}
	}
	return trees
}

type treeStamp struct {
	mod  time.Time
	size int64
}

// scanTrees stamps the files under trees an island build reads: not
// installed packages, build output, hidden files and directories, type
// declarations (vuka's islands.d.ts and vuka-live.d.ts, and those Vite
// plugins write as they build), Vite's temporary config bundles, or the Go
// sources the app's own watcher follows.
func scanTrees(trees []string) map[string]treeStamp {
	out := map[string]treeStamp{}
	for _, root := range trees {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			name := d.Name()
			if d.IsDir() {
				if p != root && (name == "node_modules" || name == "dist" || strings.HasPrefix(name, ".")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !frontendInput(name) {
				return nil
			}
			if fi, err := d.Info(); err == nil {
				out[p] = treeStamp{fi.ModTime(), fi.Size()}
			}
			return nil
		})
	}
	return out
}

func frontendInput(name string) bool {
	if strings.HasPrefix(name, ".") || strings.HasSuffix(name, ".d.ts") || strings.Contains(name, ".timestamp-") {
		return false
	}
	switch filepath.Ext(name) {
	case ".go", ".vuka", ".templ":
		return false
	}
	return name != "go.mod" && name != "go.sum"
}
