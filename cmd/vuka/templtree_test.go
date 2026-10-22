package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
)

// recCalls compiles a .templ file, instruments it and lists, per component
// body (and children block) in source order, the recorder calls it makes.
func recCalls(t *testing.T, src string) ([]string, []byte) {
	t.Helper()
	g, _, terr := compileTempl("x.templ", "x.templ", []byte(src), nil)
	if terr != nil {
		t.Fatal(terr)
	}
	out, err := instrumentTempl(g)
	if err != nil {
		t.Fatal(err)
	}
	again, err := instrumentTempl(out)
	if err != nil || string(again) != string(out) {
		t.Fatalf("instrumenting again changes the code: %v", err)
	}
	f, err := parser.ParseFile(token.NewFileSet(), "", out, 0)
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	var bodies []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || !isSel(call.Fun, "templruntime", "GeneratedTemplate") {
			return true
		}
		var seq []string
		ast.Inspect(call.Args[0].(*ast.FuncLit).Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			if c, ok := n.(*ast.CallExpr); ok {
				if s, ok := c.Fun.(*ast.SelectorExpr); ok && isIdent(s.X, recVar) {
					seq = append(seq, s.Sel.Name)
				}
				if isSel(c.Fun, recPkg, "Record") {
					seq = append(seq, "Record")
				}
			}
			return true
		})
		bodies = append(bodies, strings.Join(seq, " "))
		return true
	})
	return bodies, out
}

func TestInstrumentTempl(t *testing.T) {
	tests := []struct {
		name, src string
		want      []string
	}{
		{"text", `<p>Hello, { name }!</p><b>{ a }{ b }</b>`,
			[]string{"Record S D S D S"}},
		{"attributes", `<a href={ templ.SafeURL(u) } class={ "btn", templ.KV("on", on) } { attrs... }>x</a>`,
			// the KV classes are written as CSS items first, then the class
			// value; the href and spread are dynamics of their own
			[]string{"Record D S D S D S D S"}},
		{"bool attribute", `<input disabled?={ on }/>`,
			[]string{"Record S Open S Close S"}},
		{"for", "<ul>\n\t\tfor _, x := range xs {\n\t\t\t<li>{ x }</li>\n\t\t}\n\t</ul>",
			[]string{"Record S For Item S D S End S"}},
		{"if else", "if on {\n\t\t<b>yes</b>\n\t} else if a == \"\" {\n\t\tno\n\t} else {\n\t\t<i>{ a }</i>\n\t}",
			[]string{"Record Open S S S D S Close"}},
		{"switch", "switch a {\n\t\tcase \"x\":\n\t\t\t<p>x</p>\n\t\tcase \"y\":\n\t\t\t<p>{ a }</p>\n\t\tdefault:\n\t}",
			[]string{"Record Open S S D S Close"}},
		{"component with children", "<div>\n\t\t@Card(a) {\n\t\t\t<p>{ b }</p>\n\t\t}\n\t</div>",
			[]string{"Record S Render S", "Record S D S"}},
		{"children", `<main>{ children... }</main>`,
			[]string{"Record S Render S"}},
		{"go code", "{{ n := len(a) }}\n\t<p>{ fmt.Sprint(n) }</p>",
			[]string{"Record S D S"}},
		{"nothing", ``,
			[]string{"Record"}},
	}
	for _, tt := range tests {
		src := "package views\n\nimport \"fmt\"\n\nvar _ = fmt.Sprint\n\ntempl Card(title string) {\n\t{ children... }\n}\n\n" +
			"templ C(name, a, b, u string, on bool, xs []string, attrs templ.Attributes) {\n\t" + tt.src + "\n}\n"
		got, out := recCalls(t, src)
		want := append([]string{"Record Render"}, tt.want...)
		if strings.Join(got, " | ") != strings.Join(want, " | ") {
			t.Errorf("%s:\n got %q\nwant %q\n%s", tt.name, got, want, out)
		}
	}
}

// TestInstrumentTemplOutput: the instrumented Go imports the recorder once,
// in a declaration of its own, and keeps everything templ wrote around it.
func TestInstrumentTemplOutput(t *testing.T) {
	_, out := recCalls(t, "package views\n\n// Doc stays.\ntempl Hello(name string) {\n\t<p>{ name }</p>\n}\n\nfunc helper() int { return 1 }\n")
	for _, want := range []string{
		"import templ_vk \"github.com/vuka-lang/ui/v2/templx\"\n",
		"// Doc stays.\nfunc Hello(name string) templ.Component {",
		"templ_vk_r := templ_vk.Record(ctx, templ_7745c5c3_Buffer)",
		"func helper() int { return 1 }",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
	if n := strings.Count(string(out), "ui/v2/templx"); n != 1 {
		t.Errorf("%d imports of templx", n)
	}
}

// TestTemplTrees: .templ files are instrumented in modules whose ui has
// the recorder, and only there.
func TestTemplTrees(t *testing.T) {
	uiDir := t.TempDir()
	writeFile(t, filepath.Join(uiDir, "templx", "record.go"), "package templx\n\nfunc Record() {}\n")
	old := t.TempDir()
	writeFile(t, filepath.Join(old, "templx", "templx.go"), "package templx\n")
	tests := []struct {
		name, mod string
		want      bool
	}{
		{"no ui", "require github.com/a-h/templ v0.3.1020\n", false},
		{"no templ", "require github.com/vuka-lang/ui/v2 v2.0.0\n", false},
		{"ui v1 (not this command's ui)", "require (\n\tgithub.com/a-h/templ v0.3.1020\n\tgithub.com/vuka-lang/ui v0.4.0\n)\n", false},
		{"ui v2", "require (\n\tgithub.com/a-h/templ v0.3.1020\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n)\n", true},
		{"ui v2.1", "require (\n\tgithub.com/a-h/templ v0.3.1020\n\tgithub.com/vuka-lang/ui/v2 v2.1.0\n)\n", true},
		{"local ui with the recorder", "require (\n\tgithub.com/a-h/templ v0.3.1020\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n)\n\nreplace github.com/vuka-lang/ui/v2 => " + uiDir + "\n", true},
		{"local ui without it", "require (\n\tgithub.com/a-h/templ v0.3.1020\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n)\n\nreplace github.com/vuka-lang/ui/v2 => " + old + "\n", false},
		{"replaced by a version", "require (\n\tgithub.com/a-h/templ v0.3.1020\n\tgithub.com/vuka-lang/ui/v2 v2.0.0\n)\n\nreplace (\n\tgithub.com/vuka-lang/ui/v2 v2.0.0 => github.com/vuka-lang/ui/v2 v2.0.1\n)\n", true},
		{"ui itself", "module github.com/vuka-lang/ui/v2\n\nrequire github.com/a-h/templ v0.3.1020\n", true},
	}
	for _, tt := range tests {
		root := t.TempDir()
		mod := tt.mod
		if !strings.HasPrefix(mod, "module ") {
			mod = "module example.com/site\n\n" + mod
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(mod), 0o644); err != nil {
			t.Fatal(err)
		}
		if got := load.TemplTrees(root); got != tt.want {
			t.Errorf("%s: TemplTrees = %v, want %v", tt.name, got, tt.want)
		}
	}
}
