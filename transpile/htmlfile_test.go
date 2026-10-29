package transpile_test

import (
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// TestGoldenTemplates transpiles each package of testdata/golden/templates —
// .go, .vuka and .vuka.html files — comparing each generated file with
// name.golden (x_html.go.golden) or the errors with errors.err, its
// warnings with warnings, and runs it against out when there is one. A
// vuka.toml sets the compiler's settings ([compile]). A scopes file names
// its scoped declarations, a line each: the name, then its scope id.
func TestGoldenTemplates(t *testing.T) {
	dirs, _ := filepath.Glob("testdata/golden/templates/*")
	for _, dir := range dirs {
		t.Run(filepath.Base(dir), func(t *testing.T) {
			entries, _ := os.ReadDir(dir)
			var files []transpile.File
			for _, e := range entries {
				n := e.Name()
				if strings.HasSuffix(n, ".go") || strings.HasSuffix(n, ".vuka") || strings.HasSuffix(n, ".vuka.html") {
					src, _ := os.ReadFile(filepath.Join(dir, n))
					files = append(files, transpile.File{Name: n, Src: src})
				}
			}
			cc, err := load.LoadCompile(dir)
			if err != nil {
				t.Fatal(err)
			}
			res, err := transpile.Package(files, transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: dir, Contexts: caseContexts(t, dir), Scopes: caseScopes(dir), NoImplicitCtx: cc.NoImplicitCtx})
			if err != nil {
				compare(t, filepath.Join(dir, "errors.err"), []byte(err.Error()+"\n"))
				return
			}
			run := t.TempDir()
			for _, f := range files {
				if strings.HasSuffix(f.Name, ".go") {
					os.WriteFile(filepath.Join(run, f.Name), f.Src, 0o644)
				}
			}
			var warns []string
			for _, o := range res.Files {
				for _, w := range o.Warnings {
					line := string(w.Code) + " " + w.Error()
					if w.Fix != "" {
						line += " => " + w.Fix
					}
					warns = append(warns, line)
				}
			}
			if len(warns) > 0 {
				compare(t, filepath.Join(dir, "warnings"), []byte(strings.Join(warns, "\n")+"\n"))
			} else if _, err := os.Stat(filepath.Join(dir, "warnings")); err == nil && !*update {
				t.Errorf("no warnings, but %s has a warnings file", dir)
			}
			for _, o := range res.Files {
				compare(t, filepath.Join(dir, o.GoName+".golden"), o.Src)
				os.WriteFile(filepath.Join(run, o.GoName), o.Src, 0o644)
			}
			want, err := os.ReadFile(filepath.Join(dir, "out"))
			if err != nil || testing.Short() {
				return
			}
			mod, sum := goldenModule(t)
			os.WriteFile(filepath.Join(run, "go.mod"), mod, 0o644)
			os.WriteFile(filepath.Join(run, "go.sum"), sum, 0o644)
			cmd := exec.Command("go", "run", ".")
			cmd.Dir = run
			cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
			out, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("go run: %v\n%s", err, out)
			}
			if *update {
				os.WriteFile(filepath.Join(dir, "out"), out, 0o644)
			} else if string(out) != string(want) {
				t.Errorf("output:\n%s\nwant:\n%s", out, want)
			}
		})
	}
}

// caseScopes are a case's scoped declarations, from its scopes file.
func caseScopes(dir string) map[string]string {
	b, err := os.ReadFile(filepath.Join(dir, "scopes"))
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if f := strings.Fields(line); len(f) == 2 {
			out[f[0]] = f[1]
		}
	}
	return out
}

// caseContexts are the context processors of a case's mains, from its
// processors file: a line per main, its import path then each processor,
// path.Func, its entry on line n of main.go for the nth.
func caseContexts(t *testing.T, dir string) []transpile.Context {
	b, err := os.ReadFile(filepath.Join(dir, "processors"))
	if err != nil {
		return nil
	}
	var out []transpile.Context
	for _, line := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		f := strings.Fields(line)
		c := transpile.Context{Main: f[0]}
		for i, p := range f[1:] {
			dot := strings.LastIndex(p, ".")
			c.Processors = append(c.Processors, transpile.Processor{Path: p[:dot], Func: p[dot+1:], Pos: token.Position{Filename: f[0] + "/main.go", Line: i + 1, Column: 1}})
		}
		out = append(out, c)
	}
	return out
}

const blankSrc = `package main

// Page is a page.
type Page struct{ Title string }

func (p Page) upper() string { return p.Title }
<h1>{Title}</h1>
<p>{upper()}</p>
`

// TestBlankTemplate: the markup holding an offset is written as spaces, its
// lines kept, the struct still declared; nothing in the package clause or
// the Go header.
func TestBlankTemplate(t *testing.T) {
	spaces := func(s string) string { return strings.Repeat(" ", len(s)) }
	markup := "<h1>{Title}</h1>\n<p>{upper()}</p>"
	want := strings.Replace(blankSrc, markup, spaces("<h1>{Title}</h1>")+"\n"+spaces("<p>{upper()}</p>"), 1)
	for _, at := range []string{"{Title}", "{upper()}"} {
		if got := string(transpile.BlankTemplate([]byte(blankSrc), strings.Index(blankSrc, at))); got != want {
			t.Errorf("at %s:\n%s\nwant:\n%s", at, got, want)
		}
	}
	for _, at := range []int{3, strings.Index(blankSrc, "Title string"), strings.Index(blankSrc, "return p.Title")} {
		if got := transpile.BlankTemplate([]byte(blankSrc), at); got != nil {
			t.Errorf("at %d, outside the markup: %q", at, got)
		}
	}
}

// TestSyntaxTemplate: a template file's syntax check reads it as a
// template, with the package's other files: no errors when it has none,
// and its own at their place.
func TestSyntaxTemplate(t *testing.T) {
	goFile := transpile.File{Name: "main.go", Src: []byte("package main\n\nfunc main() {}\n")}
	src := "package main\n\ntype Page struct{ Title string }\n<title>{Title + \" · \"}</title>\n<p>Copyright © 2014</p>\n"
	opts := transpile.Options{Bare: true, Importer: load.NewImporter("testdata/golden", "")}
	if errs := transpile.Syntax(transpile.File{Name: "page.vuka.html", Src: []byte(src)}, opts, goFile); len(errs) > 0 {
		t.Errorf("errors in a template with none: %v", errs)
	}
	bad := strings.Replace(src, "</p>", "</div>", 1)
	errs := transpile.Syntax(transpile.File{Name: "page.vuka.html", Src: []byte(bad)}, opts, goFile)
	if len(errs) != 1 || errs[0].Pos.Line != 5 || !strings.Contains(errs[0].Msg, "</div>") {
		t.Errorf("the error of a template: %v", errs)
	}
}
