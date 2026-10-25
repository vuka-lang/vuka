package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

func TestCutFresh(t *testing.T) {
	for _, c := range []struct {
		in    []string
		out   []string
		fresh bool
	}{
		{[]string{"-race"}, []string{"-race"}, false},
		{[]string{"-fresh", "-race"}, []string{"-race"}, true},
		{[]string{"--fresh"}, []string{}, true},
		{[]string{"-fresh=false"}, []string{}, false},
	} {
		out, fresh := cutFresh(c.in)
		if !slices.Equal(out, c.out) || fresh != c.fresh {
			t.Errorf("cutFresh(%q) = %q, %v; want %q, %v", c.in, out, fresh, c.out, c.fresh)
		}
	}
}

func TestParseModFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "go.mod"), `module example.com/a

replace example.com/b => ../b // a comment

replace (
	example.com/c v1.0.0 => ./c
	example.com/d => example.com/e v1.2.0
)
`)
	writeFile(t, filepath.Join(dir, "go.work"), "go 1.25\n\nuse (\n\t.\n\t./sub\n)\n\nuse ../other\n")
	_, reps := parseModFile(filepath.Join(dir, "go.mod"))
	if want := []string{filepath.Join(filepath.Dir(dir), "b"), filepath.Join(dir, "c")}; !slices.Equal(reps, want) {
		t.Errorf("go.mod's local replaces: %q, want %q", reps, want)
	}
	uses, _ := parseModFile(filepath.Join(dir, "go.work"))
	if want := []string{dir, filepath.Join(dir, "sub"), filepath.Join(filepath.Dir(dir), "other")}; !slices.Equal(uses, want) {
		t.Errorf("go.work's uses: %q, want %q", uses, want)
	}
}

