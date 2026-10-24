package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// vuka run --watch builds the program to a binary and runs it; when a source
// changes it builds the next binary while the current one keeps serving, and
// swaps them only once the build is green. A failed build leaves the last good
// one running; a build that comes out byte-identical restarts nothing.
// Tailwind's watcher runs once, across the restarts.

const runUsage = `usage: vuka run [-watch [-debug]] [build flags] [package] [arguments...]

Run transpiles the module's .vuka files into an overlay and runs the
package with go run. An app on github.com/vuka-lang/web/v2 gets its Tailwind
CSS kept rebuilt while it runs (VUKA_TAILWIND=off for none).

  -watch, --watch
	rebuild the app when a source changes and swap it in: edit, save, and
	the new version answers. The current version keeps serving while the
	next one compiles; a build that fails prints its errors and leaves it
	running, and a change that leaves the binary as it was restarts
	nothing. Changes to .vuka, .templ and .go files (not tests), go.mod,
	go.sum and the files the app embeds start a rebuild, seen through the
	system's file events (VUKA_WATCH=poll looks every 200ms instead); a
	change while a build runs stops it and builds anew. Only the packages a
	change reaches are transpiled again.

  -debug, --debug
	with -watch, keep the debug information (DWARF) watch builds leave out
	to link faster: for delve.
  -embed, --embed
	with -watch, build in the files web.Frontend serves (it reads them from
	disk under vuka run; VUKA_EMBED_STUB=off does the same)

Build flags are go build's; run 'go help run' for them.
`

