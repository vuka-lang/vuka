package load

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/vuka-lang/vuka/v2/toolchain/buildlock"
)

// dirCache remembers the directories of packages outside the main module,
// keyed as exportCache is: the go command, the build environment and the
// module's go.mod, go.sum and go.work, which decide where each package is.
type dirCache struct {
	mu   sync.Mutex
	file string
	m    map[string]string // import path → directory; "" when it has none
}

var dirCaches sync.Map // key → *dirCache

func dirCacheFor(root string) *dirCache {
	key := cacheKey(root)
	if c, ok := dirCaches.Load(key); ok {
		return c.(*dirCache)
	}
	c := &dirCache{m: map[string]string{}}
	if dir, ok := CacheDir(); ok {
		c.file = filepath.Join(dir, "dirs", key+".json")
		if data, err := os.ReadFile(c.file); err == nil {
			_ = json.Unmarshal(data, &c.m)
		}
	}
	actual, _ := dirCaches.LoadOrStore(key, c)
	return actual.(*dirCache)
}

// PackageDirs are the directories of paths, packages outside the module at
// root, with one go command for those no earlier run asked about. A path
// the go command finds no directory for maps to "".
func PackageDirs(root string, paths []string) map[string]string {
	c := dirCacheFor(root)
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make(map[string]string, len(paths))
	var missing []string
	for _, p := range paths {
		if d, ok := c.m[p]; ok && (d == "" || isDir(d)) {
			out[p] = d
			continue
		}
		missing = append(missing, p)
	}
	if len(missing) == 0 {
		return out
	}
	slices.Sort(missing)
	missing = slices.Compact(missing)
	cmd := exec.Command("go", append([]string{"list", "-e", "-f", "{{.ImportPath}}\t{{.Dir}}"}, missing...)...)
	cmd.Dir = root
	release := buildlock.Hold(root)
	got, err := cmd.Output()
	release()
	if err != nil && len(got) == 0 {
		return out
	}
	for sc := bufio.NewScanner(bytes.NewReader(got)); sc.Scan(); {
		path, dir, ok := strings.Cut(sc.Text(), "\t")
		if ok && slices.Contains(missing, path) {
			c.m[path], out[path] = dir, dir
		}
	}
	for _, p := range missing {
		if _, ok := out[p]; !ok {
			c.m[p], out[p] = "", ""
		}
	}
	if c.file != "" {
		if data, err := json.Marshal(c.m); err == nil {
			writeEntry(c.file, data)
		}
	}
	return out
}