// TestReuseKey: the key a kept build is found by changes with each input of
// the build, and only with those.
func TestReuseKey(t *testing.T) {
	t.Setenv("GOWORK", "off")
	t.Setenv("GOFLAGS", "")
	root := tempModule(t, map[string]string{
		"go.mod":            "module example.com/app\n\ngo 1.25.0\n\nreplace example.com/lib => ../lib\n",
		"go.sum":            "",
		"main.go":           "package main\n\nimport (\n\t\"embed\"\n\n\t\"github.com/vuka-lang/web/v2\"\n)\n\n//go:embed dist\nvar dist embed.FS\n\nvar _ = web.Frontend(dist, \"dist\")\n\nfunc main() {}\n",
		"page.vuka":         "package main\n",
		"page.vuka.html":    "<p>hi</p>\n",
		"main_test.go":      "package main\n",
		"README.md":         "notes\n",
		"data/sessions.db":  "rows",
		"static/logo.svg":   "<svg/>",
		"dist/app.js":       "1",
		"testdata/x.go":     "package x\n",
		"node_modules/m.js": "",
	})
	lib := filepath.Join(filepath.Dir(root), "lib")
	writeFile(t, filepath.Join(lib, "go.mod"), "module example.com/lib\n")
	writeFile(t, filepath.Join(lib, "lib.go"), "package lib\n")
	t.Cleanup(func() { os.RemoveAll(lib) })

	defer func(f func(string) (string, error), v string) { moduleGoVersion, load.CompilerVersion = f, v }(moduleGoVersion, load.CompilerVersion)
	goVersion := "go1.25.0"
	moduleGoVersion = func(string) (string, error) { return goVersion, nil }

	s := &supervisor{root: root, dir: root, pkg: []string{"."}, flags: []string{"-ldflags=-w"}, env: []string{"GOWORK=off", "PATH=x"}, stub: true}
	c := &reuseCache{}
	roots := []string{filepath.Join(root, "static"), filepath.Join(root, "dist")}
	key := func() string {
		t.Helper()
		k, _, err := c.key(s, roots)
		if err != nil || k == "" {
			t.Fatalf("key: %q %v", k, err)
		}
		return k
	}
	last := key()
	if key() != last {
		t.Fatal("the key isn't the same twice")
	}
	same := func(what string) {
		t.Helper()
		if k := key(); k != last {
			t.Errorf("%s changed the key", what)
			last = k
		}
	}
	changed := func(what string) {
		t.Helper()
		if k := key(); k == last {
			t.Errorf("%s didn't change the key", what)
		} else {
			last = k
		}
	}
	edit := func(name, src string) { writeFile(t, filepath.Join(root, name), src) }

	edit("main_test.go", "package main\n\n// a test\n")
	edit("README.md", "more notes\n")
	edit("data/sessions.db", "more rows")
	edit("testdata/x.go", "package x // edited\n")
	edit("node_modules/m.js", "edited")
	edit(".hidden.go", "package main\n")
	edit("dist/app.js", "2") // stubbed: only its name counts
	s.env = append(s.env, "PATH=y", "VUKA_DEV_DIR=/tmp/x")
	same("a test, notes, data, testdata, node_modules, a hidden file, a stubbed file, PATH")

	edit("page.vuka", "package main\n\n// edited\n")
	changed("a .vuka file")
	edit("page.vuka.html", "<p>hello</p>\n")
	changed("a .vuka.html file")
	edit("other.templ", "package main\n")
	changed("a new .templ file")
	edit("main.go", strings.Replace(mustRead(t, filepath.Join(root, "main.go")), "func main() {}", "func main() { println() }", 1))
	changed("a .go file")
	edit("asm_amd64.s", "")
	changed("an assembly file")
	edit("islands/Counter.vue", "<script setup>defineProps<{ n: number }>()</script>\n")
	changed("an island component")
	edit("pages/admin/users/edit.vuka.html", "package users\nimport Form \"@/web/islands/Form.vue\"\ntype Edit struct{}\n<main><Form title=\"Edit\" /></main>\n")
	edit("web/islands/Form.vue", "<script setup>defineProps<{ title: string }>()</script>\n")
	changed("a module-root island import")
	edit("web/islands/Form.vue", "<script setup>defineProps<{ title: string; count?: number }>()</script>\n")
	changed("a module-root imported component's contents")
	edit("default.pgo", "profile")
	changed("a PGO profile")
	edit("go.mod", mustRead(t, filepath.Join(root, "go.mod"))+"\n")
	changed("go.mod")
	edit("go.sum", "example.com/x v1.0.0 h1:x\n")
	changed("go.sum")
	edit("static/logo.svg", "<svg></svg>")
	changed("an embedded file")
	edit("static/new.css", "")
	changed("a new embedded file")
	edit("dist/new.js", "")
	changed("a new stubbed file")
	writeFile(t, filepath.Join(lib, "lib.go"), "package lib\n\nvar X = 1\n")
	changed("a replaced module's source")
	s.flags = []string{"-ldflags=-w", "-race"}
	changed("a build flag")
	s.env = append(s.env, "GOFLAGS=-tags=x")
	changed("GOFLAGS")
	s.env = append(s.env, "CGO_ENABLED=0")
	changed("CGO_ENABLED")
	goVersion = "go1.25.1"
	changed("the Go version")
	load.CompilerVersion += " other"
	changed("vuka's version")
	s.stub = false
	changed("building the frontend in")
	roots = roots[:1]
	changed("embedding less")
}

