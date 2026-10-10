package format

import (
	"bytes"
	"flag"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/internal/load"
	"github.com/vuka-lang/vuka/transpile"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

func TestConstructs(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"attribute", "package p\n\n@doc( \"x\" )\n@Route{Method:\"GET\"}\nfunc f( ) {}\n",
			"package p\n\n@doc(\"x\")\n@Route{Method: \"GET\"}\nfunc f() {}\n"},
		{"attribute on the declaration's line", "package p\n\n@doc(\"x\") const A = 1\n",
			"package p\n\n@doc(\"x\")\nconst A = 1\n"},
		{"decorator", "package p\n\ndecorator  logged(c) {\nc.Next()\n}\n",
			"package p\n\ndecorator logged(c) {\n\tc.Next()\n}\n"},
		{"decorator with parameters", "package p\n\ndecorator retry(n int)(c) { for range n { c.Next() } }\n",
			"package p\n\ndecorator retry(n int)(c) {\n\tfor range n {\n\t\tc.Next()\n\t}\n}\n"},
		{"statics", "package p\n\ntype T struct {\n\tA int\n\n\tstatic Table = \"t\" // table\n\tstatic n int\n\tstatic const Max=3\n}\n",
			"package p\n\ntype T struct {\n\tA int\n\n\tstatic Table     = \"t\" // table\n\tstatic n         int\n\tstatic const Max = 3\n}\n"},
		{"static methods", "package p\n\nfunc T.New(n int)*T { return &T{} }\nfunc M[K,V].Count( ) int { return 0 }\n",
			"package p\n\nfunc T.New(n int) *T     { return &T{} }\nfunc M[K, V].Count() int { return 0 }\n"},
		{"field attributes", "package p\n\ntype Post struct {\n\tID int64 @PK\n\tTitle string `json:\"title\"` @Char{Max:200}\n\tAuthor FK[User]   @Rel{OnDelete:Cascade} @Index // the author\n\tBody string\n}\n",
			"package p\n\ntype Post struct {\n\tID     int64    @PK\n\tTitle  string   `json:\"title\"` @Char{Max: 200}\n\tAuthor FK[User] @Rel{OnDelete: Cascade} @Index // the author\n\tBody   string\n}\n"},
		{"field attribute over lines", "package p\n\ntype T struct {\n\tMeta JSON @Opts{\nA: 1,\n}\n}\n",
			"package p\n\ntype T struct {\n\tMeta JSON @Opts{\n\t\tA: 1,\n\t}\n}\n"},
		{"field references", "package p\n\nfunc f() { _ = Post.Author.Name.Eq( \"ada\" ) }\n",
			"package p\n\nfunc f() { _ = Post.Author.Name.Eq(\"ada\") }\n"},
		{"try", "package p\n\nfunc f() error {\n\tx:=g()?\n\tg()?\n\treturn h(x)?\n}\n",
			"package p\n\nfunc f() error {\n\tx := g()?\n\tg()?\n\treturn h(x)?\n}\n"},
		{"match", "package p\n\nfunc f(r Result[int]) {\n\tmatch r {\n\tcase Ok(n) if n>1: g()\n\tcase Ok(^m), Err(_):\n\t\tg()\n\t}\n\tswitch {\n\t}\n}\n",
			"package p\n\nfunc f(r Result[int]) {\n\tmatch r {\n\tcase Ok(n) if n > 1:\n\t\tg()\n\tcase Ok(^m), Err(_):\n\t\tg()\n\t}\n\tswitch {\n\t}\n}\n"},
		{"overloads", "package p\n\nfunc area(c Circle) float64 {return 1}\nfunc area(r Rect) float64 {return 2}\n",
			"package p\n\nfunc area(c Circle) float64 { return 1 }\nfunc area(r Rect) float64   { return 2 }\n"},
		{"jsx on one line", "package p\n\nfunc f() vuka.Node {\n\treturn <p   a = \"x\" b={ x+1 } >hi</p >\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <p a=\"x\" b={x + 1}>hi</p>\n}\n"},
		{"self-closing", "package p\n\nfunc f() vuka.Node {\n\treturn <><hr/><br   /><div></div></>\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <><hr /><br /><div></div></>\n}\n"},
		{"collapses short text", "package p\n\nfunc f() vuka.Node {\n\treturn <p>\n\t\thello\n\t</p>\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <p>hello</p>\n}\n"},
		{"keeps written-out children", "package p\n\nfunc f() vuka.Node {\n\treturn <ul>\n<li>a</li><li>b</li>\n</ul>\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <ul>\n\t\t<li>a</li>\n\t\t<li>b</li>\n\t</ul>\n}\n"},
		{"breaks attributes", "package p\n\nfunc f() vuka.Node {\n\treturn <input type=\"text\" name=\"a-fairly-long-name\" placeholder=\"something long enough\" />\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <input\n\t\ttype=\"text\"\n\t\tname=\"a-fairly-long-name\"\n\t\tplaceholder=\"something long enough\"\n\t/>\n}\n"},
		{"spaces that render", "package p\n\nfunc f() vuka.Node {\n\treturn <div>\n<b>a</b> <i>b</i>\n<p>  x  </p>\n</div>\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <div>\n\t\t<b>a</b> <i>b</i>\n\t\t<p>  x  </p>\n\t</div>\n}\n"},
		{"blocks", "package p\n\nfunc f(xs []int, ok bool) vuka.Node {\n\treturn <div>\n{for _,x:=range xs {<b>{x}</b>}}\n{if ok {<i>yes</i>} else { no }}\n{match len(xs) { case 0: none default: <b>some</b> }}\n</div>\n}\n",
			"package p\n\nfunc f(xs []int, ok bool) vuka.Node {\n\treturn <div>\n\t\t{for _, x := range xs { <b>{x}</b> }}\n\t\t{if ok { <i>yes</i> } else { no }}\n\t\t{match len(xs) {\n\t\tcase 0:\n\t\t\tnone\n\t\tdefault:\n\t\t\t<b>some</b>\n\t\t}}\n\t</div>\n}\n"},
		{"go in holes", "package p\n\nfunc f() vuka.Node {\n\treturn <div>\n{func() string {\nreturn \"x\"\n}()}\n{g(<b>x</b>,   1)}\n{/* as   is */}\n</div>\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <div>\n\t\t{func() string {\n\t\t\treturn \"x\"\n\t\t}()}\n\t\t{g(<b>x</b>, 1)}\n\t\t{/* as   is */}\n\t</div>\n}\n"},
		{"text lines", "package p\n\nfunc f() vuka.Node {\n\treturn <section className=\"a-long-class-name\">\n   Tom &amp; Jerry,   friends\n  forever and ever, in a sentence that runs on\n</section>\n}\n",
			"package p\n\nfunc f() vuka.Node {\n\treturn <section className=\"a-long-class-name\">\n\t\tTom &amp; Jerry,   friends\n\t\tforever and ever, in a sentence that runs on\n\t</section>\n}\n"},
		{"jsx in a one-line func that would not fit", "package p\n\nfunc Button(props ButtonProps) vuka.Node { return <button>{props.Variant}</button> }\n",
			"package p\n\nfunc Button(props ButtonProps) vuka.Node {\n\treturn <button>{props.Variant}</button>\n}\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := Source([]byte(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, c.want)
			}
			again, err := Source(got)
			if err != nil || !bytes.Equal(again, got) {
				t.Errorf("not idempotent (%v):\n%s", err, again)
			}
		})
	}
}

