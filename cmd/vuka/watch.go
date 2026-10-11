package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/vuka-lang/vuka/internal/load"
)

// vuka run --watch builds the program to a binary and runs it; when a source
// changes it builds the next binary while the current one keeps serving, and
// swaps them only once the build is green. A failed build leaves the last good
// one running; a build that comes out byte-identical restarts nothing.
// Tailwind's watcher runs once, across the restarts.

const runUsage = `usage: vuka run [-watch] [build flags] [package] [arguments...]

Run transpiles the module's .vuka files into an overlay and runs the
package with go run. An app on github.com/vuka-lang/web gets its Tailwind
CSS kept rebuilt while it runs (VUKA_TAILWIND=off for none).

  -watch, --watch
	rebuild the app when a source changes and swap it in: edit, save, and
	the new version answers. The current version keeps serving while the
	next one compiles; a build that fails prints its errors and leaves it
	running, and a change that leaves the binary as it was restarts
	nothing. Changes to .vuka, .templ and .go files (not tests), go.mod,
	go.sum and the files the app embeds start a rebuild.

Build flags are go build's; run 'go help run' for them.
`

const (
	watchInterval = 200 * time.Millisecond // between two looks at the sources
	watchQuiet    = 150 * time.Millisecond // a change waits for this long a calm before it builds
	stopGrace     = 2 * time.Second        // an app's time to stop on SIGTERM before SIGKILL
	liveKeyEnv    = "VUKA_DEV_LIVE_KEY"    // web signs live pages' tokens with it in development
	// devDirEnv names the run's session directory, which says the app runs
	// under vuka run (web turns on its development toolbar). With -watch it
	// holds buildStateFile: how the next build is going, for the toolbar to
	// show while the last good build still serves.
	devDirEnv      = "VUKA_DEV_DIR"
	buildStateFile = "build.json"
)

// cutWatch reports whether the flags before the package ask for -watch, and
// returns the arguments without it.
func cutWatch(args []string) ([]string, bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-watch" || a == "--watch":
			return append(slices.Clip(args[:i]), args[i+1:]...), true
		case a == "--" || !strings.HasPrefix(a, "-"):
			return args, false
		case valueFlag(a):
			i++
		}
	}
	return args, false
}

// valueFlag reports whether a is a build flag of go run taking the next
// argument as its value.
func valueFlag(a string) bool {
	name := strings.TrimLeft(a, "-")
	if strings.Contains(name, "=") {
		return false
	}
	switch name {
	case "C", "p", "asmflags", "buildmode", "compiler", "gccgoflags", "gcflags", "installsuffix", "ldflags",
		"mod", "modfile", "overlay", "pgo", "pkgdir", "tags", "toolexec", "exec", "coverpkg", "covermode", "o":
		return true
	}
	return false
}

// splitRun splits go run's arguments: build flags, the package (a directory,
// an import path or .go files; "." when none is named) and the program's.
func splitRun(args []string) (flags, pkg, prog []string, err error) {
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			i++
			break
		}
		if !strings.HasPrefix(a, "-") {
			break
		}
		if name, _, _ := strings.Cut(strings.TrimLeft(a, "-"), "="); name == "exec" || name == "o" {
			return nil, nil, nil, fmt.Errorf("vuka run -watch: %s isn't supported", a)
		}
		flags = append(flags, a)
		if valueFlag(a) && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	for ; i < len(args) && strings.HasSuffix(args[i], ".go"); i++ {
		pkg = append(pkg, args[i])
	}
	if pkg == nil && i < len(args) {
		pkg, i = args[i:i+1], i+1
	}
	if pkg == nil {
		pkg = []string{"."}
	}
	return flags, pkg, args[i:], nil
}

