// Command vuka transpiles Vuka source to Go and runs the go command over it.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

const usage = `vuka is Go with overloading and attributes.

Usage:
	vuka new <dir> [module path] [--ui=… --db=… --mail=… --auth=… --frontend=… --yes]
	        start an app: a few questions (or flags), then a project that runs,
	        its tests passing: pages, sign-in, a database with migrations
	vuka add module|service|component|island|page|entity … [--dry-run] [--force]
	        grow the app: a module (model, service, page, migration, test), a
	        service, a component, an island, a page (live, or backed by an
	        island), an orm entity and its migration; vuka add -h for each
	vuka ui init|add|list … [-f] [--dry-run]
	        components that are the app's own source: init writes the design
	        tokens into its stylesheet and components/ui/cn.go, add writes
	        components (button, card, dialog, tabs…) beside it; vuka ui -h
	vuka build|run|test|vet|install [go flags] [packages]
	        transpile the module's .vuka files into an overlay and run the go command;
	        vet runs vukavet's checks too (the app's go tool vukavet, else one
	        beside vuka or on PATH, else one it builds; -vettool=… overrides,
	        VUKA_VUKAVET=off for go vet's own);
	        an app on github.com/vuka-lang/web/v2 gets its Tailwind CSS (run keeps it
	        rebuilt, build embeds it; VUKA_TAILWIND=off for none); a Vite project in
	        frontend/ too (run starts its dev server, build runs vite build first;
	        VUKA_FRONTEND=off for none)
	vuka run -watch [-debug] [go flags] [package] [args]
	        run the app and rebuild it when a source changes: the new build
	        replaces it once it compiles, while the old one keeps serving
	        (-debug keeps the debug information watch builds leave out)
	vuka routes [-json]
	        list the folder routes, the pages and endpoints under pages/:
	        method, path, type, file:line and the middleware, guards and
	        layouts over each
	vuka graphql schema [-check file] [-o file] [package]
	        print the GraphQL schema the app serves (web.GraphQL) as SDL;
	        -check compares it with a committed one and fails on a change
	        that breaks clients
	vuka build-islands [-watch]
	        build the islands a module registers (in Go or Vuka) into its
	        frontend's build, for //go:embed: //go:generate go tool vuka
	        build-islands; -watch keeps them built for web.Dev
	vuka gen [-check] [-o build]
	        write build/: the module with Go in place of Vuka, for plain go tools
	vuka gen -inplace [-check] [dir | dir/...]
	        write the generated Go beside each .vuka file instead
	vuka eject [-rm] [file.vuka… | dir | dir/...]
	        gen -inplace for the files or packages named; -rm deletes each
	        .vuka file and keeps its Go (x.go) as the source, building with
	        plain go build
	vuka doctor [dir]
	        what the module's islands build with (the in-process esbuild,
	        Vite or Node) and what is missing for one to build
	vuka mod tidy|why|vendor|graph|… [args]
	        go mod, seeing the imports of .vuka files too (plain go mod tidy
	        drops requirements only Vuka code uses)
	vuka explain [-full] file.vuka
	        show each Vuka construct in file beside the Go it becomes
	vuka explain E0201
	        explain a diagnostic's code, with an example (vuka build/vet
	        -diag=json print diagnostics as JSON)
	vuka fix [-n] [fixer…]
	        apply the fixers (all by default); -n only says what would change
	vuka fmt [-l] [-w] [-d] [-imports] [paths…]
	        format .vuka files (the current directory by default), as gofmt
	        does Go: print them, write them back (-w), list those that
	        differ (-l) or show the diff (-d); -imports organizes their
	        imports first, as goimports does
	vuka lsp [-gopls path] [-log file] [-shared=false]
	        language server for .vuka files (gopls behind a proxy), over stdio;
	        run as gopls (a link named gopls) it is a drop-in gopls for .go files too
	vuka version
	        print this vuka's toolchain set and, in an app, the set its go.mod
	        requires (warning when its vuka, ui and web aren't one set)
	vuka upgrade [-n] [-no-test] [version]
	        move the app to a set (the latest by default): go get vuka, ui and
	        web at that version, run the vuka fix rewrites for the releases in
	        between, then vuka vet and vuka test; -n only says what it would do

Maintainers:
	vuka release [-yes] [-no-test] [-security] vX.Y.Z
	        release vuka, ui, web and cmd/vuka under one version, from a vuka
	        checkout with ui and web beside it; a dry run unless -yes
`