func TestErrors(t *testing.T) {
	cases := []struct{ in, want string }{
		{"package p\n\nfunc f() vuka.Node {\n\treturn <div><p>x</div>\n}\n", "4:18: expected </p> to close <p>"},
		{"package p\n\ntype T struct{}\n\nfunc f() {\n\tx := <b>x</b>\n\ty := (\n}\n", "8:1: expected operand, found '}'"},
		{"package p\n\nfunc f() {\n\tx := g() ?\n}\n", "4:11: unexpected ?"},
	}
	for _, c := range cases {
		out, err := Source([]byte(c.in))
		if err == nil || !strings.HasPrefix(err.Error(), c.want) {
			t.Errorf("%q: got %v, want %s", c.in, err, c.want)
		}
		if out != nil {
			t.Errorf("output on error: %s", out)
		}
	}
}

// TestGolden formats testdata/*.input and compares with *.golden.
func TestGolden(t *testing.T) {
	inputs, _ := filepath.Glob("testdata/*.input")
	if len(inputs) == 0 {
		t.Fatal("no golden cases")
	}
	for _, in := range inputs {
		t.Run(filepath.Base(in), func(t *testing.T) {
			src, _ := os.ReadFile(in)
			got, err := Source(src)
			if err != nil {
				t.Fatal(err)
			}
			golden := strings.TrimSuffix(in, ".input") + ".golden"
			if *update {
				os.WriteFile(golden, got, 0o644)
			}
			want, _ := os.ReadFile(golden)
			if !bytes.Equal(got, want) {
				t.Errorf("got:\n%s\nwant:\n%s", got, want)
			}
			if again, err := Source(got); err != nil || !bytes.Equal(again, got) {
				t.Errorf("not idempotent (%v):\n%s", err, again)
			}
		})
	}
}

