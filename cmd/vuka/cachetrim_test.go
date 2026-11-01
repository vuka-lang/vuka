package main

import (
	"archive/zip"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// ormProxy is a file GOPROXY serving a small orm tool at each version, and
// the environment for the go command to use it alone. It prints version.
func ormProxy(t *testing.T, versions ...string) {
	t.Helper()
	proxy := t.TempDir()
	at := filepath.Join(proxy, filepath.FromSlash(ormModule), "@v")
	if err := os.MkdirAll(at, 0o755); err != nil {
		t.Fatal(err)
	}
	mod := "module " + ormModule + "\n\ngo 1.21\n"
	for _, v := range versions {
		writeFile(t, filepath.Join(at, v+".info"), `{"Version":"`+v+`"}`)
		writeFile(t, filepath.Join(at, v+".mod"), mod)
		f, err := os.Create(filepath.Join(at, v+".zip"))
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		for name, src := range map[string]string{
			"go.mod":          mod,
			"cmd/orm/main.go": fmt.Sprintf("package main\n\nfunc main() { println(%q) }\n", v),
		} {
			w, err := z.Create(ormModule + "@" + v + "/" + name)
			if err != nil {
				t.Fatal(err)
			}
			w.Write([]byte(src))
		}
		z.Close()
		f.Close()
	}
	t.Setenv("GOPROXY", "file://"+proxy)
	t.Setenv("GOSUMDB", "off")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "-mod=mod -modcacherw")
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("VUKA_CACHE", t.TempDir())
}

func ormApp(t *testing.T, version, extra string) (string, []byte) {
	t.Helper()
	root := t.TempDir()
	mod := "module example.com/app\n\ngo 1.21\n\nrequire " + ormModule + " " + version + "\n" + extra
	writeFile(t, filepath.Join(root, "go.mod"), mod)
	return root, []byte(mod)
}

func ormBinaries(t *testing.T) []string {
	t.Helper()
	bins, err := filepath.Glob(filepath.Join(os.Getenv("VUKA_CACHE"), "orm", "*", "orm"+exeSuffix()))
	if err != nil {
		t.Fatal(err)
	}
	return bins
}

// TestOrmToolShared: two projects on one orm version share one binary, and
// the second builds nothing; an edit to go.mod that leaves the versions alone
// shares it too; another version, and a local copy's edits, get their own.
func TestOrmToolShared(t *testing.T) {
	ormProxy(t, "v2.99.0", "v2.99.1")

	first, firstMod := ormApp(t, "v2.99.0", "")
	bin, args := ormCommand(first, firstMod)
	if bin == "go" {
		t.Fatalf("no binary: go %v", args)
	}
	built, err := os.Stat(bin)
	if err != nil {
		t.Fatal(err)
	}

	second, secondMod := ormApp(t, "v2.99.0", "// another project\n")
	again, _ := ormCommand(second, secondMod)
	if again != bin {
		t.Errorf("another root built %s, want %s", again, bin)
	}
	if fi, err := os.Stat(again); err != nil || !os.SameFile(built, fi) || !fi.ModTime().Equal(built.ModTime()) {
		t.Errorf("the binary was built again: %v", err)
	}
	if n := len(ormBinaries(t)); n != 1 {
		t.Errorf("%d binaries after two projects, want 1", n)
	}

	other, otherMod := ormApp(t, "v2.99.1", "")
	if b, _ := ormCommand(other, otherMod); b == bin || b == "go" {
		t.Errorf("another version ran %s", b)
	}

	local := filepath.Join(t.TempDir(), "orm")
	writeFile(t, filepath.Join(local, "go.mod"), "module "+ormModule+"\n\ngo 1.21\n")
	main := filepath.Join(local, "cmd", "orm", "main.go")
	writeFile(t, main, "package main\n\nfunc main() { println(\"one\") }\n")
	replaced, replacedMod := ormApp(t, "v2.99.0", "replace "+ormModule+" => "+local+"\n")
	k1, err := ormToolKey(replaced)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, main, "package main\n\nfunc main() { println(\"two\") }\n")
	k2, err := ormToolKey(replaced)
	if err != nil {
		t.Fatal(err)
	}
	if k1 == k2 {
		t.Error("an edit to a replaced orm kept the key")
	}
	b, _ := ormCommand(replaced, replacedMod)
	if b == "go" {
		t.Fatal("no binary for the replaced orm")
	}
	if out, _ := exec.Command(b).CombinedOutput(); !strings.Contains(string(out), "two") {
		t.Errorf("replaced orm's binary printed %q", out)
	}
}

