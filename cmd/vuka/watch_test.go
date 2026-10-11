package main

import (
	"bufio"
	"context"
	"debug/elf"
	"debug/macho"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestCutWatch(t *testing.T) {
	for _, c := range []struct {
		args, want []string
		watch      bool
	}{
		{[]string{"--watch", "."}, []string{"."}, true},
		{[]string{"-tags", "x", "-watch", ".", "-v"}, []string{"-tags", "x", ".", "-v"}, true},
		{[]string{".", "--watch"}, []string{".", "--watch"}, false}, // the program's
		{[]string{"-tags", "-watch", "."}, []string{"-tags", "-watch", "."}, false},
		{[]string{"--", "--watch"}, []string{"--", "--watch"}, false},
	} {
		got, watch := cutWatch(c.args)
		if watch != c.watch || !slices.Equal(got, c.want) {
			t.Errorf("cutWatch(%q) = %q %v, want %q %v", c.args, got, watch, c.want, c.watch)
		}
	}
	for _, c := range []struct{ args, flags, pkg, prog []string }{
		{nil, nil, []string{"."}, nil},
		{[]string{"-race", "-ldflags", "-w", "./cmd/app", "-port", "1"}, []string{"-race", "-ldflags", "-w"}, []string{"./cmd/app"}, []string{"-port", "1"}},
		{[]string{"a.go", "b.go", "x"}, nil, []string{"a.go", "b.go"}, []string{"x"}},
	} {
		flags, pkg, prog, err := splitRun(c.args)
		if err != nil || !slices.Equal(flags, c.flags) || !slices.Equal(pkg, c.pkg) || !slices.Equal(prog, c.prog) {
			t.Errorf("splitRun(%q) = %q %q %q %v", c.args, flags, pkg, prog, err)
		}
	}
	if _, _, _, err := splitRun([]string{"-exec", "x", "."}); err == nil {
		t.Error("splitRun took -exec")
	}
}

func TestWatcher(t *testing.T) {
	files := map[string]bool{ // a file of the module: whether a change to it starts a build
		"go.mod":                    true,
		"go.sum":                    true,
		"main.vuka":                 true,
		"main.go":                   true,
		"views/page.templ":          true,
		"views/pet.html":            true, // embedded
		"assets/css/site.css":       true, // under an embedded directory
		"main_test.go":              false,
		"main_test.vuka":            false,
		"README.md":                 false,
		"views/notes.txt":           false,
		".env.go":                   false,
		".git/x.go":                 false,
		".vuka/tailwind/app.css":    false,
		"build/main.go":             false,
		"_old/a.go":                 false,
		"node_modules/x/a.go":       false,
		"testdata/a.go":             false,
		"vendor/x/a.go":             false,
		"tools/go.mod":              false,
		"tools/a.go":                false,
		"cmd/app/main.vuka":         true,
		"cmd/app/testdata/main.go":  false,
		"cmd/app/node_modules/a.go": false,
	}
	m := map[string]string{}
	for f := range files {
		m[f] = "x"
	}
	root := tempModule(t, m)
	w := newWatcher(root)
	w.setEmbeds([]string{filepath.Join(root, "views", "pet.html"), filepath.Join(root, "assets")})
	w.scan()
	if got := w.scan(); got != nil {
		t.Fatalf("an unchanged tree: %v", got)
	}
	later := time.Now().Add(time.Hour)
	for f, want := range files {
		path := filepath.Join(root, f)
		os.Chtimes(path, later, later)
		got := w.scan()
		if want && !slices.Equal(got, []string{path}) || !want && got != nil {
			t.Errorf("a change to %s: %v", f, got)
		}
	}
	os.WriteFile(filepath.Join(root, "main.vuka"), []byte("xy"), 0o644) // a size of its own
	os.Remove(filepath.Join(root, "main.go"))
	writeFile(t, filepath.Join(root, "lib", "new.vuka"), "x")
	got := w.scan()
	slices.Sort(got)
	if want := []string{filepath.Join(root, "lib", "new.vuka"), filepath.Join(root, "main.go"), filepath.Join(root, "main.vuka")}; !slices.Equal(got, want) {
		t.Errorf("an edit, a removal and a new file: %v", got)
	}
	// What a build newly embeds is looked at from then on, not taken for a change.
	w.setEmbeds([]string{filepath.Join(root, "views")})
	if got := w.scan(); got != nil {
		t.Errorf("new embeds: %v", got)
	}
	os.Chtimes(filepath.Join(root, "views", "notes.txt"), later.Add(time.Hour), later.Add(time.Hour))
	if got := w.scan(); len(got) != 1 {
		t.Errorf("a change to a newly embedded file: %v", got)
	}
}

// TestWatcherDebounce: a burst of saves is one change, sent once the
// sources are calm.
func TestWatcherDebounce(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "a.vuka": "", "b.vuka": ""})
	w := newWatcher(root)
	w.scan()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan []string, 10)
	go w.run(ctx, 10*time.Millisecond, 80*time.Millisecond, out)
	start := time.Now()
	for i := range 10 {
		writeFile(t, filepath.Join(root, []string{"a.vuka", "b.vuka"}[i%2]), strings.Repeat("x", i+1))
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case batch := <-out:
		if took := time.Since(start); took < 250*time.Millisecond {
			t.Errorf("the batch came %v after the first save, before the saves stopped", took)
		}
		slices.Sort(batch)
		if batch = slices.Compact(batch); len(batch) != 2 {
			t.Errorf("batch %v", batch)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no batch")
	}
	select {
	case batch := <-out:
		t.Errorf("a second batch: %v", batch)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestEmbedRoots(t *testing.T) {
	root := tempModule(t, map[string]string{
		"go.mod":            "module example.com/e\n\ngo 1.25.0\n",
		"main.go":           "package main\n\nimport \"embed\"\n\n//go:embed views/pet.html all:static static/*/*.css\nvar fs embed.FS\n\nfunc main() {}\n",
		"views/pet.html":    "",
		"static/a.txt":      "",
		"static/css/x.css":  "",
		"lib/lib.go":        "package lib\n\nimport _ \"embed\"\n\n//go:embed *.txt\nvar s string\n",
		"lib/one.txt":       "",
		"lib/lib_test.go":   "package lib\n\nimport _ \"embed\"\n\n//go:embed testdata/x\nvar t string\n",
		"lib/testdata/x":    "",
		"other/go.mod":      "module other\n",
		"other/embedded.go": "package other\n\nimport _ \"embed\"\n\n//go:embed x\nvar s string\n",
	})
	t.Setenv("GOWORK", "off")
	got := embedRoots(context.Background(), root, "")
	want := []string{filepath.Join(root, "lib"), filepath.Join(root, "static"), filepath.Join(root, "views", "pet.html")}
	if !slices.Equal(got, want) {
		t.Errorf("embedRoots: %q, want %q", got, want)
	}
}

const watchApp = `package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
)

// version is what the app answers with.
const version = "v1"

func main() {
	f, _ := os.OpenFile("starts", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	fmt.Fprintln(f, version)
	f.Close()
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "%s %d", version, os.Getpid())
	})
	http.HandleFunc("/state", func(w http.ResponseWriter, r *http.Request) {
		b, _ := os.ReadFile(filepath.Join(os.Getenv("VUKA_DEV_DIR"), "build.json"))
		w.Write(b)
	})
	fmt.Println("listening")
	http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), nil)
}
`

// watchRun is vuka run -watch running on a module, for a test.
type watchRun struct {
	t    *testing.T
	root string
	port int
	cmd  *exec.Cmd
	log  *syncBuffer
}

// buildVuka builds the vuka command for a test.
func buildVuka(t *testing.T) string {
	t.Helper()
	vuka := filepath.Join(t.TempDir(), "vuka")
	if out, err := exec.Command("go", "build", "-o", vuka, ".").CombinedOutput(); err != nil {
		t.Fatalf("building vuka: %v\n%s", err, out)
	}
	return vuka
}

// startWatch runs vuka run -watch with args on the module at root, the app
// listening on a free port ($PORT).
func startWatch(t *testing.T, vuka, root string, env []string, args ...string) *watchRun {
	t.Helper()
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cmd := exec.Command(vuka, append(append([]string{"run", "--watch"}, args...), ".")...)
	cmd.Dir = root
	cmd.Env = append(append(os.Environ(), fmt.Sprint("PORT=", port), twEnv+"=off"), env...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	r := &watchRun{t: t, root: root, port: port, cmd: cmd, log: &syncBuffer{}}
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			fmt.Fprintln(r.log, sc.Text())
		}
	}()
	return r
}

func (r *watchRun) get() (version string, pid int) {
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", r.port))
	if err != nil {
		return "", 0
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	v, p, _ := strings.Cut(string(b), " ")
	pid, _ = strconv.Atoi(p)
	return v, pid
}

func (r *watchRun) waitVersion(want string) int {
	r.t.Helper()
	var pid int
	for end := time.Now().Add(60 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		var v string
		if v, pid = r.get(); v == want {
			return pid
		}
		if time.Now().After(end) {
			r.t.Fatalf("no %s (last %q); output:\n%s", want, v, r.log.String())
		}
	}
}

func (r *watchRun) waitLog(s string, n int) {
	r.t.Helper()
	for end := time.Now().Add(60 * time.Second); strings.Count(r.log.String(), s) < n; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(end) {
			r.t.Fatalf("no %q; output:\n%s", s, r.log.String())
		}
	}
}

func (r *watchRun) state() (st buildState) {
	res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/state", r.port))
	if err != nil {
		return st
	}
	defer res.Body.Close()
	json.NewDecoder(res.Body).Decode(&st)
	return st
}

func (r *watchRun) edit(old, new string) {
	r.t.Helper()
	path := filepath.Join(r.root, "main.vuka")
	src, _ := os.ReadFile(path)
	if !strings.Contains(string(src), old) {
		r.t.Fatalf("main.vuka has no %q", old)
	}
	writeFile(r.t, path, strings.Replace(string(src), old, new, 1))
}

// stop sends vuka SIGTERM and waits for it to exit.
func (r *watchRun) stop() {
	r.t.Helper()
	r.cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			r.t.Errorf("vuka run -watch on SIGTERM: %v", err)
		}
	case <-time.After(10 * time.Second):
		r.t.Fatal("vuka run -watch didn't stop on SIGTERM")
	}
}

