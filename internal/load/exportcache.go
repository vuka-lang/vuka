package load

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// exportCache remembers where the go command put the export data of packages
// that can't change while you work: the standard library and modules from the
// module cache. The main module's own packages are never cached. It lives in
// memory (for the language server's many rebuilds) and on disk (for separate
// vuka commands), keyed by the go binary, the build environment, and the
// module's go.mod, go.sum and go.work.
type exportCache struct {
	mu    sync.Mutex
	file  string
	m     map[string]string // import path → export data file
	dirty bool
}

var exportCaches sync.Map // key → *exportCache

func cacheFor(root string) *exportCache {
	key := cacheKey(root)
	if c, ok := exportCaches.Load(key); ok {
		return c.(*exportCache)
	}
	c := &exportCache{m: map[string]string{}}
	if dir, err := os.UserCacheDir(); err == nil {
		c.file = filepath.Join(dir, "vuka", "exports", key+".json")
		if data, err := os.ReadFile(c.file); err == nil {
			_ = json.Unmarshal(data, &c.m)
		}
	}
	actual, _ := exportCaches.LoadOrStore(key, c)
	return actual.(*exportCache)
}

func cacheKey(root string) string {
	h := sha256.New()
	if goBin, err := exec.LookPath("go"); err == nil {
		if fi, err := os.Stat(goBin); err == nil {
			fmt.Fprintf(h, "go %s %d %d\n", goBin, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	for _, v := range []string{"GOOS", "GOARCH", "GOFLAGS", "CGO_ENABLED", "GOEXPERIMENT", "GOTOOLCHAIN", "GOWORK", "GOROOT", "GOPATH", "GOMODCACHE"} {
		fmt.Fprintf(h, "%s=%s\n", v, os.Getenv(v))
	}
	for _, f := range []string{"go.mod", "go.sum"} {
		data, _ := os.ReadFile(filepath.Join(root, f))
		h.Write(data)
	}
	if os.Getenv("GOWORK") != "off" {
		for d := root; ; d = filepath.Dir(d) {
			if data, err := os.ReadFile(filepath.Join(d, "go.work")); err == nil {
				h.Write(data)
				break
			}
			if filepath.Dir(d) == d {
				break
			}
		}
	}
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// get returns path's cached export data file, when it still exists (the go
// build cache may have been trimmed).
func (c *exportCache) get(path string) (string, bool) {
	c.mu.Lock()
	file, ok := c.m[path]
	c.mu.Unlock()
	if !ok {
		return "", false
	}
	if _, err := os.Stat(file); err != nil {
		c.mu.Lock()
		delete(c.m, path)
		c.dirty = true
		c.mu.Unlock()
		return "", false
	}
	return file, true
}

func (c *exportCache) put(path, file string) {
	c.mu.Lock()
	if c.m[path] != file {
		c.m[path], c.dirty = file, true
	}
	c.mu.Unlock()
}

// save writes the cache to disk when it changed.
func (c *exportCache) save() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.dirty || c.file == "" {
		return
	}
	data, err := json.Marshal(c.m)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(c.file), 0o755); err != nil {
		return
	}
	tmp := c.file + ".tmp"
	if os.WriteFile(tmp, data, 0o644) == nil && os.Rename(tmp, c.file) == nil {
		c.dirty = false
	}
}
