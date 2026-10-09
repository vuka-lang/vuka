package main

import (
	"bytes"
	"fmt"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// mod runs `go mod <args>` so the go command sees the imports of .vuka files:
// plain go mod tidy reads only .go files and drops requirements only Vuka code
// uses. For the command's duration each package gets a file of blank imports
// standing for its .vuka files' imports (and the runtime, when the code uses
// it); the files are removed afterwards.
func mod(args []string) error {
	root, modPath, err := load.ModuleRoot(".")
	if err != nil {
		return err
	}
	pkgs, err := load.Discover(root, modPath, root, true, nil)
	if err != nil {
		return err
	}
	stubs := importStubs(root, pkgs)
	var written []string
	cleanup := func() {
		for _, path := range written {
			_ = os.Remove(path)
		}
	}
	defer cleanup()
	for path, src := range stubs {
		if fileExists(path) {
			return fmt.Errorf("%s exists; vuka mod writes a file of that name for the command's duration", path)
		}
		if err := os.WriteFile(path, src, 0o644); err != nil {
			return err
		}
		written = append(written, path)
	}
	// Ctrl-C reaches go too; stay to remove the files after it stops.
	signal.Ignore(os.Interrupt)
	defer signal.Reset(os.Interrupt)

	c := exec.Command("go", append([]string{"mod"}, args...)...)
	c.Stdin, c.Stdout, c.Stderr = os.Stdin, os.Stdout, os.Stderr
	if wd, err := os.Getwd(); err == nil {
		if real, err := filepath.EvalSymlinks(wd); err == nil {
			c.Dir, c.Env = real, append(os.Environ(), "PWD="+real)
		}
	}
	return c.Run()
}

const stubName = "zz_vuka_mod.go"

// runtimeUse spots code that needs the runtime: Result, Option and their
// constructors, decorators, ?, and statics of generic types.
var runtimeUse = regexp.MustCompile(`\b(Result|Option)\s*\[|\b(Ok|Err|Some)\s*\(|\bNone\b|^\s*decorator\s|\)\?|\bvuka\.`)

// importStubs is, per package, a Go file of blank imports for what its .vuka
// files import: one for the package, one for its in-package tests, one for an
// external test package.
func importStubs(root string, pkgs []*load.Package) map[string][]byte {
	mod, _ := os.ReadFile(filepath.Join(root, "go.mod"))
	keepRuntime := bytes.Contains(mod, []byte(transpile.RuntimePath+" "))
	out := map[string][]byte{}
	for _, p := range pkgs {
		test := strings.HasSuffix(p.Name, "_test")
		imports := map[bool]map[string]bool{false: {}, true: {}} // in a _test file?
		for _, f := range p.Files {
			if !f.IsVuka() {
				continue
			}
			inTest := test || strings.HasSuffix(f.Name, "_test.vuka")
			file, _ := parser.ParseFile(token.NewFileSet(), f.Name, f.Src, parser.ImportsOnly)
			if file == nil {
				continue
			}
			for _, imp := range file.Imports {
				if path, err := strconv.Unquote(imp.Path.Value); err == nil && path != "C" {
					imports[inTest][path] = true
				}
			}
			if keepRuntime || runtimeUse.Match(f.Src) {
				imports[inTest][transpile.RuntimePath] = true
			}
		}
		for inTest, set := range imports {
			if len(set) == 0 {
				continue
			}
			name := stubName
			if inTest {
				name = strings.TrimSuffix(stubName, ".go") + "_test.go"
				if test {
					name = strings.TrimSuffix(stubName, ".go") + "_ext_test.go"
				}
			}
			paths := make([]string, 0, len(set))
			for path := range set {
				paths = append(paths, path)
			}
			sort.Strings(paths)
			var b strings.Builder
			b.WriteString("// Imports of this package's .vuka files, for vuka mod. Removed when it ends.\n\npackage " + p.Name + "\n\nimport (\n")
			for _, path := range paths {
				b.WriteString("\t_ " + strconv.Quote(path) + "\n")
			}
			b.WriteString(")\n")
			out[filepath.Join(p.Dir, name)] = []byte(b.String())
		}
	}
	return out
}
