package transpile_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

// TestGoldenTemplates transpiles each package of testdata/golden/templates —
// .go, .vuka and .vuka.html files — comparing each generated file with
// name.golden (x_html.go.golden) or the errors with errors.err, and runs
// it against out when there is one.
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
			res, err := transpile.Package(files, transpile.Options{Importer: load.NewImporter("testdata/golden", ""), Dir: dir})
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

const blankSrc = `package main

<h1>{Title}</h1>

// row is a row.
func row(n int)
<p>{n}</p>

func other()
<i>x</i>
`

// TestBlankTemplate: the template holding an offset loses its markup, kept
// declared; all of it from its header; nothing in the package clause.
func TestBlankTemplate(t *testing.T) {
	spaces := func(s string) string { return strings.Repeat(" ", len(s)) }
	for _, c := range []struct{ at, want string }{
		{"{Title}", strings.Replace(blankSrc, "<h1>{Title}</h1>", spaces("<h1>{Title}</h1>"), 1)},
		{"{n}", strings.Replace(blankSrc, "<p>{n}</p>", spaces("<p>{n}</p>"), 1)},
		{"row(n int)", strings.Replace(blankSrc, "// row is a row.\nfunc row(n int)\n<p>{n}</p>", spaces("// row is a row.")+"\n"+spaces("func row(n int)")+"\n"+spaces("<p>{n}</p>"), 1)},
		{"<i>", strings.Replace(blankSrc, "<i>x</i>", spaces("<i>x</i>"), 1)},
	} {
		if got := string(transpile.BlankTemplate([]byte(blankSrc), strings.Index(blankSrc, c.at))); got != c.want {
			t.Errorf("at %s:\n%s\nwant:\n%s", c.at, got, c.want)
		}
	}
	if got := transpile.BlankTemplate([]byte(blankSrc), 3); got != nil {
		t.Errorf("in the package clause: %q", got)
	}
}

// TestSyntaxTemplate: a template file's syntax check reads it as a
// template, with the package's other files: no errors when it has none,
// and its own at their place.
func TestSyntaxTemplate(t *testing.T) {
	goFile := transpile.File{Name: "page.go", Src: []byte("package main\n\ntype Page struct{ Title string }\n")}
	src := "package main\n\n<title>{Title + \" · \"}</title>\n\nfunc Footer()\n<p>Copyright © 2014</p>\n"
	opts := transpile.Options{Bare: true, Importer: load.NewImporter("testdata/golden", "")}
	if errs := transpile.Syntax(transpile.File{Name: "page.vuka.html", Src: []byte(src)}, opts, goFile); len(errs) > 0 {
		t.Errorf("errors in a template with none: %v", errs)
	}
	bad := strings.Replace(src, "</p>", "</div>", 1)
	errs := transpile.Syntax(transpile.File{Name: "page.vuka.html", Src: []byte(bad)}, opts, goFile)
	if len(errs) != 1 || errs[0].Pos.Line != 6 || !strings.Contains(errs[0].Msg, "</div>") {
		t.Errorf("the error of a template: %v", errs)
	}
}
