package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/vuka-lang/vuka/v2/toolchain/load"
	"github.com/vuka-lang/vuka/v2/transpile"
)

// goCallAdapters are the vuka_calls_gen.go files of the module's packages
// written in Go: a typed adapter for each signature their vuka.Wrap calls
// wrap, what the compiler writes for a .vuka file's call decorators. A
// package with .vuka files gets its file from the compiler.
func goCallAdapters(root string) []load.Generated {
	dirs := map[string][]string{}
	vuka := map[string]bool{}
	walkSources(root, func(p, name string) error {
		dir := filepath.Dir(p)
		switch {
		case strings.HasSuffix(name, ".vuka"):
			vuka[dir] = true
		case strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") && !strings.HasSuffix(name, "_gen.go"):
			dirs[dir] = append(dirs[dir], p)
		}
		return nil
	})
	var out []load.Generated
	for _, dir := range slices.Sorted(func(yield func(string) bool) {
		for d := range dirs {
			if !yield(d) {
				return
			}
		}
	}) {
		if vuka[dir] {
			continue
		}
		files := dirs[dir]
		slices.Sort(files)
		if src := wrapAdapters(files); src != nil {
			out = append(out, load.Generated{Target: filepath.Join(dir, transpile.CallsGoName), Source: files[0], Src: src})
		}
	}
	return out
}

// newGens are the files of more whose targets gens doesn't have.
func newGens(gens, more []load.Generated) []load.Generated {
	var out []load.Generated
	for _, g := range more {
		if !slices.ContainsFunc(gens, func(h load.Generated) bool { return h.Target == g.Target }) {
			out = append(out, g)
		}
	}
	return out
}

// wrapAdapters reads a package's Go files for the functions vuka.Wrap wraps
// — a function literal, a function or method of the package, or the type
// argument it is given — and writes their adapters.
func wrapAdapters(files []string) []byte {
	var parsed []goSrc
	pkg := ""
	for _, p := range files {
		src, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, p, src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		pkg = f.Name.Name
		parsed = append(parsed, goSrc{f, src, fset})
	}
	// decl finds a function, or a method by its receiver's type name, and
	// the file declaring it.
	decl := func(recv, name string) (*ast.FuncDecl, *goSrc) {
		for i := range parsed {
			for _, d := range parsed[i].f.Decls {
				fd, ok := d.(*ast.FuncDecl)
				if !ok || fd.Name.Name != name || (fd.Recv == nil) != (recv == "") {
					continue
				}
				if recv == "" || recvName(fd) == recv {
					return fd, &parsed[i]
				}
			}
		}
		return nil, nil
	}
	var sigs []transpile.CallSig
	for i := range parsed {
		pf := &parsed[i]
		vukaName := ""
		for _, imp := range pf.f.Imports {
			if p, _ := strconv.Unquote(imp.Path.Value); p == transpile.RuntimePath {
				vukaName = "vuka"
				if imp.Name != nil {
					vukaName = imp.Name.Name
				}
			}
		}
		if vukaName == "" || vukaName == "_" || vukaName == "." {
			continue
		}
		ast.Inspect(pf.f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			fun, targ := call.Fun, ast.Expr(nil)
			if ix, ok := fun.(*ast.IndexExpr); ok {
				fun, targ = ix.X, ix.Index
			}
			sel, ok := fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Wrap" {
				return true
			}
			if x, ok := sel.X.(*ast.Ident); !ok || x.Name != vukaName {
				return true
			}
			var ft *ast.FuncType
			recv := ""
			at := pf
			switch a := ast.Unparen(call.Args[0]).(type) {
			case *ast.FuncLit:
				ft = a.Type
			case *ast.Ident:
				if fd, df := decl("", a.Name); fd != nil {
					ft, at = fd.Type, df
				}
			case *ast.SelectorExpr: // a method expression, (*T).m or T.m
				x := ast.Unparen(a.X)
				base := x
				if star, ok := x.(*ast.StarExpr); ok {
					base = star.X
				}
				if id, ok := base.(*ast.Ident); ok {
					if fd, df := decl(id.Name, a.Sel.Name); fd != nil && !genericRecvExpr(fd) {
						ft, at, recv = fd.Type, df, pf.text(x)
					}
				}
			}
			if t, ok := targ.(*ast.FuncType); ok {
				ft, at, recv = t, pf, ""
			}
			if ft == nil || ft.TypeParams != nil {
				return true
			}
			if sig, ok := at.callSig(ft); ok {
				if recv != "" {
					sig.Params = append([]string{recv}, sig.Params...)
				}
				sigs = append(sigs, sig)
			}
			return true
		})
	}
	return transpile.CallAdapters(pkg, sigs)
}

// goSrc is a parsed Go file.
type goSrc struct {
	f    *ast.File
	src  []byte
	fset *token.FileSet
}

func (g *goSrc) text(n ast.Node) string {
	return string(g.src[g.fset.Position(n.Pos()).Offset:g.fset.Position(n.End()).Offset])
}

// genericRecvExpr reports whether a method's receiver is a generic type's.
func genericRecvExpr(fd *ast.FuncDecl) bool {
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch t.(type) {
	case *ast.IndexExpr, *ast.IndexListExpr:
		return true
	}
	return false
}

// recvName is the name of a method's receiver type: T of (t *T) or (T).
func recvName(fd *ast.FuncDecl) string {
	t := fd.Recv.List[0].Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	if id, ok := t.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// callSig is the signature of ft as g's source writes it, and the
// packages its types name by the names g imports them as; false when one
// isn't an import.
func (g *goSrc) callSig(ft *ast.FuncType) (transpile.CallSig, bool) {
	var sig transpile.CallSig
	for _, field := range ft.Params.List {
		for range max(1, len(field.Names)) {
			sig.Params = append(sig.Params, g.text(field.Type))
		}
	}
	if ft.Results != nil {
		sig.Clause = " " + g.text(ft.Results)
		for _, field := range ft.Results.List {
			for range max(1, len(field.Names)) {
				sig.Results = append(sig.Results, g.text(field.Type))
			}
		}
	}
	imports := map[string]string{}
	for _, imp := range g.f.Imports {
		p, _ := strconv.Unquote(imp.Path.Value)
		name := transpile.ImportName(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		imports[name] = p
	}
	sig.Imports = map[string]string{}
	ok := true
	ast.Inspect(ft, func(n ast.Node) bool {
		sel, isSel := n.(*ast.SelectorExpr)
		if !isSel {
			return true
		}
		if id, isID := sel.X.(*ast.Ident); isID {
			p, found := imports[id.Name]
			ok = ok && found
			sig.Imports[id.Name] = p
		}
		return false
	})
	return sig, ok
}
