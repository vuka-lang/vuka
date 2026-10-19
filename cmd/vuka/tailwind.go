package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/internal/load"
)

// Tailwind CSS for apps on github.com/vuka-lang/web: vuka run keeps the
// stylesheet rebuilt (tailwindcss --watch) and names the file to the app in
// VUKA_TAILWIND_CSS; vuka build builds it once, minified, and embeds it with
// a file generated in each main package (web.RegisterCSS), added through the
// overlay; vuka gen writes that file. The stylesheet is the project's app.css
// or styles/app.css (or web.Tailwind's path) when it imports tailwindcss, else
// `@import "tailwindcss"`, with an @source for the module's files and those
// of the dependencies shipping components.

const (
	webPath     = "github.com/vuka-lang/web"
	twEnv       = "VUKA_TAILWIND"     // off: no Tailwind
	twCSSEnv    = "VUKA_TAILWIND_CSS" // web reads the development stylesheet's path from it
	twGoName    = "vuka_tailwind_gen.go"
	twInstall   = "Tailwind isn't installed: brew install tailwindcss (or npm i -D @tailwindcss/cli)"
	twCacheDir  = ".vuka/tailwind"
	twEntryName = "input.css"
)

var (
	twNoOption   = regexp.MustCompile(`\bweb\.NoTailwind\(\)`)
	twPathOption = regexp.MustCompile(`\bweb\.Tailwind\(\s*("(?:[^"\\]|\\.)*"|` + "`[^`]*`" + `)\s*\)`)
	twRequire    = regexp.MustCompile(`(?m)^\s*(?:require\s+)?` + regexp.QuoteMeta(webPath) + `\s+v\S+`)
	twImport     = regexp.MustCompile(`@import\s+["']tailwindcss["']`)
)

// tailwind is a module's Tailwind setup.
type tailwind struct {
	root  string
	sheet string // the project's stylesheet; "" for the built-in one
}

// tailwindFor reports how the module at root uses Tailwind: nil when it
// doesn't require web, VUKA_TAILWIND=off, a source calls web.NoTailwind(), or
// it has no stylesheet and no views (.templ, .html files or a use of ui).
func tailwindFor(root string) (*tailwind, error) {
	if strings.EqualFold(os.Getenv(twEnv), "off") {
		return nil, nil
	}
	if mod, err := os.ReadFile(filepath.Join(root, "go.mod")); err != nil || !twRequire.Match(mod) {
		return nil, nil
	}
	tw, named, views, off := &tailwind{root: root}, "", false, false
	err := walkSources(root, func(path, name string) error {
		switch filepath.Ext(name) {
		case ".templ", ".html":
			views = true
		case ".vuka", ".go":
			if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.vuka") || name == twGoName {
				return nil
			}
			src, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			off = off || twNoOption.Match(src)
			if m := twPathOption.FindSubmatch(src); m != nil && named == "" {
				named, _ = strconv.Unquote(string(m[1]))
			}
			views = views || bytes.Contains(src, []byte(`"`+load.UIPath))
		}
		return nil
	})
	if err != nil || off {
		return nil, err
	}
	if named != "" {
		tw.sheet = filepath.Join(root, filepath.FromSlash(named))
		if _, err := os.Stat(tw.sheet); err != nil {
			return nil, fmt.Errorf("web.Tailwind(%q): %w", named, err)
		}
		return tw, nil
	}
	for _, c := range []string{"app.css", "styles/app.css"} {
		p := filepath.Join(root, filepath.FromSlash(c))
		if css, err := os.ReadFile(p); err == nil && twImport.Match(css) {
			tw.sheet = p
			return tw, nil
		}
	}
	if !views {
		return nil, nil
	}
	return tw, nil
}

// walkSources calls fn for each file of the module at root, as walkModule
// does, leaving out _ directories, testdata and examples too.
func walkSources(root string, fn func(path, name string) error) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			if path == root {
				return nil
			}
			if strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_") || name == "node_modules" || name == "testdata" ||
				name == "examples" || name == "vendor" || fileExists(filepath.Join(path, "go.mod")) || fileExists(filepath.Join(path, load.Marker)) ||
				fileExists(filepath.Join(path, "package.json")) { // a frontend's: its .html files aren't views
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type().IsRegular() {
			return fn(path, name)
		}
		return nil
	})
}