// runWatch is vuka run -watch.
func runWatch(args []string, stdout, stderr io.Writer) error {
	flags, pkg, prog, err := splitRun(args)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(flags, func(f string) bool { return strings.HasPrefix(strings.TrimLeft(f, "-"), "buildvcs") }) {
		flags = append(flags, "-buildvcs=false") // as go run: no VCS stamp, which would also tell builds apart
	}
	root, _, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	dir, _ := os.Getwd()
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	tmp, err := os.MkdirTemp("", "vuka-watch-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	env := append(os.Environ(), "PWD="+dir, devDirEnv+"="+tmp)
	if os.Getenv(liveKeyEnv) == "" {
		// One key for the session: a live page rendered by one build rejoins the next.
		key := make([]byte, 32)
		rand.Read(key)
		env = append(env, liveKeyEnv+"="+hex.EncodeToString(key))
	}
	assets := newDevAssets(root, stderr)
	defer assets.close()
	env = append(env, assetsEnv+"="+assets.dir())
	s := &supervisor{root: root, dir: dir, tmp: tmp, flags: flags, pkg: pkg, prog: prog, env: env, stdout: stdout, stderr: stderr,
		w: newWatcher(root), exits: make(chan *app, 4), assets: assets}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s.w.scan() // the sources as the first build sees them
	s.setState(buildState{State: "building", Since: time.Now()})
	first := s.build(ctx, 1)
	overlay := first.overlay
	if first.err != nil {
		overlay = "" // finish removes it
	}
	if first.err == nil {
		s.assets.set(first.pa)
	}
	css, stopTailwind := devTailwind(root, overlay, stderr, s.assets)
	defer stopTailwind()
	if css != "" {
		s.env = append(s.env, twCSSEnv+"="+css)
	}
	changes := make(chan []string)
	go s.w.run(ctx, watchInterval, watchQuiet, changes)

	builds := make(chan *buildResult, 1)
	var (
		building bool      // a build runs
		again    bool      // and the sources changed since it started
		changed  time.Time // when the change the next build answers was seen: rebuilt in counts from it
		n        = 1
	)
	startBuild := func() {
		n++
		building = true
		s.setState(buildState{State: "building", Since: changed})
		go func(n int) { builds <- s.build(ctx, n) }(n)
	}
	s.finish(first, time.Time{})
	for {
		select {
		case first := <-sig:
			cancel()
			stopTailwind()
			s.app.stop(first, 0, sig)
			return nil
		case <-changes:
			changed = time.Now()
			if building {
				again = true
				continue
			}
			startBuild()
		case r := <-builds:
			building = false
			if again {
				again = false
				r.discard()
				startBuild()
				continue
			}
			s.finish(r, changed)
		case a := <-s.exits:
			if a == s.app {
				s.app = nil
				status := "exit status 0"
				if a.err != nil {
					status = a.err.Error()
				}
				fmt.Fprintf(stderr, "● the app exited (%s); waiting for a change\n", status)
			}
		}
	}
}

// supervisor builds the program and runs the current build.
type supervisor struct {
	root, dir, tmp string
	flags, pkg     []string
	prog, env      []string
	stdout, stderr io.Writer
	w              *watcher
	app            *app      // the running build; nil when none runs
	exits          chan *app // each app, once it has exited
	assets         *devAssets
}

type buildResult struct {
	dir     string
	bin     string // the binary; "" when the build failed
	sum     string // binaryID
	overlay string
	out     []byte // what the build said
	err     error
	embeds  []string
	pa      *pageAssets // the styles and scripts the sources declare
}

func (r *buildResult) discard() { os.RemoveAll(r.dir) }

// build transpiles the module and builds the program into tmp/<n>/, and asks
// go list, alongside, for what its packages embed.
func (s *supervisor) build(ctx context.Context, n int) *buildResult {
	dir := filepath.Join(s.tmp, fmt.Sprint(n))
	r := &buildResult{dir: dir}
	if r.err = os.MkdirAll(dir, 0o755); r.err != nil {
		return r
	}
	_, modPath, err := load.ModuleRoot(s.root)
	if err != nil {
		r.err = err
		return r
	}
	pkgs, err := load.Discover(s.root, modPath, s.root, true, nil)
	if err != nil {
		r.err = err
		return r
	}
	if r.pa, err = findAssets(s.root, modPath); err != nil {
		r.err = err
		return r
	}
	var gens []load.Generated
	if len(pkgs) > 0 {
		if gens, r.overlay, err = load.Transpile(pkgs, dir, loadOptions(r.pa)); err != nil {
			r.err = err
			return r
		}
	}
	var embeds []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); embeds = embedRoots(ctx, s.root, r.overlay) }()
	defer wg.Wait()
	args := append([]string{"build", "-o", filepath.Join(dir, "app")}, s.flags...)
	if r.overlay != "" {
		args = append(args, "-overlay="+r.overlay)
	}
	c := exec.CommandContext(ctx, "go", append(args, s.pkg...)...)
	c.Dir, c.Env = s.dir, s.env
	var out bytes.Buffer
	resp := &respeller{w: &out, dir: s.dir, gens: gens}
	c.Stdout, c.Stderr = resp, resp
	r.err = c.Run()
	resp.flush()
	r.out = out.Bytes()
	wg.Wait()
	r.embeds = embeds
	if r.err != nil {
		return r
	}
	r.bin = filepath.Join(dir, "app")
	r.sum, r.err = binaryID(r.bin)
	return r
}

