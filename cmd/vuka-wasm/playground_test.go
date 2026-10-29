//go:build !js

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/vuka-lang/vuka/v2/diag"
)

var (
	snapOnce sync.Once
	snapPG   *playground
	snapErr  error
)

// testPlayground is a playground on a snapshot made from the docs' module,
// through the snapshot file's encoding.
func testPlayground(t *testing.T) *playground {
	t.Helper()
	snapOnce.Do(func() {
		pkgs, err := buildSnapshot("../../docs/testdata/gomode")
		if err != nil {
			snapErr = err
			return
		}
		pkgs, snapErr = decodeSnapshot(encodeSnapshot(pkgs))
		snapPG = newPlayground(pkgs)
	})
	if snapErr != nil {
		t.Fatal(snapErr)
	}
	return snapPG
}

const examples = "../../docs/playground/examples"

// example is the project in the docs' examples folder name.
func example(t *testing.T, name string) map[string]string {
	t.Helper()
	root := filepath.Join(examples, name)
	files := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		src, err := os.ReadFile(p)
		rel, _ := filepath.Rel(root, p)
		files[filepath.ToSlash(rel)] = string(src)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return files
}

func errorsOf(ds []diag.Diagnostic) []diag.Diagnostic {
	var out []diag.Diagnostic
	for _, d := range ds {
		if d.Severity == diag.Error {
			out = append(out, d)
		}
	}
	return out
}

// TestExamples transpiles every example of the playground's dropdown: no
// errors, Go for every source file, and every file as vuka fmt leaves it.
func TestExamples(t *testing.T) {
	b, err := os.ReadFile("../../docs/playground/examples.json")
	if err != nil {
		t.Fatal(err)
	}
	var list []struct{ Name, Title, Open string }
	if err := json.Unmarshal(b, &list); err != nil {
		t.Fatal(err)
	}
	dirs, _ := os.ReadDir(examples)
	var listed, found []string
	for _, e := range list {
		listed = append(listed, e.Name)
	}
	for _, d := range dirs {
		found = append(found, d.Name())
	}
	slices.Sort(listed)
	if !slices.Equal(listed, found) {
		t.Fatalf("examples.json lists %v; the folder holds %v", listed, found)
	}
	pg := testPlayground(t)
	for _, e := range list {
		t.Run(e.Name, func(t *testing.T) {
			files := example(t, e.Name)
			if _, ok := files[e.Open]; !ok {
				t.Errorf("opens %s, which it hasn't", e.Open)
			}
			res := pg.transpile(files)
			if errs := errorsOf(res.Diagnostics); len(errs) > 0 {
				t.Fatalf("errors: %s", diag.List(errs).Error())
			}
			for p, src := range files {
				if !strings.HasSuffix(p, ".go") && res.Files[p] == "" {
					t.Errorf("%s: no Go", p)
				}
				if f := formatFile(p, src); f.Error != nil || f.Src != src {
					t.Errorf("%s isn't formatted: %+v", p, f.Error)
				}
			}
		})
	}
}

func TestTranspile(t *testing.T) {
	res := testPlayground(t).transpile(map[string]string{
		"main.vuka": "package main\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/play/shapes\"\n)\n\nfunc main() { fmt.Println(shapes.Area(shapes.Circle{R: 2})) }\n",
		"shapes/shapes.vuka": `package shapes

type Circle struct{ R float64 }
type Square struct{ Side float64 }

func area(c Circle) float64 { return 3.14 * c.R * c.R }
func area(s Square) float64 { return s.Side * s.Side }

func Area(c Circle) float64 { return area(c) }
`,
		"shapes/util.go": "package shapes\n\nfunc double(x float64) float64 { return 2 * x }\n\nvar _ = double\n",
		"README.md":      "not a source",
	})
	if len(res.Diagnostics) > 0 {
		t.Fatalf("diagnostics: %s", diag.List(res.Diagnostics).Error())
	}
	if !strings.Contains(res.Files["shapes/shapes.vuka"], "func areaCircle(") {
		t.Errorf("shapes.vuka's Go has no areaCircle:\n%s", res.Files["shapes/shapes.vuka"])
	}
	if !strings.Contains(res.Files["main.vuka"], "package main") {
		t.Errorf("main.vuka's Go:\n%s", res.Files["main.vuka"])
	}
	for p := range res.Files {
		if p == "README.md" || p == "shapes/util.go" {
			t.Errorf("Go for %s", p)
		}
	}
}