func main() {
	// Invoked as gopls (a link named gopls, for VS Code's go.alternateTools):
	// a drop-in gopls that also knows .vuka files.
	if strings.HasPrefix(filepath.Base(os.Args[0]), "gopls") {
		if err := asGopls(os.Args[1:]); err != nil {
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				os.Exit(exit.ExitCode())
			}
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	load.CompilerVersion = compilerVersion()
	var err error
	cmd := os.Args[1]
	switch cmd {
	case "build", "run", "test", "vet", "install":
		err = runGo(cmd, os.Args[2:], os.Stdout, os.Stderr)
	case "new":
		err = newProject(os.Args[2:], os.Stdin, os.Stdout)
	case "add":
		err = add(os.Args[2:], os.Stdout)
	case "ui":
		err = uiCmd(os.Args[2:], os.Stdout)
	case "gen":
		err = gen(os.Args[2:], os.Stdout)
	case "routes":
		err = routesCmd(os.Args[2:], os.Stdout, os.Stderr)
	case "graphql":
		err = graphqlCmd(os.Args[2:], os.Stdout, os.Stderr)
	case "build-islands":
		err = buildIslands(os.Args[2:], os.Stdout, os.Stderr)
	case "eject":
		err = eject(os.Args[2:], os.Stdout)
	case "lsp":
		err = lsp(os.Args[2:])
	case "mod":
		err = mod(os.Args[2:])
	case "explain":
		err = explain(os.Args[2:], os.Stdout)
	case "doctor":
		err = doctor(os.Args[2:], os.Stdout)
	case "fix":
		err = fix(os.Args[2:], os.Stdout)
	case "fmt":
		err = vukaFmt(os.Args[2:], os.Stdout, os.Stderr)
	case "version":
		err = versionCmd(os.Args[2:], os.Stdout, os.Stderr)
	case "upgrade":
		err = upgrade(os.Args[2:], os.Stdout, os.Stderr)
	case "release":
		err = release(os.Args[2:], os.Stdout, os.Stderr)
	case "help", "-h", "-help", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "vuka: unknown command %q\n\n%s", cmd, usage)
		os.Exit(2)
	}
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			os.Exit(exit.ExitCode())
		}
		var st exitStatus
		if errors.As(err, &st) {
			os.Exit(int(st))
		}
		printErr(os.Stderr, err)
		os.Exit(1)
	}
}

// exitStatus ends vuka with a status, having said why already.
type exitStatus int

func (s exitStatus) Error() string { return fmt.Sprintf("exit status %d", int(s)) }