// TestReuseTrim: the cache keeps the builds used last, reuseKeep of them,
// within reuseMaxBytes.
func TestReuseTrim(t *testing.T) {
	c := &reuseCache{dir: t.TempDir()}
	now := time.Now()
	for i := range 5 {
		dir := filepath.Join(c.dir, fmt.Sprint("e", i))
		data, _ := json.Marshal(keptBuild{Key: fmt.Sprint("k", i), Size: 100})
		writeFile(t, filepath.Join(dir, reuseEntry), string(data))
		writeFile(t, filepath.Join(dir, exe("app")), "bin")
		at := now.Add(-time.Duration(i) * time.Minute) // e0 used last
		os.Chtimes(filepath.Join(dir, reuseEntry), at, at)
	}
	stale := filepath.Join(c.dir, ".tmp-1")
	writeFile(t, filepath.Join(stale, "app"), "half")
	os.Chtimes(stale, now.Add(-2*time.Hour), now.Add(-2*time.Hour))
	writing := filepath.Join(c.dir, ".tmp-2")
	writeFile(t, filepath.Join(writing, "app"), "half")
	broken := filepath.Join(c.dir, "half-removed")
	writeFile(t, filepath.Join(broken, "app"), "bin")
	os.Chtimes(broken, now.Add(-2*time.Hour), now.Add(-2*time.Hour))

	names := func() []string {
		var out []string
		for _, e := range c.entries() {
			out = append(out, filepath.Base(e.dir))
		}
		return out
	}
	c.trim()
	if got := names(); !slices.Equal(got, []string{"e0", "e1", "e2"}) {
		t.Errorf("after a trim: %q, want the 3 used last", got)
	}
	if fileExists(broken) {
		t.Error("an entry half removed an hour ago is left")
	}
	if fileExists(stale) || !fileExists(writing) {
		t.Errorf("a stopped write is left (%v), or one in progress removed (%v)", fileExists(stale), !fileExists(writing))
	}
	defer func(n int64) { reuseMaxBytes = n }(reuseMaxBytes)
	reuseMaxBytes = 250
	c.trim()
	if got := names(); !slices.Equal(got, []string{"e0", "e1"}) {
		t.Errorf("within 250 bytes: %q, want e0 and e1", got)
	}
}

// goShim builds a stand-in for the go command, run first on PATH: it logs
// each run to $SHIM_LOG ("build …" when it starts, "done build" when it
// ends), holds a go build while $SHIM_HOLD exists, and links a build with
// main.stamp set to $SHIM_STAMP, which vuka's key never sees: an input it
// misses. It returns the PATH and the environment that use it.
func goShim(t *testing.T) []string {
	t.Helper()
	real, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	src := t.TempDir()
	writeFile(t, filepath.Join(src, "go.mod"), "module shim\n\ngo 1.21\n")
	writeFile(t, filepath.Join(src, "main.go"), `package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

func logf(format string, args ...any) {
	if p := os.Getenv("SHIM_LOG"); p != "" {
		f, _ := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
		fmt.Fprintf(f, format+"\n", args...)
		f.Close()
	}
}

func main() {
	args := os.Args[1:]
	logf("%s", strings.Join(args, " "))
	build := len(args) > 1 && args[0] == "build"
	if build {
		for hold := os.Getenv("SHIM_HOLD"); hold != ""; time.Sleep(20 * time.Millisecond) {
			if _, err := os.Stat(hold); err != nil {
				break
			}
		}
		if st := os.Getenv("SHIM_STAMP"); st != "" {
			pkg := args[len(args)-1]
			args = append(args[:len(args)-1:len(args)-1], "-ldflags=-w -X main.stamp="+st, pkg)
		}
	}
	c := exec.Command(os.Getenv("SHIM_GO"), args...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	if err := c.Start(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	go func() {
		for s := range sig {
			c.Process.Signal(s)
		}
	}()
	err := c.Wait()
	if build {
		logf("done build")
	}
	if e, ok := err.(*exec.ExitError); ok {
		os.Exit(e.ExitCode())
	}
	if err != nil {
		os.Exit(1)
	}
}
`)
	bin := t.TempDir()
	c := exec.Command(real, "build", "-o", exe(filepath.Join(bin, "go")), ".")
	c.Dir = src
	c.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("building the go shim: %v\n%s", err, out)
	}
	return []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "SHIM_GO=" + real}
}

// reuseApp is watchApp answering its version with main.stamp after it.
var reuseApp = strings.Replace(strings.Replace(watchApp, `fmt.Fprintf(w, "%s %d", version, os.Getpid())`,
	`fmt.Fprintf(w, "%s %d", version+stamp, os.Getpid())`, 1), "func main() {", "var stamp string\n\nfunc main() {", 1)

