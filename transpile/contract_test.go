package transpile_test

import (
	"bufio"
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// targets are the golden module's JSX targets: copies of ui's JSX contract,
// so what they're called with is what ui would be.
var targets = map[string]string{
	"vukatest.local/stateui":       "github.com/vuka-lang/ui/v2",
	"vukatest.local/stateui/uigen": "github.com/vuka-lang/ui/v2/uigen",
	"vukatest.local/islandui":      "github.com/vuka-lang/ui/v2",
	"vukatest.local/webstub":       "github.com/vuka-lang/web/v2",
	"vukatest.local/orm":           "github.com/vuka-lang/orm/v2",
	"vukatest.local/ui":            "github.com/vuka-lang/ui/v2",
	"vukatest.local/ui/validate":   "github.com/vuka-lang/ui/v2/validate",
}

// TestContract checks the generated goldens against the contract for
// generated code (docs/design/library.md): gofmt'd, one import block, no
// /*line*/ markers, no __-prefixed or _New-suffixed names, and calls into
// vuka, ui and web only to the public API listed in testdata/contract.txt.
// The file's pending entries are the internal forms a later step of Phase 2
// replaces, each with its step; one no golden uses any more is an error too,
// so the list only shrinks. Its generated entries are what generated code
// alone calls (ui's uigen): public so it builds, not an API to write by
// hand; one no golden uses is an error too.
func TestContract(t *testing.T) {
	public, generated, pending := readContract(t, "testdata/contract.txt")
	used := map[string]bool{}
	goldens, _ := filepath.Glob("testdata/golden/*.golden")
	templates, _ := filepath.Glob("testdata/golden/templates/*/*_html.go.golden")
	goldens = append(goldens, templates...)
	for _, path := range goldens {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(path)
		for _, problem := range contractProblems(src, func(sym string) bool {
			used[sym] = true
			return public[sym] || generated[sym] || pending[sym] != ""
		}) {
			t.Errorf("%s: %s", name, problem)
		}
	}
	for sym := range generated {
		if !used[sym] {
			t.Errorf("contract.txt: no golden uses %s (generated) any more: remove it", sym)
		}
	}
	for sym, step := range pending {
		if !used[sym] {
			t.Errorf("contract.txt: no golden uses %s (pending until %s) any more: remove it", sym, step)
		}
	}
}

// contractProblems lists how a generated file breaks the contract; allowed
// reports whether a vuka, ui or web symbol (path.Name), or a generated name
// pattern (*_New), may be used.
func contractProblems(src []byte, allowed func(sym string) bool) []string {
	var out []string
	if bytes.Contains(src, []byte("/*line ")) {
		out = append(out, "a /*line*/ marker")
	}
	if fmted, err := format.Source(src); err != nil || !bytes.Equal(fmted, src) {
		out = append(out, "not gofmt'd")
	}
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "", src, parser.ParseComments)
	if err != nil {
		return append(out, err.Error())
	}
	imports := 0
	names := map[string]string{}
	for _, d := range file.Decls {
		if g, ok := d.(*ast.GenDecl); ok && g.Tok == token.IMPORT {
			imports++
		}
	}
	if imports > 1 {
		out = append(out, fmt.Sprintf("%d import declarations", imports))
	}
	for _, s := range file.Imports {
		path, _ := strconv.Unquote(s.Path.Value)
		name := path[strings.LastIndexByte(path, '/')+1:]
		if s.Name != nil {
			name = s.Name.Name
		}
		if t, ok := targets[path]; ok {
			path = t
		}
		names[name] = path
	}
	for _, c := range file.Comments {
		for _, l := range c.List {
			if strings.HasPrefix(l.Text, "//line ") && fset.PositionFor(l.Pos(), false).Column != 1 {
				out = append(out, "a //line directive not at the start of its line")
			}
		}
	}
	seen := map[string]bool{}
	report := func(msg string) {
		if !seen[msg] {
			seen[msg] = true
			out = append(out, msg)
		}
	}
	ast.Inspect(file, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.Ident:
			switch {
			case strings.HasPrefix(n.Name, "__"):
				report("names " + n.Name)
			case strings.HasSuffix(n.Name, "_New") && !allowed("*_New"):
				report("names " + n.Name)
			}
		case *ast.SelectorExpr:
			x, ok := n.X.(*ast.Ident)
			if !ok {
				break
			}
			path, ok := names[x.Name]
			if !ok || !strings.HasPrefix(path, "github.com/vuka-lang/") {
				break
			}
			if sym := path + "." + n.Sel.Name; !allowed(sym) {
				report("calls " + sym + ", which isn't public API")
			}
			return false
		}
		return true
	})
	sort.Strings(out)
	return out
}

// readContract reads the contract file: "public <symbol>", "generated
// <symbol>" and "step<N> <symbol>" lines, # comments.
func readContract(t *testing.T, path string) (public, generated map[string]bool, pending map[string]string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	public, generated, pending = map[string]bool{}, map[string]bool{}, map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(data))
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch {
		case len(f) != 2:
			t.Fatalf("%s:%d: want <public|generated|stepN> <symbol>", path, n)
		case f[0] == "public":
			public[f[1]] = true
		case f[0] == "generated":
			generated[f[1]] = true
		case strings.HasPrefix(f[0], "step"):
			if _, err := strconv.Atoi(f[0][4:]); err != nil {
				t.Fatalf("%s:%d: %s isn't a step", path, n, f[0])
			}
			pending[f[1]] = f[0]
		default:
			t.Fatalf("%s:%d: want <public|generated|stepN> <symbol>", path, n)
		}
		if public[f[1]] && pending[f[1]] != "" {
			t.Fatalf("%s:%d: %s is both public and pending", path, n, f[1])
		}
	}
	return public, generated, pending
}
