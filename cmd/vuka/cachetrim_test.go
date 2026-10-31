package main

import (
	"archive/zip"
	"errors"
	"fmt"
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
