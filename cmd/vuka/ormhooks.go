package main

import (
	"bytes"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vuka-lang/vuka/v2/diag"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// ormHooks are the orm's interfaces a model's methods are found by: what
// it declares (indexes, checks, generated columns) and its lifecycle.
// vukavet's ormhooks check reads the same.
var ormHooks = []string{
	"IndexesDeclarer", "ChecksDeclarer", "GeneratedDeclarer",
	"BeforeCreater", "AfterCreater", "BeforeSaver", "AfterSaver", "BeforeDeleter", "AfterDeleter", "RowValidator",
}

// modelMethod matches a method declaration: its receiver (the type's
// name), its name and its signature up to the body.
var modelMethod = regexp.MustCompile(`(?m)^func\s*\(\s*(?:\w+\s+)?(\*?)\s*(\w+)\s*(?:\[[^\]]*\])?\s*\)\s*(\w+)\s*(\([^{]*)\{`)

// ormHookDiags are the methods of the orm models of src's package named
// like the ORM's hooks with another signature (W0503): the ORM ignores
// them. The hooks' signatures are those of the orm the module at root
// requires (ormHookSigs). srcs are the package's .vuka files, src first.
// The editor reports them from the text; vuka vet has them from vukavet.
func ormHookDiags(root, file string, srcs [][]byte) []diag.Diagnostic {
	src := srcs[0]
	orm := importedAs(src, ormModule)
	if orm == "" {
		return nil
	}
	models := map[string]bool{}
	for _, s := range srcs {
		if as := importedAs(s, ormModule); as != "" {
			for _, lt := range structTypes(s) {
				models[lt.name] = models[lt.name] || lt.orm == as
			}
		}
	}
	var sigs map[string]hookSig
	toks, line := scanFile(file, src)
	imports := fileImports(toks)
	byPath := map[string]string{}
	for name, path := range imports {
		byPath[path] = name
	}
	var out []diag.Diagnostic
	for _, m := range modelMethod.FindAllSubmatchIndex(src, -1) {
		typ, name := string(src[m[4]:m[5]]), string(src[m[6]:m[7]])
		if !models[typ] {
			continue
		}
		if sigs == nil {
			if sigs = ormHookSigs(root); sigs == nil {
				return nil
			}
		}
		want, ok := sigs[name]
		if !ok {
			continue
		}
		got := strings.TrimSpace(string(src[m[8]:m[9]]))
		canon, ok := signatureText(got, func(q string) string { return imports[q] })
		if !ok || canon == want.canon {
			continue
		}
		recv := string(src[m[2]:m[3]]) + typ
		must, _ := signatureText(want.text, func(q string) string {
			path := ormModule
			if q != "" {
				path = want.imports[q]
			}
			if as, ok := byPath[path]; ok {
				return as
			}
			return transpile.ImportName(path)
		})
		shown, _ := signatureText(got, func(q string) string { return q })
		is, must := "func ("+recv+") "+name+shown, "func ("+recv+") "+name+must
		out = append(out, *diag.Warn("W0503", "%s.%s looks like an ORM hook, but its signature is %s; it must be %s: the ORM ignores it otherwise", typ, name, is, must).
			At(span(file, src, line, m[6], len(name))).WithHelp("declare it as %s, or name it otherwise", must))
	}
	return out
}

// hookSig is a hook's method as orm declares it: its signature's text,
// the imports of the file declaring it, and the signature with each
// package by its path (signatureText).
type hookSig struct {
	text    string
	imports map[string]string
	canon   string
	doc     string // the interface's doc, else its declaration group's
}

// ormHookSigs are the hooks' methods by name, read from the source of the
// orm the module at root requires; nil when it requires none. They are
// read again when go.mod changes.
func ormHookSigs(root string) map[string]hookSig {
	if root == "" {
		return nil
	}
	st, err := os.Stat(filepath.Join(root, "go.mod"))
	if err != nil {
		return nil
	}
	if c, ok := hookSigCache.Load(root); ok && c.(hookSigsAt).mod.Equal(st.ModTime()) {
		return c.(hookSigsAt).sigs
	}
	sigs := readHookSigs(root)
	hookSigCache.Store(root, hookSigsAt{st.ModTime(), sigs})
	return sigs
}

type hookSigsAt struct {
	mod  time.Time
	sigs map[string]hookSig
}

var hookSigCache sync.Map // module root → hookSigsAt

func readHookSigs(root string) map[string]hookSig {
	c := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", ormModule)
	c.Dir = root
	out, err := c.Output()
	if err != nil {
		return nil
	}
	dir := strings.TrimSpace(string(out))
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	wanted := map[string]bool{}
	for _, n := range ormHooks {
		wanted[n] = true
	}
	sigs := map[string]hookSig{}
	fset := token.NewFileSet()
	for _, path := range files {
		src, err := os.ReadFile(path)
		if strings.HasSuffix(path, "_test.go") || err != nil || !bytes.Contains(src, []byte("interface")) {
			continue
		}
		f, err := parser.ParseFile(fset, path, src, parser.SkipObjectResolution|parser.ParseComments)
		if err != nil {
			continue
		}
		imports := map[string]string{}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			name := transpile.ImportName(p)
			if im.Name != nil {
				name = im.Name.Name
			}
			imports[name] = p
		}
		groupDoc := map[*ast.TypeSpec]*ast.CommentGroup{}
		for _, d := range f.Decls {
			if g, ok := d.(*ast.GenDecl); ok {
				for _, sp := range g.Specs {
					if ts, ok := sp.(*ast.TypeSpec); ok {
						groupDoc[ts] = g.Doc
					}
				}
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok || !wanted[ts.Name.Name] {
				return true
			}
			it, ok := ts.Type.(*ast.InterfaceType)
			if !ok || len(it.Methods.List) != 1 || len(it.Methods.List[0].Names) != 1 {
				return false
			}
			doc := ts.Doc
			if doc == nil {
				doc = groupDoc[ts]
			}
			m := it.Methods.List[0]
			ft := m.Type.(*ast.FuncType)
			text := string(src[fset.Position(ft.Params.Pos()).Offset:fset.Position(ft.End()).Offset])
			canon, ok := signatureText(text, func(q string) string {
				if q == "" {
					return ormModule
				}
				return imports[q]
			})
			if ok {
				sigs[m.Names[0].Name] = hookSig{text, imports, canon, docText(doc)}
			}
			return false
		})
	}
	return sigs
}