// runGo transpiles every Vuka package in the current module and runs
// `go <cmd> -overlay=…` so the generated files exist only for the compiler.
// vuka build also brings the build/ module up to date.
func runGo(cmd string, args []string, stdout, stderr io.Writer) error {
	args = cutDiagFlag(args)
	if cmd == "run" {
		if len(args) > 0 && slices.Contains([]string{"-h", "-help", "--help"}, args[0]) {
			fmt.Fprint(stdout, runUsage)
			return nil
		}
		if args, watch := cutWatch(args); watch {
			return runWatch(args, stdout, stderr)
		}
	}
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return err
	}
	pa, err := findAssets(root, modPath)
	if err != nil {
		return err
	}
	var vetFailed bool
	var scope *vetScope // nil: the whole module's report
	if cmd == "vet" {
		scope = newVetScope(root, modPath, pkgs, args)
		var ds []diag.Diagnostic
		if pa != nil {
			ds = vetIslands(root, modPath, pa)
			ds = append(ds, vetScripts(root, modPath, pa)...)
		}
		ds = append(ds, routesUnserved(root, modPath, scanRoutes(root, modPath, nil))...)
		if ds = scope.warnings(append(ds, vetLivePages(root, pkgs)...)); len(ds) > 0 {
			writeDiags(stderr, ds, colorFor(stderr))
			for _, d := range ds {
				vetFailed = vetFailed || d.Severity == diag.Error
			}
		}
	}
	goArgs := []string{cmd}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	routes := scanRoutes(root, modPath, nil)
	if pkgs, err = routes.stub(pkgs); err != nil {
		return err
	}
	var gens []load.Generated
	var overlay string
	ro := reportOptions{color: colorFor(stderr), verbose: slices.Contains(args, "-v"), all: cmd == "vet"}
	if len(pkgs) > 0 {
		if gens, overlay, err = cachedTranspile(root)(pkgs, tmp, loadOptions(pa)); err != nil {
			in := buildInput{root, modPath, overlay, gens}
			rep := transpileReport(in, err)
			left := scope.report(rep, in)
			if scope == nil || rep == nil || !rep.empty() {
				err = failedBuild(stderr, rep, err, ro)
				writeLeftOut(stderr, left)
				return err
			}
			// Only packages it wasn't asked about fail: the asked ones,
			// transpiled, are vetted.
			writeLeftOut(stderr, left)
		}
	}
	if cmd == "vet" {
		if ds := scope.warnings(transpileWarnings(root, gens)); len(ds) > 0 {
			writeDiags(stderr, ds, colorFor(stderr))
		}
	}
	if gens, overlay, err = routes.apply(gens, overlay, tmp); err != nil {
		return err
	}
	if cmd == "run" {
		if _, err := routes.writeFiles(); err != nil {
			return err
		}
	}
	var linkMsg string
	if overlay, gens, linkMsg, err = autoLink(root, modPath, tmp, overlay, pkgs, gens); err != nil {
		return err
	}
	overlay, gens = typedFields(root, tmp, overlay, gens, cmd == "test" || cmd == "vet", stderr)
	if calls := newGens(gens, goCallAdapters(root)); len(calls) > 0 {
		if overlay, err = addToOverlay(overlay, tmp, calls); err != nil {
			return err
		}
		gens = append(gens, calls...)
	}
	if linkMsg != "" && (cmd == "run" || cmd == "build" || cmd == "install") {
		fmt.Fprintln(stderr, linkMsg)
	}
	var env []string
	stopTailwind, stopFrontend := func() {}, func() {}
	switch cmd {
	case "build", "install":
		if err := buildFrontend(root, stderr); err != nil {
			return err
		}
		css, err := buildTailwind(root, overlay, pa, stderr)
		if err != nil {
			return err
		}
		if len(css) > 0 {
			if overlay, err = addToOverlay(overlay, tmp, css); err != nil {
				return err
			}
			gens = append(gens, css...)
		}
	case "run":
		if _, stub := stubbing(nil); stub {
			if overlay, err = stubEmbeds(overlay, tmp, frontendEmbeds(root)); err != nil {
				return err
			}
		}
		env = append(env, devDirEnv+"="+tmp)
		var css string
		assets := newDevAssets(root, stderr)
		defer assets.close()
		assets.set(pa)
		env = append(env, assetsEnv+"="+assets.dir())
		css, stopTailwind = devTailwind(root, overlay, stderr, assets)
		defer stopTailwind()
		if css != "" {
			env = append(env, twCSSEnv+"="+css)
		}
		stopFrontend = devFrontend(root, stderr)
		defer stopFrontend()
	}
	if cmd == "build" && len(pkgs) > 0 {
		if _, err := load.Mirror(root, filepath.Join(root, "build"), gens, false); err != nil {
			return err
		}
	}
	if overlay != "" {
		goArgs = append(goArgs, "-overlay="+overlay)
	}
	if cmd == "vet" && !hasVettool(args) {
		tool, err := vetTool(root, stderr)
		if err != nil {
			return err
		}
		if tool != "" {
			goArgs = append(goArgs, "-vettool="+tool)
		}
	}
	if cmd == "test" || cmd == "vet" {
		done, err := overlayDirs(overlay)
		defer done()
		if err != nil {
			return err
		}
	}
	if cmd != "run" {
		args = goPatterns(root, args)
	}
	c := exec.Command("go", append(goArgs, args...)...)
	c.Env = append(os.Environ(), env...)
	// The overlay names real paths; run go from the real directory, or in a
	// symlinked one (macOS's /tmp is /private/tmp) it finds no files.
	if wd, err := os.Getwd(); err == nil {
		c.Dir = wd
		if real, err := filepath.EvalSymlinks(wd); err == nil {
			c.Dir, c.Env = real, append(c.Env, "PWD="+real)
		}
	}
	r := &respeller{w: stderr, dir: c.Dir, gens: gens}
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, stdout, r
	// The compile errors of the module's packages are reported at their
	// source once the go command is done; go test's stay in its output.
	var blocks *blockWriter
	if cmd != "test" {
		blocks = &blockWriter{next: r, respell: r.respell}
		c.Stderr = blocks
	}
	if cmd == "run" {
		// A signal stops what vuka started beside the program and goes on to
		// the go command; vuka waits for it to exit.
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		defer signal.Stop(sig)
		if err = c.Start(); err == nil {
			go func() {
				for s := range sig {
					stopTailwind()
					c.Process.Signal(s)
					stopFrontend()
				}
			}()
			err = c.Wait()
		}
	} else {
		err = c.Run()
	}
	if blocks != nil {
		blocks.flush()
		blocks.report(stderr, err, buildInput{root, modPath, overlay, gens}, ro)
	}
	r.flush()
	if err == nil && vetFailed {
		return exitStatus(1)
	}
	return err
}