// TestTranspileGolden formats every transpile golden case: formatting is
// idempotent, and the formatted file transpiles to the same Go (up to layout),
// and, where the case has an expected output, runs to it.
func TestTranspileGolden(t *testing.T) {
	cases, _ := filepath.Glob("../../transpile/testdata/golden/*.vuka")
	more, _ := filepath.Glob("testdata/*.input")
	if len(cases) == 0 {
		t.Fatal("no transpile golden cases")
	}
	for _, path := range append(cases, more...) {
		name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
		t.Run(name, func(t *testing.T) {
			src, _ := os.ReadFile(path)
			got, err := Source(src)
			if err != nil {
				if strings.HasPrefix(name, "err_") {
					return
				}
				t.Fatal(err)
			}
			if again, err := Source(got); err != nil || !bytes.Equal(again, got) {
				t.Fatalf("not idempotent (%v):\n%s\nthen:\n%s", err, got, again)
			}
			if strings.HasPrefix(name, "err_") {
				return
			}
			dir := filepath.Dir(path)
			before, after := lower(t, dir, name, src), lower(t, dir, name, got)
			if a, b := goTokens(t, before), goTokens(t, after); !slices.Equal(a, b) {
				t.Fatalf("formatting changed the generated Go:\n%s\nformatted:\n%s", before, after)
			}
			if want, err := os.ReadFile(filepath.Join(dir, name+".out")); err == nil {
				if out := run(t, after); out != string(want) {
					t.Errorf("formatted program prints:\n%s\nwant:\n%s", out, want)
				}
			}
		})
	}
}

func lower(t *testing.T, dir, name string, src []byte) []byte {
	t.Helper()
	res, err := transpile.Package([]transpile.File{{Name: name + ".vuka", Src: src}},
		transpile.Options{Importer: load.NewImporter(dir, "")})
	if err != nil {
		t.Fatalf("transpile: %v\n%s", err, src)
	}
	return res.Files[0].Src
}

// goTokens is the generated Go as tokens, without comments, line directives
// or layout, imports sorted.
func goTokens(t *testing.T, src []byte) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "", src, parser.ImportsOnly)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, imp := range f.Imports {
		name := ""
		if imp.Name != nil {
			name = imp.Name.Name + " "
		}
		out = append(out, name+imp.Path.Value)
	}
	slices.Sort(out)
	rest := 0
	if n := len(f.Decls); n > 0 {
		rest = fset.Position(f.Decls[n-1].End()).Offset
	}
	for _, tk := range scanTokens(src, rest) {
		if tk.tok == token.COMMENT || tk.tok == token.SEMICOLON {
			continue
		}
		out = append(out, tk.tok.String()+" "+tk.lit)
	}
	return out
}

func run(t *testing.T, src []byte) string {
	t.Helper()
	if testing.Short() {
		t.Skip("compiles and runs the program")
	}
	dir := t.TempDir()
	root, _ := filepath.Abs("../..")
	mod := "module golden\n\ngo 1.25.0\n\nrequire (\n\tgithub.com/a-h/templ v0.3.1020 // indirect\n\tgithub.com/vuka-lang/vuka v0.0.0\n)\n\nreplace github.com/vuka-lang/vuka => " + root + "\n"
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte(mod), 0o644)
	sum, _ := os.ReadFile(filepath.Join(root, "go.sum"))
	os.WriteFile(filepath.Join(dir, "go.sum"), sum, 0o644)
	os.WriteFile(filepath.Join(dir, "main.go"), src, 0o644)
	os.CopyFS(filepath.Join(dir, "assets"), os.DirFS("../../transpile/testdata/golden/assets"))
	cmd := exec.Command("go", "run", ".")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go run: %v\n%s", err, out)
	}
	return string(out)
}

// TestMarkupCheck makes sure the render check notices a change.
func TestMarkupCheck(t *testing.T) {
	a := []byte("package p\n\nfunc f() vuka.Node { return <p>a b</p> }\n")
	for _, b := range []string{"<p>a  b</p>", "<p>a\nb</p>", "<p> a b</p>", "<p>a b<i/></p>", "<p x>a b</p>"} {
		changed := bytes.Replace(a, []byte("<p>a b</p>"), []byte(b), 1)
		if bytes.Equal(changed, a) {
			t.Fatal("bad replacement")
		}
		err := sameMarkup(a, changed)
		if (err == nil) != (b == "<p>a\nb</p>") {
			t.Errorf("%s: %v", strconv.Quote(b), err)
		}
	}
}

// FuzzSource checks that formatting never panics and settles. gofmt itself
// takes a few passes over some odd input (stray semicolons, comments), so
// what is checked is that a fixed point comes within four passes (the render
// check runs inside Source).
func FuzzSource(f *testing.F) {
	paths, _ := filepath.Glob("../../transpile/testdata/golden/*.vuka")
	more, _ := filepath.Glob("testdata/*.input")
	for _, p := range append(paths, more...) {
		src, _ := os.ReadFile(p)
		f.Add(src)
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		out, err := Source(src)
		if err != nil {
			return
		}
		for range 4 {
			again, err := Source(out)
			if err != nil {
				t.Fatalf("formatted file doesn't format: %v\n%s", err, out)
			}
			if bytes.Equal(again, out) {
				return
			}
			out = again
		}
		t.Errorf("formatting doesn't settle:\n%s", out)
	})
}