// signatureText is a signature's text, (ctx context.Context) error, as its
// types alone, each package named by pkg: pkg(q) for a type qualified by q,
// pkg("") for an unqualified exported one, when it isn't "". It is false
// when the text isn't a signature.
func signatureText(sig string, pkg func(q string) string) (string, bool) {
	e, err := parser.ParseExpr("func" + sig)
	ft, ok := e.(*ast.FuncType)
	if err != nil || !ok {
		return "", false
	}
	list := func(fl *ast.FieldList) []string {
		var out []string
		if fl == nil {
			return out
		}
		for _, f := range fl.List {
			qualify(f.Type, pkg)
			for range max(1, len(f.Names)) {
				out = append(out, types.ExprString(f.Type))
			}
		}
		return out
	}
	s := "(" + strings.Join(list(ft.Params), ", ") + ")"
	switch res := list(ft.Results); len(res) {
	case 0:
	case 1:
		s += " " + res[0]
	default:
		s += " (" + strings.Join(res, ", ") + ")"
	}
	return s, true
}

// qualify names the packages of type expression e by pkg, in place.
func qualify(e ast.Expr, pkg func(q string) string) {
	switch e := e.(type) {
	case *ast.Ident:
		if p := pkg(""); p != "" && token.IsExported(e.Name) {
			e.Name = p + "." + e.Name
		}
	case *ast.SelectorExpr:
		if x, ok := e.X.(*ast.Ident); ok {
			if p := pkg(x.Name); p != "" {
				x.Name = p
			}
		}
	case *ast.StarExpr:
		qualify(e.X, pkg)
	case *ast.ArrayType:
		qualify(e.Elt, pkg)
	case *ast.MapType:
		qualify(e.Key, pkg)
		qualify(e.Value, pkg)
	case *ast.Ellipsis:
		qualify(e.Elt, pkg)
	case *ast.ChanType:
		qualify(e.Value, pkg)
	case *ast.IndexExpr:
		qualify(e.X, pkg)
		qualify(e.Index, pkg)
	case *ast.IndexListExpr:
		qualify(e.X, pkg)
		for _, x := range e.Indices {
			qualify(x, pkg)
		}
	}
}