// cachedTranspile is load.Transpile for the module at root, through a
// session keeping its work in the user's cache directory: a package whose
// inputs are as a run saw them before isn't transpiled again
// (VUKA_CACHE=off: every package is).
func cachedTranspile(root string) func([]*load.Package, string, load.Options) ([]load.Generated, string, error) {
	sess, err := load.NewSession(root)
	if err != nil {
		return load.Transpile
	}
	sess.UseCache("")
	return sess.Transpile
}

// compilerVersion names this vuka for the transpile cache: its version and
// its executable, so a rebuilt vuka never reads what another wrote.
func compilerVersion() string {
	v := version
	if exe, err := os.Executable(); err == nil {
		if fi, err := os.Stat(exe); err == nil {
			v += fmt.Sprintf(" %s %d %d", exe, fi.Size(), fi.ModTime().UnixNano())
		}
	}
	return v
}

// overlayDirs makes the directories of the overlay's files that aren't on
// disk — a bracketed, grouped or private folder's Go name, param_id — for
// the go command's test and vet, which run their tools in a package's
// directory. Another vuka may be using the same directories (the editor's
// language server, a second vuka test): each one this run needs gets a
// mark of its own, a hidden file naming the process, which keeps another
// vuka from removing it as empty (routeTree.goDirs) until done removes the
// mark and then each directory left empty.
func overlayDirs(overlay string) (done func(), err error) {
	mark := inUseMark + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(markSeq.Add(1), 10)
	var marked []string
	made := map[string]bool{}
	done = func() {
		for _, d := range marked {
			os.Remove(filepath.Join(d, mark))
			// os.Remove leaves a directory holding anything: files, or
			// another vuka's mark.
			for p := d; made[p] || goNamed(p); p = filepath.Dir(p) {
				if os.Remove(p) != nil {
					break
				}
			}
		}
	}
	ov, err := readOverlay(overlay)
	if err != nil {
		return done, err
	}
	dirs := map[string]bool{}
	for target, file := range ov {
		if file != "" {
			dirs[filepath.Dir(target)] = true
		}
	}
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		if fileExists(dir) && !goNamed(dir) {
			continue
		}
		for p := dir; !fileExists(p); p = filepath.Dir(p) {
			made[p] = true
		}
		// Another vuka's done may remove the directory between the two
		// steps: make it again.
		for try := 0; ; try++ {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return done, err
			}
			err := os.WriteFile(filepath.Join(dir, mark), nil, 0o644)
			if err == nil {
				break
			}
			if !errors.Is(err, fs.ErrNotExist) || try == 10 {
				return done, err
			}
		}
		marked = append(marked, dir)
	}
	return done, nil
}

