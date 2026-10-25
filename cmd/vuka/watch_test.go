package main

import (
	"bufio"
	"context"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

// A testClock is the watcher's clock in a test: each wait the watcher
// starts is handed to the test, in order, and ends only when the test fires
// it. The watcher blocks until its wait is taken, so a wait in the test's
// hands says the watcher has finished what came before it (a look, a batch
// sent) and is waiting.
type testClock struct {
	t     *testing.T
	waits chan *testWait
	done  chan struct{}
}

type testWait struct {
	d time.Duration
	c chan time.Time
}

func (w *testWait) fire() { w.c <- time.Time{} }

func newTestClock(t *testing.T, w *watcher) *testClock {
	k := &testClock{t: t, waits: make(chan *testWait), done: make(chan struct{})}
	t.Cleanup(func() { close(k.done) })
	w.after = func(d time.Duration) <-chan time.Time {
		tw := &testWait{d, make(chan time.Time, 1)}
		select {
		case k.waits <- tw:
		case <-k.done:
		}
		return tw.c
	}
	return k
}

// next is the watcher's next wait.
func (k *testClock) next(what string) *testWait {
	k.t.Helper()
	select {
	case w := <-k.waits:
		return w
	case <-time.After(10 * time.Second):
		k.t.Fatalf("%s: the watcher doesn't wait", what)
		return nil
	}
}

// TestWatcherEvents: with the system's file events, a save is seen, whether
// the editor writes the file, renames another over it or writes it twice (a
// formatter after the save); a new directory is watched, and so is a file
// the app newly embeds. The test's clock holds each calm until the test's
// writes are made, so the look after it sees them all: one change is one
// batch whatever the machine's load.
func TestWatcherEvents(t *testing.T) {
	ow, err := newOSWatch(nil)
	if err != nil && runtime.GOOS == "windows" {
		t.Skip("vuka uses no file events on Windows: it looks every interval (TestWatcher)")
	}
	if err != nil {
		needs(t, "no file events: %v", err)
	}
	ow.close()
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "a.vuka": "a", "b.vuka": "b", "views/pet.html": "p"})
	w := newWatcher(root)
	clock := newTestClock(t, w)
	w.scan()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan []string, 10)
	const pollEvery = 2 * time.Hour
	go w.run(ctx, pollEvery, pollEvery, out)
	if wait := clock.next("start"); wait.d == pollEvery || !w.events.Load() {
		t.Fatal("the watcher doesn't use file events")
	}
	path := func(rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }
	// settle lets the watcher look once the test has made its writes: it
	// ends each calm the watcher waits for, until the watcher waits for
	// the next event after a look, and returns the batches sent. The watcher
	// looks after a calm only, but when kicked: calmed says it needn't wait
	// for one.
	settle := func(what string, calmed bool) (batches [][]string) {
		t.Helper()
		for {
			wait := clock.next(what + ": no event")
			if wait.d == osQuiet {
				calmed = true
				wait.fire()
				continue
			}
			if !calmed {
				t.Fatalf("%s: the watcher looked without waiting for a calm", what)
			}
			for len(out) > 0 {
				batches = append(batches, <-out)
			}
			return batches
		}
	}
	// quiet checks that nothing more comes: an event late for the writes
	// already seen finds nothing.
	quiet := func(what string) {
		t.Helper()
		for {
			select {
			case wait := <-clock.waits:
				if wait.d != osQuiet {
					t.Fatalf("%s: the watcher looked without waiting for a calm", what)
				}
				wait.fire()
				if more := settle(what, true); len(more) > 0 {
					t.Errorf("%s: another batch %v", what, more)
				}
			case <-time.After(100 * time.Millisecond):
				return
			}
		}
	}
	expect := func(what string, want ...string) {
		t.Helper()
		batches := settle(what, false)
		for i := range want {
			want[i] = path(want[i])
		}
		if len(batches) != 1 {
			t.Errorf("%s: batches %v, want one of %v", what, batches, want)
		} else if got := slices.Compact(slices.Sorted(slices.Values(batches[0]))); !slices.Equal(got, want) {
			t.Errorf("%s: %v, want %v", what, got, want)
		}
		quiet(what)
	}

	writeFile(t, path("a.vuka"), "a2")
	expect("a write", "a.vuka")

	writeFile(t, path("a.vuka.tmp"), "a3") // an editor's save: a temporary file renamed over the source
	if err := os.Rename(path("a.vuka.tmp"), path("a.vuka")); err != nil {
		t.Fatal(err)
	}
	expect("a save by rename", "a.vuka")

	writeFile(t, path("a.vuka"), "a4") // the file renamed over is watched in turn
	expect("a write to the renamed file", "a.vuka")

	writeFile(t, path("b.vuka"), "b2 unformatted") // a save, then a formatter's rewrite
	writeFile(t, path("b.vuka"), "b2")
	expect("a save and a format", "b.vuka")

	if err := os.Mkdir(path("lib"), 0o755); err != nil {
		t.Fatal(err)
	}
	if batches := settle("a new directory", false); len(batches) > 0 { // looked at before anything is in it
		t.Errorf("an empty directory: %v", batches)
	}
	writeFile(t, path("lib/c.vuka"), "c")
	expect("a file in a new directory", "lib/c.vuka")
	writeFile(t, path("lib/c.vuka"), "c2")
	expect("a write to it", "lib/c.vuka")

	for i := range 5 { // the app writing its log: not a source
		writeFile(t, path("app.log"), strings.Repeat("x", i+1))
	}
	quiet("a log written")

	w.setEmbeds([]string{path("views/pet.html")}) // a build found it embedded
	if batches := settle("new embeds", true); len(batches) > 0 {
		t.Errorf("new embeds: %v", batches)
	}
	writeFile(t, path("views/pet.html"), "p2")
	expect("an embedded file", "views/pet.html")
}