func alive(pid int) bool { return syscall.Kill(pid, 0) == nil }

// TestRunWatch runs vuka run -watch on a small app and edits it: a change
// swaps in a new process, a broken build leaves the old one answering, a
// comment changes nothing, and SIGTERM stops it all.
func TestRunWatch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOWORK", "off")
	vuka := buildVuka(t)
	root := tempModule(t, map[string]string{"go.mod": "module example.com/w\n\ngo 1.25.0\n", "main.vuka": watchApp})
	r := startWatch(t, vuka, root, nil)

	pid1 := r.waitVersion("v1")
	if st := r.state(); st.State != "ok" || st.At.IsZero() {
		t.Errorf("the first build's state: %+v", st)
	}
	r.edit(`"v1"`, `"v2"`)
	for end := time.Now().Add(60 * time.Second); ; time.Sleep(5 * time.Millisecond) { // the first build answers while the next compiles
		if st := r.state(); st.State == "building" && !st.Since.IsZero() {
			break
		}
		if v, _ := r.get(); v == "v2" || time.Now().After(end) {
			t.Fatalf("no building state before v2; output:\n%s", r.log.String())
		}
	}
	pid2 := r.waitVersion("v2")
	if pid2 == pid1 || alive(pid1) {
		t.Errorf("the first build (%d) still runs beside the second (%d)", pid1, pid2)
	}
	r.waitLog("● rebuilt in ", 1)

	r.edit(`"v2"`, `"v3" + 1`)
	r.waitLog("● build failed; the last good build keeps running", 1)
	if out := r.log.String(); !strings.Contains(out, "main.vuka:11:") || !strings.Contains(out, "mismatched types") {
		t.Errorf("the compiler's error isn't shown:\n%s", out)
	}
	if v, pid := r.get(); v != "v2" || pid != pid2 {
		t.Errorf("after a failed build: %s %d, want v2 %d", v, pid, pid2)
	}
	if st := r.state(); st.State != "failed" || !strings.Contains(st.Output, "mismatched types") {
		t.Errorf("after a failed build, the state: %+v", st)
	}
	r.edit(`"v3" + 1`, `"v3`)
	r.waitLog("● build failed", 2)
	if !strings.Contains(r.log.String(), "main.vuka:11:") {
		t.Errorf("the transpiler's error isn't shown:\n%s", r.log.String())
	}

	r.edit(`"v3`, `"v3"`)
	pid3 := r.waitVersion("v3")
	r.waitLog("● rebuilt in ", 2)
	if st := r.state(); st.State != "ok" || st.Took <= 0 || st.Output != "" {
		t.Errorf("after a good build, the state: %+v", st)
	}

	r.edit("is what the app answers with", "is the answer")
	writeFile(t, filepath.Join(root, "notes.md"), "not a source")
	r.waitLog("● unchanged", 1)
	if v, pid := r.get(); v != "v3" || pid != pid3 {
		t.Errorf("after a comment: %s %d, want v3 %d", v, pid, pid3)
	}

	r.stop()
	if alive(pid3) {
		t.Errorf("the app (%d) outlived vuka", pid3)
	}
	if starts, _ := os.ReadFile(filepath.Join(root, "starts")); string(starts) != "v1\nv2\nv3\n" {
		t.Errorf("the app's main ran for %q, want v1, v2 and v3 once each", starts) // not for a prewarm
	}
}

