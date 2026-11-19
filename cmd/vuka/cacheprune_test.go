package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pruneModule struct {
	t     *testing.T
	root  string
	cache string
	s     *supervisor
	n     int
}

func newPruneModule(t *testing.T, cache string, prune bool) *pruneModule {
	t.Helper()
	root := tempModule(t, map[string]string{
		"go.mod":      "module example.com/p\n\ngo 1.25.0\n",
		"main.go":     "package main\n\nimport \"example.com/p/lib\"\n\nfunc main() { println(lib.V) }\n",
		"lib/lib.go":  "package lib\n\nconst V = 0\n",
		"other/o.go":  "package other\n\nconst X = 0\n",
		"other/p.go":  "package other\n\nconst Y = 0\n",
		"unused/u.go": "package unused\n\nconst Z = 0\n",
	})
	t.Chdir(root)
	env := append(os.Environ(), "GOCACHE="+cache, "GOFLAGS=", "GOWORK=off", "PWD="+root)
	m := &pruneModule{t: t, root: root, cache: cache}
	m.s = &supervisor{root: root, dir: root, tmp: t.TempDir(), pkg: []string{"."}, flags: []string{"-buildvcs=false"}, env: env, stderr: new(bytes.Buffer), modPath: "example.com/p"}
	if prune {
		m.s.pruner = &cachePruner{root: root}
	}
	return m
}

func (m *pruneModule) write(rel, src string) {
	m.t.Helper()
	if err := os.WriteFile(filepath.Join(m.root, rel), []byte(src), 0o644); err != nil {
		m.t.Fatal(err)
	}
}

func (m *pruneModule) edit(v int) {
	m.write("lib/lib.go", fmt.Sprintf("package lib\n\nconst V = %d\n", v))
}

func (m *pruneModule) build() *buildResult {
	m.t.Helper()
	m.n++
	r := m.s.build(context.Background(), m.n, &app{})
	if r.listed != nil {
		<-r.pruned
	}
	return r
}

// isDirIndex: Go also caches an index of a source directory under -d, a few
// hundred bytes, which isn't a build's archive.
func isDirIndex(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 8)
	f.Read(head)
	return string(head) == "go index"
}

func archiveSet(cache string) map[string]bool {
	set := map[string]bool{}
	filepath.WalkDir(cache, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, "-d") && !isDirIndex(p) {
			set[p] = true
		}
		return nil
	})
	return set
}

func archives(t *testing.T, cache string) int {
	t.Helper()
	return len(archiveSet(cache))
}