// tailwindBinary finds the Tailwind CLI: tailwindcss on PATH, else the npm
// package's in the module or its frontend/ directory.
func tailwindBinary(root string) (string, error) {
	if p, err := exec.LookPath("tailwindcss"); err == nil {
		return p, nil
	}
	for _, dir := range []string{root, filepath.Join(root, "frontend")} {
		p := filepath.Join(dir, "node_modules", ".bin", "tailwindcss")
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}
	return "", errors.New(twInstall)
}

// goPackages is what go list says of the module's packages and their
// dependencies.
type goPackages struct {
	mains []string // directories of main packages depending on web
	deps  []string // directories of the packages of other modules
	web   string   // web's module directory
}

func listPackages(root, overlay string) (goPackages, error) {
	args := []string{"list", "-e", "-deps", "-f",
		`{{.Name}}	{{.Dir}}	{{if .Module}}{{.Module.Path}}	{{.Module.Dir}}	{{.Module.Main}}{{end}}	{{if not .DepOnly}}{{join .Deps " "}}{{end}}`}
	if overlay != "" {
		args = append(args, "-overlay="+overlay)
	}
	c := exec.Command("go", append(args, "./...")...)
	c.Dir = root
	var stderr bytes.Buffer
	c.Stderr = &stderr
	out, err := c.Output()
	if err != nil {
		return goPackages{}, fmt.Errorf("go list: %v\n%s", err, stderr.Bytes())
	}
	var res goPackages
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Split(line, "\t")
		switch {
		case len(f) < 6:
		case f[4] == "false":
			res.deps = append(res.deps, f[1])
			if f[2] == webPath {
				res.web = f[3]
			}
		case f[0] == "main" && slices.Contains(strings.Fields(f[5]), webPath):
			res.mains = append(res.mains, f[1])
		}
	}
	return res, nil
}

// componentDirs are the directories in dirs holding .vuka or .templ files:
// the packages of component libraries the app links, whose class names the
// stylesheet needs.
func componentDirs(dirs []string) []string {
	var out []string
	for _, dir := range dirs {
		entries, _ := os.ReadDir(dir)
		if slices.ContainsFunc(entries, func(e fs.DirEntry) bool {
			n := e.Name()
			return strings.HasSuffix(n, ".templ") || strings.HasSuffix(n, ".vuka") && !strings.HasSuffix(n, "_test.vuka")
		}) {
			out = append(out, dir)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// writeEntry writes the stylesheet Tailwind builds, in the module's
// .vuka/tailwind (git-ignored by its own .gitignore), and returns its path.
// css are the pages' stylesheets (compileAssets), imported after the app's
// in order: the global ones, then each component's, scoped.
func (tw *tailwind) writeEntry(deps, css []string) (string, error) {
	dir := filepath.Join(tw.root, filepath.FromSlash(twCacheDir))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := writeIfChanged(filepath.Join(tw.root, ".vuka", ".gitignore"), []byte("*\n")); err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString("/* Generated by vuka: the app's stylesheet and the files Tailwind scans for class names. */\n")
	if tw.sheet != "" {
		fmt.Fprintf(&b, "@import %q;\n", filepath.ToSlash(tw.sheet))
	} else {
		b.WriteString("@import \"tailwindcss\";\n")
	}
	for _, f := range css {
		fmt.Fprintf(&b, "@import %q;\n", filepath.ToSlash(f))
	}
	fmt.Fprintf(&b, "@source %q;\n", filepath.ToSlash(tw.root)+"/**/*.{vuka,templ,html,go}")
	// The stylesheet and the assets generated last time are output, not
	// sources: their CSS would add classes to the next build.
	fmt.Fprintf(&b, "@source not %q;\n", filepath.ToSlash(tw.root)+"/**/{"+twGoName+","+assetsGoName+"}")
	for _, d := range deps {
		fmt.Fprintf(&b, "@source %q;\n", filepath.ToSlash(d)+"/*.{vuka,templ,go}")
	}
	entry := filepath.Join(dir, twEntryName)
	return entry, writeIfChanged(entry, []byte(b.String()))
}

// writeIfChanged writes data to path unless path holds it already, through a
// rename, so a reader never sees half a file.
func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return os.WriteFile(path, data, 0o644)
	}
	_, err = tmp.Write(data)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		os.Remove(tmp.Name())
	}
	return err
}