// TestRunWatchReuse: a start with nothing changed runs the build an earlier
// run kept, before any go build has run; the build that runs all the same
// leaves it running. A change builds before the app starts. An input the
// key misses is caught by that build and swapped in.
func TestRunWatchReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	if !strings.Contains(reuseApp, "version+stamp") || !strings.Contains(reuseApp, "var stamp string") {
		t.Fatal("reuseApp isn't watchApp with a stamp")
	}
	t.Setenv("GOWORK", "off")
	vuka := buildVuka(t)
	tmp := t.TempDir()
	cache, goLog, hold := filepath.Join(tmp, "cache"), filepath.Join(tmp, "go.log"), filepath.Join(tmp, "hold")
	env := append(goShim(t), "VUKA_CACHE="+cache, reuseEnv+"=on", traceEnv+"=1", "SHIM_LOG="+goLog, "SHIM_HOLD="+hold)
	root := tempModule(t, map[string]string{"go.mod": "module example.com/w\n\ngo 1.25.0\n", "main.vuka": reuseApp})
	kept := func() int {
		des, _ := filepath.Glob(filepath.Join(cache, "run", "*", "*", reuseEntry))
		return len(des)
	}
	waitKept := func(r *watchRun, n int) {
		t.Helper()
		for end := time.Now().Add(30 * time.Second); kept() < n; time.Sleep(20 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("%d builds kept, want %d; output:\n%s", kept(), n, r.log.String())
			}
		}
	}

	// The first run builds, and keeps the build.
	r := startWatch(t, vuka, root, env)
	r.waitVersion("v1")
	waitKept(r, 1)
	r.stop()

	// Nothing changed: the kept build answers while go build is held.
	writeFile(t, hold, "")
	os.Remove(goLog)
	r = startWatch(t, vuka, root, env)
	pid := r.waitVersion("v1")
	r.waitLog("● reusing the last build (no changes)", 1)
	if log := mustReadOr(goLog); strings.Contains(log, "done build") {
		t.Errorf("a go build ran before the kept build answered:\n%s", log)
	}
	os.Remove(hold)
	r.waitLog("the kept build verified", 1)
	if log := mustReadOr(goLog); strings.Count(log, "done build") != 1 {
		t.Errorf("the go command's runs, want one build:\n%s", log)
	}
	if v, p := r.get(); v != "v1" || p != pid {
		t.Errorf("after the build: %s %d, want the kept build (v1 %d) still answering", v, p, pid)
	}
	r.stop()
	if out := r.log.String(); strings.Contains(out, "warning") || strings.Contains(out, "● rebuilt") {
		t.Errorf("an unchanged start:\n%s", out)
	}

	// A change: the next start builds first.
	r.edit(`"v1"`, `"v2"`)
	r = startWatch(t, vuka, root, env)
	r.waitVersion("v2")
	waitKept(r, 2)
	r.stop()
	if strings.Contains(r.log.String(), "reusing") {
		t.Errorf("a changed start reused a build:\n%s", r.log.String())
	}

	// An input the key misses: the kept build answers, then the build
	// that came out different.
	r = startWatch(t, vuka, root, append(env, "SHIM_STAMP=x"))
	r.waitLog("● reusing the last build (no changes)", 1)
	r.waitVersion("v2x")
	r.waitLog("● warning: the reused build was out of date: one of go build's inputs changed", 1)
	r.stop()

	if starts := mustReadOr(filepath.Join(root, "starts")); starts != "v1\nv1\nv2\nv2\nv2\n" {
		t.Errorf("the app's main ran for %q, want v1 twice and v2 three times", starts)
	}

	// -fresh builds first.
	writeFile(t, hold, "")
	r = startWatch(t, vuka, root, append(env, "SHIM_STAMP=x"), "-fresh")
	time.Sleep(time.Second)
	if v, _ := r.get(); v != "" {
		t.Errorf("with -fresh, %q answered before go build ran", v)
	}
	os.Remove(hold)
	r.waitVersion("v2x")
	r.stop()
	if strings.Contains(r.log.String(), "reusing") {
		t.Errorf("-fresh reused a build:\n%s", r.log.String())
	}
}

func mustReadOr(path string) string {
	b, _ := os.ReadFile(path)
	return string(b)
}