// binaryID tells builds of different programs apart: the content part of
// the binary's Go build ID, a hash of the binary but its build ID, which
// changes with any input of the build (a comment does); a hash of the
// whole file when it has none.
func binaryID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	const mark = "\xff Go build ID: \""
	if i := bytes.Index(data, []byte(mark)); i >= 0 {
		id := data[i+len(mark):]
		if j := bytes.IndexByte(id, '"'); j > 0 && j < 256 {
			id = id[:j]
			return string(id[bytes.LastIndexByte(id, '/')+1:]), nil
		}
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// finish acts on a finished build: a failed one says why, a byte-identical
// one is dropped, a new one replaces the running app.
func (s *supervisor) finish(r *buildResult, changed time.Time) {
	if r.embeds != nil {
		s.w.setEmbeds(r.embeds)
	}
	if r.err != nil {
		s.stderr.Write(r.out)
		if len(r.out) == 0 || !isExit(r.err) {
			fmt.Fprintln(s.stderr, r.err)
		}
		if s.app != nil {
			fmt.Fprintln(s.stderr, "● build failed; the last good build keeps running")
		} else {
			fmt.Fprintln(s.stderr, "● build failed; waiting for a change")
		}
		out := string(r.out)
		if len(r.out) == 0 || !isExit(r.err) {
			out += r.err.Error() + "\n"
		}
		s.setState(buildState{State: "failed", At: time.Now(), Output: out})
		r.discard()
		return
	}
	if s.assets != nil {
		s.assets.set(r.pa)
	}
	if s.app != nil && s.app.sum == r.sum {
		fmt.Fprintln(s.stderr, "● unchanged")
		s.setState(buildState{State: "ok", At: time.Now(), Took: took(changed)})
		r.discard()
		return
	}
	prewarm(r.bin)
	if s.app != nil {
		s.app.stop(syscall.SIGTERM, stopGrace, nil)
		os.RemoveAll(filepath.Dir(s.app.bin))
		s.app = nil
	}
	s.app = s.start(r)
	s.setState(buildState{State: "ok", At: time.Now(), Took: took(changed)})
	if !changed.IsZero() && s.app != nil {
		fmt.Fprintf(s.stderr, "● rebuilt in %.1fs\n", time.Since(changed).Seconds())
	}
}

// buildState is what buildStateFile holds: "building" since a time,
// "failed" with what the build said, or "ok", the app at At being the
// build that took Took seconds from the change.
type buildState struct {
	State  string    `json:"state"`
	Since  time.Time `json:"since,omitzero"`
	At     time.Time `json:"at,omitzero"`
	Took   float64   `json:"took,omitempty"`
	Output string    `json:"output,omitempty"`
}

func took(since time.Time) float64 {
	if since.IsZero() {
		return 0
	}
	return time.Since(since).Seconds()
}

// setState writes the build state into the session directory, whole: a
// reader never sees half of it.
func (s *supervisor) setState(st buildState) {
	b, _ := json.Marshal(st)
	tmp := filepath.Join(s.tmp, buildStateFile+".tmp")
	if os.WriteFile(tmp, b, 0o644) == nil {
		os.Rename(tmp, filepath.Join(s.tmp, buildStateFile))
	}
}

// prewarm executes a new binary once, while the last one still serves, so
// the swap doesn't pay its first exec: on macOS that is the code signature's
// check, about half a second per new file. A malformed GOMEMLIMIT stops the
// Go runtime before any package's init, so none of the app's code runs.
func prewarm(bin string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, bin)
	c.Env = append(os.Environ(), "GOMEMLIMIT=vuka-prewarm")
	c.Run()
}

func isExit(err error) bool {
	_, ok := err.(*exec.ExitError)
	return ok
}

// app is a running build.
type app struct {
	bin  string
	sum  string
	cmd  *exec.Cmd
	done chan struct{} // closed when it has exited
	err  error
}

func (s *supervisor) start(r *buildResult) *app {
	c := exec.Command(r.bin, s.prog...)
	c.Dir, c.Env = s.dir, s.env
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, s.stdout, s.stderr
	if err := c.Start(); err != nil {
		fmt.Fprintln(s.stderr, "vuka:", err)
		r.discard()
		return nil
	}
	a := &app{bin: r.bin, sum: r.sum, cmd: c, done: make(chan struct{})}
	go func() {
		a.err = c.Wait()
		close(a.done)
		select {
		case s.exits <- a:
		default:
		}
	}()
	return a
}

