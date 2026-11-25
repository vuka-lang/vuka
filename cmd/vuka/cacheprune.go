package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/buildlock"
)

// Every edit a watch build compiles leaves an archive of the edited package
// and of each importer in Go's build cache, and nothing reads them again:
// go trims entries only after days unused. After a good build the loop
// removes the archives of the module's own packages that the build before
// replaced, so a session keeps about one copy of the app. It removes only
// files it recorded itself, only inside the cache, and only while no build
// of the module runs (buildlock.go); what it can't remove then waits for the
// next good build. Dependencies are never touched.

const keepCacheEnv = "VUKA_KEEP_CACHE" // set (not 0, off or false): watch builds leave the cache alone

// cutKeepCache reports whether flags ask for -keep-cache, and returns them
// without it.
func cutKeepCache(flags []string) ([]string, bool) {
	keep := false
	out := flags[:0:0]
	for _, f := range flags {
		switch strings.TrimLeft(f, "-") {
		case "keep-cache", "keep-cache=true":
			keep = true
		case "keep-cache=false":
		default:
			out = append(out, f)
		}
	}
	if v := strings.ToLower(os.Getenv(keepCacheEnv)); v != "" && v != "0" && v != "off" && v != "false" {
		keep = true
	}
	return out, keep
}

type cachePruner struct {
	mu      sync.Mutex
	root    string
	dir     string // go env GOCACHE; "" until asked, "-" for none
	prev    map[string]string
	pending map[string]bool
}

// goCache is the build cache directory the session's builds use.
func (s *supervisor) goCache() string {
	p := s.pruner
	if p.dir == "" {
		p.dir = "-"
		c := exec.Command("go", "env", "GOCACHE")
		c.Dir, c.Env = s.dir, s.env
		if out, err := c.Output(); err == nil {
			if d := strings.TrimSpace(string(out)); filepath.IsAbs(d) {
				p.dir = filepath.Clean(d)
			}
		}
	}
	return p.dir
}

// pruneBuild runs after a good build, beside the swap to it, which it
// doesn't delay: go list (some hundreds of ms, from the cache) names the
// build's archives, which the next change preloads, and the module's that
// it replaced go.
func (s *supervisor) pruneBuild(r *buildResult) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute) // the build's own is cancelled once the loop has its result
	defer cancel()
	release := func() {}
	if s.pruner != nil {
		release = buildlock.Hold(s.root)
	}
	exports := s.listExports(ctx, r)
	release()
	close(r.listed)
	s.preload.set(exports)
	s.prune(exports)
	close(r.pruned)
}

// listExports asks go list, which finds the build just made in the cache,
// for the archive of each package the build linked. Nil when it can't say.
func (s *supervisor) listExports(ctx context.Context, r *buildResult) map[string]string {
	t0 := time.Now()
	args := []string{"list", "-e", "-export", "-deps", "-f", `{{if .Export}}{{.ImportPath}} {{.Export}}{{end}}`}
	args = append(args, s.flags...)
	if r.overlay != "" {
		args = append(args, "-overlay="+r.overlay)
	}
	c := exec.CommandContext(ctx, "go", append(args, s.pkg...)...)
	c.Dir, c.Env = s.dir, s.env
	out, err := c.Output()
	if err != nil {
		return nil
	}
	exports := map[string]string{}
	for _, line := range bytes.Split(out, []byte("\n")) {
		path, file, ok := strings.Cut(string(line), " ")
		if ok && file != "" {
			exports[path] = file
		}
	}
	traceStep("export list %v (%d packages)", time.Since(t0), len(exports))
	return exports
}

// prune removes the module's archives exports replaces, or an earlier call
// could not.
func (s *supervisor) prune(all map[string]string) {
	p := s.pruner
	if p == nil || len(all) == 0 {
		return
	}
	exports := map[string]string{}
	for path, file := range all {
		if path == s.modPath || strings.HasPrefix(path, s.modPath+"/") {
			exports[path] = file
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	dir := s.goCache()
	stale := p.pending
	if stale == nil {
		stale = map[string]bool{}
	}
	for pkg, file := range p.prev {
		if exports[pkg] != file {
			stale[file] = true
		}
	}
	for _, file := range exports {
		delete(stale, file)
	}
	p.prev, p.pending = exports, stale
	if len(stale) == 0 || dir == "-" {
		return
	}
	release, ok := buildlock.TryPrune(p.root)
	if !ok {
		traceStep("cache prune deferred: a build holds the lock")
		return
	}
	defer release()
	n := 0
	for file := range stale {
		if inCache(dir, file) && os.Remove(file) == nil {
			n++
		}
	}
	p.pending = nil
	traceStep("cache prune removed %d of %d", n, len(stale))
}

// inCache reports whether file is a build output inside the cache dir.
func inCache(dir, file string) bool {
	rel, err := filepath.Rel(dir, file)
	return err == nil && filepath.IsLocal(rel) && strings.HasSuffix(file, "-d")
}