// TestWatcherDebounce: looking every interval, a burst of saves is one
// change, sent once a look finds the sources calm.
func TestWatcherDebounce(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "a.vuka": "", "b.vuka": ""})
	w := newWatcher(root)
	w.poll = true
	clock := newTestClock(t, w)
	w.scan()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan []string, 10)
	const interval, quiet = time.Hour, time.Minute
	go w.run(ctx, interval, quiet, out)
	wait := clock.next("start")
	if wait.d != interval {
		t.Fatalf("the first wait: %v, want the interval", wait.d)
	}
	for i := range 10 {
		writeFile(t, filepath.Join(root, []string{"a.vuka", "b.vuka"}[i%2]), strings.Repeat("x", i+1))
		wait.fire()
		if wait = clock.next("a save"); wait.d != quiet {
			t.Fatalf("after save %d the watcher waits %v, want the calm %v", i, wait.d, quiet)
		}
		if len(out) > 0 {
			t.Fatalf("a batch before the saves stopped: %v", <-out)
		}
	}
	wait.fire() // the sources calm: the look finds nothing new
	if wait = clock.next("the calm"); wait.d != interval {
		t.Errorf("once calm the watcher waits %v, want the interval", wait.d)
	}
	select {
	case batch := <-out:
		if batch = slices.Compact(slices.Sorted(slices.Values(batch))); len(batch) != 2 {
			t.Errorf("batch %v", batch)
		}
	default:
		t.Fatal("no batch once calm")
	}
	wait.fire() // a calm tree: nothing to send
	clock.next("a look at a calm tree")
	if len(out) > 0 {
		t.Errorf("a second batch: %v", <-out)
	}
}

// testOSWatch is file events a test sends.
type testOSWatch struct {
	c      chan struct{}
	err    chan error
	synced chan struct{} // a look has had the watch follow the sources
	follow func() bool   // when set, what watch does and reports
}

func (o *testOSWatch) watch(dirs, files map[string]uint64) (bool, error) {
	if o.follow != nil {
		return o.follow(), nil
	}
	select {
	case o.synced <- struct{}{}:
	default:
	}
	return false, nil
}
func (o *testOSWatch) events() <-chan struct{} { return o.c }
func (o *testOSWatch) errors() <-chan error    { return o.err }
func (o *testOSWatch) close()                  {}

