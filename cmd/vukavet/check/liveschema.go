package check

import (
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// liveSchema is what a page's LiveSchema method declares, read from its
// source as ui reads it at boot: its fields' attributes and its hooks.
type liveSchema struct {
	fields   map[*types.Var]schemaField
	loads    map[string]bool // the groups loaded, as loaders name them: Lists
	hooks    map[string]bool // the methods hooked
}

type schemaField struct {
	at         analysis.Range // its l.Field call
	bind       bool
	url, group string
	deferred   bool
}

// schema reads n's LiveSchema method when the package declares it,
// reporting a field that isn't the page's own or a nil attribute (E0335),
// and a hook that doesn't fit (E0336); nil without one.
func (l *liveCheck) schema(n *types.Named) *liveSchema {
	pass, info := l.pass, l.pass.TypesInfo
	obj, _, _ := types.LookupFieldOrMethod(types.NewPointer(n), false, n.Obj().Pkg(), "LiveSchema")
	m, ok := obj.(*types.Func)
	if !ok || m.Pkg() != pass.Pkg {
		return nil
	}
	var fd *ast.FuncDecl
	for _, f := range pass.Files {
		for _, d := range f.Decls {
			if d, ok := d.(*ast.FuncDecl); ok && info.Defs[d.Name] == m {
				fd = d
			}
		}
	}
	if fd == nil || fd.Body == nil || len(fd.Recv.List[0].Names) == 0 {
		return nil
	}
	recv := info.Defs[fd.Recv.List[0].Names[0]]
	name := n.Obj().Name()
	s := &liveSchema{fields: map[*types.Var]schemaField{}, loads: map[string]bool{}, hooks: map[string]bool{}}
	hook := func(what string, fn ast.Expr) {
		if tv, ok := info.Types[fn]; ok && tv.IsNil() {
			l.report(fn, "E0336", "pass a method of the page: l.Mount(d.mount)", "%s.LiveSchema: l.%s is given nil", name, what)
			return
		}
		if sel, ok := ast.Unparen(fn).(*ast.SelectorExpr); ok {
			if x, ok := ast.Unparen(sel.X).(*ast.Ident); ok && info.Uses[x] == recv {
				s.hooks[sel.Sel.Name] = true
			}
		}
	}
	ast.Inspect(fd.Body, func(x ast.Node) bool {
		call, ok := x.(*ast.CallExpr)
		if !ok {
			return true
		}
		switch m, _ := method(info, call, uiPath, "LiveSchema"); m {
		case "Field":
			if len(call.Args) > 0 {
				l.schemaField(s, name, recv, call)
			}
		case "Mount", "OnChange":
			if len(call.Args) == 1 {
				hook(m, call.Args[0])
			}
		case "Loads":
			if len(call.Args) != 2 {
				return true
			}
			g, ok := str(info, call.Args[0])
			switch {
			case !ok:
			case !isIdent(g):
				l.report(call.Args[0], "E0336", `name the group as ui.Defer does: l.Loads("stats", …)`, "%s.LiveSchema: l.Loads(%q, …): a group is a name", name, g)
			case s.loads[groupName(g)]:
				l.report(call, "E0336", "give the group one loader", "%s.LiveSchema: l.Loads(%q, …) is called twice", name, g)
			default:
				s.loads[groupName(g)] = true
				hook("Loads", call.Args[1])
			}
		}
		return true
	})
	return s
}

// schemaField reads l.Field(&p.X, attrs…): X one of the page's own fields.
func (l *liveCheck) schemaField(s *liveSchema, page string, recv types.Object, call *ast.CallExpr) {
	info := l.pass.TypesInfo
	arg := call.Args[0]
	var field *types.Var
	if u, ok := ast.Unparen(arg).(*ast.UnaryExpr); ok && u.Op == token.AND {
		if sel, ok := ast.Unparen(u.X).(*ast.SelectorExpr); ok {
			x, _ := ast.Unparen(sel.X).(*ast.Ident)
			if sl, ok := info.Selections[sel]; ok && sl.Kind() == types.FieldVal && len(sl.Index()) == 1 && x != nil && info.Uses[x] == recv {
				field = sl.Obj().(*types.Var)
			}
		}
	}
	if field == nil {
		l.report(arg, "E0335", "pass the address of the receiver's field: l.Field(&"+recv.Name()+".Q, …)",
			"%s.LiveSchema: l.Field is given %s, not the address of one of %s's fields", page, types.ExprString(arg), page)
		return
	}
	o := s.fields[field]
	o.at = call
	for _, a := range call.Args[1:] {
		if tv, ok := info.Types[a]; ok && tv.IsNil() {
			l.report(a, "E0335", "drop it", "%s.LiveSchema: l.Field(&%s.%s, …) is given a nil attribute", page, recv.Name(), field.Name())
			continue
		}
		t := info.TypeOf(a)
		switch {
		case is(t, uiPath, "Bind"):
			o.bind = true
		case is(t, uiPath, "URL"):
			o.url, _ = str(info, a)
		case is(t, uiPath, "Defer"):
			o.group, _ = str(info, a)
			o.deferred = true
		}
	}
	s.fields[field] = o
}

// handWrittenSchema reports a LiveSchema method written by hand for a page
// declared in Vuka source (a .vuka or .vuka.html file, whose Go is
// generated): the compiler writes it from the page's attributes
// (@ui.Mount, @ui.Bind, …), and a person writes those. A page declared in
// Go — ejected, or not yet moved to a .vuka.html file — has no attributes
// to write, so its LiveSchema isn't reported.
func handWrittenSchema(pass *analysis.Pass) {
	generated := map[*ast.File]bool{}
	for _, f := range pass.Files {
		generated[f] = ast.IsGenerated(f)
	}
	fileOf := func(pos token.Pos) *ast.File {
		for _, f := range pass.Files {
			if f.FileStart <= pos && pos <= f.FileEnd {
				return f
			}
		}
		return nil
	}
	for _, f := range pass.Files {
		if generated[f] {
			continue
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Recv == nil || fd.Name.Name != "LiveSchema" {
				continue
			}
			m, ok := pass.TypesInfo.Defs[fd.Name].(*types.Func)
			if !ok {
				continue
			}
			sig := m.Type().(*types.Signature)
			if sig.Params().Len() != 1 {
				continue
			}
			p, ok := sig.Params().At(0).Type().(*types.Pointer)
			if !ok || !named(p.Elem(), uiPath, "LiveSchema") {
				continue
			}
			recv, ok := types.Unalias(sig.Recv().Type()).(*types.Pointer)
			if !ok {
				continue
			}
			n, ok := types.Unalias(recv.Elem()).(*types.Named)
			if !ok || !generated[fileOf(n.Obj().Pos())] {
				continue
			}
			report(pass, fd.Name, "W0306", "write the attributes in the page's source: @ui.Mount, @ui.OnChange and @ui.Loads(\"group\") on its methods, @ui.Bind, @ui.URL(\"q\") and @ui.Defer(\"group\") on its fields",
				"LiveSchema is what the compiler writes; it is not a method to write by hand")
		}
	}
}

// groupName is a group as its loader names it: lists is Lists.
func groupName(g string) string {
	return strings.ToUpper(g[:1]) + g[1:]
}