func TestCachePrune(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	cache := t.TempDir()
	const edits = 8

	var grown, pruned int
	t.Run("without", func(t *testing.T) {
		m := newPruneModule(t, cache, false)
		m.build()
		before := archives(t, cache)
		for i := 1; i <= edits; i++ {
			m.edit(i)
			if r := m.build(); r.err != nil {
				t.Fatalf("build %d: %v\n%s", i, r.err, r.out)
			}
		}
		grown = archives(t, cache) - before
		if grown < 2*edits {
			t.Errorf("without pruning the cache grew by %d archives in %d edits, want at least %d", grown, edits, 2*edits)
		}
	})

	t.Run("with", func(t *testing.T) {
		m := newPruneModule(t, cache, true)
		first := m.build()
		if first.err != nil || len(m.s.pruner.prev) != 2 {
			t.Fatalf("first build: %v, exports %v", first.err, m.s.pruner.prev)
		}
		var deps []string
		lc := exec.Command("go", "list", "-export", "-deps", "-f", "{{if .Standard}}{{.Export}}{{end}}", ".")
		lc.Dir, lc.Env = m.root, m.s.env
		out, _ := lc.Output()
		for _, f := range strings.Fields(string(out)) {
			deps = append(deps, f)
		}
		before := archives(t, cache)
		for i := 1; i <= edits; i++ {
			m.edit(i + 100)
			if r := m.build(); r.err != nil {
				t.Fatalf("build %d: %v\n%s", i, r.err, r.out)
			}
		}
		pruned = archives(t, cache) - before
		for _, d := range deps {
			if _, err := os.Stat(d); err != nil {
				t.Errorf("a dependency's archive is gone: %v", err)
			}
		}
		t.Logf("%d edits: +%d archives without pruning, %+d with", edits, grown, pruned)
	})

	t.Run("failed build", func(t *testing.T) {
		m := newPruneModule(t, cache, true)
		m.build()
		m.edit(500)
		m.build()
		before := archives(t, cache)
		m.write("lib/lib.go", "package lib\n\nconst V = \n")
		if r := m.build(); r.err == nil {
			t.Fatal("the broken build succeeded")
		}
		if got := archives(t, cache); got != before {
			t.Errorf("a failed build changed the cache: %d archives, was %d", got, before)
		}
		if len(m.s.pruner.pending) != 0 {
			t.Errorf("a failed build left pending removals: %v", m.s.pruner.pending)
		}
	})

	t.Run("lock", func(t *testing.T) {
		m := newPruneModule(t, cache, true)
		m.build()
		m.edit(600)
		m.build()
		held := holdBuild(m.root)
		before := archives(t, cache)
		m.edit(601)
		m.build()
		if got := archives(t, cache); got <= before {
			t.Errorf("a held build lock didn't defer the removals: %d archives, was %d", got, before)
		}
		if len(m.s.pruner.pending) == 0 {
			t.Error("nothing is pending behind the held lock")
		}
		held()
		m.edit(602)
		m.build()
		if len(m.s.pruner.pending) != 0 {
			t.Errorf("still pending once the lock is free: %v", m.s.pruner.pending)
		}
		if got := archives(t, cache); got != before {
			t.Errorf("the deferred removals didn't catch up: %d archives, was %d", got, before)
		}
	})
}

func TestCachePruneGuards(t *testing.T) {
	if !inCache("/c", "/c/ab/abcd-d") || inCache("/c", "/c/ab/abcd-a") || inCache("/c", "/x/abcd-d") || inCache("/c", "/c/../x/abcd-d") {
		t.Error("inCache accepts or refuses the wrong files")
	}
	t.Setenv(keepCacheEnv, "")
	for in, want := range map[string]bool{"": false, "0": false, "off": false, "1": true} {
		t.Setenv(keepCacheEnv, in)
		if _, got := cutKeepCache(nil); got != want {
			t.Errorf("%s=%q: %v", keepCacheEnv, in, got)
		}
	}
	t.Setenv(keepCacheEnv, "")
	if rest, keep := cutKeepCache([]string{"-race", "-keep-cache"}); !keep || len(rest) != 1 {
		t.Errorf("flag: %v %v", rest, keep)
	}
}

func TestRunWatchPrunesCache(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOFLAGS", "")
	vuka := buildVuka(t)
	for _, keep := range []bool{false, true} {
		cache := t.TempDir()
		env := []string{"GOCACHE=" + cache}
		if keep {
			env = append(env, keepCacheEnv+"=1")
		}
		root := tempModule(t, map[string]string{"go.mod": "module example.com/w\n\ngo 1.25.0\n", "main.vuka": watchApp})
		r := startWatch(t, vuka, root, env)
		r.waitVersion("v1")
		time.Sleep(time.Second) // the first build's pruning
		before := archives(t, cache)
		for i := 2; i <= 6; i++ {
			r.edit(fmt.Sprintf(`"v%d"`, i-1), fmt.Sprintf(`"v%d"`, i))
			r.waitVersion(fmt.Sprintf("v%d", i))
			time.Sleep(time.Second)
		}
		got := archives(t, cache) - before
		t.Logf("keep=%v: %+d archives over 5 edits", keep, got)
		if keep && got < 5 || !keep && got != 0 {
			t.Errorf("keep=%v: the cache changed by %d archives over 5 edits", keep, got)
		}
	}
}