const (
	watchInterval = 200 * time.Millisecond // between two looks at the sources
	watchQuiet    = 150 * time.Millisecond // a change waits for this long a calm before it builds
	stopGrace     = 2 * time.Second        // an app's time to stop on SIGTERM (Windows: CTRL_BREAK_EVENT) before it is killed
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
	flags, debug := cutDebug(flags)
	flags, stub := stubbing(flags)
	if !slices.ContainsFunc(flags, func(f string) bool { return strings.HasPrefix(strings.TrimLeft(f, "-"), "buildvcs") }) {
		flags = append(flags, "-buildvcs=false") // as go run: no VCS stamp, which would also tell builds apart
	}
	if !debug {
		flags = noDWARF(flags)
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
	defer removeAll(tmp)
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
		w: newWatcher(root), exits: make(chan *app, 4), assets: assets, stub: stub}
	if s.sess, _ = load.NewSession(root); s.sess != nil { // nil: each build transpiles from scratch
		s.sess.UseCache("")
	}

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
	stopFrontend := devFrontend(root, stderr)
	defer stopFrontend()
	changes := make(chan []string)
	go s.w.run(ctx, watchInterval, watchQuiet, changes)

	builds := make(chan *buildResult, 1)
	var (
		building    bool               // a build runs
		again       bool               // and the sources changed since it started: it is cancelled
		cancelBuild context.CancelFunc // cancels the build that runs
		changed     time.Time          // when the change the next build answers was seen: rebuilt in counts from it
		n           = 1
	)
	startBuild := func() {
		n++
		building = true
		s.setState(buildState{State: "building", Since: changed})
		var bctx context.Context
		bctx, cancelBuild = context.WithCancel(ctx)
		go func(n int) { builds <- s.build(bctx, n) }(n)
	}
	s.finish(first, time.Time{})
	for {
		select {
		case first := <-sig:
			cancel()
			stopTailwind()
			s.app.stop(first, 0, sig)
			stopFrontend()
			return nil
		case <-changes:
			changed = time.Now()
			traceStep("change")
			if building {
				// What it builds is stale already: stop it, and build anew
				// once it has stopped.
				again = true
				cancelBuild()
				continue
			}
			startBuild()
		case r := <-builds:
			building = false
			cancelBuild()
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
	sess           *load.Session // what the last builds transpiled; nil for none
	stub           bool          // the frontend's build is left out of the binary (stubEmbeds)
	app            *app          // the running build; nil when none runs
	exits          chan *app     // each app, once it has exited
	assets         *devAssets
	linked         string // the last build's auto-link message, said once per change
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

func (r *buildResult) discard() { removeAll(r.dir) }

// build transpiles the module and builds the program into tmp/<n>/, and asks
// go list, alongside, for what its packages embed.
func (s *supervisor) build(ctx context.Context, n int) *buildResult {
	dir := filepath.Join(s.tmp, fmt.Sprint(n))
	r := &buildResult{dir: dir}
	if r.err = os.MkdirAll(dir, 0o755); r.err != nil {
		return r
	}
	ph := newPhases(s.stderr)
	defer ph.done()
	ph.step("scanning the module")
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
	t0 := time.Now()
	routes := scanRoutes(s.root, modPath, nil)
	if pkgs, err = routes.stub(pkgs); err != nil {
		r.err = err
		return r
	}
	if len(pkgs) > 0 {
		ph.step(fmt.Sprintf("transpiling %d %s", len(pkgs), map[bool]string{true: "package", false: "packages"}[len(pkgs) == 1]))
		transpile := load.Transpile
		if s.sess != nil {
			transpile = s.sess.Transpile
		}
		if gens, r.overlay, err = transpile(pkgs, dir, loadOptions(r.pa)); err != nil {
			r.err = err
			return r
		}
	}
	if gens, r.overlay, err = routes.apply(gens, r.overlay, dir); err != nil {
		r.err = err
		return r
	}
	if _, err := routes.writeFiles(); err != nil {
		r.err = err
		return r
	}
	ph.step("linking the module's packages")
	var linkMsg string
	if r.overlay, gens, linkMsg, err = autoLink(s.root, modPath, dir, r.overlay, pkgs, gens); err != nil {
		r.err = err
		return r
	}
	r.overlay, gens = typedFields(s.root, dir, r.overlay, gens, false, s.stderr)
	if linkMsg != s.linked {
		// Said once per change: a package created mid-session is linked by the
		// next rebuild, a deleted one unlinked.
		if linkMsg != "" {
			fmt.Fprintln(s.stderr, linkMsg)
		}
		s.linked = linkMsg
	}
	if s.stub {
		if r.overlay, err = stubEmbeds(r.overlay, dir, frontendEmbeds(s.root)); err != nil {
			r.err = err
			return r
		}
	}
	if r.err = ctx.Err(); r.err != nil {
		return r
	}
	tTranspile := time.Since(t0)
	t0 = time.Now()
	ph.step("compiling")
	defer func() { traceStep("transpile %v build %v", tTranspile, time.Since(t0)) }()
	var embeds []string
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); embeds = embedRoots(ctx, s.root, r.overlay) }()
	defer wg.Wait()
	bin := exe(filepath.Join(dir, "app")) // app.exe on Windows, which runs no file without it
	args := append([]string{"build", "-o", bin}, s.flags...)
	if r.overlay != "" {
		args = append(args, "-overlay="+r.overlay)
	}
	c := exec.CommandContext(ctx, "go", append(args, s.pkg...)...)
	c.Dir, c.Env = s.dir, s.env
	// Cancelled, the go command is interrupted: it stops the compilers and
	// linker it runs and removes its work directory.
	c.Cancel = func() error { return c.Process.Signal(os.Interrupt) }
	c.WaitDelay = 5 * time.Second
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
	r.bin = bin
	r.sum, r.err = binaryID(r.bin)
	return r
}

// binaryID tells builds of different programs apart: the content part of
// the binary's Go build ID, a hash of the binary but its build ID, which
// changes with any input of the build (a comment does); a hash of the
// whole file when it has none. ELF binaries keep the ID in a note only.
func binaryID(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	const mark = "\xff Go build ID: \""
	var id []byte
	if i := bytes.Index(data, []byte(mark)); i >= 0 {
		rest := data[i+len(mark):]
		if j := bytes.IndexByte(rest, '"'); j > 0 && j < 256 {
			id = rest[:j]
		}
	} else if f, err := elf.NewFile(bytes.NewReader(data)); err == nil {
		// the note: namesz, descsz, type, "Go\x00\x00", the ID
		if sec := f.Section(".note.go.buildid"); sec != nil {
			if note, err := sec.Data(); err == nil && len(note) > 16 && string(note[12:16]) == "Go\x00\x00" {
				if n := int(f.ByteOrder.Uint32(note[4:8])); 16+n <= len(note) {
					id = note[16 : 16+n]
				}
			}
		}
		f.Close()
	}
	if len(id) > 0 {
		return string(id[bytes.LastIndexByte(id, '/')+1:]), nil
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
	t0 := time.Now()
	prewarm(r.bin)
	traceStep("prewarm %v", time.Since(t0))
	if s.app != nil {
		s.app.stop(syscall.SIGTERM, stopGrace, nil)
		removeAll(filepath.Dir(s.app.bin)) // on Windows, once the exited app's binary is let go
		s.app = nil
	}
	s.app = s.start(r)
	s.setState(buildState{State: "ok", At: time.Now(), Took: took(changed)})
	if !changed.IsZero() && s.app != nil {
		fmt.Fprintf(s.stderr, "● rebuilt in %.1fs\n", time.Since(changed).Seconds())
	}
}

// cutDebug reports whether flags ask for -debug, and returns them without it.
func cutDebug(flags []string) ([]string, bool) {
	debug := false
	out := flags[:0:0]
	for _, f := range flags {
		switch strings.TrimLeft(f, "-") {
		case "debug", "debug=true":
			debug = true
		case "debug=false":
		default:
			out = append(out, f)
		}
	}
	return out, debug
}

// noDWARF adds -w to the linker's flags: no debug information, which is
// most of what the linker writes. A build is then faster to link and to
// start. Linker flags for a package pattern are left as they are.
func noDWARF(flags []string) []string {
	out := slices.Clone(flags)
	at, valueAt := -1, -1 // the last -ldflags: go build takes it
	for i := 0; i < len(out); i++ {
		name, _, hasValue := strings.Cut(strings.TrimLeft(out[i], "-"), "=")
		if name == "ldflags" && strings.HasPrefix(out[i], "-") {
			at, valueAt = i, i
			if !hasValue {
				valueAt = i + 1
				i++
			}
		}
	}
	if at < 0 {
		return append(out, "-ldflags=-w")
	}
	if valueAt >= len(out) {
		return out
	}
	value := out[valueAt]
	if valueAt == at {
		_, value, _ = strings.Cut(value, "=")
	}
	if first := strings.Fields(value); len(first) > 0 && !strings.HasPrefix(first[0], "-") && strings.Contains(first[0], "=") {
		return out // pattern=flags
	}
	if valueAt == at {
		out[at] = "-ldflags=-w " + value
	} else {
		out[valueAt] = "-w " + value
	}
	return out
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
	appGroup(c)
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

// stop signals the app (on Windows, any signal is a CTRL_BREAK_EVENT: see
// interrupt) and waits for it to exit, killing it after grace (with none, as
// long as it takes), on another signal from more, or at once when it can't
// be signalled.
func (a *app) stop(sig os.Signal, grace time.Duration, more <-chan os.Signal) {
	if a == nil {
		return
	}
	var timeout <-chan time.Time
	if grace > 0 {
		timeout = time.After(grace)
	}
	if err := interrupt(a.cmd, sig); err != nil {
		select {
		case <-a.done: // gone already
			return
		default:
		}
		killTree(a.cmd)
		<-a.done
		return
	}
	select {
	case <-a.done:
	case <-timeout:
		killTree(a.cmd)
		<-a.done
	case <-more:
		killTree(a.cmd)
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
// times, sizes and identities: the files a build reads, without the
// module's tests, hidden files and directories, build/, node_modules,
// testdata, vendor and nested modules. It is told to look by the system's
// file events (kqueue, inotify), and else looks every interval.
type watcher struct {
	root   string
	poll   bool        // look every interval, without the system's events
	events atomic.Bool // the system's events tell when to look
	kick   chan struct{}
	after  func(time.Duration) <-chan time.Time // time.After; a test's clock drives the waits
	mu     sync.Mutex
	embeds []string          // what the app embeds, from embedRoots
	fresh  []string          // embeds new since the last look: their files aren't changes
	files  map[string]stamp  // as the last look saw them
	dirs   map[string]uint64 // the directories the last look went through, by identity
}

type stamp struct {
	mod  time.Time
	size int64
	id   uint64 // the file's identity (inode): a file renamed over it is another
}

// An osWatch tells, through events, when something in the directories and
// files it watches may have changed; a look at the sources finds what.
type osWatch interface {
	// watch makes it watch exactly these directories and files, which map
	// to their identities: one whose identity changed is watched anew. It
	// reports whether it started watching one: what changed in it before
	// then had no event.
	watch(dirs, files map[string]uint64) (bool, error)
	events() <-chan struct{}
	errors() <-chan error
	close()
}

const (
	osQuiet  = 30 * time.Millisecond // with file events: a change waits for this long a calm before it builds
	watchEnv = "VUKA_WATCH"          // poll: no file events, a look every interval
)

// osSafetyNet is how often the sources are looked at with file events all
// the same.
var osSafetyNet = time.Second

func newWatcher(root string) *watcher {
	return &watcher{root: root, kick: make(chan struct{}, 1), after: time.After, poll: strings.EqualFold(os.Getenv(watchEnv), "poll")}
}

func (w *watcher) setEmbeds(roots []string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	added := false
	for _, r := range roots {
		if !slices.Contains(w.embeds, r) {
			w.fresh = append(w.fresh, r)
			added = true
		}
	}
	w.embeds = roots
	if added {
		select { // their files are watched once looked at
		case w.kick <- struct{}{}:
		default:
		}
	}
}

// relevant reports whether a file event about path may be a change to a
// source.
func (w *watcher) relevant(path string, dir bool) bool {
	if dir {
		return true
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.watched(path)
}

// skipDir reports whether the directory at path holds no sources: a
// private folder below pages/, _parts, does.
func (w *watcher) skipDir(path, name string) bool {
	rel, _ := filepath.Rel(w.root, path)
	return path != w.root && (strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") && !load.InPages(rel) ||
		name == "node_modules" || name == "testdata" || name == "vendor" || path == filepath.Join(w.root, "build") ||
		fileExists(filepath.Join(path, "go.mod")) || fileExists(filepath.Join(path, load.Marker)))
}

// watched reports whether a change to the file at path starts a build.
func (w *watcher) watched(path string) bool {
	name := filepath.Base(path)
	if strings.HasPrefix(name, ".") || routeFile(path) {
		return false // a route file is vuka's, written by the build it would start
	}
	switch filepath.Ext(name) {
	case ".vuka", ".go":
		if !strings.HasSuffix(name, "_test"+filepath.Ext(name)) {
			return true
		}
	case ".templ":
		return true
	case ".html":
		if strings.HasSuffix(name, ".vuka.html") {
			return true
		}
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
	dirs := make(map[string]uint64, len(w.dirs))
	var changed []string
	load.WalkDir(w.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // gone since its directory was read
		}
		if d.IsDir() {
			if w.skipDir(path, d.Name()) {
				return filepath.SkipDir
			}
			if fi, err := d.Info(); err == nil {
				dirs[path] = fileID(fi)
			}
			return nil
		}
		if !w.watched(path) {
			return nil
		}
		fi, err := entryInfo(path, d)
		if err != nil {
			return nil
		}
		st := stamp{fi.ModTime(), fi.Size(), fileID(fi)}
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
	w.files, w.dirs, w.fresh = files, dirs, nil
	return changed
}

// run sends each batch of changes once the sources have been calm for
// quiet. It looks at the sources when the system's file events say
// something changed, and else every interval (a tree that takes long to
// look at less often).
func (w *watcher) run(ctx context.Context, interval, quiet time.Duration, out chan<- []string) {
	if !w.poll {
		if ow, err := newOSWatch(w.relevant); err == nil {
			err := w.runEvents(ctx, ow, out)
			ow.close()
			if err == nil {
				return
			}
			fmt.Fprintf(os.Stderr, "● file events failed (%v); looking for changes every %v\n", err, interval)
		}
	}
	w.runPoll(ctx, interval, quiet, out)
}

func (w *watcher) runPoll(ctx context.Context, interval, quiet time.Duration, out chan<- []string) {
	var batch []string
	wait := interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.after(wait):
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

// runEvents looks at the sources when file events come, once they have
// been calm for osQuiet, and every osSafetyNet all the same. It returns
// nil when ctx is done, or why the file events stopped.
func (w *watcher) runEvents(ctx context.Context, ow osWatch, out chan<- []string) error {
	if _, err := w.sync(ow); err != nil {
		return err
	}
	w.events.Store(true)
	defer w.events.Store(false)
	// What changed between the first look and the watch is looked for now.
	pending, err := w.look(ow)
	if err != nil {
		return err
	}
	safety := osSafetyNet
	for {
		if len(pending) > 0 {
			select {
			case out <- pending:
				pending = nil
			case <-ctx.Done():
				return nil
			case err := <-ow.errors():
				return err
			case <-ow.events(): // a newer change: it joins this batch
				if err := w.calm(ctx, ow); err != nil {
					return err
				}
				changed, err := w.look(ow)
				if err != nil {
					return err
				}
				pending = append(pending, changed...)
			}
			continue
		}
		select {
		case <-ctx.Done():
			return nil
		case err := <-ow.errors():
			return err
		case <-ow.events():
			if err := w.calm(ctx, ow); err != nil {
				return err
			}
		case <-w.kick:
		case <-w.after(safety):
		}
		start := time.Now()
		changed, err := w.look(ow)
		if err != nil {
			return err
		}
		safety = max(osSafetyNet, 20*time.Since(start))
		pending = changed
	}
}

// calm waits until no file event has come for osQuiet: the writes of one
// save (a temporary file renamed over the source, a formatter's rewrite)
// are one change.
func (w *watcher) calm(ctx context.Context, ow osWatch) error {
	quiet := w.after(osQuiet)
	for {
		select {
		case <-ctx.Done():
			return nil
		case err := <-ow.errors():
			return err
		case <-ow.events():
			quiet = w.after(osQuiet)
		case <-quiet:
			return nil
		}
	}
}

// look looks at the sources and has the watch follow them: new directories
// and files watched, replaced ones watched anew. What changed in one before
// it was watched sent no event, so the sources are looked at again until the
// watch has nothing new to follow.
func (w *watcher) look(ow osWatch) ([]string, error) {
	var changed []string
	for range 4 {
		changed = append(changed, w.scan()...)
		added, err := w.sync(ow)
		if err != nil {
			return nil, err
		}
		if !added {
			break
		}
	}
	return changed, nil
}

// sync has the watch watch what the last look went through.
func (w *watcher) sync(ow osWatch) (bool, error) {
	w.mu.Lock()
	dirs := maps.Clone(w.dirs)
	files := make(map[string]uint64, len(w.files))
	for path, st := range w.files {
		files[path] = st.id
	}
	w.mu.Unlock()
	return ow.watch(dirs, files)
}

// traceStep prints, with VUKA_WATCH_TRACE set, how long a step of a
// rebuild took: how the time from a save to the new version answering
// divides, when it is measured.
func traceStep(format string, args ...any) {
	if os.Getenv(traceEnv) != "" {
		fmt.Fprintf(os.Stderr, "trace %s "+format+"\n", append([]any{time.Now().Format("15:04:05.000")}, args...)...)
	}
}

const traceEnv = "VUKA_WATCH_TRACE"