func TestDiagnostics(t *testing.T) {
	files := example(t, "counter")
	files["pages/index.vuka.html"] = strings.Replace(files["pages/index.vuka.html"], "p.count++", "p.count += \"one\"", 1)
	files["main.vuka"] = strings.Replace(files["main.vuka"], "func main() {", "func main() {\n\tunused := 1", 1)
	res := testPlayground(t).transpile(files)
	if len(res.Diagnostics) != 3 {
		t.Fatalf("want 3 diagnostics, got %s", diag.List(res.Diagnostics).Error())
	}
	b, _ := json.Marshal(res.Diagnostics)
	var ds []map[string]any
	json.Unmarshal(b, &ds)
	if ds[2]["code"] != "E0005" || ds[2]["message"] != "example.com/play/pages has errors" {
		t.Errorf("the import of pages: %v", ds[2])
	}
	main, page := ds[0], ds[1]
	if main["code"] != "E0002" || main["severity"] != "error" || main["message"] != "declared and not used: unused" {
		t.Errorf("main.vuka: %s", b)
	}
	if sp := main["span"].(map[string]any); sp["file"] != "main.vuka" || sp["line"] != 13.0 || sp["col"] != 2.0 {
		t.Errorf("main.vuka's span: %v", sp)
	}
	if sp := page["span"].(map[string]any); page["code"] != "E0002" || sp["file"] != "pages/index.vuka.html" || sp["line"] != 16.0 || sp["col"] == nil {
		t.Errorf("index.vuka.html: %v", page)
	}

	files = example(t, "props")
	files["pages/pets.vuka"] += "\ntype Extra struct{}\n"
	files["pages/index.vuka.html"] = strings.Replace(files["pages/index.vuka.html"], "type Index struct", "type Other struct{}\n\ntype Index struct", 1)
	res = testPlayground(t).transpile(files)
	var codes []string
	for _, d := range res.Diagnostics {
		codes = append(codes, string(d.Code)+" "+d.Span.String())
	}
	want := []string{"E0147 pages/index.vuka.html:9:6", "E0005 main.vuka:9:2"}
	if !slices.Equal(codes, want) {
		t.Errorf("got %q, want %q", codes, want)
	}
	if d := res.Diagnostics[0]; d.Help == "" {
		t.Errorf("E0147 without help: %+v", d)
	}
}

func TestUnknownImport(t *testing.T) {
	res := testPlayground(t).transpile(map[string]string{
		"main.vuka": "package main\n\nimport (\n\t\"go/ast\"\n\n\t\"github.com/google/uuid\"\n)\n\nfunc main() {\n\t_ = uuid.New()\n\t_ = ast.Ident{}\n}\n",
	})
	want := []string{
		"main.vuka:4:2: cannot import go/ast: of std, the playground knows the packages apps commonly import; it knows ui, web and orm too",
		"main.vuka:6:2: cannot import github.com/google/uuid: the playground knows std, ui, web and orm",
	}
	var got []string
	for _, d := range res.Diagnostics {
		got = append(got, d.Span.String()+": "+d.Message)
	}
	if !slices.Equal(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestFormat(t *testing.T) {
	f := formatFile("orders/status.vuka", "package orders\nenum Status {Draft,Sent}\nfunc f( ) int {return 1}\n")
	if f.Error != nil || f.Src != "package orders\n\nenum Status { Draft, Sent }\n\nfunc f() int { return 1 }\n" {
		t.Errorf("vuka: %q %+v", f.Src, f.Error)
	}
	f = formatFile("pages/about.vuka.html", "<main>\n<p>{Title()}</p>\n</main>\n")
	if f.Error != nil || !strings.Contains(f.Src, "package pages") || !strings.Contains(f.Src, "\n  <p>{Title()}</p>\n") {
		t.Errorf("vuka.html: %q %+v", f.Src, f.Error)
	}
	f = formatFile("util.go", "package main\nfunc f( ) {}\n")
	if f.Error != nil || f.Src != "package main\n\nfunc f() {}\n" {
		t.Errorf("go: %q %+v", f.Src, f.Error)
	}
	f = formatFile("main.vuka", "package main\n\nfunc f() {\n")
	if f.Error == nil || f.Error.Span.File != "main.vuka" || f.Error.Span.Line == 0 {
		t.Errorf("syntax error: %+v", f.Error)
	}
}

func TestBadPaths(t *testing.T) {
	res := testPlayground(t).transpile(map[string]string{"../x.vuka": "package x\n", "/abs.vuka": "package x\n"})
	if len(res.Diagnostics) != 2 || res.Diagnostics[0].Code != "E0003" {
		t.Errorf("%+v", res.Diagnostics)
	}
}

func TestSnapshot(t *testing.T) {
	if _, err := decodeSnapshot([]byte("nope")); err == nil {
		t.Error("garbage decoded")
	}
	pkgs, err := decodeSnapshot(encodeSnapshot(map[string][]byte{"a": []byte("x"), "b/c": {}}))
	if err != nil || string(pkgs["a"]) != "x" || pkgs["b/c"] == nil {
		t.Errorf("%v %v", pkgs, err)
	}
}