// stop signals the app and waits for it to exit, killing it after grace (with
// none, as long as it takes) or on another signal from more.
func (a *app) stop(sig os.Signal, grace time.Duration, more <-chan os.Signal) {
	if a == nil {
		return
	}
	a.cmd.Process.Signal(sig)
	var timeout <-chan time.Time
	if grace > 0 {
		timeout = time.After(grace)
	}
	select {
	case <-a.done:
	case <-timeout:
		a.cmd.Process.Kill()
		<-a.done
	case <-more:
		a.cmd.Process.Kill()
		<-a.done
	}
}

// embedRoots asks go list what the module's packages embed: the files and
// directories their //go:embed patterns name, or the directory a pattern's
// wildcard starts in. Nil when go list fails.
func embedRoots(ctx context.Context, root, overlay string) []string {
	args := []string{"list", "-e", "-f", `{{$d := .Dir}}{{range .EmbedPatterns}}{{$d}}{{"\t"}}{{.}}{{"\n"}}{{end}}`}
	if overlay != "" {
		args = append(args, "-overlay="+overlay)
	}
	c := exec.CommandContext(ctx, "go", append(args, "./...")...)
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		return nil
	}
	roots := []string{}
	for _, line := range strings.Split(string(out), "\n") {
		dir, pattern, ok := strings.Cut(line, "\t")
		if !ok {
			continue
		}
		var lit []string
		for _, el := range strings.Split(strings.TrimPrefix(pattern, "all:"), "/") {
			if strings.ContainsAny(el, `*?[\`) {
				break
			}
			lit = append(lit, el)
		}
		roots = append(roots, filepath.Join(dir, filepath.FromSlash(strings.Join(lit, "/"))))
	}
	slices.Sort(roots)
	return slices.Compact(roots)
}

// A watcher looks at the module's sources for changes by their modification
// times and sizes: the files a build reads, without the module's tests,
// hidden files and directories, build/, node_modules, testdata, vendor and
// nested modules.
type watcher struct {
	root   string
	mu     sync.Mutex
	embeds []string         // what the app embeds, from embedRoots
	fresh  []string         // embeds new since the last look: their files aren't changes
	files  map[string]stamp // as the last look saw them
}

type stamp struct {
	mod  time.Time
	size int64
}

func newWatcher(root string) *watcher { return &watcher{root: root} }

func (w *watcher) setEmbeds(roots []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, r := range roots {
		if !slices.Contains(w.embeds, r) {
			w.fresh = append(w.fresh, r)
		}
	}
	w.embeds = roots
}

// skipDir reports whether the directory at path holds no sources.
func (w *watcher) skipDir(path, name string) bool {
	return path != w.root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") ||
		name == "node_modules" || name == "testdata" || name == "vendor" || path == filepath.Join(w.root, "build") ||
		fileExists(filepath.Join(path, "go.mod")) || fileExists(filepath.Join(path, load.Marker)))
}

// watched reports whether a change to the file at path starts a build.
func (w *watcher) watched(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") {
		return false
	}
	switch filepath.Ext(name) {
	case ".vuka", ".go":
		if !strings.HasSuffix(name, "_test"+filepath.Ext(name)) {
			return true
		}
	case ".templ":
		return true
	}
	return name == "go.mod" || name == "go.sum" || under(path, w.embeds)
}

func under(path string, roots []string) bool {
	return slices.ContainsFunc(roots, func(r string) bool {
		return path == r || strings.HasPrefix(path, r+string(filepath.Separator))
	})
}

// scan looks at the sources and returns those changed, added or removed
// since the last look; the first look returns them all.
func (w *watcher) scan() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	files := make(map[string]stamp, len(w.files))
	var changed []string
	filepath.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // gone since its directory was read
		}
		if d.IsDir() {
			if w.skipDir(path, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !w.watched(path) {
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		st := stamp{fi.ModTime(), fi.Size()}
		files[path] = st
		if old, seen := w.files[path]; seen && old != st || !seen && !under(path, w.fresh) {
			changed = append(changed, path)
		}
		return nil
	})
	for path := range w.files {
		if _, ok := files[path]; !ok && w.watched(path) { // not a file the app stopped embedding
			changed = append(changed, path)
		}
	}
	w.files, w.fresh = files, nil
	return changed
}

// run looks at the sources every interval and sends each batch of changes
// once a look finds none for quiet. A tree that takes long to look at is
// looked at less often.
func (w *watcher) run(ctx context.Context, interval, quiet time.Duration, out chan<- []string) {
	var batch []string
	wait := interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		start := time.Now()
		changed := w.scan()
		took := time.Since(start)
		if len(changed) > 0 {
			batch, wait = append(batch, changed...), quiet
			continue
		}
		if batch != nil {
			select {
			case out <- batch:
			case <-ctx.Done():
				return
			}
			batch = nil
		}
		wait = max(interval, 10*took)
	}
}