// TestRunWatchCancel: a change while a build runs stops that build, stale
// already, and builds the change: the stale version never runs.
func TestRunWatchCancel(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOWORK", "off")
	vuka := buildVuka(t)
	goCmd, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	// A go command whose build of a source marked SLOW takes a minute,
	// unless interrupted.
	bin, mark := t.TempDir(), filepath.Join(t.TempDir(), "mark")
	writeFile(t, filepath.Join(bin, "go"), `#!/bin/sh
if [ "$1" = build ] && grep -q SLOW main.vuka; then
	echo started >> "$MARK"
	sleep 60 &
	trap 'kill $! 2>/dev/null; echo interrupted >> "$MARK"; exit 130' INT
	wait $!
fi
exec `+goCmd+` "$@"
`)
	os.Chmod(filepath.Join(bin, "go"), 0o755)
	root := tempModule(t, map[string]string{"go.mod": "module example.com/w\n\ngo 1.25.0\n", "main.vuka": watchApp})
	r := startWatch(t, vuka, root, []string{"PATH=" + bin + string(os.PathListSeparator) + os.Getenv("PATH"), "MARK=" + mark})
	r.waitVersion("v1")

	r.edit(`"v1"`, `"v2" // SLOW`)
	for end := time.Now().Add(60 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if b, _ := os.ReadFile(mark); strings.Contains(string(b), "started") {
			break
		}
		if time.Now().After(end) {
			t.Fatalf("the slow build didn't start; output:\n%s", r.log.String())
		}
	}
	start := time.Now()
	r.edit(`"v2" // SLOW`, `"v3"`)
	r.waitVersion("v3")
	if took := time.Since(start); took > 30*time.Second {
		t.Errorf("v3 answered %v after the change: the stale build ran to its end", took)
	}
	if b, _ := os.ReadFile(mark); !strings.Contains(string(b), "interrupted") {
		t.Errorf("the stale build wasn't interrupted: %q", b)
	}
	if out := r.log.String(); strings.Contains(out, "build failed") {
		t.Errorf("a cancelled build reported as failed:\n%s", out)
	}
	r.stop()
	if starts, _ := os.ReadFile(filepath.Join(root, "starts")); string(starts) != "v1\nv3\n" {
		t.Errorf("the app's main ran for %q, want v1 and v3", starts)
	}
}