// goPatterns are args with each package pattern naming a folder the go
// command sees by its Go name — ./pages/(admin), ./pages/items/[id]/... —
// written with it: ./pages/group_admin, ./pages/items/param_id/....
func goPatterns(root string, args []string) []string {
	root = realPathOf(root)
	out := slices.Clone(args)
	for i, a := range args {
		if !strings.HasPrefix(a, ".") && !filepath.IsAbs(a) || !strings.ContainsAny(a, "[(_") {
			continue
		}
		dir, rest := a, ""
		if d, ok := strings.CutSuffix(a, "/..."); ok {
			dir, rest = d, "/..."
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			continue
		}
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			abs = real
		}
		if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
			continue
		}
		g := load.GoDir(root, abs)
		if g == abs {
			continue
		}
		if !filepath.IsAbs(dir) {
			wd, err := os.Getwd()
			if err != nil {
				continue
			}
			if real, err := filepath.EvalSymlinks(wd); err == nil {
				wd = real
			}
			rel, err := filepath.Rel(wd, g)
			if err != nil {
				continue
			}
			g = rel
			if !strings.HasPrefix(g, ".") {
				g = "." + string(filepath.Separator) + g
			}
		}
		out[i] = filepath.ToSlash(g) + rest
	}
	return out
}

// inUseMark starts the name of the file marking a Go-name directory as
// used by a running vuka; the process ID and a count of the process's
// marks follow.
const inUseMark = ".vuka-inuse-"

var markSeq atomic.Int64

// goNamed reports whether dir is, or is below, the Go name of a bracketed,
// grouped or private folder: a directory vuka makes for the go command.
func goNamed(dir string) bool {
	for p := dir; p != filepath.Dir(p); p = filepath.Dir(p) {
		if load.IsGoName(p) {
			return true
		}
	}
	return false
}

// clearStaleMarks removes the marks below dir of vuka processes that have
// exited, so the directories they marked can be tidied.
func clearStaleMarks(dir string) {
	filepath.WalkDir(dir, func(path string, e fs.DirEntry, err error) error {
		if err != nil || e.IsDir() {
			return nil
		}
		if rest, ok := strings.CutPrefix(e.Name(), inUseMark); ok {
			pid, _, _ := strings.Cut(rest, "-")
			if n, err := strconv.Atoi(pid); err != nil || !processAlive(n) {
				os.Remove(path)
			}
		}
		return nil
	})
}

// respeller passes the go command's errors on, with the code they quote in a
// .vuka file spelt as the source spells it (transpile.SourceMap.Message).
type respeller struct {
	w    io.Writer
	dir  string
	gens []load.Generated
	buf  []byte
}