// TestOrmToolKeyIgnoresParallelism: -p changes no byte of the binary.
func TestOrmToolKeyIgnoresParallelism(t *testing.T) {
	ormProxy(t, "v2.99.0")
	root, _ := ormApp(t, "v2.99.0", "")
	k1, err := ormToolKey(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GOFLAGS", os.Getenv("GOFLAGS")+" -p=2")
	k2, err := ormToolKey(root)
	if err != nil {
		t.Fatal(err)
	}
	if k1 != k2 {
		t.Error("-p changed the key")
	}
}

// TestCachedOrmToolInterrupted: a build that dies part way leaves nothing a
// later vuka would run, and that vuka builds it whole.
func TestCachedOrmToolInterrupted(t *testing.T) {
	base := t.TempDir()
	_, err := cachedOrmTool(base, "k", func(out string) error {
		os.WriteFile(out, []byte("half a bin"), 0o755)
		return errors.New("killed")
	})
	if err == nil {
		t.Fatal("a failed build returned a binary")
	}
	if _, err := os.Stat(filepath.Join(base, "k", "orm"+exeSuffix())); err == nil {
		t.Fatal("a failed build left a binary in place")
	}
	if des, _ := os.ReadDir(base); len(des) != 0 {
		t.Errorf("left behind: %v", des)
	}
	bin, err := cachedOrmTool(base, "k", func(out string) error { return os.WriteFile(out, []byte("whole"), 0o755) })
	if err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(bin); string(data) != "whole" {
		t.Errorf("binary is %q", data)
	}
}

// TestCachedOrmToolConcurrent: while vukas build at once, a reader sees the
// whole binary or none.
func TestCachedOrmToolConcurrent(t *testing.T) {
	base := t.TempDir()
	whole := strings.Repeat("x", 1<<20)
	final := filepath.Join(base, "k", "orm"+exeSuffix())
	done := make(chan struct{})
	var torn atomic.Int32
	go func() {
		for {
			select {
			case <-done:
				return
			default:
			}
			if data, err := os.ReadFile(final); err == nil && string(data) != whole {
				torn.Add(1)
			}
		}
	}()
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := cachedOrmTool(base, "k", func(out string) error {
				f, err := os.Create(out)
				if err != nil {
					return err
				}
				defer f.Close()
				f.WriteString(whole[:1<<19])
				time.Sleep(10 * time.Millisecond)
				_, err = f.WriteString(whole[1<<19:])
				return err
			})
			if err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	close(done)
	if torn.Load() != 0 {
		t.Errorf("read a partial binary %d times", torn.Load())
	}
	des, _ := os.ReadDir(base)
	if len(des) != 1 {
		t.Errorf("left behind: %v", des)
	}
}

func cacheFile(t *testing.T, path string, size int, age time.Duration, now time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
	at := now.Add(-age)
	os.Chtimes(path, at, at)
	if dir := filepath.Dir(path); filepath.Base(filepath.Dir(dir)) != "vuka-cache" {
		os.Chtimes(dir, at, at)
	}
}

func cacheNames(t *testing.T, dir string) []string {
	t.Helper()
	des, err := os.ReadDir(dir)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var out []string
	for _, de := range des {
		out = append(out, de.Name())
	}
	return out
}

// TestTrimCaches: what nothing has used for two weeks goes; a limit takes
// the entries used longest ago; nothing used in the last hour goes, nor
// vukavet's current version; what was left mid-write goes after a day.
func TestTrimCaches(t *testing.T) {
	base := filepath.Join(t.TempDir(), "vuka-cache")
	t.Setenv("VUKA_CACHE", base)
	now := time.Now()
	day := 24 * time.Hour

	cacheFile(t, filepath.Join(base, "orm", "stale", "orm"), 1, 15*day, now)
	for i := range 10 {
		cacheFile(t, filepath.Join(base, "orm", fmt.Sprintf("k%d", i), "orm"), 1, time.Duration(i+2)*time.Hour, now)
	}
	cacheFile(t, filepath.Join(base, "orm", cacheTmpPrefix+"old", "orm"), 1, 2*day, now)
	cacheFile(t, filepath.Join(base, "orm", cacheTmpPrefix+"building", "orm"), 1, time.Minute, now)
	cacheFile(t, filepath.Join(base, "exports", "old.json"), 1, 15*day, now)
	cacheFile(t, filepath.Join(base, "exports", "new.json"), 1, day, now)
	cacheFile(t, filepath.Join(base, "exports", "x.json.tmp"), 1, 2*day, now)
	cacheFile(t, filepath.Join(base, "islands", "old.gob"), 1, 15*day, now)
	cacheFile(t, filepath.Join(base, "vukavet", version, "vukavet"), 1, 100*day, now)
	cacheFile(t, filepath.Join(base, "vukavet", "v0.0.1", "vukavet"), 1, 15*day, now)
	cacheFile(t, filepath.Join(base, "run", "module", "x"), 1, 100*day, now) // run trims itself

	trimCaches(now)

	got := cacheNames(t, filepath.Join(base, "orm"))
	want := []string{cacheTmpPrefix + "building", "k0", "k1", "k2", "k3", "k4", "k5", "k6", "k7"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("orm: %v, want %v", got, want)
	}
	if got := cacheNames(t, filepath.Join(base, "exports")); len(got) != 1 || got[0] != "new.json" {
		t.Errorf("exports: %v", got)
	}
	if got := cacheNames(t, filepath.Join(base, "islands")); len(got) != 0 {
		t.Errorf("islands: %v", got)
	}
	if got := cacheNames(t, filepath.Join(base, "vukavet")); len(got) != 1 || got[0] != version {
		t.Errorf("vukavet: %v", got)
	}
	if got := cacheNames(t, filepath.Join(base, "run")); len(got) != 1 {
		t.Errorf("run: %v", got)
	}
}

// TestTrimCachesOnceADay: a second trim the same day does nothing, one the
// next day trims; a cache that's off or absent is left alone.
func TestTrimCachesOnceADay(t *testing.T) {
	base := filepath.Join(t.TempDir(), "vuka-cache")
	t.Setenv("VUKA_CACHE", base)
	now := time.Now()
	trimCaches(now)
	if _, err := os.Stat(base); err == nil {
		t.Error("a trim made the cache directory")
	}

	old := filepath.Join(base, "exports", "old.json")
	cacheFile(t, filepath.Join(base, "exports", "new.json"), 1, 0, now)
	trimCaches(now)
	cacheFile(t, old, 1, 15*24*time.Hour, now)
	trimCaches(now.Add(time.Hour))
	if _, err := os.Stat(old); err != nil {
		t.Error("a second trim the same day ran")
	}
	trimCaches(now.Add(25 * time.Hour))
	if _, err := os.Stat(old); err == nil {
		t.Error("the next day's trim didn't run")
	}

	t.Setenv("VUKA_CACHE", "off")
	trimCaches(now.Add(100 * time.Hour))
}

// TestTrimCachesSize: over the size limit the entries used longest ago go,
// and a limit never takes one in use.
func TestTrimCachesSize(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	for i, age := range []time.Duration{5 * time.Hour, 4 * time.Hour, 3 * time.Hour, 2 * time.Minute} {
		cacheFile(t, filepath.Join(dir, fmt.Sprintf("k%d", i), "out", "x"), 1000, age, now)
		at := now.Add(-age)
		os.Chtimes(filepath.Join(dir, fmt.Sprintf("k%d", i)), at, at)
	}
	trimCache(dir, cacheLimit{maxBytes: 2500}, now)
	if got := strings.Join(cacheNames(t, dir), " "); got != "k2 k3" {
		t.Errorf("after 2500 bytes: %s", got)
	}
	trimCache(dir, cacheLimit{maxBytes: 100}, now)
	if got := strings.Join(cacheNames(t, dir), " "); got != "k3" {
		t.Errorf("after 100 bytes: %s (the one in use must stay)", got)
	}
}

// goCacheEntry writes an entry of a Go build cache at dir.
func goCacheEntry(t *testing.T, dir, name string, size int, age time.Duration, now time.Time) string {
	t.Helper()
	p := filepath.Join(dir, name[:2], name)
	cacheFile(t, p, size, age, now)
	return p
}

// TestTrimGoCache: over the limit the entries used longest ago go, in any
// subdirectory; what isn't an entry stays, and nothing used in the last hour
// goes, whatever the limit.
func TestTrimGoCache(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	oldest := goCacheEntry(t, dir, "aa11-d", 1000, 9*time.Hour, now)
	older := goCacheEntry(t, dir, "bb11-a", 1000, 8*time.Hour, now)
	kept := goCacheEntry(t, dir, "00ff-d", 1000, 3*time.Hour, now)
	recent := goCacheEntry(t, dir, "ff00-d", 1000, 2*time.Minute, now)
	other := filepath.Join(dir, "aa", "README")
	cacheFile(t, other, 5000, 100*time.Hour, now)
	exe := filepath.Join(dir, "cc", "cc11-d", "a.out")
	cacheFile(t, exe, 1000, 7*time.Hour, now)
	at := now.Add(-7 * time.Hour)
	os.Chtimes(filepath.Dir(exe), at, at)

	exists := func(p string) bool { _, err := os.Stat(p); return err == nil }

	trimGoCache(dir, 2000, now)
	for _, p := range []string{oldest, older, filepath.Dir(exe)} {
		if exists(p) {
			t.Errorf("%s stayed", p)
		}
	}
	for _, p := range []string{kept, recent, other} {
		if !exists(p) {
			t.Errorf("%s went", p)
		}
	}

	trimGoCache(dir, 1, now)
	if exists(kept) {
		t.Error("an entry past the limit stayed")
	}
	if !exists(recent) {
		t.Error("an entry in use went")
	}
}

// TestTrimGoCacheUnderLimit: a cache that fits is left alone.
func TestTrimGoCacheUnderLimit(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	p := goCacheEntry(t, dir, "aa11-d", 1000, 100*time.Hour, now)
	trimGoCache(dir, 1000, now)
	if _, err := os.Stat(p); err != nil {
		t.Error("an entry went from a cache within the limit")
	}
}

func TestGoCacheLimit(t *testing.T) {
	for v, want := range map[string]int64{
		"":      goCacheMax,
		"off":   0,
		"OFF":   0,
		"20GB":  20 << 30,
		"500mb": 500 << 20,
		"1 GB":  1 << 30,
		"12345": 12345,
		"0":     0,
		"lots":  goCacheMax,
		"-1":    goCacheMax,
	} {
		t.Setenv("VUKA_GOCACHE_MAX", v)
		if got := goCacheLimit(); got != want {
			t.Errorf("%q: %d, want %d", v, got, want)
		}
	}
}

// TestTrimGoBuildCache: the trim runs at most every ten minutes, not with
// GOCACHEPROG set and not when it's off.
func TestTrimGoBuildCache(t *testing.T) {
	gocache := t.TempDir()
	t.Setenv("GOCACHE", gocache)
	t.Setenv("VUKA_CACHE", t.TempDir())
	t.Setenv("VUKA_GOCACHE_MAX", "100")
	now := time.Now()
	entry := func(name string) string { return goCacheEntry(t, gocache, name, 1000, 5*time.Hour, now) }
	gone := func(p string) bool { _, err := os.Stat(p); return err != nil }

	t.Setenv("VUKA_GOCACHE_MAX", "off")
	off := entry("aa11-d")
	trimGoBuildCache(now)
	if gone(off) {
		t.Error("the trim ran while off")
	}

	t.Setenv("VUKA_GOCACHE_MAX", "100")
	trimGoBuildCache(now)
	if !gone(off) {
		t.Error("the trim didn't run")
	}

	soon := entry("aa22-d")
	trimGoBuildCache(now.Add(time.Minute))
	if gone(soon) {
		t.Error("a second trim within ten minutes ran")
	}
	trimGoBuildCache(now.Add(11 * time.Minute))
	if !gone(soon) {
		t.Error("the trim after ten minutes didn't run")
	}
}

// TestTrimGoCacheThenBuild: the go command takes a trimmed cache as a miss. A
// real build fills the cache, a trim leaves what was just used, and once
// aged the entries all go and the same build compiles again and succeeds.
func TestTrimGoCacheThenBuild(t *testing.T) {
	gocache := t.TempDir()
	t.Setenv("GOCACHE", gocache)
	t.Setenv("GOFLAGS", "")
	t.Setenv("GOWORK", "off")
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "go.mod"), "module example.com/app\n\ngo 1.21\n")
	writeFile(t, filepath.Join(root, "main.go"), "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println(\"ok\") }\n")
	build := func() {
		t.Helper()
		c := exec.Command("go", "build", "-o", filepath.Join(root, "app"), ".")
		c.Dir = root
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
	}
	build()

	now := time.Now()
	trimGoCache(gocache, 0, now)
	if goCacheEntries(t, gocache) == 0 {
		t.Fatal("a limit of 0 removed entries in use")
	}

	age := now.Add(-2 * time.Hour)
	filepath.WalkDir(gocache, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			os.Chtimes(p, age, age)
		}
		return nil
	})
	trimGoCache(gocache, 0, now)
	if n := goCacheEntries(t, gocache); n != 0 {
		t.Errorf("%d entries stayed past a limit of 0", n)
	}

	os.Remove(filepath.Join(root, "app"))
	build()
	if out, err := exec.Command(filepath.Join(root, "app")).Output(); err != nil || strings.TrimSpace(string(out)) != "ok" {
		t.Errorf("rebuilt binary: %q, %v", out, err)
	}
}

func goCacheEntries(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && (strings.HasSuffix(p, "-a") || strings.HasSuffix(p, "-d")) {
			n++
		}
		return nil
	})
	return n
}
