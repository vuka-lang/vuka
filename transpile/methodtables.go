package transpile

import (
	"go/ast"
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"strings"
)

// MethodTables are vuka.RegisterMethods calls for the types of a package,
// whose Go sources (a .vuka file's generated Go, a .go file) are srcs: the
// methods the runtime looks up by name, so that it needn't ask reflect's
// Method or MethodByName, which turn the linker's dead-method elimination
// off. A type the runtime renders, routes or runs live — one declaring its
// markup (View(), a component's or a page's; Render() ui.Node, a live
// one's), or registered (vuka.RegisterType, a type decorator, attributes
// on a method, ui.RegisterIsland) — has each of its exported methods; any
// other type its methods named by a convention: Init, Clean…, …Choices,
// Update…, …TableName. Generic types have none: there is no one type to
// name.
func MethodTables(srcs ...[]byte) []string {
	type typ struct {
		ui      bool
		methods map[string]bool // exported method → declared on the type (not its pointer)
	}
	types := map[string]*typ{}
	of := func(name string) *typ {
		if types[name] == nil {
			types[name] = &typ{methods: map[string]bool{}}
		}
		return types[name]
	}
	declared := map[string]bool{}
	for _, src := range srcs {
		f, err := parser.ParseFile(token.NewFileSet(), "", src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		for _, d := range f.Decls {
			switch d := d.(type) {
			case *ast.GenDecl:
				if d.Tok != token.TYPE {
					continue
				}
				for _, s := range d.Specs {
					ts := s.(*ast.TypeSpec)
					if ts.TypeParams != nil || ts.Assign.IsValid() {
						continue
					}
					declared[ts.Name.Name] = true
				}
			case *ast.FuncDecl:
				if d.Recv == nil || len(d.Recv.List) == 0 || !d.Name.IsExported() {
					continue
				}
				rt, ptr := d.Recv.List[0].Type, false
				if s, ok := rt.(*ast.StarExpr); ok {
					rt, ptr = s.X, true
				}
				id, ok := rt.(*ast.Ident)
				if !ok {
					continue // a generic type's
				}
				t := of(id.Name)
				t.methods[d.Name.Name] = !ptr
				switch {
				case d.Name.Name == "View" && d.Type.Params.NumFields() == 0,
					d.Name.Name == "Render" && d.Type.Params.NumFields() == 0 && d.Type.Results.NumFields() == 1:
					t.ui = true // a component's or a page's markup, a live one's Render() ui.Node
				}
			}
		}
		// vuka.RegisterType[T], vuka.TypeFor[T] (a type decorator's),
		// vuka.RegisterFunc of a method of T (a method's attributes) and
		// ui.RegisterIsland[T] (bare in package ui) register T.
		imported := map[string]string{} // name → path
		for _, imp := range f.Imports {
			p, _ := strconv.Unquote(imp.Path.Value)
			name := ImportName(p)
			if imp.Name != nil {
				name = imp.Name.Name
			}
			imported[name] = p
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok && len(call.Args) > 0 {
				if fn, ok := call.Fun.(*ast.SelectorExpr); ok && fn.Sel.Name == "RegisterFunc" {
					if x, ok := fn.X.(*ast.Ident); ok && imported[x.Name] == RuntimePath {
						if recv := methodExprRecv(call.Args[0]); recv != "" {
							of(recv).ui = true
						}
					}
				}
			}
			ix, ok := n.(*ast.IndexExpr)
			if !ok {
				return true
			}
			id, ok := ix.Index.(*ast.Ident)
			if !ok {
				return true
			}
			switch fn := ix.X.(type) {
			case *ast.SelectorExpr:
				x, _ := fn.X.(*ast.Ident)
				switch {
				case x == nil:
				case imported[x.Name] == RuntimePath && (fn.Sel.Name == "RegisterType" || fn.Sel.Name == "TypeFor"),
					imported[x.Name] == uiImportPath && fn.Sel.Name == "RegisterIsland":
					of(id.Name).ui = true
				}
			case *ast.Ident:
				if fn.Name == "RegisterIsland" && f.Name.Name == "ui" {
					of(id.Name).ui = true
				}
			}
			return true
		})
	}
	var out []string
	for _, name := range slices.Sorted(func(yield func(string) bool) {
		for n := range types {
			if !yield(n) {
				return
			}
		}
	}) {
		t := types[name]
		if !declared[name] {
			continue
		}
		var fns []string
		for _, m := range slices.Sorted(func(yield func(string) bool) {
			for m := range t.methods {
				if !yield(m) {
					return
				}
			}
		}) {
			if !t.ui && !conventionMethod(m) {
				continue
			}
			if t.methods[m] {
				fns = append(fns, name+"."+m)
			} else {
				fns = append(fns, "(*"+name+")."+m)
			}
		}
		if len(fns) > 0 {
			out = append(out, "vuka.RegisterMethods["+name+"]("+strings.Join(fns, ", ")+")")
		}
	}
	return out
}

// methodExprRecv is the receiver type's name of a method expression,
// (*T).M or T.M; "" for anything else.
func methodExprRecv(x ast.Expr) string {
	sel, ok := ast.Unparen(x).(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	recv := ast.Unparen(sel.X)
	if s, ok := recv.(*ast.StarExpr); ok {
		recv = ast.Unparen(s.X)
	}
	if id, ok := recv.(*ast.Ident); ok {
		return id.Name
	}
	return ""
}

// conventionMethod reports whether a method's name is one the runtime
// finds a method of any type by: a form's hooks, a live component's
// Update overloads, a model's <Names>TableName.
func conventionMethod(name string) bool {
	return name == "Init" || strings.HasPrefix(name, "Clean") || strings.HasSuffix(name, "Choices") ||
		strings.HasPrefix(name, "Update") || strings.HasSuffix(name, "TableName")
}