func (r *respeller) Write(p []byte) (int, error) {
	r.buf = append(r.buf, p...)
	for {
		i := bytes.IndexByte(r.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := string(r.buf[:i+1])
		r.buf = r.buf[i+1:]
		if _, err := io.WriteString(r.w, r.respell(line)); err != nil {
			return len(p), err
		}
	}
}

func (r *respeller) flush() {
	if len(r.buf) > 0 {
		io.WriteString(r.w, r.respell(string(r.buf)))
		r.buf = nil
	}
}

// respell rewrites one line of the form path.vuka:line:col: message.
func (r *respeller) respell(line string) string {
	m := vukaErr.FindStringSubmatchIndex(line)
	if m == nil {
		return line
	}
	path, n := line[m[2]:m[3]], 0
	fmt.Sscan(line[m[4]:m[5]], &n)
	if !filepath.IsAbs(path) {
		path = filepath.Join(r.dir, path)
	}
	for _, g := range r.gens {
		if g.Map != nil && sameFile(g.Source, path) {
			return line[:m[6]] + g.Map.Message(n, line[m[6]:m[7]]) + line[m[7]:]
		}
	}
	return line
}

var vukaErr = regexp.MustCompile(`^\s*(\S+\.vuka):(\d+):(?:\d+:)? (.*?)\r?\n?$`)

func sameFile(a, b string) bool {
	if filepath.Clean(a) == filepath.Clean(b) {
		return true
	}
	ra, ok1 := load.RealPath(a)
	rb, ok2 := load.RealPath(b)
	return ok1 && ok2 && ra == rb
}

// gen writes the build/ module: the module with each .vuka file replaced by
// its generated Go, which plain Go tools build. -inplace writes the generated
// files beside the .vuka files instead. With -check it writes nothing and
// fails if anything is out of date.
func gen(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	check := fs.Bool("check", false, "report generated files that are missing or stale, write nothing")
	inplace := fs.Bool("inplace", false, "write each generated file beside its .vuka file instead of the build module")
	out := fs.String("o", "build", "the build module's directory, relative to the module root")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if !*inplace {
		return genModule(*out, *check, stdout)
	}
	patterns := fs.Args()
	if len(patterns) == 0 {
		patterns = []string{"./..."}
	}
	root, files, err := inPlace(patterns, nil, load.Options{})
	if err != nil {
		return err
	}
	stale := 0
	for _, f := range files {
		old, err := os.ReadFile(f.Target)
		if err == nil && bytes.Equal(old, f.Src) {
			continue
		}
		rel, _ := filepath.Rel(root, f.Target)
		if *check {
			fmt.Fprintln(stdout, "stale:", rel)
			stale++
			continue
		}
		if err := os.MkdirAll(filepath.Dir(f.Target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(f.Target, f.Src, 0o644); err != nil {
			return err
		}
		fmt.Fprintln(stdout, "wrote", rel)
	}
	if stale > 0 {
		return fmt.Errorf("%d generated file(s) out of date; run vuka gen", stale)
	}
	return nil
}

// inPlace transpiles the packages patterns name (dir or dir/...) and
// returns the files vuka gen -inplace writes for them: each generated file
// beside its source, line directives naming it by its base name, plus the
// link files when the patterns cover the module. Extra packages (dirs of
// single files eject names) are transpiled too, their files left to the
// caller to pick.
func inPlace(patterns, extra []string, lo load.Options) (string, []load.Generated, error) {
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return "", nil, err
	}
	var roots []*load.Package
	asked, dirs := map[string]bool{}, map[string]bool{}
	for _, p := range append(slices.Clip(patterns), extra...) {
		dir, recursive := strings.CutSuffix(p, "/...")
		if dir == "" || dir == "." && recursive {
			dir = "."
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return "", nil, err
		}
		found, err := load.Discover(root, modPath, dir, recursive, nil)
		if err != nil {
			return "", nil, err
		}
		for _, p := range found {
			if !asked[p.ImportPath] {
				asked[p.ImportPath], dirs[p.BuildDir()] = true, true
				roots = append(roots, p)
			}
		}
	}
	// What they import is transpiled too, through the module's Go packages.
	all, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return "", nil, err
	}
	all = append(slices.DeleteFunc(all, func(p *load.Package) bool { return asked[p.ImportPath] }), roots...)
	pkgs := load.Closure(all, roots)
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return "", nil, err
	}
	defer os.RemoveAll(tmp)
	pa, err := findAssets(root, modPath)
	if err != nil {
		return "", nil, err
	}
	opts := loadOptions(pa)
	routes := scanRoutes(root, modPath, nil)
	if pkgs, err = routes.stub(pkgs); err != nil {
		return "", nil, err
	}
	files, overlay, err := load.Transpile(pkgs, tmp, opts)
	if err != nil {
		return "", nil, err
	}
	if files, overlay, err = routes.apply(files, overlay, tmp); err != nil {
		return "", nil, err
	}
	routeFiles := routes.files()
	// The link files need the whole module's declarer packages, so they are
	// written (and checked) only when the gen covers the module; a narrower
	// gen leaves them as they are.
	var links []load.Generated
	if slices.ContainsFunc(patterns, func(p string) bool { return moduleWide(root, p) }) {
		if links, _, err = load.AutoLink(root, modPath, pkgs, files); err != nil {
			return "", nil, err
		}
	}
	css, err := buildTailwind(root, overlay, pa, os.Stderr)
	if err != nil {
		return "", nil, err
	}
	fields, err := ormMissing(root, overlay, files, true, true)
	if err != nil {
		return "", nil, fmt.Errorf("the models' typed fields: %w", err)
	}
	calls := newGens(files, goCallAdapters(root))
	files = slices.DeleteFunc(files, func(g load.Generated) bool {
		return !dirs[filepath.Dir(g.Target)] || slices.ContainsFunc(routeFiles, func(r load.Generated) bool { return r.Target == g.Target })
	})
	for _, g := range append(append(append(append(links, css...), fields...), calls...), routeFiles...) {
		if slices.ContainsFunc(patterns, func(p string) bool { return inPattern(filepath.Dir(g.Target), p) }) {
			files = append(files, g)
		}
	}
	for i := range files {
		files[i].Src = relativeLines(files[i].Src, filepath.Dir(files[i].Source), filepath.Dir(files[i].Target))
		if strings.HasSuffix(files[i].Source, ".vuka") {
			files[i].Src = arrange(files[i].Src)
		}
	}
	return root, files, nil
}