// TestBuildDWARF: watch builds leave out debug information, unless -debug.
func TestBuildDWARF(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	t.Setenv("GOWORK", "off")
	root := tempModule(t, map[string]string{"go.mod": "module example.com/d\n\ngo 1.25.0\n", "main.go": "package main\n\nfunc main() {}\n"})
	for _, debug := range []bool{false, true} {
		flags, _ := cutDebug([]string{"-buildvcs=false"})
		if !debug {
			flags = noDWARF(flags)
		}
		s := &supervisor{root: root, dir: root, tmp: t.TempDir(), flags: flags, pkg: []string{"."}, env: os.Environ()}
		r := s.build(context.Background(), 1)
		if r.err != nil {
			t.Fatalf("build: %v\n%s", r.err, r.out)
		}
		has, err := hasDWARF(r.bin)
		if err != nil {
			t.Skip(err)
		}
		if has != debug {
			t.Errorf("debug %v: the binary has DWARF: %v", debug, has)
		}
	}
}

func hasDWARF(bin string) (bool, error) {
	if f, err := macho.Open(bin); err == nil {
		defer f.Close()
		return f.Section("__debug_info") != nil || f.Section("__zdebug_info") != nil, nil
	}
	if f, err := elf.Open(bin); err == nil {
		defer f.Close()
		return f.Section(".debug_info") != nil || f.Section(".zdebug_info") != nil, nil
	}
	return false, errors.New("neither Mach-O nor ELF")
}

func TestNoDWARF(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{nil, []string{"-ldflags=-w"}},
		{[]string{"-race"}, []string{"-race", "-ldflags=-w"}},
		{[]string{"-ldflags=-X main.v=1"}, []string{"-ldflags=-w -X main.v=1"}},
		{[]string{"-ldflags", "-s"}, []string{"-ldflags", "-w -s"}},
		{[]string{"-ldflags=-s", "--ldflags", "-X a.b=c"}, []string{"-ldflags=-s", "--ldflags", "-w -X a.b=c"}}, // the last one counts
		{[]string{"-ldflags=main=-X main.v=1"}, []string{"-ldflags=main=-X main.v=1"}},                          // a pattern's: as it is
	} {
		if got := noDWARF(c.in); !slices.Equal(got, c.want) {
			t.Errorf("noDWARF(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	if flags, debug := cutDebug([]string{"-race", "-debug", "-tags", "x"}); !debug || !slices.Equal(flags, []string{"-race", "-tags", "x"}) {
		t.Errorf("cutDebug: %q %v", flags, debug)
	}
	if _, debug := cutDebug([]string{"--debug=false"}); debug {
		t.Error("cutDebug took --debug=false for -debug")
	}
}