// TestWatcherCalm: with file events, a look waits for osQuiet without one,
// each event starting the wait anew; a change made while the last batch
// waits for the supervisor joins it; an event that finds nothing sends
// nothing.
func TestWatcherCalm(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "a.vuka": "a", "b.vuka": "b"})
	w := newWatcher(root)
	clock := newTestClock(t, w)
	w.scan()
	ow := &testOSWatch{c: make(chan struct{}, 1), err: make(chan error, 1), synced: make(chan struct{}, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan []string) // the supervisor takes a batch when it is ready for it
	go w.runEvents(ctx, ow, out)
	if wait := clock.next("start"); wait.d == osQuiet {
		t.Fatal("the watcher waits for a calm with no event")
	}
	<-ow.synced
	event := func() { ow.c <- struct{}{} }
	looked := func(what string) {
		t.Helper()
		select {
		case <-ow.synced:
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: no look", what)
		}
	}

	writeFile(t, filepath.Join(root, "a.vuka"), "a2")
	event()
	first := clock.next("an event")
	if first.d != osQuiet {
		t.Fatalf("after an event the watcher waits %v, want osQuiet", first.d)
	}
	writeFile(t, filepath.Join(root, "a.vuka"), "a22") // a formatter's rewrite
	event()
	second := clock.next("a second event")
	if second.d != osQuiet {
		t.Fatalf("after a second event the watcher waits %v, want osQuiet", second.d)
	}
	first.fire() // the wait the second event replaced
	select {
	case <-ow.synced:
		t.Fatal("the watcher looked before the sources were calm")
	case <-time.After(50 * time.Millisecond):
	}
	second.fire()
	looked("the calm")

	// The batch waits for the supervisor: a change meanwhile joins it.
	writeFile(t, filepath.Join(root, "b.vuka"), "b2")
	event()
	clock.next("an event while a batch waits").fire()
	looked("the calm while a batch waits")
	var idle *testWait
	select {
	case batch := <-out:
		if want := []string{filepath.Join(root, "a.vuka"), filepath.Join(root, "b.vuka")}; !slices.Equal(slices.Compact(slices.Sorted(slices.Values(batch))), want) {
			t.Errorf("batch %v, want %v", batch, want)
		}
	case idle = <-clock.waits:
		t.Fatalf("the watcher waits %v with its batch unsent", idle.d)
	case <-time.After(10 * time.Second):
		t.Fatal("no batch")
	}
	clock.next("after the batch")

	// An event about nothing a look finds.
	event()
	clock.next("a stray event").fire()
	looked("a stray event")
	select {
	case batch := <-out:
		t.Errorf("a look that found nothing sent %v", batch)
	case wait := <-clock.waits:
		if wait.d == osQuiet {
			t.Errorf("after a look the watcher waits for a calm")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the watcher doesn't wait after a look")
	}
}

// TestWatcherLook: a write between a look and the watch starting on what
// it found (a new file, a new directory) sends no event, so the look looks
// again.
func TestWatcherLook(t *testing.T) {
	root := tempModule(t, map[string]string{"go.mod": "module x\n", "a.vuka": "a"})
	w := newWatcher(root)
	w.scan()
	for _, c := range []struct{ what, create, write string }{
		{"a new file", "b.vuka", "b.vuka"},
		{"a new directory", "lib", "lib/c.vuka"},
	} {
		create, write := filepath.Join(root, c.create), filepath.Join(root, c.write)
		if c.create == c.write {
			writeFile(t, create, "")
		} else if err := os.Mkdir(create, 0o755); err != nil {
			t.Fatal(err)
		}
		calls := 0
		ow := &testOSWatch{follow: func() bool {
			calls++
			if calls == 1 { // watched only now: the write had no event
				writeFile(t, write, "written")
				return true
			}
			return false
		}}
		changed, err := w.look(ow)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(changed, write) || calls != 2 {
			t.Errorf("%s: a look found %v (%d watches)", c.what, changed, calls)
		}
		if later := w.scan(); later != nil {
			t.Errorf("%s: the write was left for a later look: %v", c.what, later)
		}
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
	vuka := exe(filepath.Join(t.TempDir(), "vuka"))
	c := exec.Command("go", "build", "-o", vuka, ".")
	if w := os.Getenv("VUKA_TEST_GOWORK"); w != "" {
		c.Env = append(os.Environ(), "GOWORK="+w) // a vuka the released modules can't build yet
	}
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("building vuka: %v\n%s", err, out)
	}
	besideVukavet(t, vuka)
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
	appGroup(cmd) // on Windows, its own group, for interrupt's CTRL_BREAK_EVENT
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		// SIGTERM first: vuka stops the app, which holds the output pipe.
		interrupt(cmd, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			cmd.Process.Kill()
		}
	})
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