// moduleWide reports whether a package pattern of vuka gen -inplace covers
// the whole module: recursive from its root.
func moduleWide(root, pattern string) bool {
	base, recursive := strings.CutSuffix(pattern, "/...")
	if !recursive {
		return false
	}
	if base == "" {
		base = "."
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	return realPathOf(abs) == realPathOf(root)
}

// inPattern reports whether dir is matched by a package pattern of vuka gen
// -inplace (a directory, or dir/... for the tree below it).
func inPattern(dir, pattern string) bool {
	base, recursive := strings.CutSuffix(pattern, "/...")
	if base == "" {
		base = "."
	}
	abs, err := filepath.Abs(base)
	if err != nil {
		return false
	}
	abs, dir = realPathOf(abs), realPathOf(dir)
	return dir == abs || recursive && strings.HasPrefix(dir, abs+string(filepath.Separator))
}

func realPathOf(p string) string {
	if r, ok := load.RealPath(p); ok {
		return r
	}
	// A path not on disk, a [id] folder's Go directory: its parent's.
	if parent := filepath.Dir(p); parent != p && filepath.IsAbs(p) {
		return filepath.Join(realPathOf(parent), filepath.Base(p))
	}
	return p
}

// relativeLines makes the line directives of a file generated in target
// for a source in dir name the source relative to target: by its base name
// beside it, ../[id]/x.vuka for a parameter folder's (whose Go is in param_id).
// The compiler reads a relative name against the directive's own
// directory, and a committed file then doesn't carry the path of the
// checkout it was generated in.
func relativeLines(src []byte, dir, target string) []byte {
	prefix := filepath.ToSlash(dir) + "/"
	to := ""
	if rel, err := filepath.Rel(target, dir); err == nil && rel != "." {
		to = filepath.ToSlash(rel) + "/"
	}
	src = bytes.ReplaceAll(src, []byte("//line "+prefix), []byte("//line "+to))
	return bytes.ReplaceAll(src, []byte("/*line "+prefix), []byte("/*line "+to))
}

func genModule(out string, check bool, stdout io.Writer) error {
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return err
	}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	pa, err := findAssets(root, modPath)
	if err != nil {
		return err
	}
	routes := scanRoutes(root, modPath, nil)
	if pkgs, err = routes.stub(pkgs); err != nil {
		return err
	}
	gens, overlay, err := cachedTranspile(root)(pkgs, tmp, loadOptions(pa))
	if err != nil {
		return err
	}
	if gens, overlay, err = routes.apply(gens, overlay, tmp); err != nil {
		return err
	}
	links, _, err := load.AutoLink(root, modPath, pkgs, gens)
	if err != nil {
		return err
	}
	gens = append(gens, links...)
	css, err := buildTailwind(root, overlay, pa, os.Stderr)
	if err != nil {
		return err
	}
	gens = append(gens, css...)
	if !filepath.IsAbs(out) {
		out = filepath.Join(root, out)
	}
	res, err := load.Mirror(root, out, gens, check)
	if err != nil {
		return err
	}
	rel, _ := filepath.Rel(root, out)
	if check {
		for _, f := range res.Written {
			fmt.Fprintln(stdout, "stale:", filepath.Join(rel, f))
		}
		for _, f := range res.Removed {
			fmt.Fprintln(stdout, "extra:", filepath.Join(rel, f))
		}
		if n := len(res.Written) + len(res.Removed); n > 0 {
			return fmt.Errorf("%s is out of date (%d file(s)); run vuka gen", rel, n)
		}
		return nil
	}
	fmt.Fprintf(stdout, "%s: %d written, %d removed\n", rel, len(res.Written), len(res.Removed))
	return nil
}
