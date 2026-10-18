package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

const depLib = `package lib

func Count() (int, error) { return 1, nil }
`

const depMain = `package main

import (
	"fmt"

	"lsptest/lib"
)

func run() Result[int] {
	n := lib.Count()?
	return Ok(n)
}

func main() { fmt.Println(run()) }
`

// TestLSPDependents edits a package another imports, in the editor and on
// disk, and checks the importer's diagnostics follow, as a transpile from
// scratch reports them.
func TestLSPDependents(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": depMain, "lib/lib.vuka": depLib})
	mainPath, libPath := filepath.Join(dir, "main.vuka"), filepath.Join(dir, "lib", "lib.vuka")
	mainURI, libURI := pathToURI(mainPath), pathToURI(libPath)
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": mainURI, "languageId": "vuka", "version": 1, "text": depMain}})
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": libURI, "languageId": "vuka", "version": 1, "text": depLib}})
	bufs := map[string]string{mainPath: depMain, libPath: depLib}
	version := 1
	edit := func(path, text string) {
		version++
		bufs[path] = text
		c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": pathToURI(path), "version": version},
			"contentChanges": []any{map[string]any{"text": text}}})
	}
	// vukaErrs is main.vuka's transpiler errors, as published.
	vukaErrs := func(ds []any) []string {
		var out []string
		for _, d := range ds {
			if m, _ := d.(map[string]any); m["source"] == "vuka" {
				msg, _, _ := strings.Cut(m["message"].(string), "\nhelp: ")
				out = append(out, msg)
			}
		}
		slices.Sort(out)
		return out
	}
	// scratch is main.vuka's errors from a transpile of the module from
	// scratch, with the buffers as the editor has them.
	scratch := func() []string {
		t.Helper()
		root := realDir(dir)
		read := func(path string) ([]byte, error) {
			for p, text := range bufs {
				if realDir(filepath.Dir(p)) == filepath.Dir(path) && filepath.Base(p) == filepath.Base(path) {
					return []byte(text), nil
				}
			}
			return os.ReadFile(path)
		}
		pkgs, err := load.Discover(root, "lsptest", root, true, read)
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = load.Transpile(pkgs, t.TempDir(), load.Options{Bare: true})
		var list transpile.ErrorList
		errors.As(err, &list)
		var out []string
		for _, e := range list {
			if filepath.Base(e.Pos.Filename) == "main.vuka" {
				out = append(out, e.Msg)
			}
		}
		slices.Sort(out)
		return out
	}
	// settle waits for main.vuka's transpiler errors to be want, then checks
	// they stay so.
	settle := func(what string, want func([]string) bool) {
		t.Helper()
		var got []string
		deadline := time.After(30 * time.Second)
		for {
			select {
			case p := <-c.diags:
				if p["uri"] != mainURI {
					continue
				}
				ds, _ := p["diagnostics"].([]any)
				got = vukaErrs(ds)
				if !want(got) {
					continue
				}
				for {
					select {
					case p := <-c.diags:
						if p["uri"] == mainURI {
							ds, _ := p["diagnostics"].([]any)
							got = vukaErrs(ds)
						}
						continue
					case <-time.After(1500 * time.Millisecond):
					}
					break
				}
				if s := scratch(); !slices.Equal(got, s) {
					t.Fatalf("%s: main.vuka's errors\n%q\nfrom scratch\n%q", what, got, s)
				}
				return
			case <-deadline:
				t.Fatalf("%s: main.vuka's errors are %q", what, got)
			}
		}
	}
	none := func(errs []string) bool { return len(errs) == 0 }
	some := func(errs []string) bool { return len(errs) > 0 }
	settle("at first", none)

	// An exported signature main uses, edited in the editor: main's ? can't
	// lower a plain int.
	edit(libPath, strings.Replace(depLib, "func Count() (int, error) { return 1, nil }", "func Count() int { return 1 }", 1))
	settle("Count returns an int", some)
	edit(libPath, depLib)
	settle("Count mended", none)

	// The same on disk, without the editor: a file added to lib, then
	// removed.
	c.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": libURI}})
	delete(bufs, libPath)
	edit(mainPath, strings.Replace(depMain, "lib.Count()?", "lib.More()?", 1))
	settle("lib.More missing", some)
	more := filepath.Join(dir, "lib", "more.vuka")
	writeFile(t, more, "package lib\n\nfunc More() Result[int] { return Ok(2) }\n")
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": pathToURI(more), "type": 1}}})
	settle("lib.More added", none)
	os.Remove(more)
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": pathToURI(more), "type": 3}}})
	settle("lib.More removed", some)

	// go.mod changed: the session starts over.
	edit(mainPath, depMain)
	settle("main mended", none)
	gomod := filepath.Join(dir, "go.mod")
	mod, _ := os.ReadFile(gomod)
	writeFile(t, gomod, strings.Replace(string(mod), "go 1.25.0", "go 1.25.1", 1))
	c.notify("workspace/didChangeWatchedFiles", map[string]any{"changes": []any{map[string]any{"uri": pathToURI(gomod), "type": 2}}})
	edit(libPath+"", strings.Replace(depLib, "func Count() (int, error) { return 1, nil }", "func Count() int { return 1 }", 1))
	settle("after go.mod changed", some)
}

// TestLSPSyntaxFirst checks a syntax error shows before the module's build
// would, and that requests don't wait for that build.
func TestLSPSyntaxFirst(t *testing.T) {
	c, dir, _ := startLSPWith(t, map[string]string{"main.vuka": depMain, "lib/lib.vuka": depLib})
	uri := pathToURI(filepath.Join(dir, "main.vuka"))
	c.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": "vuka", "version": 1, "text": depMain}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
	broken := depMain + "\nfunc zz() { ) }\n"
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 2},
		"contentChanges": []any{map[string]any{"text": broken}}})
	ds := c.waitDiags(uri, func(ds []any) bool { return len(ds) > 0 })
	b, _ := json.Marshal(ds)
	if line := strings.Count(depMain, "\n") + 1; !strings.Contains(string(b), `"source":"vuka"`) || !strings.Contains(string(b), `"line":`+itoaTest(uint32(line))) {
		t.Fatalf("syntax error: %s", b)
	}
	c.notify("textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": 3},
		"contentChanges": []any{map[string]any{"text": depMain}}})
	c.waitDiags(uri, func(ds []any) bool { return len(ds) == 0 })
}
