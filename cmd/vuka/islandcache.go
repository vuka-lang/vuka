package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/gob"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// The island build's cache: a Vite build of the framework islands is
// skipped when nothing it reads changed since the last one — the island
// sources and everything else in the frontend root (Tailwind scans it for
// classes), its config and lockfiles, what the installed packages are, the
// directories its stylesheets name with @source, the islands' names and
// frameworks, the adapters, the build's mode and this vuka. Its output is
// kept in the user's cache directory, one per frontend root.

// islandCacheFile is the cache of the frontend root's last build.
func islandCacheFile(frontendRoot string) string {
	if strings.EqualFold(os.Getenv("VUKA_CACHE"), "off") {
		return ""
	}
	d, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	h := sha256.Sum256([]byte(frontendRoot))
	return filepath.Join(d, "vuka", "islands", hex.EncodeToString(h[:12])+".gob")
}

type islandCache struct {
	Key     string
	Bundles []islandCacheBundle
	Entries map[string]*manifestIsland
	CSS     []string
}

type islandCacheBundle struct {
	Name string
	JS   []byte
}

// islandInputs is the key of a build of islands in frontendRoot.
func islandInputs(frontendRoot string, islands []*islandComp, dev bool) string {
	h := sha256.New()
	fmt.Fprintf(h, "vuka %s\ndev %v\n", compilerVersion(), dev)
	names := make([]string, 0, len(liveModules))
	for k := range liveModules {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		fmt.Fprintf(h, "live %s %s\n", k, liveModules[k])
	}
	fmt.Fprintf(h, "link %s\n", vukaLinkSvelte)
	seenFw := map[string]bool{}
	for _, ic := range islands {
		fmt.Fprintf(h, "island %s %s %s\n", ic.name, ic.src, ic.framework)
		if !seenFw[ic.framework] {
			seenFw[ic.framework] = true
			imp, call := findSetup(frontendRoot, ic.framework)
			fmt.Fprintf(h, "adapter %s\n%s\n", ic.framework, adapterSrc(ic.framework, imp, call))
		}
		if !strings.HasPrefix(ic.src, frontendRoot+string(filepath.Separator)) {
			hashTree(h, filepath.Dir(ic.src)) // a component outside the root, and what is beside it
		}
	}
	hashTree(h, frontendRoot)
	// what the installed packages are
	for _, f := range []string{"node_modules/.package-lock.json", "node_modules/.modules.yaml", "node_modules/.yarn-state.yml", "node_modules/.yarn-integrity"} {
		hashFile(h, filepath.Join(frontendRoot, filepath.FromSlash(f)))
	}
	// @source directories outside the root
	for _, dir := range cssSources(frontendRoot) {
		hashTree(h, dir)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// hashTree hashes the files under root (or root, a file), leaving out
// installed packages, build output and hidden directories.
func hashTree(h io.Writer, root string) {
	load.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			fmt.Fprintf(h, "missing %s\n", p)
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); p != root && (n == "node_modules" || n == "dist" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") && p != root {
			return nil
		}
		hashFile(h, p)
		return nil
	})
}

func hashFile(h io.Writer, p string) {
	data, err := os.ReadFile(p)
	if err != nil {
		fmt.Fprintf(h, "no %s\n", p)
		return
	}
	fmt.Fprintf(h, "file %s %d\n", p, len(data))
	h.Write(data)
}

var cssSourceRe = regexp.MustCompile(`@source\s+(?:not\s+)?["']([^"']+)["']`)

// cssSources are the paths the frontend root's stylesheets name with
// @source, outside the root.
func cssSources(frontendRoot string) []string {
	var out []string
	load.WalkDir(frontendRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if n := d.Name(); p != frontendRoot && (n == "node_modules" || n == "dist" || strings.HasPrefix(n, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".css") {
			return nil
		}
		data, _ := os.ReadFile(p)
		for _, m := range cssSourceRe.FindAllSubmatch(data, -1) {
			src := filepath.Clean(filepath.Join(filepath.Dir(p), filepath.FromSlash(string(m[1]))))
			if i := strings.IndexAny(src, "*{"); i >= 0 {
				src = filepath.Dir(src[:i])
			}
			if !strings.HasPrefix(src, frontendRoot+string(filepath.Separator)) {
				out = append(out, src)
			}
		}
		return nil
	})
	sort.Strings(out)
	return out
}

// cachedViteIslands is buildViteIslands, skipped when its inputs are as the
// last build of the root saw them.
func cachedViteIslands(root, frontendRoot string, islands []*islandComp, dev bool, warn io.Writer) ([]*islandBundle, map[string]*manifestIsland, []string, error) {
	file := islandCacheFile(frontendRoot)
	if file == "" {
		return buildViteIslands(root, frontendRoot, islands, dev, warn)
	}
	key := islandInputs(frontendRoot, islands, dev)
	if data, err := os.ReadFile(file); err == nil {
		var c islandCache
		if gob.NewDecoder(bytes.NewReader(data)).Decode(&c) == nil && c.Key == key {
			bundles := make([]*islandBundle, len(c.Bundles))
			for i, b := range c.Bundles {
				bundles[i] = &islandBundle{name: b.Name, js: b.JS}
			}
			return bundles, c.Entries, c.CSS, nil
		}
	}
	bundles, entries, css, err := buildViteIslands(root, frontendRoot, islands, dev, warn)
	if err != nil {
		return nil, nil, nil, err
	}
	// Keyed by what it saw before: a file changed while it built (an edit, an
	// install) is built again next time.
	c := islandCache{Key: key, Entries: entries, CSS: css}
	for _, b := range bundles {
		c.Bundles = append(c.Bundles, islandCacheBundle{b.name, b.js})
	}
	var buf bytes.Buffer
	if gob.NewEncoder(&buf).Encode(c) == nil {
		os.MkdirAll(filepath.Dir(file), 0o755)
		writeIfChanged(file, buf.Bytes())
	}
	return bundles, entries, css, nil
}
