package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// The files recorded after a build are what its link read: each packagefile
// of the linker's importcfg that lives in the cache, and the toolchain's
// compiler and linker.
func TestPreloadRecordsLinkInputs(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	for _, prune := range []bool{false, true} {
		cache := t.TempDir()
		m := newPruneModule(t, cache, prune)
		m.s.preload = &preloader{dir: m.root, env: m.s.env}
		if r := m.build(); r.err != nil {
			t.Fatalf("build: %v\n%s", r.err, r.out)
		}
		files := m.s.preload.files
		if len(files) < 3 || filepath.Base(files[0]) != exe("compile") || filepath.Base(files[1]) != exe("link") {
			t.Fatalf("prune=%v: the tools don't lead: %v", prune, files[:min(3, len(files))])
		}
		for _, f := range files[:2] {
			if _, err := os.Stat(f); err != nil {
				t.Error(err)
			}
		}

		c := exec.Command("go", "build", "-work", "-o", filepath.Join(t.TempDir(), "app"), "-buildvcs=false", ".")
		c.Dir, c.Env = m.root, m.s.env
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("go build -work: %v\n%s", err, out)
		}
		work := strings.TrimPrefix(strings.TrimSpace(string(out)), "WORK=")
		t.Cleanup(func() { os.RemoveAll(work) })
		cfg, err := os.ReadFile(filepath.Join(work, "b001", "importcfg.link"))
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for _, line := range strings.Split(string(cfg), "\n") {
			_, file, ok := strings.Cut(strings.TrimPrefix(line, "packagefile "), "=")
			if !ok || !strings.HasPrefix(line, "packagefile ") || strings.HasPrefix(file, work) {
				continue
			}
			n++
			if !slices.Contains(files, file) {
				t.Errorf("prune=%v: the link reads %s, which isn't recorded", prune, line)
			}
		}
		if n < 10 {
			t.Errorf("prune=%v: only %d cached link inputs", prune, n)
		}
	}
}

func TestWarmFiles(t *testing.T) {
	dir := t.TempDir()
	var files []string
	for i, size := range []int{0, 10, 3 << 20} {
		f := filepath.Join(dir, string(rune('a'+i)))
		if err := os.WriteFile(f, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	files = append(files, filepath.Join(dir, "missing"))
	if n := warmFiles(context.Background(), files, 2); n != 10+3<<20 {
		t.Errorf("read %d bytes", n)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if n := warmFiles(ctx, files, 2); n != 0 {
		t.Errorf("a cancelled read read %d bytes", n)
	}
	if n := warmFiles(context.Background(), nil, 2); n != 0 {
		t.Errorf("no files: %d bytes", n)
	}
}

func TestPreloadStartStop(t *testing.T) {
	var none *preloader
	none.set(map[string]string{"a": "b"})
	none.start()
	none.stop()

	p := &preloader{tools: []string{}}
	p.start()
	if p.cancel != nil {
		t.Error("started with nothing to read")
	}
	f := filepath.Join(t.TempDir(), "a")
	os.WriteFile(f, []byte("x"), 0o644)
	p.set(map[string]string{"x": f})
	if !slices.Equal(p.files, []string{f}) {
		t.Errorf("files %v", p.files)
	}
	p.start()
	p.start()
	p.stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		t.Error("stop left the read running")
	}
}