// devTailwind starts tailwindcss --watch for vuka run and returns the
// stylesheet's path, for the app's VUKA_TAILWIND_CSS, and a function
// stopping it. Without the Tailwind CLI it says how to install it and
// returns "". The watcher starts in the background, while go builds.
func devTailwind(root, overlay string, stderr io.Writer, assets *devAssets) (string, func()) {
	tw, err := tailwindFor(root)
	if err != nil {
		fmt.Fprintln(stderr, "vuka: tailwind:", err)
	}
	if tw == nil {
		return plainCSS(root, assets), func() {}
	}
	bin, err := tailwindBinary(root)
	if err != nil {
		fmt.Fprintln(stderr, "vuka:", err)
		return plainCSS(root, assets), func() {}
	}
	out := filepath.Join(root, filepath.FromSlash(twCacheDir), "app.css")
	var (
		mu      sync.Mutex
		cmd     *exec.Cmd
		stdin   io.WriteCloser
		stopped bool
	)
	go func() {
		pkgs, err := listPackages(root, overlay)
		if err != nil {
			fmt.Fprintln(stderr, "vuka: tailwind:", err)
		}
		deps := componentDirs(pkgs.deps)
		entry, err := tw.writeEntry(deps, assets.stylesheets())
		if err != nil {
			fmt.Fprintln(stderr, "vuka: tailwind:", err)
			return
		}
		if assets != nil {
			assets.onEntry(func() { tw.writeEntry(deps, assets.stylesheets()) })
		}
		mu.Lock()
		defer mu.Unlock()
		if stopped {
			return
		}
		// tailwindcss --watch stops when its stdin closes.
		cmd = exec.Command(bin, "-i", entry, "-o", out, "--watch")
		cmd.Dir = root
		w := &twLog{w: stderr}
		cmd.Stdout, cmd.Stderr = w, w
		if stdin, err = cmd.StdinPipe(); err == nil {
			err = cmd.Start()
		}
		if err != nil {
			fmt.Fprintln(stderr, "vuka: tailwind:", err)
			cmd = nil
		}
	}()
	return out, sync.OnceFunc(func() {
		mu.Lock()
		stopped = true
		c := cmd
		mu.Unlock()
		if c == nil {
			return
		}
		stdin.Close()
		done := make(chan struct{})
		go func() { c.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			c.Process.Kill()
			<-done
		}
	})
}

// twLog passes on what tailwindcss says but its banner and timings.
type twLog struct {
	w   io.Writer
	buf []byte
}

func (l *twLog) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		line := strings.TrimSpace(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
		if line != "" && !strings.HasPrefix(line, "≈") && !strings.HasPrefix(line, "Done in") &&
			!strings.Contains(line, "DeprecationWarning") && !strings.Contains(line, "--trace-deprecation") {
			fmt.Fprintln(l.w, "tailwind:", line)
		}
	}
}

// plainCSS is, for vuka run without Tailwind, the pages' stylesheets
// (web.Style) as they are, scoped, in one file the app serves as its
// stylesheet; "" when they have none.
func plainCSS(root string, assets *devAssets) string {
	if assets == nil || len(assets.stylesheets()) == 0 {
		return ""
	}
	f := filepath.Join(root, filepath.FromSlash(twCacheDir), "app.css")
	assets.mu.Lock()
	assets.plain = f
	assets.compile()
	assets.mu.Unlock()
	return f
}

