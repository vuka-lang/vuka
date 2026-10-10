package main

import (
	"bufio"
	"context"
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
	fmt.Println("listening")
	http.ListenAndServe("127.0.0.1:"+os.Getenv("PORT"), nil)
}
`

// TestRunWatch runs vuka run -watch on a small app and edits it: a change
// swaps in a new process, a broken build leaves the old one answering, a
// comment changes nothing, and SIGTERM stops it all.
func TestRunWatch(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the vuka command and runs the go command")
	}
	t.Setenv("GOWORK", "off")
	vuka := filepath.Join(t.TempDir(), "vuka")
	if out, err := exec.Command("go", "build", "-o", vuka, ".").CombinedOutput(); err != nil {
		t.Fatalf("building vuka: %v\n%s", err, out)
	}
	root := tempModule(t, map[string]string{"go.mod": "module example.com/w\n\ngo 1.25.0\n", "main.vuka": watchApp})
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	cmd := exec.Command(vuka, "run", "--watch", ".")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), fmt.Sprint("PORT=", port), twEnv+"=off")
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()
	var log syncBuffer
	go func() {
		sc := bufio.NewScanner(pr)
		for sc.Scan() {
			fmt.Fprintln(&log, sc.Text())
		}
	}()
	get := func() (version string, pid int) {
		res, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/", port))
		if err != nil {
			return "", 0
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		v, p, _ := strings.Cut(string(b), " ")
		pid, _ = strconv.Atoi(p)
		return v, pid
	}
	waitVersion := func(want string) int {
		t.Helper()
		var pid int
		for end := time.Now().Add(60 * time.Second); ; time.Sleep(50 * time.Millisecond) {
			var v string
			if v, pid = get(); v == want {
				return pid
			}
			if time.Now().After(end) {
				t.Fatalf("no %s (last %q); output:\n%s", want, v, log.String())
			}
		}
	}
	waitLog := func(s string, n int) {
		t.Helper()
		for end := time.Now().Add(60 * time.Second); strings.Count(log.String(), s) < n; time.Sleep(50 * time.Millisecond) {
			if time.Now().After(end) {
				t.Fatalf("no %q; output:\n%s", s, log.String())
			}
		}
	}
	alive := func(pid int) bool { return syscall.Kill(pid, 0) == nil }
	edit := func(old, new string) {
		t.Helper()
		path := filepath.Join(root, "main.vuka")
		src, _ := os.ReadFile(path)
		if !strings.Contains(string(src), old) {
			t.Fatalf("main.vuka has no %q", old)
		}
		writeFile(t, path, strings.Replace(string(src), old, new, 1))
	}

	pid1 := waitVersion("v1")
	edit(`"v1"`, `"v2"`)
	pid2 := waitVersion("v2")
	if pid2 == pid1 || alive(pid1) {
		t.Errorf("the first build (%d) still runs beside the second (%d)", pid1, pid2)
	}
	waitLog("● rebuilt in ", 1)

	edit(`"v2"`, `"v3" + 1`)
	waitLog("● build failed; the last good build keeps running", 1)
	if out := log.String(); !strings.Contains(out, "main.vuka:10:") || !strings.Contains(out, "mismatched types") {
		t.Errorf("the compiler's error isn't shown:\n%s", out)
	}
	if v, pid := get(); v != "v2" || pid != pid2 {
		t.Errorf("after a failed build: %s %d, want v2 %d", v, pid, pid2)
	}
	edit(`"v3" + 1`, `"v3`)
	waitLog("● build failed", 2)
	if !strings.Contains(log.String(), "main.vuka:10:") {
		t.Errorf("the transpiler's error isn't shown:\n%s", log.String())
	}

	edit(`"v3`, `"v3"`)
	pid3 := waitVersion("v3")
	waitLog("● rebuilt in ", 2)

	edit("is what the app answers with", "is the answer")
	writeFile(t, filepath.Join(root, "notes.md"), "not a source")
	waitLog("● unchanged", 1)
	if v, pid := get(); v != "v3" || pid != pid3 {
		t.Errorf("after a comment: %s %d, want v3 %d", v, pid, pid3)
	}

	cmd.Process.Signal(syscall.SIGTERM)
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("vuka run -watch on SIGTERM: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("vuka run -watch didn't stop on SIGTERM")
	}
	if alive(pid3) {
		t.Errorf("the app (%d) outlived vuka", pid3)
	}
	if starts, _ := os.ReadFile(filepath.Join(root, "starts")); string(starts) != "v1\nv2\nv3\n" {
		t.Errorf("the app's main ran for %q, want v1, v2 and v3 once each", starts) // not for a prewarm
	}
}