// stop sends vuka SIGTERM (on Windows, CTRL_BREAK_EVENT) and waits for it
// to exit.
func (r *watchRun) stop() {
	r.t.Helper()
	if err := interrupt(r.cmd, syscall.SIGTERM); err != nil {
		r.t.Fatalf("stopping vuka run -watch: %v", err)
	}
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

func alive(pid int) bool { return processAlive(pid) }

// stateAtMessage is a supervisor's stderr that records the build state
// as each message is written.
type stateAtMessage struct {
	dir  string
	seen []string // each message, then the state as it was written
}

func (w *stateAtMessage) Write(b []byte) (int, error) {
	st, _ := os.ReadFile(filepath.Join(w.dir, buildStateFile))
	w.seen = append(w.seen, strings.TrimSpace(string(b))+" | "+string(st))
	return len(b), nil
}

// TestBuildStateBeforeMessage: the state of a finished build is written
// before the message saying how it went, so what reads the message (a test,
// the toolbar's next look) reads the state too, and the state of a build
// before its app starts.
func TestBuildStateBeforeMessage(t *testing.T) {
	dir := t.TempDir()
	w := &stateAtMessage{dir: dir}
	s := &supervisor{tmp: dir, stderr: w}
	s.setState(buildState{State: "building", Since: time.Now()})
	s.finish(&buildResult{dir: filepath.Join(dir, "1"), out: []byte("main.vuka:11: mismatched types\n"), err: errors.New("exit status 1")}, time.Now())
	s.app = &app{sum: "a"}
	s.setState(buildState{State: "building", Since: time.Now()})
	s.finish(&buildResult{dir: filepath.Join(dir, "2"), sum: "a"}, time.Now())
	want := []string{`● build failed; waiting for a change | {"state":"failed"`, `● unchanged | {"state":"ok"`}
	for _, want := range want {
		if !slices.ContainsFunc(w.seen, func(s string) bool { return strings.HasPrefix(s, want) }) {
			t.Errorf("no %q in %q", want, w.seen)
		}
	}
}

// TestBuildStateWhileRead: the state is written while the app has the
// file open, as it reads it: on Windows a rename onto an open file fails
// until it is closed.
func TestBuildStateWhileRead(t *testing.T) {
	dir := t.TempDir()
	s := &supervisor{tmp: dir, stderr: io.Discard}
	s.setState(buildState{State: "building", Since: time.Now()})
	f, err := os.Open(filepath.Join(dir, buildStateFile))
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { s.setState(buildState{State: "failed", At: time.Now()}); close(done) }()
	time.Sleep(100 * time.Millisecond)
	f.Close()
	<-done
	if b, _ := os.ReadFile(filepath.Join(dir, buildStateFile)); !strings.Contains(string(b), `"failed"`) {
		t.Errorf("the state: %s", b)
	}
}

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
	if runtime.GOOS == "windows" {
		t.Skip("its stand-in go command is a shell script")
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
		if filepath.Base(r.bin) != "app"+exeSuffix() { // without .exe, Windows runs no binary
			t.Errorf("the binary is %s, want app%s", r.bin, exeSuffix())
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
	if f, err := pe.Open(bin); err == nil {
		defer f.Close()
		return f.Section(".debug_info") != nil || f.Section(".zdebug_info") != nil, nil
	}
	return false, errors.New("neither Mach-O, ELF nor PE")
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

// TestBinaryID: on each binary format a comment leaves a build's id as it
// was and a change of the program doesn't (Linux binaries carry a GNU build
// ID derived from the inputs).
func TestBinaryID(t *testing.T) {
	if testing.Short() {
		t.Skip("runs the go command")
	}
	for _, goos := range []string{"linux", "darwin", "windows"} {
		ids := map[string]string{}
		for name, src := range map[string]string{
			"a": "package main\n\n// one\nfunc main() { println(1) }\n",
			"b": "package main\n\n// two\nfunc main() { println(1) }\n",
			"c": "package main\n\n// one\nfunc main() { println(2) }\n",
		} {
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "go.mod"), "module example.com/b\n\ngo 1.25.0\n")
			writeFile(t, filepath.Join(dir, "main.go"), src)
			c := exec.Command("go", "build", "-ldflags=-w", "-o", "bin", ".")
			c.Dir = dir
			c.Env = append(os.Environ(), "GOOS="+goos, "GOARCH=amd64", "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=-trimpath")
			if out, err := c.CombinedOutput(); err != nil {
				t.Fatalf("%s: %v\n%s", goos, err, out)
			}
			id, err := binaryID(filepath.Join(dir, "bin"))
			if err != nil {
				t.Fatal(err)
			}
			ids[name] = id
		}
		if ids["a"] != ids["b"] || ids["a"] == ids["c"] {
			t.Errorf("%s: a comment %q→%q, a change →%q", goos, ids["a"], ids["b"], ids["c"])
		}
	}
}