// buildTailwind builds the minified stylesheet, with the pages' scoped
// stylesheets (pa), and returns, for each main package depending on web,
// the file embedding it, and the one embedding the components' scripts. A
// web too old to take them (no RegisterCSS, RegisterAssets) gets none.
// Without Tailwind (VUKA_TAILWIND=off) the stylesheet is the pages' own
// stylesheets, as they are. Warnings (selectors matching nothing) go to warn.
func buildTailwind(root, overlay string, pa *pageAssets, warn io.Writer) ([]load.Generated, error) {
	tw, err := tailwindFor(root)
	if err != nil || tw == nil && pa.empty() {
		return nil, err
	}
	pkgs, err := listPackages(root, overlay)
	if err != nil || len(pkgs.mains) == 0 || !hasRegisterCSS(pkgs.web) {
		return nil, err // a web without RegisterCSS predates Tailwind support
	}
	comp, err := compileAssets(pa, false, warn)
	if err != nil {
		return nil, err
	}
	var gens []load.Generated
	if !pa.empty() && hasWebFunc(pkgs.web, "RegisterAssets") {
		src := assetsGo(comp)
		for _, dir := range pkgs.mains {
			gens = append(gens, load.Generated{Target: filepath.Join(dir, assetsGoName), Src: src})
		}
	}
	var css []byte
	if tw == nil {
		if len(comp.css) == 0 {
			return gens, nil
		}
		if src, ok := twNoTailwindIn(root); ok {
			return nil, fmt.Errorf("%s: web.NoTailwind() turns off the stylesheet web.Style and web.CSS compile into", src)
		}
		css = comp.plain
	} else {
		bin, err := tailwindBinary(root)
		if err != nil {
			return nil, fmt.Errorf("%v; or VUKA_TAILWIND=off to build without it", err)
		}
		entry, err := tw.writeEntry(componentDirs(pkgs.deps), comp.css)
		if err != nil {
			return nil, err
		}
		out := filepath.Join(filepath.Dir(entry), "app.min.css")
		c := exec.Command(bin, "-i", entry, "-o", out, "--minify")
		c.Dir = root
		if msg, err := c.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("tailwindcss: %v\n%s", err, msg)
		}
		if css, err = os.ReadFile(out); err != nil {
			return nil, err
		}
	}
	src := []byte("// Code generated by vuka: the app's Tailwind CSS. DO NOT EDIT.\n\npackage main\n\n" +
		"import vukaweb \"" + webPath + "\"\n\n" +
		"func init() { vukaweb.RegisterCSS(\"app.css\", []byte(vukaTailwindCSS)) }\n\n" +
		"const vukaTailwindCSS = " + strconv.Quote(string(css)) + "\n")
	from, sheet := []byte(nil), ""
	if tw != nil && tw.sheet != "" {
		sheet = tw.sheet
		from, _ = os.ReadFile(sheet)
	}
	for _, dir := range pkgs.mains {
		gens = append(gens, load.Generated{Target: filepath.Join(dir, twGoName), Source: sheet, Src: src, From: from})
	}
	return gens, nil
}

// twNoTailwindIn finds a source calling web.NoTailwind().
func twNoTailwindIn(root string) (string, bool) {
	found := ""
	walkSources(root, func(path, name string) error {
		if found == "" && (strings.HasSuffix(name, ".vuka") || strings.HasSuffix(name, ".go")) && !strings.HasSuffix(name, "_test.go") {
			if src, err := os.ReadFile(path); err == nil && twNoOption.Match(src) {
				found, _ = filepath.Rel(root, path)
			}
		}
		return nil
	})
	return found, found != ""
}

func hasRegisterCSS(dir string) bool {
	if dir == "" {
		return false
	}
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	for _, f := range files {
		if src, err := os.ReadFile(f); err == nil && bytes.Contains(src, []byte("func RegisterCSS(")) {
			return true
		}
	}
	return false
}

// addToOverlay adds gens to the overlay file (one in tmp when overlay is
// ""), each generated file written in tmp, and returns the overlay's path.
func addToOverlay(overlay, tmp string, gens []load.Generated) (string, error) {
	ov := struct{ Replace map[string]string }{map[string]string{}}
	if overlay != "" {
		data, err := os.ReadFile(overlay)
		if err != nil {
			return "", err
		}
		if err := json.Unmarshal(data, &ov); err != nil {
			return "", err
		}
	} else {
		overlay = filepath.Join(tmp, "overlay.json")
	}
	for i, g := range gens {
		file := filepath.Join(tmp, fmt.Sprintf("tw%d_%s", i, filepath.Base(g.Target)))
		if err := os.WriteFile(file, g.Src, 0o644); err != nil {
			return "", err
		}
		ov.Replace[g.Target] = file
	}
	data, err := json.Marshal(ov)
	if err != nil {
		return "", err
	}
	return overlay, os.WriteFile(overlay, data, 0o644)
}
