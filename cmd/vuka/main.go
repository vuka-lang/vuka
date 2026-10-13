// Command vuka transpiles Vuka source to Go and runs the go command over it.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/vuka-lang/vuka/internal/load"
)

const version = "v0.26.0"

const usage = `vuka is Go with overloading and attributes.

Usage:
	vuka new <dir> [module path] [--ui=… --db=… --mail=… --auth=… --frontend=… --yes]
	        start an app: a few questions (or flags), then a project that runs,
	        its tests passing: pages, sign-in, a database with migrations
	vuka add module <name>
	        grow the app: a module with a model, service, page, migration and test
	vuka build|run|test|vet|install [go flags] [packages]
	        transpile the module's .vuka files into an overlay and run the go command;
	        an app on github.com/vuka-lang/web gets its Tailwind CSS (run keeps it
	        rebuilt, build embeds it; VUKA_TAILWIND=off for none); a Vite project in
	        frontend/ too (run starts its dev server, build runs vite build first;
	        VUKA_FRONTEND=off for none)
	vuka run -watch [-debug] [go flags] [package] [args]
	        run the app and rebuild it when a source changes: the new build
	        replaces it once it compiles, while the old one keeps serving
	        (-debug keeps the debug information watch builds leave out)
	vuka gen [-check] [-o build]
	        write build/: the module with Go in place of Vuka, for plain go tools
	vuka gen -inplace [-check] [dir | dir/...]
	        write the generated Go beside each .vuka file instead
	vuka mod tidy|why|vendor|graph|… [args]
	        go mod, seeing the imports of .vuka files too (plain go mod tidy
	        drops requirements only Vuka code uses)
	vuka explain [-full] file.vuka
	        show each Vuka construct in file beside the Go it becomes
	vuka fix [-n] [fixer…]
	        apply the fixers (all by default); -n only says what would change
	vuka fmt [-l] [-w] [-d] [paths…]
	        format .vuka files (the current directory by default), as gofmt
	        does Go: print them, write them back (-w), list those that
	        differ (-l) or show the diff (-d)
	vuka lsp [-gopls path] [-log file] [-shared=false]
	        language server for .vuka files (gopls behind a proxy), over stdio;
	        run as gopls (a link named gopls) it is a drop-in gopls for .go files too
	vuka version
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
	var err error
	cmd := os.Args[1]
	switch cmd {
	case "build", "run", "test", "vet", "install":
		err = runGo(cmd, os.Args[2:], os.Stdout, os.Stderr)
	case "new":
		err = newProject(os.Args[2:], os.Stdin, os.Stdout)
	case "add":
		err = add(os.Args[2:], os.Stdout)
	case "gen":
		err = gen(os.Args[2:], os.Stdout)
	case "lsp":
		err = lsp(os.Args[2:])
	case "mod":
		err = mod(os.Args[2:])
	case "explain":
		err = explain(os.Args[2:], os.Stdout)
	case "fix":
		err = fix(os.Args[2:], os.Stdout)
	case "fmt":
		err = vukaFmt(os.Args[2:], os.Stdout, os.Stderr)
	case "version":
		fmt.Println("vuka", version)
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
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// runGo transpiles every Vuka package in the current module and runs
// `go <cmd> -overlay=…` so the generated files exist only for the compiler.
// vuka build also brings the build/ module up to date.
func runGo(cmd string, args []string, stdout, stderr io.Writer) error {
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
	goArgs := []string{cmd}
	tmp, err := os.MkdirTemp("", "vuka-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	var gens []load.Generated
	var overlay string
	if len(pkgs) > 0 {
		if gens, overlay, err = load.Transpile(pkgs, tmp, loadOptions(pa)); err != nil {
			return err
		}
	}
	var linkMsg string
	if overlay, gens, linkMsg, err = autoLink(root, modPath, tmp, overlay, pkgs, gens); err != nil {
		return err
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
	r.flush()
	return err
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
	ra, err1 := filepath.EvalSymlinks(a)
	rb, err2 := filepath.EvalSymlinks(b)
	return err1 == nil && err2 == nil && ra == rb
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
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	var pkgs []*load.Package
	for _, p := range patterns {
		dir, recursive := strings.CutSuffix(p, "/...")
		if dir == "" || dir == "." && recursive {
			dir = "."
		}
		dir, err := filepath.Abs(dir)
		if err != nil {
			return err
		}
		found, err := load.Discover(root, modPath, dir, recursive, nil)
		if err != nil {
			return err
		}
		pkgs = append(pkgs, found...)
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
	files, overlay, err := load.Transpile(pkgs, tmp, loadOptions(pa))
	if err != nil {
		return err
	}
	// The link files need the whole module's declarer packages, so they are
	// written (and checked) only when the gen covers the module; a narrower
	// gen leaves them as they are.
	var links []load.Generated
	if slices.ContainsFunc(patterns, func(p string) bool { return moduleWide(root, p) }) {
		if links, _, err = load.AutoLink(root, modPath, pkgs, files); err != nil {
			return err
		}
	}
	css, err := buildTailwind(root, overlay, pa, os.Stderr)
	if err != nil {
		return err
	}
	for _, g := range append(links, css...) {
		if slices.ContainsFunc(patterns, func(p string) bool { return inPattern(filepath.Dir(g.Target), p) }) {
			files = append(files, g)
		}
	}
	stale := 0
	for _, f := range files {
		f.Src = relativeLines(f.Src, filepath.Dir(f.Source))
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
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// relativeLines makes the line directives of a file generated beside its
// source name the source by its base name: the compiler reads a relative
// name against the directive's own directory, and a committed file then
// doesn't carry the path of the checkout it was generated in.
func relativeLines(src []byte, dir string) []byte {
	prefix := filepath.ToSlash(dir) + "/"
	src = bytes.ReplaceAll(src, []byte("//line "+prefix), []byte("//line "))
	return bytes.ReplaceAll(src, []byte("/*line "+prefix), []byte("/*line "))
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
	gens, overlay, err := load.Transpile(pkgs, tmp, loadOptions(pa))
	if err != nil {
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
